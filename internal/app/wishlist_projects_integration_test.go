package app_test

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/maksimkurb/cublyverse/internal/auth"
)

// TestWishlistAndProjectsAPI drives the wish list and project endpoints through
// the real HTTP stack: status grouping, shop links, marking a purchase bought
// with a stock change, the project material plan, the shortage transfer and the
// membership rules.
func TestWishlistAndProjectsAPI(t *testing.T) {
	env := newMCPTestApp(t)
	ctx := context.Background()
	token := env.session.Token

	status, body := env.api(http.MethodPost, "/workspaces", token, `{"name":"Дом"}`)
	if status != http.StatusCreated {
		t.Fatalf("create workspace status %d: %s", status, body)
	}
	var workspace struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal([]byte(body), &workspace); err != nil || workspace.ID == "" {
		t.Fatalf("workspace = %s (%v)", body, err)
	}
	ws := workspace.ID

	status, body = env.api(http.MethodPost, "/workspaces/"+ws+"/catalog", token,
		`{"category":"pearls","name":"Жемчуг айвори 2 мм","unit":"шт"}`)
	if status != http.StatusCreated {
		t.Fatalf("create item status %d: %s", status, body)
	}
	var item struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal([]byte(body), &item); err != nil || item.ID == "" {
		t.Fatalf("item = %s (%v)", body, err)
	}
	if status, body = env.api(http.MethodPut, "/workspaces/"+ws+"/inventory/"+item.ID, token, `{"qty":10}`); status != http.StatusOK {
		t.Fatalf("set stock status %d: %s", status, body)
	}

	// --- wish list ----------------------------------------------------------

	status, body = env.api(http.MethodPost, "/workspaces/"+ws+"/wishlist", token,
		`{"title":"Лента атласная","qty_wanted":2,"priority":1,"note":"для серег"}`)
	if status != http.StatusCreated {
		t.Fatalf("add free-text wish status %d: %s", status, body)
	}
	var freeWish struct {
		ID            string  `json:"id"`
		Status        string  `json:"status"`
		CatalogItemID string  `json:"catalog_item_id"`
		QtyWanted     float64 `json:"qty_wanted"`
	}
	if err := json.Unmarshal([]byte(body), &freeWish); err != nil || freeWish.ID == "" || freeWish.Status != "wanted" {
		t.Fatalf("free-text wish = %s (%v)", body, err)
	}
	if freeWish.CatalogItemID != "" || freeWish.QtyWanted != 2 {
		t.Fatalf("free-text wish = %+v", freeWish)
	}

	status, body = env.api(http.MethodPost, "/workspaces/"+ws+"/wishlist", token,
		`{"catalog_item_id":"`+item.ID+`","qty_wanted":3,"priority":2}`)
	if status != http.StatusCreated {
		t.Fatalf("add item wish status %d: %s", status, body)
	}
	var itemWish struct {
		ID     string `json:"id"`
		Title  string `json:"title"`
		Item   *struct {
			ID       string  `json:"id"`
			StockQty float64 `json:"stock_qty"`
		} `json:"item"`
		Links []struct {
			ID       string `json:"id"`
			Currency string `json:"currency"`
		} `json:"links"`
	}
	if err := json.Unmarshal([]byte(body), &itemWish); err != nil || itemWish.ID == "" {
		t.Fatalf("item wish = %s (%v)", body, err)
	}
	if itemWish.Title != "Жемчуг айвори 2 мм" || itemWish.Item == nil || itemWish.Item.StockQty != 10 {
		t.Fatalf("item wish = %s", body)
	}

	// A catalogue item can be wanted once at a time.
	if status, body = env.api(http.MethodPost, "/workspaces/"+ws+"/wishlist", token,
		`{"catalog_item_id":"`+item.ID+`"}`); status != http.StatusBadRequest {
		t.Fatalf("duplicate wish status = %d, want 400: %s", status, body)
	}
	// An unknown filter is a client mistake.
	if status, body = env.api(http.MethodGet, "/workspaces/"+ws+"/wishlist?status=lost", token, ""); status != http.StatusBadRequest {
		t.Fatalf("bad status filter = %d, want 400: %s", status, body)
	}
	if status, body = env.api(http.MethodGet, "/workspaces/"+ws+"/wishlist", token, ""); status != http.StatusOK || !containsID(t, body, freeWish.ID) || !containsID(t, body, itemWish.ID) {
		t.Fatalf("list wanted status %d: %s", status, body)
	}
	if status, body = env.api(http.MethodGet, "/workspaces/"+ws+"/wishlist?status=all", token, ""); status != http.StatusOK || !strings.Contains(body, "Лента атласная") {
		t.Fatalf("list all status %d: %s", status, body)
	}

	// --- shop links ---------------------------------------------------------

	status, body = env.api(http.MethodPost, "/workspaces/"+ws+"/wishlist/"+itemWish.ID+"/links", token,
		`{"url":"https://shop.example/beads","shop_name":"Магазин бусин","price":12.5,"currency":"rub","in_stock":true}`)
	if status != http.StatusCreated {
		t.Fatalf("add link status %d: %s", status, body)
	}
	var link struct {
		ID       string `json:"id"`
		Currency string `json:"currency"`
		InStock  *bool  `json:"in_stock"`
	}
	if err := json.Unmarshal([]byte(body), &link); err != nil || link.ID == "" || link.Currency != "RUB" {
		t.Fatalf("link = %s (%v)", body, err)
	}
	if link.InStock == nil || !*link.InStock {
		t.Fatalf("link in_stock = %v, want true", link.InStock)
	}
	if status, body = env.api(http.MethodPost, "/workspaces/"+ws+"/wishlist/"+itemWish.ID+"/links", token,
		`{"url":"ftp://shop.example/x"}`); status != http.StatusBadRequest {
		t.Fatalf("ftp link status = %d, want 400: %s", status, body)
	}
	if status, body = env.api(http.MethodGet, "/workspaces/"+ws+"/wishlist?status=all", token, ""); status != http.StatusOK || !strings.Contains(body, "Магазин бусин") {
		t.Fatalf("links in list = %s", body)
	}

	// --- bought adds the purchase to the stock -------------------------------

	status, body = env.api(http.MethodPost, "/workspaces/"+ws+"/wishlist/"+itemWish.ID+"/bought", token, `{"add_to_stock":true}`)
	if status != http.StatusOK || !strings.Contains(body, `"status":"bought"`) {
		t.Fatalf("mark bought status %d: %s", status, body)
	}
	status, body = env.api(http.MethodGet, "/workspaces/"+ws+"/inventory", token, "")
	if status != http.StatusOK || !strings.Contains(body, `"qty":13`) {
		t.Fatalf("stock after purchase status %d: %s", status, body)
	}
	// A repeated call must not add the quantity again.
	if status, body = env.api(http.MethodPost, "/workspaces/"+ws+"/wishlist/"+itemWish.ID+"/bought", token, `{"add_to_stock":true}`); status != http.StatusOK {
		t.Fatalf("repeat bought status %d: %s", status, body)
	}
	if status, body = env.api(http.MethodGet, "/workspaces/"+ws+"/inventory", token, ""); status != http.StatusOK || !strings.Contains(body, `"qty":13`) {
		t.Fatalf("stock after repeated purchase = %s", body)
	}
	if status, body = env.api(http.MethodGet, "/workspaces/"+ws+"/wishlist?status=bought", token, ""); status != http.StatusOK || !containsID(t, body, itemWish.ID) {
		t.Fatalf("bought list status %d: %s", status, body)
	}
	if status, body = env.api(http.MethodGet, "/workspaces/"+ws+"/wishlist", token, ""); status != http.StatusOK || containsID(t, body, itemWish.ID) {
		t.Fatalf("wanted list after purchase = %s", body)
	}

	// --- projects -----------------------------------------------------------

	status, body = env.api(http.MethodPost, "/workspaces/"+ws+"/projects", token, `{"name":"Серьги «Майорка»","notes":"подарок"}`)
	if status != http.StatusCreated {
		t.Fatalf("create project status %d: %s", status, body)
	}
	var project struct {
		ID            string `json:"id"`
		MaterialCount int    `json:"material_count"`
	}
	if err := json.Unmarshal([]byte(body), &project); err != nil || project.ID == "" || project.MaterialCount != 0 {
		t.Fatalf("project = %s (%v)", body, err)
	}
	if status, body = env.api(http.MethodGet, "/workspaces/"+ws+"/projects", token, ""); status != http.StatusOK || !containsID(t, body, project.ID) {
		t.Fatalf("list projects status %d: %s", status, body)
	}

	status, body = env.api(http.MethodPut, "/workspaces/"+ws+"/projects/"+project.ID+"/materials/"+item.ID, token, `{"qty_planned":20}`)
	if status != http.StatusOK {
		t.Fatalf("plan material status %d: %s", status, body)
	}
	var detailed struct {
		Materials []struct {
			QtyPlanned float64 `json:"qty_planned"`
			QtyUsed    float64 `json:"qty_used"`
			Shortage   float64 `json:"shortage"`
		} `json:"materials"`
	}
	if err := json.Unmarshal([]byte(body), &detailed); err != nil || len(detailed.Materials) != 1 {
		t.Fatalf("planned project = %s (%v)", body, err)
	}
	if detailed.Materials[0].Shortage != 7 || detailed.Materials[0].QtyUsed != 0 {
		t.Fatalf("shortage = %+v, want 7 with 0 used", detailed.Materials[0])
	}

	// Using more than the stock changes nothing.
	if status, body = env.api(http.MethodPost, "/workspaces/"+ws+"/projects/"+project.ID+"/materials/"+item.ID+"/use", token, `{"qty":100}`); status != http.StatusConflict {
		t.Fatalf("overdraw status = %d, want 409: %s", status, body)
	}
	if status, body = env.api(http.MethodGet, "/workspaces/"+ws+"/inventory", token, ""); status != http.StatusOK || !strings.Contains(body, `"qty":13`) {
		t.Fatalf("stock after failed use = %s", body)
	}

	status, body = env.api(http.MethodPost, "/workspaces/"+ws+"/projects/"+project.ID+"/materials/"+item.ID+"/use", token, `{"qty":3}`)
	if status != http.StatusOK {
		t.Fatalf("use material status %d: %s", status, body)
	}
	if err := json.Unmarshal([]byte(body), &detailed); err != nil || len(detailed.Materials) != 1 {
		t.Fatalf("project after use = %s (%v)", body, err)
	}
	if detailed.Materials[0].QtyUsed != 3 || detailed.Materials[0].Shortage != 7 {
		t.Fatalf("material after use = %+v, want 3 used and 7 short", detailed.Materials[0])
	}
	if status, body = env.api(http.MethodGet, "/workspaces/"+ws+"/inventory", token, ""); status != http.StatusOK || !strings.Contains(body, `"qty":10`) {
		t.Fatalf("stock after use = %s", body)
	}

	// The shortage becomes a wish; a repeated call adds nothing.
	status, body = env.api(http.MethodPost, "/workspaces/"+ws+"/projects/"+project.ID+"/shortage-to-wishlist", token, "")
	if status != http.StatusOK {
		t.Fatalf("shortage to wishlist status %d: %s", status, body)
	}
	var added []struct {
		ID        string  `json:"id"`
		QtyWanted float64 `json:"qty_wanted"`
		Note      string  `json:"note"`
	}
	if err := json.Unmarshal([]byte(body), &added); err != nil || len(added) != 1 {
		t.Fatalf("shortage wishes = %s (%v)", body, err)
	}
	if added[0].QtyWanted != 7 || !strings.Contains(added[0].Note, "Серьги") {
		t.Fatalf("shortage wish = %+v", added[0])
	}
	if status, body = env.api(http.MethodPost, "/workspaces/"+ws+"/projects/"+project.ID+"/shortage-to-wishlist", token, ""); status != http.StatusOK || strings.TrimSpace(body) != "[]" {
		t.Fatalf("repeat shortage status %d: %s", status, body)
	}

	// A material can be removed from the plan; stock does not change.
	if status, body = env.api(http.MethodDelete, "/workspaces/"+ws+"/projects/"+project.ID+"/materials/"+item.ID, token, ""); status != http.StatusNoContent {
		t.Fatalf("remove material status %d: %s", status, body)
	}
	if status, body = env.api(http.MethodGet, "/workspaces/"+ws+"/projects/"+project.ID, token, ""); status != http.StatusOK || strings.Contains(body, `"qty_planned":20`) {
		t.Fatalf("project after material removal = %s", body)
	}

	// --- membership rules ---------------------------------------------------

	stranger := env.createUser("wishlist-stranger")
	strangerSession, err := env.app.Auth().CreateSession(ctx, stranger.ID, auth.SessionKindBearer)
	if err != nil {
		t.Fatal(err)
	}
	if status, _ = env.api(http.MethodGet, "/workspaces/"+ws+"/wishlist", strangerSession.Token, ""); status != http.StatusNotFound {
		t.Fatalf("foreign wishlist status = %d, want 404", status)
	}
	if status, _ = env.api(http.MethodGet, "/workspaces/"+ws+"/projects/"+project.ID, strangerSession.Token, ""); status != http.StatusNotFound {
		t.Fatalf("foreign project status = %d, want 404", status)
	}

	// --- cleanup ------------------------------------------------------------

	if status, body = env.api(http.MethodDelete, "/workspaces/"+ws+"/projects/"+project.ID, token, ""); status != http.StatusNoContent {
		t.Fatalf("delete project status %d: %s", status, body)
	}
	if status, body = env.api(http.MethodDelete, "/workspaces/"+ws+"/wishlist/"+freeWish.ID, token, ""); status != http.StatusNoContent {
		t.Fatalf("delete wish status %d: %s", status, body)
	}
	if status, body = env.api(http.MethodGet, "/workspaces/"+ws+"/wishlist?status=all", token, ""); status != http.StatusOK || containsID(t, body, freeWish.ID) {
		t.Fatalf("wishlist after delete status %d: %s", status, body)
	}
}
