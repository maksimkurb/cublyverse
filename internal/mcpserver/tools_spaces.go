package mcpserver

import (
	"context"
	"errors"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// Space is one space of a mini-app as spaces_list reports it.
type Space struct {
	ID      string        `json:"id" jsonschema:"space id (UUID); pass it as the space argument of other tools"`
	Name    string        `json:"name" jsonschema:"space name"`
	App     string        `json:"app" jsonschema:"mini-app of the space: warehouse or reminders"`
	Default bool          `json:"personal" jsonschema:"the user's own default space of this mini-app"`
	Members []SpaceMember `json:"members" jsonschema:"people who share the space"`
}

// SpaceMember is a member of a space.
type SpaceMember struct {
	UserID string `json:"user_id" jsonschema:"user id (UUID), for example a reminder recipient"`
	Name   string `json:"name" jsonschema:"display name, or e-mail when there is no name"`
}

type spacesInput struct {
	App string `json:"app,omitempty" jsonschema:"only spaces of this mini-app: warehouse or reminders"`
}

type spacesOutput struct {
	Spaces []Space `json:"spaces" jsonschema:"spaces the user is a member of"`
}

// registerSpaces adds spaces_list. It needs no scope beyond a valid token: it
// returns only what the user's own app shows.
func (s *Server) registerSpaces() {
	AddTool(s, &mcp.Tool{
		Name:  "spaces_list",
		Title: "List spaces",
		Description: "List the spaces of the user: every mini-app (warehouse, reminders) keeps its " +
			"data in spaces that can be shared with other people. Warehouse and reminder tools take " +
			"a space argument (id or name); call this to learn the valid values and the members.",
		Annotations: &mcp.ToolAnnotations{Title: "List spaces", ReadOnlyHint: true, OpenWorldHint: boolPtr(false)},
	}, s.listSpaces)
}

func (s *Server) listSpaces(ctx context.Context, _ *mcp.CallToolRequest, in spacesInput) (*mcp.CallToolResult, spacesOutput, error) {
	principal, ok := PrincipalFromContext(ctx)
	if !ok {
		return nil, spacesOutput{}, ErrUnauthenticated
	}
	out := spacesOutput{Spaces: []Space{}}
	if s.deps.Spaces == nil {
		return nil, out, nil
	}
	spaces, err := s.deps.Spaces(ctx, principal.ID)
	if err != nil {
		s.deps.Logger.ErrorContext(ctx, "list spaces for mcp", "error", err.Error())
		return nil, out, errors.New("failed to list spaces")
	}
	for _, space := range spaces {
		if in.App == "" || in.App == space.App {
			out.Spaces = append(out.Spaces, space)
		}
	}
	return nil, out, nil
}
