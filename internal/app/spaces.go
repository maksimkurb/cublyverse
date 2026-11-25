package app

import (
	"context"

	"github.com/maksimkurb/cublyverse/internal/mcpserver"
	"github.com/maksimkurb/cublyverse/internal/workspace"
)

// spacesOf adapts the workspace service to the spaces_list MCP tool.
func spacesOf(workspaces *workspace.Service) func(context.Context, string) ([]mcpserver.Space, error) {
	return func(ctx context.Context, userID string) ([]mcpserver.Space, error) {
		list, err := workspaces.List(ctx, userID, "")
		if err != nil {
			return nil, err
		}
		out := make([]mcpserver.Space, 0, len(list))
		for _, space := range list {
			members, err := workspaces.Members(ctx, userID, space.ID)
			if err != nil {
				return nil, err
			}
			item := mcpserver.Space{ID: space.ID, Name: space.Name, App: space.App, Default: space.Personal}
			for _, member := range members {
				name := member.DisplayName
				if name == "" {
					name = member.Email
				}
				item.Members = append(item.Members, mcpserver.SpaceMember{UserID: member.UserID, Name: name})
			}
			out = append(out, item)
		}
		return out, nil
	}
}
