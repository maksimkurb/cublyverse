package craft

import (
	"github.com/maksimkurb/cublyverse/internal/bot"
	"github.com/maksimkurb/cublyverse/internal/feature"
	"github.com/maksimkurb/cublyverse/internal/mcpserver"
)

// Feature wires the craft service into the application. It has no bot commands
// and no background jobs yet; catalogue scrapers will bring jobs.
type Feature struct {
	service *Service
}

var _ feature.Feature = (*Feature)(nil)

// NewFeature returns the feature around service.
func NewFeature(service *Service) *Feature { return &Feature{service: service} }

// Name is the stable feature identifier.
func (f *Feature) Name() string { return "craft" }

// Service exposes the shared service to adapters outside the package.
func (f *Feature) Service() *Service { return f.service }

// RegisterBot registers nothing: the feature has no chat commands.
func (f *Feature) RegisterBot(*bot.Router) {}

// RegisterMCP adds the craft tools to the per-workspace MCP endpoint.
func (f *Feature) RegisterMCP(server *mcpserver.Server) { f.registerMCP(server) }

// Jobs returns no tasks.
func (f *Feature) Jobs() []feature.Job { return nil }
