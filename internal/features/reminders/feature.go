package reminders

import (
	"context"
	"log/slog"
	"time"

	"github.com/maksimkurb/cublyverse/internal/bot"
	"github.com/maksimkurb/cublyverse/internal/feature"
	"github.com/maksimkurb/cublyverse/internal/mcpserver"
)

// Feature wires the reminders service into the application: the bot router, the
// MCP server and the background job runner.
type Feature struct {
	service      *Service
	scheduler    *Scheduler
	tickInterval time.Duration
	logger       *slog.Logger
	// timezone returns the profile time zone of a user; it is the default for a
	// reminder created through MCP. Nil falls back to UTC.
	timezone func(ctx context.Context, principalID string) (string, error)
}

var _ feature.Feature = (*Feature)(nil)

// FeatureOption customises the feature.
type FeatureOption func(*Feature)

// WithTickInterval sets how often the scheduler looks for due reminders.
func WithTickInterval(interval time.Duration) FeatureOption {
	return func(f *Feature) { f.tickInterval = interval }
}

// WithTimezoneLookup supplies the user's profile time zone. The MCP create tool
// uses it when the caller does not name a time zone, so an LLM does not have to
// ask for one in the common case.
func WithTimezoneLookup(lookup func(ctx context.Context, principalID string) (string, error)) FeatureOption {
	return func(f *Feature) { f.timezone = lookup }
}

// NewFeature returns the feature around service.
func NewFeature(service *Service, logger *slog.Logger, opts ...FeatureOption) *Feature {
	if logger == nil {
		logger = slog.Default()
	}
	f := &Feature{service: service, logger: logger}
	for _, opt := range opts {
		opt(f)
	}
	f.scheduler = NewScheduler(service, SchedulerConfig{
		Interval: f.tickInterval,
		Logger:   logger,
	})
	return f
}

// Name is the stable feature identifier.
func (f *Feature) Name() string { return "reminders" }

// Service exposes the shared service to adapters outside the package.
func (f *Feature) Service() *Service { return f.service }

// RegisterBot adds the reminder commands and action prefixes to the router.
func (f *Feature) RegisterBot(router *bot.Router) { f.registerBot(router) }

// RegisterMCP adds the reminder tools to the MCP server. They call the very same
// service as the HTTP API and the bot.
func (f *Feature) RegisterMCP(server *mcpserver.Server) { f.registerMCP(server) }

// Jobs returns the background tasks of the feature: the scheduler that fires
// due reminders.
func (f *Feature) Jobs() []feature.Job { return []feature.Job{f.scheduler} }
