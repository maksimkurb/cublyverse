package craft

import (
	"log/slog"

	"github.com/maksimkurb/cublyverse/internal/bot"
	"github.com/maksimkurb/cublyverse/internal/feature"
	"github.com/maksimkurb/cublyverse/internal/features/craft/catalog"
	"github.com/maksimkurb/cublyverse/internal/mcpserver"
)

// Feature wires the craft service into the application. It has no bot
// commands; the optional background job refreshes the official catalogue.
type Feature struct {
	service    *Service
	catalogJob feature.Job
}

var _ feature.Feature = (*Feature)(nil)

// Option configures optional parts of the feature.
type Option func(*Feature)

// WithCatalogSync enables the weekly sync of the official catalogues and the
// reading of colours from new product photos. It is wired only when CRAFT_CATALOG_SYNC=1, so a local instance never scrapes by
// accident.
func WithCatalogSync(syncer *catalog.Syncer, logger *slog.Logger) Option {
	return func(f *Feature) {
		job := NewCatalogSyncJob(syncer, logger)
		job.photos = catalog.NewPhotoColors(syncer.Store(), logger, nil)
		f.catalogJob = job
	}
}

// NewFeature returns the feature around service.
func NewFeature(service *Service, opts ...Option) *Feature {
	f := &Feature{service: service}
	for _, opt := range opts {
		opt(f)
	}
	return f
}

// Name is the stable feature identifier.
func (f *Feature) Name() string { return "craft" }

// Service exposes the shared service to adapters outside the package.
func (f *Feature) Service() *Service { return f.service }

// RegisterBot registers nothing: the feature has no chat commands.
func (f *Feature) RegisterBot(*bot.Router) {}

// RegisterMCP adds the warehouse tools to the MCP server.
func (f *Feature) RegisterMCP(server *mcpserver.Server) { f.registerMCP(server) }

// Jobs returns the catalogue sync job when it is enabled.
func (f *Feature) Jobs() []feature.Job {
	if f.catalogJob == nil {
		return nil
	}
	return []feature.Job{f.catalogJob}
}
