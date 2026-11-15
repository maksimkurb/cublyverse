package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"

	"github.com/maksimkurb/cublyverse/internal/features/reminders"
	"github.com/maksimkurb/cublyverse/internal/store"
)

// runImportN8N implements `cublyverse import-n8n <file>`: one-off import of the
// reminders table of the old n8n workflow into the waiting room of the new
// service.
//
// The command is deliberately read-only with respect to the old system: it reads
// an export file, validates every row with the reminder rules, stores the valid
// rows and attaches them where a Telegram binding already exists. Imported
// reminders start fresh (next_due_at from now); nothing of the in-flight state
// of the old workflow is carried over.
func runImportN8N(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	flags := flag.NewFlagSet("import-n8n", flag.ContinueOnError)
	flags.SetOutput(stderr)
	format := flags.String("format", string(reminders.LegacyFormatAuto),
		"export format: csv, json or auto")
	dryRun := flags.Bool("dry-run", false, "parse and validate without writing anything")
	databaseURL := flags.String("database-url", "", "PostgreSQL URL; defaults to DATABASE_URL")

	// Flags may come before or after the file: the standard library parser stops
	// at the first argument, so it is restarted for each remaining one.
	var positional []string
	for rest := args; ; {
		if err := flags.Parse(rest); err != nil {
			return err
		}
		rest = flags.Args()
		if len(rest) == 0 {
			break
		}
		positional = append(positional, rest[0])
		rest = rest[1:]
	}
	if len(positional) != 1 {
		return fmt.Errorf("import-n8n takes exactly one file, got %d arguments", len(positional))
	}
	path := positional[0]

	exportFormat := reminders.LegacyFormat(strings.ToLower(strings.TrimSpace(*format)))
	switch exportFormat {
	case reminders.LegacyFormatAuto, reminders.LegacyFormatCSV, reminders.LegacyFormatJSON:
	default:
		return fmt.Errorf("unknown --format %q, want csv, json or auto", *format)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read the export: %w", err)
	}
	export, err := reminders.ParseLegacyExport(data, exportFormat)
	if err != nil {
		return err
	}

	// A dry run writes nothing. When a database is reachable it also reports how
	// many rows are already imported; without one the validation report alone is
	// still useful.
	if *dryRun {
		var result reminders.LegacyImportResult
		if url := databaseURLOrEnv(*databaseURL); url != "" {
			result, err = importLegacy(ctx, url, export, reminders.LegacyImportOptions{DryRun: true}, stderr)
			if err != nil {
				return err
			}
		} else {
			result = reminders.LegacyImportResult{
				Format:  export.Format,
				Total:   export.Total,
				Deleted: export.Deleted,
				Invalid: export.Invalid,
			}
			fmt.Fprintln(stderr, "warning: no --database-url and no DATABASE_URL, duplicates are not checked")
		}
		printLegacySummary(stdout, path, result, true)
		return nil
	}

	url := databaseURLOrEnv(*databaseURL)
	if url == "" {
		return errors.New("database url is required: pass --database-url or set DATABASE_URL")
	}
	result, err := importLegacy(ctx, url, export, reminders.LegacyImportOptions{}, stderr)
	if err != nil {
		return err
	}
	printLegacySummary(stdout, path, result, false)
	return nil
}

// importLegacy opens the database and imports. A real run applies the migrations
// first (the command can be the very first thing that runs against a fresh
// database); a dry run never writes, not even the schema.
func importLegacy(ctx context.Context, databaseURL string, export reminders.LegacyExport, opts reminders.LegacyImportOptions, stderr io.Writer) (reminders.LegacyImportResult, error) {
	logger := slog.New(slog.NewTextHandler(stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))
	if !opts.DryRun {
		if err := store.Migrate(ctx, databaseURL, logger); err != nil {
			return reminders.LegacyImportResult{}, err
		}
	}
	st, err := store.Open(ctx, databaseURL)
	if err != nil {
		return reminders.LegacyImportResult{}, err
	}
	defer st.Close()

	service := reminders.New(st, reminders.WithLogger(logger))
	return service.ImportLegacy(ctx, export, opts)
}

// databaseURLOrEnv resolves the database URL: the flag wins over the
// environment, so a one-off import cannot silently hit the wrong database.
func databaseURLOrEnv(flagValue string) string {
	if value := strings.TrimSpace(flagValue); value != "" {
		return value
	}
	return strings.TrimSpace(os.Getenv("DATABASE_URL"))
}

// printLegacySummary writes the report of an import run. The numbers are
// additive: imported + duplicates + deleted + invalid = total rows.
func printLegacySummary(w io.Writer, path string, result reminders.LegacyImportResult, dryRun bool) {
	fmt.Fprintf(w, "file: %s (%s)\n", path, result.Format)
	fmt.Fprintf(w, "rows: %d\n", result.Total)
	fmt.Fprintf(w, "imported: %d\n", result.Imported)
	fmt.Fprintf(w, "already imported: %d\n", result.Duplicates)
	fmt.Fprintf(w, "deleted in the old bot: %d\n", result.Deleted)
	fmt.Fprintf(w, "invalid: %d\n", len(result.Invalid))
	for _, issue := range result.Invalid {
		fmt.Fprintf(w, "  row %d: %s\n", issue.Row, issue.Reason)
	}
	fmt.Fprintf(w, "attached to already bound users: %d\n", result.Attached)
	fmt.Fprintf(w, "waiting for a sign-in: %d\n", result.Pending)
	if dryRun {
		fmt.Fprintln(w, "dry run: nothing was written")
	}
}
