package app_test

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/maksimkurb/cublyverse/internal/auth"
)

// TestWorkspacesAndCraftAPI drives the workspace, catalogue and stock endpoints
// through the real HTTP stack: CRUD, membership rules, candidate search and the
// error mapping of the services.
func TestWorkspacesAndCraftAPI(t *testing.T) {
	env := newMCPTestApp(t)
	ctx := context.Background()
	token := env.session.Token

	// --- workspaces ---------------------------------------------------------

	status, body := env.api(http.MethodPost, "/workspaces", token, `{"name":"Дом","app":"warehouse"}`)
	if status != http.StatusCreated {
		t.Fatalf("create workspace status %d: %s", status, body)
	}
	var workspace struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	}
	if err := json.Unmarshal([]byte(body), &workspace); err != nil || workspace.ID == "" || workspace.Name != "Дом" {
		t.Fatalf("create workspace = %s (%v)", body, err)
	}
	ws := workspace.ID

	status, body = env.api(http.MethodGet, "/workspaces", token, "")
	if status != http.StatusOK || !containsID(t, body, ws) {
		t.Fatalf("list workspaces status %d: %s", status, body)
	}

	status, body = env.api(http.MethodPatch, "/workspaces/"+ws, token, `{"name":"Мастерская"}`)
	if status != http.StatusOK || !strings.Contains(body, "Мастерская") {
		t.Fatalf("rename workspace status %d: %s", status, body)
	}

	// A user outside the workspace sees nothing.
	stranger := env.createUser("stranger")
	strangerSession, err := env.app.Auth().CreateSession(ctx, stranger.ID, auth.SessionKindBearer)
	if err != nil {
		t.Fatal(err)
	}
	if status, _ = env.api(http.MethodGet, "/workspaces/"+ws+"/members", strangerSession.Token, ""); status != http.StatusNotFound {
		t.Fatalf("foreign members status = %d, want 404", status)
	}
	if status, _ = env.api(http.MethodGet, "/workspaces/"+ws+"/catalog", strangerSession.Token, ""); status != http.StatusNotFound {
		t.Fatalf("foreign catalog status = %d, want 404", status)
	}

	// A personal access token may not use the session-only workspace API.
	if status, _ = env.api(http.MethodGet, "/workspaces", env.issueToken(auth.ScopeProfileRead), ""); status != http.StatusForbidden {
		t.Fatalf("PAT list workspaces status = %d, want 403", status)
	}

	// Candidates exclude current members; adding is idempotent.
	friend := env.createUser("friend")
	status, body = env.api(http.MethodGet, "/workspaces/"+ws+"/member-candidates", token, "")
	if status != http.StatusOK || !containsID(t, body, friend.ID) {
		t.Fatalf("candidates status %d: %s", status, body)
	}
	addFriend := `{"user_id":"` + friend.ID + `"}`
	if status, body = env.api(http.MethodPost, "/workspaces/"+ws+"/members", token, addFriend); status != http.StatusNoContent {
		t.Fatalf("add member status %d: %s", status, body)
	}
	if status, body = env.api(http.MethodPost, "/workspaces/"+ws+"/members", token, addFriend); status != http.StatusNoContent {
		t.Fatalf("re-add member status %d: %s", status, body)
	}
	status, body = env.api(http.MethodGet, "/workspaces/"+ws+"/members", token, "")
	if status != http.StatusOK {
		t.Fatalf("members status %d: %s", status, body)
	}
	var members []struct {
		UserID    string `json:"user_id"`
		AvatarURL string `json:"avatar_url"`
	}
	if err := json.Unmarshal([]byte(body), &members); err != nil || len(members) != 2 {
		t.Fatalf("members = %s (%v)", body, err)
	}
	for _, member := range members {
		if member.AvatarURL == "" {
			t.Errorf("member %s has no avatar_url", member.UserID)
		}
	}
	if status, body = env.api(http.MethodGet, "/workspaces/"+ws+"/member-candidates", token, ""); status != http.StatusOK || containsID(t, body, friend.ID) {
		t.Fatalf("candidates after add status %d: %s", status, body)
	}

	// --- catalogue ----------------------------------------------------------

	status, body = env.api(http.MethodGet, "/craft/categories", token, "")
	if status != http.StatusOK || !strings.Contains(body, `"slug":"pearls"`) || !strings.Contains(body, `"default_unit":"шт"`) {
		t.Fatalf("categories status %d: %s", status, body)
	}
	if status, body = env.api(http.MethodGet, "/craft/manufacturers", token, ""); status != http.StatusOK {
		t.Fatalf("manufacturers status %d: %s", status, body)
	}

	status, body = env.api(http.MethodPost, "/workspaces/"+ws+"/catalog", token,
		`{"category":"pearls","name":"Жемчуг айвори 2 мм","unit":"шт","color_name":"айвори","color_hex":"#f2e8d5"}`)
	if status != http.StatusCreated {
		t.Fatalf("create item status %d: %s", status, body)
	}
	var item struct {
		ID          string `json:"id"`
		Private     bool   `json:"private"`
		ColorHex    string `json:"color_hex"`
		ColorSource string `json:"color_source"`
		Unit        string `json:"unit"`
	}
	if err := json.Unmarshal([]byte(body), &item); err != nil || item.ID == "" || !item.Private || item.Unit != "шт" {
		t.Fatalf("created item = %s (%v)", body, err)
	}
	if item.ColorHex != "#F2E8D5" || item.ColorSource != "approx" {
		t.Fatalf("colour = %q/%q, want #F2E8D5/approx", item.ColorHex, item.ColorSource)
	}

	if status, body = env.api(http.MethodGet, "/workspaces/"+ws+"/catalog/"+item.ID, token, ""); status != http.StatusOK || !strings.Contains(body, "Жемчуг айвори") {
		t.Fatalf("get item status %d: %s", status, body)
	}
	if status, body = env.api(http.MethodGet, "/workspaces/"+ws+"/catalog?q=Жемчуг", token, ""); status != http.StatusOK || !containsID(t, body, item.ID) {
		t.Fatalf("search catalog status %d: %s", status, body)
	}

	// --- stock --------------------------------------------------------------

	status, body = env.api(http.MethodPut, "/workspaces/"+ws+"/inventory/"+item.ID, token, `{"qty":5,"reason":"пересчёт"}`)
	if status != http.StatusOK || !strings.Contains(body, `"qty":5`) {
		t.Fatalf("set stock status %d: %s", status, body)
	}
	status, body = env.api(http.MethodPost, "/workspaces/"+ws+"/inventory/"+item.ID+"/adjust", token, `{"delta":2,"reason":"покупка"}`)
	if status != http.StatusOK || !strings.Contains(body, `"qty":7`) {
		t.Fatalf("adjust stock status %d: %s", status, body)
	}
	if status, body = env.api(http.MethodPost, "/workspaces/"+ws+"/inventory/"+item.ID+"/adjust", token, `{"delta":-100}`); status != http.StatusConflict {
		t.Fatalf("overdraw status = %d, want 409: %s", status, body)
	}
	status, body = env.api(http.MethodGet, "/workspaces/"+ws+"/inventory?q=Жемчуг&max_qty=10", token, "")
	if status != http.StatusOK || !strings.Contains(body, `"id":"`+item.ID+`"`) {
		t.Fatalf("low stock list status %d: %s", status, body)
	}
	if status, body = env.api(http.MethodPatch, "/workspaces/"+ws+"/inventory/"+item.ID, token, `{"location":"Ящик 2","notes":"ярмарка"}`); status != http.StatusNoContent {
		t.Fatalf("update stock meta status %d: %s", status, body)
	}
	status, body = env.api(http.MethodGet, "/workspaces/"+ws+"/inventory", token, "")
	if status != http.StatusOK || !strings.Contains(body, "Ящик 2") || !strings.Contains(body, "ярмарка") {
		t.Fatalf("stock meta = %s", body)
	}

	// Linking a private item to another one merges the stock and deletes the
	// source row.
	status, body = env.api(http.MethodPost, "/workspaces/"+ws+"/catalog", token,
		`{"category":"pearls","name":"Жемчуг второй","unit":"шт"}`)
	if status != http.StatusCreated {
		t.Fatalf("create second item status %d: %s", status, body)
	}
	var second struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal([]byte(body), &second); err != nil || second.ID == "" {
		t.Fatalf("second item = %s (%v)", body, err)
	}
	if status, body = env.api(http.MethodPost, "/workspaces/"+ws+"/inventory/"+second.ID+"/adjust", token, `{"delta":3}`); status != http.StatusOK {
		t.Fatalf("adjust second item status %d: %s", status, body)
	}
	status, body = env.api(http.MethodPost, "/workspaces/"+ws+"/catalog/"+second.ID+"/link", token, `{"target_item_id":"`+item.ID+`"}`)
	if status != http.StatusOK || !strings.Contains(body, `"qty":10`) {
		t.Fatalf("link status %d: %s", status, body)
	}
	if status, body = env.api(http.MethodGet, "/workspaces/"+ws+"/catalog/"+second.ID, token, ""); status != http.StatusNotFound {
		t.Fatalf("linked item status = %d, want 404: %s", status, body)
	}

	if status, body = env.api(http.MethodGet, "/workspaces/"+ws+"/catalog/"+item.ID, token, ""); status != http.StatusOK || !strings.Contains(body, `"in_inventory":true`) {
		t.Fatalf("stocked item = %s", body)
	}
	if status, body = env.api(http.MethodDelete, "/workspaces/"+ws+"/inventory/"+item.ID, token, ""); status != http.StatusNoContent {
		t.Fatalf("remove stock status %d: %s", status, body)
	}
	if status, body = env.api(http.MethodDelete, "/workspaces/"+ws+"/catalog/"+item.ID, token, ""); status != http.StatusNoContent {
		t.Fatalf("delete item status %d: %s", status, body)
	}

	// --- membership rules and workspace deletion ----------------------------

	if status, body = env.api(http.MethodDelete, "/workspaces/"+ws+"/members/"+friend.ID, token, ""); status != http.StatusNoContent {
		t.Fatalf("remove member status %d: %s", status, body)
	}

	status, body = env.api(http.MethodPost, "/workspaces", token, `{"name":"Один","app":"warehouse"}`)
	if status != http.StatusCreated {
		t.Fatalf("create single workspace status %d: %s", status, body)
	}
	var single struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal([]byte(body), &single); err != nil || single.ID == "" {
		t.Fatalf("single workspace = %s (%v)", body, err)
	}
	if status, body = env.api(http.MethodDelete, "/workspaces/"+single.ID+"/members/"+env.user.ID, token, ""); status != http.StatusConflict {
		t.Fatalf("remove last member status = %d, want 409: %s", status, body)
	}

	if status, body = env.api(http.MethodDelete, "/workspaces/"+ws, token, ""); status != http.StatusNoContent {
		t.Fatalf("delete workspace status %d: %s", status, body)
	}
	status, body = env.api(http.MethodGet, "/workspaces", token, "")
	if status != http.StatusOK || containsID(t, body, ws) {
		t.Fatalf("workspaces after delete status %d: %s", status, body)
	}
}

// containsID reports whether a JSON array in body has an "id" or "user_id"
// equal to id.
func containsID(t *testing.T, body, id string) bool {
	t.Helper()
	var rows []map[string]any
	if err := json.Unmarshal([]byte(body), &rows); err != nil {
		return false
	}
	for _, row := range rows {
		if row["id"] == id || row["user_id"] == id {
			return true
		}
	}
	return false
}
