package mcpserver

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"slices"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// ToolDoc is the documentation view of one registered tool: what it does, how
// a client may treat it and the JSON Schemas of its arguments and result.
//
// The documentation is generated from the live server, so a tool that exists in
// code but not in docs/MCP.md fails the documentation check instead of drifting.
type ToolDoc struct {
	// Name is the tool name a client calls.
	Name string
	// Title is the human readable title, falling back to the name.
	Title string
	// Description is the English description shown to the model.
	Description string
	// ReadOnly, Destructive and Idempotent mirror the tool annotations.
	ReadOnly    bool
	Destructive bool
	Idempotent  bool
	// InputSchema and OutputSchema are the JSON Schemas as sent to clients.
	InputSchema  json.RawMessage
	OutputSchema json.RawMessage
}

// GeneratedToolsBegin and GeneratedToolsEnd delimit the block of docs/MCP.md
// that `cublyverse docs mcp --write` owns. Everything outside stays hand
// written.
const (
	GeneratedToolsBegin = "<!-- BEGIN GENERATED MCP TOOLS -->"
	GeneratedToolsEnd   = "<!-- END GENERATED MCP TOOLS -->"
)

// ToolDocs lists the registered tools with their schemas by connecting an
// in-memory client, which is exactly what a real client sees on tools/list.
func (s *Server) ToolDocs(ctx context.Context) ([]ToolDoc, error) {
	var docs []ToolDoc
	seen := map[string]bool{}
	for _, sdk := range []*mcp.Server{s.sdk, s.wsSDK} {
		part, err := s.listToolDocs(ctx, sdk)
		if err != nil {
			return nil, err
		}
		for _, doc := range part {
			if !seen[doc.Name] {
				seen[doc.Name] = true
				docs = append(docs, doc)
			}
		}
	}
	return docs, nil
}

func (s *Server) listToolDocs(ctx context.Context, sdk *mcp.Server) ([]ToolDoc, error) {
	clientTransport, serverTransport := mcp.NewInMemoryTransports()
	serverSession, err := sdk.Connect(ctx, serverTransport, nil)
	if err != nil {
		return nil, fmt.Errorf("connect documentation server: %w", err)
	}
	defer serverSession.Close()

	client := mcp.NewClient(&mcp.Implementation{Name: "cublyverse-docs", Version: s.deps.Version}, nil)
	clientSession, err := client.Connect(ctx, clientTransport, nil)
	if err != nil {
		return nil, fmt.Errorf("connect documentation client: %w", err)
	}
	defer clientSession.Close()

	result, err := clientSession.ListTools(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("list tools: %w", err)
	}

	docs := make([]ToolDoc, 0, len(result.Tools))
	for _, tool := range result.Tools {
		title := tool.Title
		if title == "" && tool.Annotations != nil {
			title = tool.Annotations.Title
		}
		if title == "" {
			title = tool.Name
		}
		doc := ToolDoc{
			Name:        tool.Name,
			Title:       title,
			Description: tool.Description,
		}
		if tool.Annotations != nil {
			doc.ReadOnly = tool.Annotations.ReadOnlyHint
			doc.Idempotent = tool.Annotations.IdempotentHint
			// destructiveHint defaults to true in the protocol, so an
			// annotation without it means destructive.
			doc.Destructive = tool.Annotations.DestructiveHint == nil || *tool.Annotations.DestructiveHint
		}
		if doc.InputSchema, err = marshalSchema(tool.InputSchema); err != nil {
			return nil, fmt.Errorf("input schema of %s: %w", tool.Name, err)
		}
		if doc.OutputSchema, err = marshalSchema(tool.OutputSchema); err != nil {
			return nil, fmt.Errorf("output schema of %s: %w", tool.Name, err)
		}
		docs = append(docs, doc)
	}
	return docs, nil
}

func marshalSchema(value any) (json.RawMessage, error) {
	if value == nil {
		return nil, nil
	}
	encoded, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return nil, err
	}
	return encoded, nil
}

// RenderToolsMarkdown renders the generated block of docs/MCP.md.
func RenderToolsMarkdown(docs []ToolDoc) string {
	var builder strings.Builder
	builder.WriteString(GeneratedToolsBegin + "\n\n")
	builder.WriteString("| Инструмент | Название | Доступ |\n|---|---|---|\n")
	for _, doc := range docs {
		fmt.Fprintf(&builder, "| `%s` | %s | %s |\n", doc.Name, doc.Title, accessLabel(doc))
	}
	for _, doc := range docs {
		builder.WriteString("\n### `" + doc.Name + "`\n\n")
		builder.WriteString(doc.Title + "\n\n")
		builder.WriteString(doc.Description + "\n\n")
		builder.WriteString("**Доступ:** " + accessLabel(doc) + ". ")
		builder.WriteString(annotationSentence(doc) + "\n\n")
		builder.WriteString("**Входная схема**\n\n```json\n")
		builder.WriteString(schemaOrEmpty(doc.InputSchema))
		builder.WriteString("\n```\n\n**Выходная схема**\n\n```json\n")
		builder.WriteString(schemaOrEmpty(doc.OutputSchema))
		builder.WriteString("\n```\n")
	}
	builder.WriteString("\n" + GeneratedToolsEnd + "\n")
	return builder.String()
}

func accessLabel(doc ToolDoc) string {
	switch {
	case doc.ReadOnly:
		return "только чтение"
	case doc.Destructive:
		return "изменяет и удаляет"
	default:
		return "изменяет"
	}
}

func annotationSentence(doc ToolDoc) string {
	switch {
	case doc.ReadOnly:
		return "Инструмент ничего не меняет, его можно вызывать свободно."
	case doc.Destructive:
		return "Вызов может удалить данные; клиент должен подтверждать его у пользователя."
	case doc.Idempotent:
		return "Повторный вызов не создаёт нового состояния; ответ может отличаться (например, «уже подтверждено»)."
	default:
		return "Каждый вызов создаёт новое состояние."
	}
}

func schemaOrEmpty(schema json.RawMessage) string {
	if len(schema) == 0 {
		return "{}"
	}
	return string(schema)
}

// UpdateToolsBlock rewrites the generated block of path, keeping every hand
// written section intact. The file must already contain the markers.
func UpdateToolsBlock(path string, docs []ToolDoc) error {
	content, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read %s: %w", path, err)
	}
	text := string(content)
	begin := strings.Index(text, GeneratedToolsBegin)
	if begin < 0 {
		return fmt.Errorf("%s has no %s marker", path, GeneratedToolsBegin)
	}
	end := strings.Index(text[begin:], GeneratedToolsEnd)
	if end < 0 {
		return fmt.Errorf("%s has no %s marker", path, GeneratedToolsEnd)
	}
	end += begin + len(GeneratedToolsEnd)
	if !strings.HasSuffix(strings.TrimRight(text[:end], "\n"), GeneratedToolsEnd) {
		return fmt.Errorf("%s has malformed generated markers", path)
	}

	var builder strings.Builder
	builder.WriteString(text[:begin])
	builder.WriteString(RenderToolsMarkdown(docs))
	builder.WriteString(strings.TrimPrefix(text[end:], "\n"))
	if err := os.WriteFile(path, []byte(builder.String()), 0o644); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	return nil
}

// ToolNames returns the names of the documented tools in registration order.
// The documentation check compares them with the MCP.md text.
func ToolNames(docs []ToolDoc) []string {
	names := make([]string, 0, len(docs))
	for _, doc := range docs {
		names = append(names, doc.Name)
	}
	slices.Sort(names)
	return names
}
