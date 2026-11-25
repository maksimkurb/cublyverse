package app_test

import (
	"context"
	"testing"

	"github.com/maksimkurb/cublyverse/internal/auth"
	"github.com/maksimkurb/cublyverse/internal/store/db"
	"github.com/maksimkurb/cublyverse/internal/workspace"
)

// TestCraftWishlistAndProjectsMCP covers the wish list and project tools over
// the per-workspace MCP endpoint: adding wishes, shop links, planning materials,
// using stock, the shortage to wish list flow and the scope check.
func TestCraftWishlistAndProjectsMCP(t *testing.T) {
	env := newMCPTestApp(t)
	ctx := context.Background()
	q := env.app.Store().Queries()

	owner, _ := workspace.ParseID(env.user.ID)
	ws, err := q.CreateWorkspace(ctx, db.CreateWorkspaceParams{Name: "Дом", CreatedBy: owner})
	if err != nil {
		t.Fatal(err)
	}
	if err := q.AddWorkspaceMember(ctx, db.AddWorkspaceMemberParams{WorkspaceID: ws.ID, UserID: owner, AddedBy: owner}); err != nil {
		t.Fatal(err)
	}
	path := "/mcp/w/" + workspace.FormatID(ws.ID)
	session, err := env.connectAt(path, env.issueToken(auth.ScopeCraftRead, auth.ScopeCraftWrite))
	if err != nil {
		t.Fatalf("connect workspace endpoint: %v", err)
	}

	created := env.call(session, "craft_item_create", map[string]any{
		"category": "seed-beads", "name": "Бисер зелёный", "unit": "упаковка",
	})
	if created.IsError {
		t.Fatalf("item_create: %s", toolText(created))
	}
	itemID := structured(t, created)["id"].(string)
	if added := env.call(session, "craft_stock_adjust", map[string]any{"item_id": itemID, "delta": 10}); added.IsError {
		t.Fatalf("stock_adjust: %s", toolText(added))
	}

	wishResult := env.call(session, "craft_wishlist_add", map[string]any{
		"catalog_item_id": itemID, "qty_wanted": 4, "priority": 1,
	})
	if wishResult.IsError {
		t.Fatalf("wishlist_add: %s", toolText(wishResult))
	}
	wish := structured(t, wishResult)
	wishID := wish["id"].(string)
	if wish["title"] != "Бисер зелёный" || wish["status"] != "wanted" {
		t.Fatalf("wish = %v", wish)
	}
	if duplicate := env.call(session, "craft_wishlist_add", map[string]any{"catalog_item_id": itemID}); !duplicate.IsError {
		t.Fatal("a second wanted entry for the same item must be a tool error")
	}

	linkResult := env.call(session, "craft_wishlist_link_add", map[string]any{
		"wish_id": wishID, "url": "https://shop.example/beads", "shop_name": "Магазин",
		"price": 12.5, "currency": "rub", "in_stock": true,
	})
	if linkResult.IsError {
		t.Fatalf("wishlist_link_add: %s", toolText(linkResult))
	}
	linkID := structured(t, linkResult)["id"].(string)

	listed := env.call(session, "craft_wishlist_list", map[string]any{"status": "all"})
	if listed.IsError {
		t.Fatalf("wishlist_list: %s", toolText(listed))
	}
	wishes := structured(t, listed)["wishes"].([]any)
	if len(wishes) != 1 || len(wishes[0].(map[string]any)["links"].([]any)) != 1 {
		t.Fatalf("wishlist_list = %s", toolText(listed))
	}
	if removed := env.call(session, "craft_wishlist_link_remove", map[string]any{"wish_id": wishID, "link_id": linkID}); removed.IsError {
		t.Fatalf("wishlist_link_remove: %s", toolText(removed))
	}

	projectResult := env.call(session, "craft_project_create", map[string]any{"name": "Ёлка", "notes": "подарок"})
	if projectResult.IsError {
		t.Fatalf("project_create: %s", toolText(projectResult))
	}
	projectID := structured(t, projectResult)["id"].(string)
	planned := env.call(session, "craft_project_set_material", map[string]any{
		"project_id": projectID, "item_id": itemID, "qty_planned": 20,
	})
	if planned.IsError {
		t.Fatalf("project_set_material: %s", toolText(planned))
	}
	got := env.call(session, "craft_project_get", map[string]any{"project_id": projectID})
	if got.IsError {
		t.Fatalf("project_get: %s", toolText(got))
	}
	materials := structured(t, got)["materials"].([]any)
	if len(materials) != 1 {
		t.Fatalf("project_get = %s", toolText(got))
	}
	material := materials[0].(map[string]any)
	if material["qty_planned"] != float64(20) || material["shortage"] != float64(10) {
		t.Fatalf("material = %v, want planned 20 and shortage 10", material)
	}

	used := env.call(session, "craft_project_use_material", map[string]any{
		"project_id": projectID, "item_id": itemID, "qty": 8,
	})
	if used.IsError {
		t.Fatalf("project_use_material: %s", toolText(used))
	}
	got = env.call(session, "craft_project_get", map[string]any{"project_id": projectID})
	material = structured(t, got)["materials"].([]any)[0].(map[string]any)
	if material["qty_used"] != float64(8) || material["shortage"] != float64(10) {
		t.Fatalf("material after use = %v, want used 8 and shortage 10", material)
	}
	over := env.call(session, "craft_project_use_material", map[string]any{
		"project_id": projectID, "item_id": itemID, "qty": 100,
	})
	if !over.IsError {
		t.Fatal("an excessive withdrawal must be a tool error")
	}

	// The already wanted item is skipped, so remove it first and let the
	// shortage fill the wish list instead.
	if removed := env.call(session, "craft_wishlist_remove", map[string]any{"wish_id": wishID}); removed.IsError {
		t.Fatalf("wishlist_remove: %s", toolText(removed))
	}
	shortage := env.call(session, "craft_project_shortage_to_wishlist", map[string]any{"project_id": projectID})
	if shortage.IsError {
		t.Fatalf("project_shortage_to_wishlist: %s", toolText(shortage))
	}
	addedWishes := structured(t, shortage)["wishes"].([]any)
	if len(addedWishes) != 1 {
		t.Fatalf("shortage_to_wishlist = %s, want one wish", toolText(shortage))
	}
	shortageWish := addedWishes[0].(map[string]any)
	if shortageWish["qty_wanted"] != float64(10) {
		t.Fatalf("shortage wish = %v, want qty 10", shortageWish)
	}
	if again := env.call(session, "craft_project_shortage_to_wishlist", map[string]any{"project_id": projectID}); again.IsError ||
		len(structured(t, again)["wishes"].([]any)) != 0 {
		t.Fatalf("repeated shortage_to_wishlist must add nothing: %s", toolText(again))
	}

	bought := env.call(session, "craft_wishlist_mark_bought", map[string]any{
		"wish_id": shortageWish["id"], "add_to_stock": true,
	})
	if bought.IsError {
		t.Fatalf("wishlist_mark_bought: %s", toolText(bought))
	}
	if structured(t, bought)["status"] != "bought" {
		t.Fatalf("mark_bought = %s, want status bought", toolText(bought))
	}
	projectList := env.call(session, "craft_project_list", map[string]any{})
	if projectList.IsError || len(structured(t, projectList)["projects"].([]any)) != 1 {
		t.Fatalf("project_list = %s", toolText(projectList))
	}

	// A read-only token cannot change the wish list.
	readOnly, err := env.connectAt(path, env.issueToken(auth.ScopeCraftRead))
	if err != nil {
		t.Fatal(err)
	}
	if denied := env.call(readOnly, "craft_wishlist_add", map[string]any{"title": "Лента"}); !denied.IsError {
		t.Fatal("a read-only token must not add a wish")
	}
	if denied := env.call(readOnly, "craft_project_create", map[string]any{"name": "Чужой"}); !denied.IsError {
		t.Fatal("a read-only token must not create a project")
	}
}
