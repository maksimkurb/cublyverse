package app_test

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/maksimkurb/cublyverse/internal/app"
	"github.com/maksimkurb/cublyverse/internal/bot"
	"github.com/maksimkurb/cublyverse/internal/mcpserver"
)

type fakeJob struct {
	name    string
	started chan struct{}
	stopped chan struct{}
}

func newFakeJob(name string) *fakeJob {
	return &fakeJob{name: name, started: make(chan struct{}), stopped: make(chan struct{})}
}

func (j *fakeJob) Name() string { return j.name }

func (j *fakeJob) Run(ctx context.Context) error {
	close(j.started)
	<-ctx.Done()
	close(j.stopped)
	return nil
}

type fakeFeature struct {
	name      string
	jobs      []app.Job
	botRouter *bot.Router
	mcpServer *mcpserver.Server
}

func (f *fakeFeature) Name() string { return f.name }

func (f *fakeFeature) RegisterBot(router *bot.Router) { f.botRouter = router }

func (f *fakeFeature) RegisterMCP(server *mcpserver.Server) { f.mcpServer = server }

func (f *fakeFeature) Jobs() []app.Job { return f.jobs }

func TestRegistryWiresFeatures(t *testing.T) {
	first := &fakeFeature{name: "reminders", jobs: []app.Job{newFakeJob("reminders.scheduler")}}
	second := &fakeFeature{name: "notes"}
	registry := app.NewRegistry(first, second)

	names := registry.Names()
	if len(names) != 2 || names[0] != "reminders" || names[1] != "notes" {
		t.Fatalf("Names() = %v, want [reminders notes]", names)
	}
	if jobs := registry.Jobs(); len(jobs) != 1 || jobs[0].Name() != "reminders.scheduler" {
		t.Fatalf("Jobs() = %v, want the reminders scheduler", jobs)
	}

	router := bot.NewRouter()
	server := mcpserver.New()
	registry.RegisterBot(router)
	registry.RegisterMCP(server)

	if first.botRouter != router || second.botRouter != router {
		t.Error("RegisterBot did not reach every feature")
	}
	if first.mcpServer != server || second.mcpServer != server {
		t.Error("RegisterMCP did not reach every feature")
	}
}

func TestRunnerStopsEveryJobOnCancel(t *testing.T) {
	jobs := []*fakeJob{newFakeJob("first"), newFakeJob("second")}
	runner := app.NewRunner(slog.New(slog.NewTextHandler(io.Discard, nil)))

	ctx, cancel := context.WithCancel(context.Background())
	runner.Start(ctx, []app.Job{jobs[0], jobs[1]})

	for _, job := range jobs {
		waitClosed(t, job.started, job.name+" did not start")
	}

	cancel()
	waitClosed(t, makeClosed(runner.Wait), "runner did not return after cancel")

	for _, job := range jobs {
		waitClosed(t, job.stopped, job.name+" did not stop")
	}
}

func TestRunnerWithoutJobsWaits(t *testing.T) {
	runner := app.NewRunner(nil)
	runner.Start(context.Background(), nil)
	waitClosed(t, makeClosed(runner.Wait), "runner without jobs blocked")
}

func makeClosed(done func()) chan struct{} {
	closed := make(chan struct{})
	go func() {
		done()
		close(closed)
	}()
	return closed
}

func waitClosed(t *testing.T, ch chan struct{}, message string) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(5 * time.Second):
		t.Fatal(message)
	}
}
