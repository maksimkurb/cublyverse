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

// runCatalog implements `cublyverse catalog sync <dmc|preciosa|all>`.
func runCatalog(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	if len(args) == 0 || args[0] != "sync" {
		fmt.Fprint(stderr, usage)
		return errors.New("catalog requires a command: sync")
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

	url := databaseURLOrEnv(*databaseURL)
	if url == "" {
		return errors.New("database url is required: pass --database-url or set DATABASE_URL")
	}

	logger := slog.New(slog.NewTextHandler(stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))
	if err := store.Migrate(ctx, url, logger); err != nil {
		return err
	}
	st, err := store.Open(ctx, url)
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
