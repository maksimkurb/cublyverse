package reminders

import (
	"log/slog"

	"github.com/maksimkurb/cublyverse/internal/bot"
	"github.com/maksimkurb/cublyverse/internal/feature"
	"github.com/maksimkurb/cublyverse/internal/mcpserver"
)

// Feature wires the reminders service into the application: the bot router, the
// MCP server and the background job runner.
//
// The bot commands and the scheduler job arrive with the next stories (M3-3 and
// M3-4); the registration points exist now so `internal/app` can assemble the
// feature exactly like any other.
type Feature struct {
	service *Service
	logger  *slog.Logger
}

var _ feature.Feature = (*Feature)(nil)

// NewFeature returns the feature around service.
func NewFeature(service *Service, logger *slog.Logger) *Feature {
	if logger == nil {
		logger = slog.Default()
	}
	return &Feature{service: service, logger: logger}
}

// Name is the stable feature identifier.
func (f *Feature) Name() string { return "reminders" }

// Service exposes the shared service to adapters outside the package.
func (f *Feature) Service() *Service { return f.service }

// RegisterBot adds the reminder commands and action prefixes to the router.
// The commands themselves arrive in M3-4.
func (f *Feature) RegisterBot(_ *bot.Router) {}

// RegisterMCP registers the reminder tools. They arrive in M5-2.
func (f *Feature) RegisterMCP(_ *mcpserver.Server) {}

// Jobs returns the background tasks of the feature. The scheduler arrives in
// M3-3.
func (f *Feature) Jobs() []feature.Job { return nil }
