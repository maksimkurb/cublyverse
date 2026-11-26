// Command cublyverse catalog fills the global craft catalogue from the
// manufacturers' own product lists. It shares the syncer with the weekly
// background job, so a manual run and a scheduled run behave identically.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"

	"github.com/maksimkurb/cublyverse/internal/features/craft/catalog"
	"github.com/maksimkurb/cublyverse/internal/store"
)

// runCatalog implements `cublyverse catalog sync <dmc|preciosa|all>` and
// `cublyverse catalog colors`.
func runCatalog(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	if len(args) > 0 && args[0] == "colors" {
		return runPhotoColors(ctx, args[1:], stdout, stderr)
	}
	if len(args) == 0 || args[0] != "sync" {
		fmt.Fprint(stderr, usage)
		return errors.New("catalog requires a command: sync or colors")
	}

	flags := flag.NewFlagSet("catalog sync", flag.ContinueOnError)
	flags.SetOutput(stderr)
	databaseURL := flags.String("database-url", "", "PostgreSQL URL; defaults to DATABASE_URL")
	if err := flags.Parse(args[1:]); err != nil {
		return err
	}
	if flags.NArg() != 1 {
		return fmt.Errorf("catalog sync takes exactly one source (dmc, preciosa or all), got %d arguments", flags.NArg())
	}

	sources, err := catalogSources(flags.Arg(0))
	if err != nil {
		return err
	}

	st, logger, err := openCatalogStore(ctx, *databaseURL, stderr)
	if err != nil {
		return err
	}
	defer st.Close()

	syncer := catalog.NewSyncer(st, logger)
	for _, source := range sources {
		result, err := syncer.Run(ctx, source)
		if err != nil {
			return err
		}
		printCatalogResult(stdout, result)
	}
	return nil
}

// openCatalogStore connects to the database and applies the migrations.
func openCatalogStore(ctx context.Context, flagURL string, stderr io.Writer) (*store.Store, *slog.Logger, error) {
	url := databaseURLOrEnv(flagURL)
	if url == "" {
		return nil, nil, errors.New("database url is required: pass --database-url or set DATABASE_URL")
	}
	logger := slog.New(slog.NewTextHandler(stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))
	if err := store.Migrate(ctx, url, logger); err != nil {
		return nil, nil, err
	}
	st, err := store.Open(ctx, url)
	if err != nil {
		return nil, nil, err
	}
	return st, logger, nil
}

// runPhotoColors reads colours from the product photos that have none yet.
func runPhotoColors(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	flags := flag.NewFlagSet("catalog colors", flag.ContinueOnError)
	flags.SetOutput(stderr)
	databaseURL := flags.String("database-url", "", "PostgreSQL URL; defaults to DATABASE_URL")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return errors.New("catalog colors takes no arguments")
	}
	st, logger, err := openCatalogStore(ctx, *databaseURL, stderr)
	if err != nil {
		return err
	}
	defer st.Close()
	result, err := catalog.NewPhotoColors(st, logger, nil).Run(ctx)
	fmt.Fprintf(stdout, "colored: %d\nunreadable: %d\nfailed: %d\n", result.Colored, result.Unreadable, result.Failed)
	return err
}

// catalogSources resolves the positional argument. "all" runs both catalogues
// in a stable order and stops at the first failure, so the failing run row is
// the one that needs attention.
func catalogSources(name string) ([]catalog.Source, error) {
	switch name {
	case "dmc":
		return []catalog.Source{catalog.NewDMC(nil)}, nil
	case "preciosa":
		return []catalog.Source{catalog.NewPreciosa(nil)}, nil
	case "all":
		return []catalog.Source{catalog.NewDMC(nil), catalog.NewPreciosa(nil)}, nil
	default:
		return nil, fmt.Errorf("unknown catalogue %q, want dmc, preciosa or all", name)
	}
}

func printCatalogResult(w io.Writer, result catalog.Result) {
	fmt.Fprintf(w, "source: %s\n", result.Source)
	fmt.Fprintf(w, "created: %d\n", result.Created)
	fmt.Fprintf(w, "updated: %d\n", result.Updated)
	fmt.Fprintf(w, "unchanged: %d\n", result.Unchanged)
	fmt.Fprintf(w, "deprecated: %d\n", result.Deprecated)
}
