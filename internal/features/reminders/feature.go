package reminders

import (
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
}

var _ feature.Feature = (*Feature)(nil)

// FeatureOption customises the feature.
type FeatureOption func(*Feature)

// WithTickInterval sets how often the scheduler looks for due reminders.
func WithTickInterval(interval time.Duration) FeatureOption {
	return func(f *Feature) { f.tickInterval = interval }
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

// RegisterMCP registers the reminder tools. They arrive in M5-2.
func (f *Feature) RegisterMCP(_ *mcpserver.Server) {}

// Jobs returns the background tasks of the feature: the scheduler that fires
// due reminders.
func (f *Feature) Jobs() []feature.Job { return []feature.Job{f.scheduler} }
