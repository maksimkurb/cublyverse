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
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"text/tabwriter"
	"time"

	"github.com/maksimkurb/cublyverse/internal/app"
	"github.com/maksimkurb/cublyverse/internal/config"
	"github.com/maksimkurb/cublyverse/internal/features/reminders"
	"github.com/maksimkurb/cublyverse/internal/mcpserver"
	"github.com/maksimkurb/cublyverse/internal/store"
	"github.com/maksimkurb/cublyverse/internal/version"
)

const usage = `usage: cublyverse <command> [arguments]

commands:
  serve            start the HTTP server
  migrate [up|status]
                   apply pending database migrations (default: up)
                   or print their state
  docs mcp [--write] [--names]
                   print the MCP tool reference, rewrite docs/MCP.md between
                   the generated markers, or list the registered tool names

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
	case "docs":
		return runDocs(ctx, args[1:], stdout, stderr)
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

// runDocs renders documentation that is derived from code. `docs mcp` exists so
// docs/MCP.md cannot drift from the registered tools: make gen rewrites the
// generated block, and make check fails when it differs.
func runDocs(_ context.Context, args []string, stdout, stderr io.Writer) error {
	if len(args) == 0 {
		fmt.Fprint(stderr, usage)
		return errors.New("docs requires a topic: mcp")
	}
	if args[0] != "mcp" {
		return fmt.Errorf("unknown docs topic %q, want mcp", args[0])
	}

	flags := flag.NewFlagSet("docs mcp", flag.ContinueOnError)
	flags.SetOutput(stderr)
	write := flags.Bool("write", false, "rewrite the generated block of the documentation file")
	names := flags.Bool("names", false, "print the registered tool names, one per line")
	file := flags.String("file", "docs/MCP.md", "documentation file updated by --write")
	if err := flags.Parse(args[1:]); err != nil {
		return err
	}
	if flags.NArg() > 0 {
		return fmt.Errorf("docs mcp takes no arguments, got %q", flags.Arg(0))
	}

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	// The documentation server has no service dependencies: listing tools only
	// needs the registrations, never a database or a messenger.
	server := mcpserver.New(mcpserver.Deps{Version: version.String(), Logger: logger})
	reminders.NewFeature(nil, logger).RegisterMCP(server)

	ctx := context.Background()
	docs, err := server.ToolDocs(ctx)
	if err != nil {
		return err
	}
	switch {
	case *names:
		for _, name := range mcpserver.ToolNames(docs) {
			fmt.Fprintln(stdout, name)
		}
		return nil
	case *write:
		return mcpserver.UpdateToolsBlock(*file, docs)
	default:
		fmt.Fprint(stdout, mcpserver.RenderToolsMarkdown(docs))
		return nil
	}
}
