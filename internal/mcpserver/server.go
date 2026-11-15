// Package mcpserver hosts the MCP tools of the features over Streamable HTTP.
//
// The transport and the tool registration are implemented in M5; the type is
// declared now so features can target a stable interface.
package mcpserver

// Server is the MCP server. Features register their tools on it.
type Server struct{}

// New returns an empty server.
func New() *Server { return &Server{} }
