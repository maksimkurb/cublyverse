package craft

import (
	"context"
	"log/slog"
	"time"

	"github.com/maksimkurb/cublyverse/internal/feature"
	"github.com/maksimkurb/cublyverse/internal/features/craft/catalog"
)

// CatalogSyncInterval is how often the feature refreshes the official
// catalogues. A week keeps the data current without hammering the manufacturers.
const CatalogSyncInterval = 7 * 24 * time.Hour

// CatalogSyncJob runs every official scraper once, then every interval until
// ctx is cancelled. One failing source is logged and does not stop the others.
type CatalogSyncJob struct {
	syncer   *catalog.Syncer
	sources  []catalog.Source
	interval time.Duration
	logger   *slog.Logger
}

var _ feature.Job = (*CatalogSyncJob)(nil)

// NewCatalogSyncJob returns the job. Empty sources mean both built-in
// catalogues. A non-positive interval means CatalogSyncInterval.
func NewCatalogSyncJob(syncer *catalog.Syncer, logger *slog.Logger, sources ...catalog.Source) *CatalogSyncJob {
	if logger == nil {
		logger = slog.Default()
	}
	if len(sources) == 0 {
		sources = []catalog.Source{catalog.NewDMC(nil), catalog.NewPreciosa(nil)}
	}
	return &CatalogSyncJob{
		syncer:   syncer,
		sources:  sources,
		interval: CatalogSyncInterval,
		logger:   logger,
	}
}

// Name identifies the job in logs.
func (j *CatalogSyncJob) Name() string { return "craft.catalog-sync" }

// Run syncs immediately and then on every tick. The first sync at process
// start is intentional: an instance that just deployed sees a fresh catalogue
// instead of waiting a week.
func (j *CatalogSyncJob) Run(ctx context.Context) error {
	j.syncAll(ctx)

	ticker := time.NewTicker(j.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			j.syncAll(ctx)
		}
	}
}

func (j *CatalogSyncJob) syncAll(ctx context.Context) {
	for _, source := range j.sources {
		if ctx.Err() != nil {
			return
		}
		if _, err := j.syncer.Run(ctx, source); err != nil {
			j.logger.Error("craft catalogue sync failed",
				slog.String("source", source.Name()),
				slog.String("error", err.Error()),
			)
		}
	}
}
