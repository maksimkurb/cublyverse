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

// catalogCheckInterval is how often the job asks whether a source is due.
const catalogCheckInterval = time.Hour

// CatalogSyncJob keeps every official catalogue at most interval old until ctx
// is cancelled. One failing source is logged and does not stop the others.
type CatalogSyncJob struct {
	syncer   *catalog.Syncer
	sources  []catalog.Source
	interval time.Duration
	// shop and photos, when set, add approximate colours after each check: the
	// shop's table weekly, new product photos at once.
	shop   *catalog.BiserShop
	photos *catalog.PhotoColors
	logger *slog.Logger
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

// Run checks at start and then hourly. A fresh instance syncs at once; a
// restart of an instance with a recent catalogue does not crawl again.
func (j *CatalogSyncJob) Run(ctx context.Context) error {
	j.syncAll(ctx)

	ticker := time.NewTicker(min(j.interval, catalogCheckInterval))
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
		due, err := j.syncer.Due(ctx, source.Name(), j.interval, time.Now())
		if err != nil {
			j.logger.Error("craft catalogue sync check failed", slog.String("source", source.Name()), slog.String("error", err.Error()))
			continue
		}
		if !due {
			continue
		}
		if _, err := j.syncer.Run(ctx, source); err != nil {
			j.logger.Error("craft catalogue sync failed",
				slog.String("source", source.Name()),
				slog.String("error", err.Error()),
			)
		}
	}
	j.syncShopColors(ctx)
	if j.photos == nil || ctx.Err() != nil {
		return
	}
	if _, err := j.photos.Run(ctx); err != nil && ctx.Err() == nil {
		j.logger.Error("craft photo colours failed", slog.String("error", err.Error()))
	}
}

func (j *CatalogSyncJob) syncShopColors(ctx context.Context) {
	if j.shop == nil || ctx.Err() != nil {
		return
	}
	due, err := j.syncer.Due(ctx, catalog.BiserShopSource, j.interval, time.Now())
	if err == nil && due {
		_, err = j.syncer.RunShopColors(ctx, j.shop)
	}
	if err != nil && ctx.Err() == nil {
		j.logger.Error("craft shop colours failed", slog.String("error", err.Error()))
	}
}
