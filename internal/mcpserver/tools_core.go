package mcpserver

import (
	"context"
	"errors"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// whoAmIInput has no arguments: the identity comes from the token.
type whoAmIInput struct{}

// whoAmIOutput describes the caller of whoami.
type whoAmIOutput struct {
	// PrincipalID is the stable user id shared by the bot, the API and MCP.
	PrincipalID string `json:"principal_id" jsonschema:"stable user id (UUID) that owns every object this token can reach"`
	// Email is the e-mail from the SSO provider, when the profile has one.
	Email string `json:"email,omitempty" jsonschema:"e-mail from the SSO profile, empty when the provider does not send one"`
	// DisplayName is the display name from the SSO provider.
	DisplayName string `json:"display_name,omitempty" jsonschema:"display name from the SSO profile, empty when unavailable"`
	// Timezone is the user's IANA time zone, the default for reminders.
	Timezone string `json:"time_zone" jsonschema:"IANA time zone of the user, the default for new reminders"`
	// Scopes are the scopes of the token that issued this call.
	Scopes []string `json:"scopes" jsonschema:"scopes granted to the token used for this call"`
}

// registerCore adds the tools that belong to the server itself rather than to a
// feature.
func (s *Server) registerCore() {
	tool := &mcp.Tool{
		Name:  "whoami",
		Title: "Who am I",
		Description: "Return the Cublyverse user behind the personal access token: " +
			"principal id, profile fields, time zone and the scopes of the token. " +
			"Use it to learn the default time zone before creating a reminder and " +
			"to check what the token is allowed to do.",
		Annotations: &mcp.ToolAnnotations{
			Title:         "Who am I",
			ReadOnlyHint:  true,
			OpenWorldHint: boolPtr(false),
		},
	}
	AddTool(s, tool, s.whoAmI)
	s.registerSpaces()
}

func (s *Server) whoAmI(ctx context.Context, _ *mcp.CallToolRequest, _ whoAmIInput) (*mcp.CallToolResult, whoAmIOutput, error) {
	principal, ok := PrincipalFromContext(ctx)
	if !ok {
		return nil, whoAmIOutput{}, ErrUnauthenticated
	}
	out := whoAmIOutput{
		PrincipalID: principal.ID,
		Scopes:      append([]string(nil), principal.Scopes...),
	}
	if s.deps.Profile == nil {
		return nil, out, nil
	}
	profile, err := s.deps.Profile(ctx, principal.ID)
	if err != nil {
		return nil, whoAmIOutput{}, errors.New("failed to load the user profile: " + err.Error())
	}
	out.Email = stringOrEmpty(profile.User.Email)
	out.DisplayName = stringOrEmpty(profile.User.DisplayName)
	out.Timezone = profile.User.Timezone
	return nil, out, nil
}

func stringOrEmpty(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func boolPtr(value bool) *bool { return &value }
