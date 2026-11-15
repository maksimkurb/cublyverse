package auth

import (
	"context"
	"time"
)

// CleanupJob deletes expired sign-in flows and dead sessions on a ticker.
// It satisfies the job interface of internal/app structurally, so this package
// does not have to import the application assembly.
type CleanupJob struct {
	service  *Service
	interval time.Duration
}

// CleanupJob returns the background cleanup task of the service.
func (s *Service) CleanupJob() *CleanupJob {
	return &CleanupJob{service: s, interval: s.cfg.CleanupInterval}
}

// Name identifies the job in logs.
func (j *CleanupJob) Name() string { return "auth.cleanup" }

// Run deletes expired rows every interval until ctx is cancelled. A failed
// sweep is logged and retried on the next tick; it never stops the job.
func (j *CleanupJob) Run(ctx context.Context) error {
	ticker := time.NewTicker(j.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			if err := j.service.Cleanup(ctx); err != nil {
				j.service.logger.ErrorContext(ctx, "auth cleanup failed", "error", err)
			}
		}
	}
}
