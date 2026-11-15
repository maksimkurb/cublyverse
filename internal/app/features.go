package app

import (
	"context"
	"log/slog"

	"github.com/maksimkurb/cublyverse/internal/bot"
	"github.com/maksimkurb/cublyverse/internal/mcpserver"
)

// Job is a background task owned by a feature. Run blocks until ctx is
// cancelled; returning nil after cancellation is the normal shutdown path.
type Job interface {
	// Name identifies the job in logs.
	Name() string
	// Run executes the job until ctx is cancelled.
	Run(ctx context.Context) error
}

// Feature is a self-contained product module. It exposes the very same service
// to the messenger bot, the HTTP API and MCP, so behaviour never diverges
// between the three entry points.
type Feature interface {
	// Name is the stable identifier of the feature, for example "reminders".
	Name() string
	// RegisterBot adds the feature commands and action prefixes to the router.
	RegisterBot(r *bot.Router)
	// RegisterMCP registers the feature tools on the MCP server.
	RegisterMCP(s *mcpserver.Server)
	// Jobs returns the background tasks the feature needs.
	Jobs() []Job
}

// Registry holds every enabled feature.
type Registry struct {
	features []Feature
}

// NewRegistry returns a registry with the given features. M0 ships no features
// yet; reminders arrive in M3.
func NewRegistry(features ...Feature) *Registry {
	return &Registry{features: features}
}

// Features returns the registered features in registration order.
func (r *Registry) Features() []Feature {
	out := make([]Feature, len(r.features))
	copy(out, r.features)
	return out
}

// Names returns the registered feature names.
func (r *Registry) Names() []string {
	out := make([]string, 0, len(r.features))
	for _, feature := range r.features {
		out = append(out, feature.Name())
	}
	return out
}

// Jobs collects the background jobs of every feature.
func (r *Registry) Jobs() []Job {
	var out []Job
	for _, feature := range r.features {
		out = append(out, feature.Jobs()...)
	}
	return out
}

// RegisterBot wires every feature into the bot router.
func (r *Registry) RegisterBot(router *bot.Router) {
	for _, feature := range r.features {
		feature.RegisterBot(router)
	}
}

// RegisterMCP wires every feature into the MCP server.
func (r *Registry) RegisterMCP(server *mcpserver.Server) {
	for _, feature := range r.features {
		feature.RegisterMCP(server)
	}
}

// Runner starts background jobs, one goroutine per job, and waits for them on
// shutdown.
type Runner struct {
	logger *slog.Logger
	jobs   []Job
	done   chan struct{}
}

// NewRunner returns a runner that logs through logger.
func NewRunner(logger *slog.Logger) *Runner {
	return &Runner{logger: logger, done: make(chan struct{})}
}

// Start launches one goroutine per job. Every job stops when ctx is cancelled.
func (r *Runner) Start(ctx context.Context, jobs []Job) {
	if r.logger == nil {
		r.logger = slog.Default()
	}
	if len(jobs) == 0 {
		close(r.done)
		return
	}

	r.jobs = jobs
	finished := make(chan struct{}, len(jobs))
	for _, job := range jobs {
		go func() {
			defer func() {
				if recovered := recover(); recovered != nil {
					r.logger.Error("background job panicked", slog.String("job", job.Name()), slog.Any("panic", recovered))
				}
				finished <- struct{}{}
			}()
			r.logger.Info("background job started", slog.String("job", job.Name()))
			if err := job.Run(ctx); err != nil {
				r.logger.Error("background job failed", slog.String("job", job.Name()), slog.String("error", err.Error()))
				return
			}
			r.logger.Info("background job stopped", slog.String("job", job.Name()))
		}()
	}

	go func() {
		for range len(jobs) {
			<-finished
		}
		close(r.done)
	}()
}

// Wait blocks until every job returned.
func (r *Runner) Wait() {
	<-r.done
}
