package oauthserver

import (
	"context"
	"fmt"
	"time"
)

// cleanupInterval is how often expired codes, tokens and unused dynamic clients
// are swept.
const cleanupInterval = time.Hour

// Cleanup deletes expired codes and tokens and abandoned dynamic clients.
func (s *Service) Cleanup(ctx context.Context) error {
	q := s.st.Queries()
	if err := q.DeleteExpiredOAuth(ctx); err != nil {
		return fmt.Errorf("delete expired oauth rows: %w", err)
	}
	if _, err := q.PruneOAuthClients(ctx); err != nil {
		return fmt.Errorf("prune oauth clients: %w", err)
	}
	return nil
}

// CleanupJob is the background sweep. It satisfies the job interface of
// internal/app structurally.
type CleanupJob struct{ service *Service }

// CleanupJob returns the background task of the service.
func (s *Service) CleanupJob() *CleanupJob { return &CleanupJob{service: s} }

// Name identifies the job in logs.
func (j *CleanupJob) Name() string { return "oauth.cleanup" }

// Run sweeps every interval until ctx is cancelled; a failed sweep is logged
// and retried on the next tick.
func (j *CleanupJob) Run(ctx context.Context) error {
	ticker := time.NewTicker(cleanupInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			if err := j.service.Cleanup(ctx); err != nil {
				j.service.cfg.Logger.ErrorContext(ctx, "oauth cleanup failed", "error", err)
			}
		}
	}
}
