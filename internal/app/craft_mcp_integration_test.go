package app_test

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/maksimkurb/cublyverse/internal/auth"
	"github.com/maksimkurb/cublyverse/internal/features/craft"
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

// TestCraftMCP covers the warehouse tools on the single endpoint: tool list,
// stock changes, scopes, choosing a space and isolation between users.
func TestCraftMCP(t *testing.T) {
	env := newMCPTestApp(t)
	ctx := context.Background()

	spaces := workspace.New(env.app.Store())
	craft.New(env.app.Store(), spaces) // new warehouses start from a template
	home, err := spaces.Create(ctx, env.user.ID, workspace.AppWarehouse, "Дом")
	if err != nil {
		t.Fatal(err)
	}
	const path = "/mcp"

	full := env.issueToken(auth.ScopeCraftRead, auth.ScopeCraftWrite)
	session, err := env.connectAt(path, full)
	if err != nil {
		t.Fatalf("connect mcp endpoint: %v", err)
	}
	tools, err := session.ListTools(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	have := map[string]bool{}
	for _, tool := range tools.Tools {
		have[tool.Name] = true
	}
	for _, want := range []string{"whoami", "spaces_list", "reminders_list", "craft_reference", "craft_catalog_search", "craft_stock_adjust", "craft_item_create"} {
		if !have[want] {
			t.Errorf("endpoint lacks %s; has %v", want, have)
		}
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

	// A second warehouse makes the space argument necessary; a name picks one.
	if _, err := spaces.Create(ctx, env.user.ID, workspace.AppWarehouse, "Гараж"); err != nil {
		t.Fatal(err)
	}
	if ambiguous := env.call(session, "craft_inventory_list", map[string]any{}); !ambiguous.IsError || !strings.Contains(toolText(ambiguous), "Гараж") {
		t.Fatalf("ambiguous space must list the choices: %s", toolText(ambiguous))
	}
	byName := env.call(session, "craft_inventory_list", map[string]any{"space": "дом"})
	if byName.IsError || len(structured(t, byName)["stock"].([]any)) != 1 {
		t.Fatalf("inventory by name: %s", toolText(byName))
	}

	// A user outside the space cannot reach it, neither by id nor by name.
	stranger := env.createUser("stranger")
	other, err := env.connectAt(path, env.issueTokenFor(stranger, auth.ScopeCraftRead))
	if err != nil {
		t.Fatal(err)
	}
	for _, ref := range []string{"Дом", home.ID, ""} {
		if denied := env.call(other, "craft_inventory_list", map[string]any{"space": ref}); !denied.IsError {
			t.Fatalf("non-member must not read the space %q", ref)
		}
	}
}
