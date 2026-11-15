package postgres

import (
	"context"
	"log/slog"
	"time"
)

// DefaultCleanupInterval is how often expired interactions are deleted.
const DefaultCleanupInterval = 5 * time.Minute

// CleanupJob deletes expired interaction rows on a ticker. It satisfies the
// job interface of internal/app structurally.
type CleanupJob struct {
	store    *Store
	interval time.Duration
	logger   *slog.Logger
}

// CleanupJob returns the background cleanup task of the store.
func (s *Store) CleanupJob(logger *slog.Logger) *CleanupJob {
	if logger == nil {
		logger = slog.Default()
	}
	return &CleanupJob{store: s, interval: DefaultCleanupInterval, logger: logger}
}

// Name identifies the job in logs.
func (j *CleanupJob) Name() string { return "interactions.cleanup" }

// Run deletes expired interactions every interval until ctx is cancelled.
func (j *CleanupJob) Run(ctx context.Context) error {
	ticker := time.NewTicker(j.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			deleted, err := j.store.Cleanup(ctx)
			if err != nil {
				j.logger.ErrorContext(ctx, "interaction cleanup failed", "error", err)
				continue
			}
			if deleted > 0 {
				j.logger.InfoContext(ctx, "interaction cleanup finished", "deleted", deleted)
			}
		}
	}
}
