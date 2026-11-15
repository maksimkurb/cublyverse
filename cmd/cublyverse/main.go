// Command cublyverse is the Cublyverse server.
//
// Subcommands:
//
//	serve     start the HTTP server (and, later, the messenger transports)
//	migrate   apply or inspect the embedded database migrations
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"
	"text/tabwriter"
	"time"

	"github.com/maksimkurb/cublyverse/internal/app"
	"github.com/maksimkurb/cublyverse/internal/config"
	"github.com/maksimkurb/cublyverse/internal/store"
)

const usage = `usage: cublyverse <command> [arguments]

commands:
  serve            start the HTTP server
  migrate [up|status]
                   apply pending database migrations (default: up)
                   or print their state

Run "cublyverse <command> -h" for the flags of a command.
`

func main() {
	if err := run(context.Background(), os.Args[1:], os.Stdout, os.Stderr); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return
		}
		fmt.Fprintln(os.Stderr, "cublyverse:", err)
		os.Exit(1)
	}
}

// run dispatches a command line. It returns an error instead of exiting so the
// behaviour is testable.
func run(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	if len(args) == 0 {
		fmt.Fprint(stderr, usage)
		return errors.New("no command given")
	}

	switch args[0] {
	case "serve":
		return runServe(ctx, args[1:], stdout, stderr)
	case "migrate":
		return runMigrate(ctx, args[1:], stdout, stderr)
	case "help", "-h", "--help":
		fmt.Fprint(stdout, usage)
		return nil
	default:
		fmt.Fprint(stderr, usage)
		return fmt.Errorf("unknown command %q", args[0])
	}
}

func runServe(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	flags := flag.NewFlagSet("serve", flag.ContinueOnError)
	flags.SetOutput(stderr)
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() > 0 {
		return fmt.Errorf("serve takes no arguments, got %q", flags.Arg(0))
	}

	cfg, err := config.Load()
	if err != nil {
		return err
	}
	logger := cfg.NewLogger(stdout)

	ctx, stop := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer stop()

	application, err := app.New(ctx, cfg, logger)
	if err != nil {
		return err
	}
	defer application.Close()

	return application.Run(ctx)
}

func runMigrate(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	flags := flag.NewFlagSet("migrate", flag.ContinueOnError)
	flags.SetOutput(stderr)
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() > 1 {
		return fmt.Errorf("migrate takes at most one argument (up or status), got %q", flags.Args())
	}

	command := "up"
	if flags.NArg() == 1 {
		command = flags.Arg(0)
	}

	cfg, err := config.Load()
	if err != nil {
		return err
	}
	logger := cfg.NewLogger(stdout)

	switch command {
	case "up":
		if err := store.Migrate(ctx, cfg.DatabaseURL, logger); err != nil {
			return err
		}
		fmt.Fprintln(stdout, "migrations are up to date")
		return nil
	case "status":
		statuses, err := store.MigrationStatus(ctx, cfg.DatabaseURL, logger)
		if err != nil {
			return err
		}
		writer := tabwriter.NewWriter(stdout, 0, 0, 2, ' ', 0)
		fmt.Fprintln(writer, "VERSION\tSTATE\tAPPLIED AT\tPATH")
		for _, status := range statuses {
			applied := "-"
			if !status.AppliedAt.IsZero() {
				applied = status.AppliedAt.UTC().Format(time.RFC3339)
			}
			fmt.Fprintf(writer, "%d\t%s\t%s\t%s\n", status.Version, status.State, applied, status.Path)
		}
		return writer.Flush()
	default:
		return fmt.Errorf("unknown migrate command %q, want up or status", command)
	}
}
