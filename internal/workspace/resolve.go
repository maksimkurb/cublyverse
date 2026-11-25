package workspace

import (
	"context"
	"fmt"
	"strings"
)

// Resolve finds the space of a mini-app a client named by id or by name
// (case-insensitive). An empty reference works when the choice is obvious: the
// user has exactly one space of the mini-app. Otherwise the answer is a
// ValidationError that lists the candidates, so an MCP client can correct
// itself.
func (s *Service) Resolve(ctx context.Context, userID, app, ref string) (Workspace, error) {
	spaces, err := s.List(ctx, userID, app)
	if err != nil {
		return Workspace{}, err
	}
	ref = strings.TrimSpace(ref)
	var named []Workspace
	for _, space := range spaces {
		if ref != "" && strings.EqualFold(space.ID, ref) {
			return space, nil
		}
		if ref != "" && strings.EqualFold(space.Name, ref) {
			named = append(named, space)
		}
	}
	switch {
	case ref == "" && len(spaces) == 1:
		return spaces[0], nil
	case len(named) == 1:
		return named[0], nil
	case len(spaces) == 0:
		return Workspace{}, &ValidationError{Message: "Нет ни одного пространства: создайте его в приложении."}
	}
	choices := make([]string, 0, len(spaces))
	for _, space := range spaces {
		choices = append(choices, fmt.Sprintf("«%s» (%s)", space.Name, space.ID))
	}
	return Workspace{}, &ValidationError{Message: "Укажите space — id или название пространства. Доступные: " + strings.Join(choices, ", ") + "."}
}
