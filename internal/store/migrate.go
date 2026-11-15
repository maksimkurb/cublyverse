package store

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"io/fs"
	"log/slog"
	"time"

	"github.com/pressly/goose/v3"

	// Registers the "pgx" driver with database/sql for goose.
	_ "github.com/jackc/pgx/v5/stdlib"
)

// migrationsFS holds the goose migrations embedded in the binary, so a
// deployment never needs the SQL files on disk.
//
//go:embed migrations/*.sql
var migrationsFS embed.FS

// migrationsDir is the directory inside migrationsFS that goose reads.
const migrationsDir = "migrations"

// Migration describes a single embedded migration and whether it has been
// applied to the database.
type Migration struct {
	Version   int64
	Path      string
	State     string
	AppliedAt time.Time
}

// Migrate applies every pending embedded migration. It is safe to call on
// every start: goose only runs migrations that are not applied yet.
func Migrate(ctx context.Context, databaseURL string, logger *slog.Logger) error {
	db, err := openMigrationDB(ctx, databaseURL)
	if err != nil {
		return err
	}
	defer db.Close()

	provider, err := newMigrationProvider(db, logger)
	if err != nil {
		return err
	}
	defer provider.Close()

	results, err := provider.Up(ctx)
	if err != nil {
		return fmt.Errorf("apply migrations: %w", err)
	}
	logger.Info("database migrations applied", slog.Int("applied", len(results)))
	return nil
}

// MigrationStatus reports the state of every embedded migration without
// changing the database.
func MigrationStatus(ctx context.Context, databaseURL string, logger *slog.Logger) ([]Migration, error) {
	db, err := openMigrationDB(ctx, databaseURL)
	if err != nil {
		return nil, err
	}
	defer db.Close()

	provider, err := newMigrationProvider(db, logger)
	if err != nil {
		return nil, err
	}
	defer provider.Close()

	statuses, err := provider.Status(ctx)
	if err != nil {
		return nil, fmt.Errorf("read migration status: %w", err)
	}

	out := make([]Migration, 0, len(statuses))
	for _, status := range statuses {
		out = append(out, Migration{
			Version:   status.Source.Version,
			Path:      status.Source.Path,
			State:     string(status.State),
			AppliedAt: status.AppliedAt,
		})
	}
	return out, nil
}

func newMigrationProvider(db *sql.DB, logger *slog.Logger) (*goose.Provider, error) {
	// goose expects the migrations at the root of the filesystem it is given.
	root, err := fs.Sub(migrationsFS, migrationsDir)
	if err != nil {
		return nil, fmt.Errorf("open embedded migrations: %w", err)
	}

	provider, err := goose.NewProvider(goose.DialectPostgres, db, root,
		goose.WithSlog(logger),
		goose.WithVerbose(false),
	)
	if err != nil {
		return nil, fmt.Errorf("create migration provider: %w", err)
	}
	return provider, nil
}

func openMigrationDB(ctx context.Context, databaseURL string) (*sql.DB, error) {
	db, err := sql.Open("pgx", databaseURL)
	if err != nil {
		return nil, fmt.Errorf("open database for migrations: %w", err)
	}
	if err := db.PingContext(ctx); err != nil {
		db.Close()
		return nil, fmt.Errorf("ping database for migrations: %w", err)
	}
	return db, nil
}
