// Package store owns the PostgreSQL connection pool and the generated SQL
// queries.
package store

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/maksimkurb/cublyverse/internal/store/db"
)

// pingTimeout bounds the connectivity check performed by Open and Ping.
const pingTimeout = 5 * time.Second

// Store is the PostgreSQL data store.
type Store struct {
	pool    *pgxpool.Pool
	queries *db.Queries
}

// Open creates a connection pool and verifies that the database is reachable.
// A successful Open means the service can talk to PostgreSQL right now.
func Open(ctx context.Context, databaseURL string) (*Store, error) {
	cfg, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		return nil, fmt.Errorf("parse database url: %w", err)
	}

	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("create connection pool: %w", err)
	}

	s := &Store{pool: pool, queries: db.New(pool)}
	pingCtx, cancel := context.WithTimeout(ctx, pingTimeout)
	defer cancel()
	if err := pool.Ping(pingCtx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ping database: %w", err)
	}
	return s, nil
}

// Queries returns the generated query set. Every call is safe for concurrent
// use.
func (s *Store) Queries() *db.Queries { return s.queries }

// Pool exposes the underlying pool for code that needs transactions.
func (s *Store) Pool() *pgxpool.Pool { return s.pool }

// Ping verifies that the database still answers. It backs the readiness probe.
func (s *Store) Ping(ctx context.Context) error {
	pingCtx, cancel := context.WithTimeout(ctx, pingTimeout)
	defer cancel()
	if err := s.pool.Ping(pingCtx); err != nil {
		return fmt.Errorf("ping database: %w", err)
	}
	return nil
}

// Close releases all pooled connections.
func (s *Store) Close() {
	if s.pool != nil {
		s.pool.Close()
	}
}
