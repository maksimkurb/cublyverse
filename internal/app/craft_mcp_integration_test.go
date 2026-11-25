package app_test

import (
	"context"
	"net/http"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/maksimkurb/cublyverse/internal/auth"
	"github.com/maksimkurb/cublyverse/internal/store/db"
	"github.com/maksimkurb/cublyverse/internal/workspace"
)

// connectAt opens an MCP session at a path under the server.
func (a *mcpTestApp) connectAt(path, token string) (*mcp.ClientSession, error) {
	client := mcp.NewClient(&mcp.Implementation{Name: "cublyverse-test", Version: "1"}, nil)
	transport := &mcp.StreamableClientTransport{
		Endpoint:             a.server.URL + path,
		HTTPClient:           &http.Client{Transport: bearerTransport{token: token, base: http.DefaultTransport}},
		DisableStandaloneSSE: true,
		MaxRetries:           -1,
	}
	session, err := client.Connect(context.Background(), transport, nil)
	if err == nil {
		a.t.Cleanup(func() { _ = session.Close() })
	}
	return session, err
}

func (a *mcpTestApp) call(session *mcp.ClientSession, name string, args map[string]any) *mcp.CallToolResult {
	a.t.Helper()
	result, err := session.CallTool(context.Background(), &mcp.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		a.t.Fatalf("call %s: %v", name, err)
	}
	return result
}

// TestCraftMCPPerWorkspace covers the per-workspace endpoint: tool list,
// stock changes, scopes and workspace isolation.
func TestCraftMCPPerWorkspace(t *testing.T) {
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

	full := env.issueToken(auth.ScopeCraftRead, auth.ScopeCraftWrite)
	session, err := env.connectAt(path, full)
	if err != nil {
		t.Fatalf("connect workspace endpoint: %v", err)
	}
	tools, err := session.ListTools(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	have := map[string]bool{}
	for _, tool := range tools.Tools {
		have[tool.Name] = true
	}
	for _, want := range []string{"whoami", "craft_reference", "craft_catalog_search", "craft_stock_adjust", "craft_item_create"} {
		if !have[want] {
			t.Errorf("workspace endpoint lacks %s; has %v", want, have)
		}
	}
	if have["reminders_list"] {
		t.Error("reminders must not be exposed on the workspace endpoint")
	}

	created := env.call(session, "craft_item_create", map[string]any{
		"category": "pearls", "name": "Жемчуг Майорка айвори 2 мм", "unit": "см",
	})
	if created.IsError {
		t.Fatalf("item_create: %s", toolText(created))
	}
	itemID := structured(t, created)["id"].(string)

	added := env.call(session, "craft_stock_adjust", map[string]any{"item_id": itemID, "delta": 32, "reason": "импорт"})
	if added.IsError || structured(t, added)["qty"] != float64(32) {
		t.Fatalf("adjust +32: error=%v %s", added.IsError, toolText(added))
	}
	if over := env.call(session, "craft_stock_adjust", map[string]any{"item_id": itemID, "delta": -100}); !over.IsError {
		t.Fatal("overdraw must be a tool error")
	}
	list := env.call(session, "craft_inventory_list", map[string]any{"max_qty": 50})
	if list.IsError || len(structured(t, list)["stock"].([]any)) != 1 {
		t.Fatalf("inventory_list: %s", toolText(list))
	}

	// A read-only token cannot change stock.
	readOnly, err := env.connectAt(path, env.issueToken(auth.ScopeCraftRead))
	if err != nil {
		t.Fatal(err)
	}
	if denied := env.call(readOnly, "craft_stock_adjust", map[string]any{"item_id": itemID, "delta": 1}); !denied.IsError {
		t.Fatal("read-only token must not adjust stock")
	}

	// A user outside the workspace cannot reach it at all.
	stranger := env.createUser("stranger")
	if _, err := env.connectAt(path, env.issueTokenFor(stranger, auth.ScopeCraftRead)); err == nil {
		t.Fatal("non-member must not connect to the workspace endpoint")
	}
}
