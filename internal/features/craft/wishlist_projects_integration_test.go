package craft_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/maksimkurb/cublyverse/internal/features/craft"
	"github.com/maksimkurb/cublyverse/internal/workspace"
)

func f64(v float64) *float64 { return &v }

// inventoryQty returns the stock of an item and fails when it has no stock row.
func (e *env) inventoryQty(t *testing.T, user, workspaceID, itemID string) float64 {
	t.Helper()
	rows, err := e.craft.ListInventory(context.Background(), user, workspaceID, craft.CatalogFilter{})
	if err != nil {
		t.Fatalf("list inventory: %v", err)
	}
	for _, row := range rows {
		if row.Item.ID == itemID {
			return row.Qty
		}
	}
	t.Fatalf("item %s is not in inventory", itemID)
	return 0
}

func TestWishlistLifecycleAndIsolation(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	user := e.user(t, "lilya", "lilya@example.com")
	other := e.user(t, "other", "o@example.com")
	ws, _ := e.workspaces.Create(ctx, user, "Дом")
	otherWS, _ := e.workspaces.Create(ctx, other, "Чужой")

	pearl, err := e.craft.CreateItem(ctx, user, ws.ID, craft.ItemInput{
		Category: "pearls", Name: "Жемчуг Майорка айвори 2 мм", Unit: "шт",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.craft.Adjust(ctx, user, ws.ID, pearl.ID, 10, "из Grist"); err != nil {
		t.Fatal(err)
	}

	wish, err := e.craft.AddWish(ctx, user, ws.ID, craft.WishInput{
		CatalogItemID: pearl.ID, QtyWanted: f64(3), Priority: 2, Note: "на подарок",
	})
	if err != nil {
		t.Fatalf("AddWish: %v", err)
	}
	if wish.Title != pearl.Name || wish.Status != "wanted" || wish.Item == nil {
		t.Fatalf("wish = %+v", wish)
	}
	if wish.Item.StockQty != 10 {
		t.Fatalf("wish stock = %v, want 10", wish.Item.StockQty)
	}

	// A catalogue item can be wanted only once at a time.
	_, err = e.craft.AddWish(ctx, user, ws.ID, craft.WishInput{CatalogItemID: pearl.ID})
	var invalid *craft.ValidationError
	if !errors.As(err, &invalid) || !strings.Contains(invalid.Message, "уже в wishlist") {
		t.Fatalf("duplicate wish = %v, want a validation error", err)
	}

	free, err := e.craft.AddWish(ctx, user, ws.ID, craft.WishInput{Title: "Лента атласная"})
	if err != nil {
		t.Fatalf("free-text wish: %v", err)
	}
	if free.Item != nil || free.CatalogItemID != "" {
		t.Fatalf("free-text wish = %+v", free)
	}

	wanted, err := e.craft.ListWishes(ctx, user, ws.ID, craft.WishFilter{})
	if err != nil || len(wanted) != 2 {
		t.Fatalf("ListWishes default = %v, %v; want 2 wanted", wanted, err)
	}
	if wanted[0].ID != wish.ID {
		t.Fatalf("priority order: first = %s, want %s", wanted[0].ID, wish.ID)
	}
	if ordered, err := e.craft.ListWishes(ctx, user, ws.ID, craft.WishFilter{Status: "ordered"}); err != nil || len(ordered) != 0 {
		t.Fatalf("ListWishes ordered = %v, %v; want none", ordered, err)
	}
	if all, err := e.craft.ListWishes(ctx, user, ws.ID, craft.WishFilter{Status: "all"}); err != nil || len(all) != 2 {
		t.Fatalf("ListWishes all = %v, %v; want 2", all, err)
	}

	// Links: only http(s) is accepted, price must not be negative.
	if _, err := e.craft.AddWishLink(ctx, user, ws.ID, wish.ID, craft.WishLinkInput{URL: "ftp://shop.example/x"}); !errors.As(err, &invalid) {
		t.Fatalf("ftp link = %v, want a validation error", err)
	}
	if _, err := e.craft.AddWishLink(ctx, user, ws.ID, wish.ID, craft.WishLinkInput{
		URL: "https://shop.example/x", Price: f64(-1),
	}); !errors.As(err, &invalid) {
		t.Fatalf("negative price = %v, want a validation error", err)
	}
	link, err := e.craft.AddWishLink(ctx, user, ws.ID, wish.ID, craft.WishLinkInput{
		URL: "https://shop.example/x", ShopName: "Магазин", Price: f64(12.5),
		Currency: "rub", InStock: boolPtrTest(true),
	})
	if err != nil {
		t.Fatalf("AddWishLink: %v", err)
	}
	if link.Currency != "RUB" || link.Price == nil || *link.Price != 12.5 {
		t.Fatalf("link = %+v", link)
	}
	detailed, err := e.craft.GetWish(ctx, user, ws.ID, wish.ID)
	if err != nil || len(detailed.Links) != 1 {
		t.Fatalf("GetWish = %+v, %v; want one link", detailed, err)
	}
	if err := e.craft.RemoveWishLink(ctx, user, ws.ID, wish.ID, link.ID); err != nil {
		t.Fatalf("RemoveWishLink: %v", err)
	}
	if err := e.craft.RemoveWishLink(ctx, user, ws.ID, wish.ID, link.ID); !errors.Is(err, craft.ErrNotFound) {
		t.Fatalf("second RemoveWishLink = %v, want ErrNotFound", err)
	}

	updated, err := e.craft.UpdateWish(ctx, user, ws.ID, wish.ID, craft.WishUpdate{
		QtyWanted: f64(5), Priority: 1, Note: "", Status: "ordered",
	})
	if err != nil || updated.Status != "ordered" || updated.QtyWanted == nil || *updated.QtyWanted != 5 {
		t.Fatalf("UpdateWish = %+v, %v", updated, err)
	}

	// Data of a workspace is invisible to another workspace.
	if _, err := e.craft.GetWish(ctx, other, otherWS.ID, wish.ID); !errors.Is(err, craft.ErrNotFound) {
		t.Fatalf("foreign GetWish = %v, want ErrNotFound", err)
	}
	if _, err := e.craft.UpdateWish(ctx, other, otherWS.ID, wish.ID, craft.WishUpdate{Priority: 0, Status: "wanted"}); !errors.Is(err, craft.ErrNotFound) {
		t.Fatalf("foreign UpdateWish = %v, want ErrNotFound", err)
	}
	if _, err := e.craft.AddWishLink(ctx, other, otherWS.ID, wish.ID, craft.WishLinkInput{URL: "https://x.example"}); !errors.Is(err, craft.ErrNotFound) {
		t.Fatalf("foreign AddWishLink = %v, want ErrNotFound", err)
	}
	if err := e.craft.DeleteWish(ctx, other, otherWS.ID, wish.ID); !errors.Is(err, craft.ErrNotFound) {
		t.Fatalf("foreign DeleteWish = %v, want ErrNotFound", err)
	}
	if _, err := e.craft.ListWishes(ctx, other, ws.ID, craft.WishFilter{}); !errors.Is(err, workspace.ErrNotFound) {
		t.Fatalf("foreign workspace ListWishes = %v, want ErrNotFound", err)
	}

	// Buying adds the wanted quantity to the stock, once.
	bought, err := e.craft.MarkBought(ctx, user, ws.ID, wish.ID, true)
	if err != nil {
		t.Fatalf("MarkBought: %v", err)
	}
	if bought.Status != "bought" || bought.Item == nil || bought.Item.StockQty != 15 {
		t.Fatalf("bought = %+v, want status bought and stock 15", bought)
	}
	if qty := e.inventoryQty(t, user, ws.ID, pearl.ID); qty != 15 {
		t.Fatalf("stock after MarkBought = %v, want 15", qty)
	}
	if _, err := e.craft.MarkBought(ctx, user, ws.ID, wish.ID, true); err != nil {
		t.Fatalf("repeated MarkBought: %v", err)
	}
	if qty := e.inventoryQty(t, user, ws.ID, pearl.ID); qty != 15 {
		t.Fatalf("stock after repeated MarkBought = %v, want 15", qty)
	}
	// The bought entry no longer holds the "wanted" slot of the item.
	if _, err := e.craft.AddWish(ctx, user, ws.ID, craft.WishInput{CatalogItemID: pearl.ID, QtyWanted: f64(1)}); err != nil {
		t.Fatalf("AddWish after bought: %v", err)
	}
}

func TestProjectMaterialsShortageAndIsolation(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	user := e.user(t, "lilya", "lilya@example.com")
	other := e.user(t, "other", "o@example.com")
	ws, _ := e.workspaces.Create(ctx, user, "Дом")
	otherWS, _ := e.workspaces.Create(ctx, other, "Чужой")

	beads, err := e.craft.CreateItem(ctx, user, ws.ID, craft.ItemInput{
		Category: "seed-beads", Name: "Бисер зелёный", Unit: "упаковка",
	})
	if err != nil {
		t.Fatal(err)
	}
	thread, err := e.craft.CreateItem(ctx, user, ws.ID, craft.ItemInput{
		Category: "floss", Name: "Мулине красное", Unit: "моток",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.craft.Adjust(ctx, user, ws.ID, beads.ID, 4, ""); err != nil {
		t.Fatal(err)
	}
	if _, err := e.craft.Adjust(ctx, user, ws.ID, thread.ID, 2, ""); err != nil {
		t.Fatal(err)
	}

	project, err := e.craft.CreateProject(ctx, user, ws.ID, craft.ProjectInput{Name: "Ёлка", Notes: "подарок"})
	if err != nil || project.MaterialCount != 0 || len(project.Materials) != 0 {
		t.Fatalf("CreateProject = %+v, %v", project, err)
	}
	if _, err := e.craft.SetProjectMaterial(ctx, user, ws.ID, project.ID, beads.ID, f64(10)); err != nil {
		t.Fatalf("SetProjectMaterial beads: %v", err)
	}
	if _, err := e.craft.SetProjectMaterial(ctx, user, ws.ID, project.ID, thread.ID, nil); err != nil {
		t.Fatalf("SetProjectMaterial thread: %v", err)
	}
	project, err = e.craft.GetProject(ctx, user, ws.ID, project.ID)
	if err != nil || project.MaterialCount != 2 {
		t.Fatalf("GetProject = %+v, %v", project, err)
	}
	beadMaterial := findMaterial(t, project, beads.ID)
	if beadMaterial.Shortage != 6 || beadMaterial.QtyUsed != 0 {
		t.Fatalf("beads material = %+v, want shortage 6", beadMaterial)
	}
	threadMaterial := findMaterial(t, project, thread.ID)
	if threadMaterial.QtyPlanned != nil || threadMaterial.Shortage != 0 {
		t.Fatalf("thread material = %+v, want no plan and no shortage", threadMaterial)
	}

	// Using material withdraws stock and records the usage.
	project, err = e.craft.UseMaterial(ctx, user, ws.ID, project.ID, beads.ID, 3)
	if err != nil {
		t.Fatalf("UseMaterial: %v", err)
	}
	beadMaterial = findMaterial(t, project, beads.ID)
	if beadMaterial.QtyUsed != 3 || beadMaterial.Item.StockQty != 1 || beadMaterial.Shortage != 6 {
		t.Fatalf("after using 3: %+v", beadMaterial)
	}

	// An excessive withdrawal changes nothing.
	if _, err := e.craft.UseMaterial(ctx, user, ws.ID, project.ID, beads.ID, 5); !errors.Is(err, craft.ErrInsufficientStock) {
		t.Fatalf("excessive UseMaterial = %v, want ErrInsufficientStock", err)
	}
	project, err = e.craft.GetProject(ctx, user, ws.ID, project.ID)
	if err != nil {
		t.Fatal(err)
	}
	beadMaterial = findMaterial(t, project, beads.ID)
	if beadMaterial.QtyUsed != 3 || beadMaterial.Item.StockQty != 1 {
		t.Fatalf("after failed withdrawal: %+v", beadMaterial)
	}
	if qty := e.inventoryQty(t, user, ws.ID, beads.ID); qty != 1 {
		t.Fatalf("stock after failed withdrawal = %v, want 1", qty)
	}

	// A material without a plan can still be used.
	if _, err := e.craft.UseMaterial(ctx, user, ws.ID, project.ID, thread.ID, 1); err != nil {
		t.Fatalf("UseMaterial without a plan: %v", err)
	}

	// The shortage becomes a wish, once.
	added, err := e.craft.ShortageToWishlist(ctx, user, ws.ID, project.ID)
	if err != nil {
		t.Fatalf("ShortageToWishlist: %v", err)
	}
	if len(added) != 1 || added[0].Title != "Бисер зелёный" || added[0].QtyWanted == nil || *added[0].QtyWanted != 6 {
		t.Fatalf("ShortageToWishlist = %+v, want only the bead shortage of 6", added)
	}
	if again, err := e.craft.ShortageToWishlist(ctx, user, ws.ID, project.ID); err != nil || len(again) != 0 {
		t.Fatalf("repeated ShortageToWishlist = %+v, %v; want nothing", again, err)
	}

	// Data of a workspace is invisible to another workspace.
	if _, err := e.craft.GetProject(ctx, other, otherWS.ID, project.ID); !errors.Is(err, craft.ErrNotFound) {
		t.Fatalf("foreign GetProject = %v, want ErrNotFound", err)
	}
	if _, err := e.craft.UseMaterial(ctx, other, otherWS.ID, project.ID, beads.ID, 1); !errors.Is(err, craft.ErrNotFound) {
		t.Fatalf("foreign UseMaterial = %v, want ErrNotFound", err)
	}
	if _, err := e.craft.ShortageToWishlist(ctx, other, otherWS.ID, project.ID); !errors.Is(err, craft.ErrNotFound) {
		t.Fatalf("foreign ShortageToWishlist = %v, want ErrNotFound", err)
	}
	if _, err := e.craft.GetProject(ctx, other, ws.ID, project.ID); !errors.Is(err, workspace.ErrNotFound) {
		t.Fatalf("foreign workspace GetProject = %v, want ErrNotFound", err)
	}

	if err := e.craft.RemoveProjectMaterial(ctx, user, ws.ID, project.ID, thread.ID); err != nil {
		t.Fatalf("RemoveProjectMaterial: %v", err)
	}
	if err := e.craft.RemoveProjectMaterial(ctx, user, ws.ID, project.ID, thread.ID); !errors.Is(err, craft.ErrNotFound) {
		t.Fatalf("second RemoveProjectMaterial = %v, want ErrNotFound", err)
	}
	if _, err := e.craft.UpdateProject(ctx, user, ws.ID, project.ID, craft.ProjectInput{Name: "Ёлка 2"}); err != nil {
		t.Fatalf("UpdateProject: %v", err)
	}
	if err := e.craft.DeleteProject(ctx, user, ws.ID, project.ID); err != nil {
		t.Fatalf("DeleteProject: %v", err)
	}
	if _, err := e.craft.GetProject(ctx, user, ws.ID, project.ID); !errors.Is(err, craft.ErrNotFound) {
		t.Fatalf("GetProject after delete = %v, want ErrNotFound", err)
	}
}

func findMaterial(t *testing.T, project craft.Project, itemID string) craft.ProjectMaterial {
	t.Helper()
	for _, material := range project.Materials {
		if material.Item.ID == itemID {
			return material
		}
	}
	t.Fatalf("material %s is not in project %s", itemID, project.ID)
	return craft.ProjectMaterial{}
}

func boolPtrTest(v bool) *bool { return &v }
