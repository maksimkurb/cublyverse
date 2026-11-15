// Package feature holds the contracts a product module implements.
//
// The contracts live outside internal/app because the feature modules
// themselves implement them: a feature that imported internal/app would create
// an import cycle, since internal/app imports the features it assembles.
// internal/app re-exports both interfaces, so `app.Feature` stays the name used
// in the architecture document.
package feature

import (
	"context"

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
