package main

import (
	"bytes"
	"context"
	"errors"
	"flag"
	"strings"
	"testing"
)

func execute(t *testing.T, args ...string) (stdout, stderr string, err error) {
	t.Helper()
	var out, errOut bytes.Buffer
	err = run(context.Background(), args, &out, &errOut)
	return out.String(), errOut.String(), err
}

func TestRunWithoutCommand(t *testing.T) {
	_, stderr, err := execute(t)
	if err == nil {
		t.Fatal("run returned no error")
	}
	if !strings.Contains(stderr, "usage: cublyverse") {
		t.Errorf("stderr %q does not contain the usage", stderr)
	}
}

func TestRunUnknownCommand(t *testing.T) {
	_, stderr, err := execute(t, "frobnicate")
	if err == nil || !strings.Contains(err.Error(), `unknown command "frobnicate"`) {
		t.Fatalf("err = %v, want an unknown command error", err)
	}
	if !strings.Contains(stderr, "usage: cublyverse") {
		t.Errorf("stderr %q does not contain the usage", stderr)
	}
}

func TestRunHelp(t *testing.T) {
	stdout, _, err := execute(t, "help")
	if err != nil {
		t.Fatalf("run help: %v", err)
	}
	if !strings.Contains(stdout, "serve") || !strings.Contains(stdout, "migrate") {
		t.Errorf("help output %q does not list the commands", stdout)
	}
}

// TestServeWithoutDatabaseURL is the M0-1 acceptance test: `serve` without
// DATABASE_URL must fail with an error that names the missing variable, before
// any network or database work happens.
func TestServeWithoutDatabaseURL(t *testing.T) {
	t.Setenv("DATABASE_URL", "")

	_, _, err := execute(t, "serve")
	if err == nil {
		t.Fatal("serve without DATABASE_URL returned no error")
	}
	if !strings.Contains(err.Error(), "missing required environment variables") {
		t.Errorf("error %q does not explain that variables are missing", err)
	}
	if !strings.Contains(err.Error(), "DATABASE_URL") {
		t.Errorf("error %q does not name DATABASE_URL", err)
	}
}

func TestServeRejectsArguments(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://user:pass@localhost:5432/cublyverse?sslmode=disable")

	_, _, err := execute(t, "serve", "extra")
	if err == nil || !strings.Contains(err.Error(), "serve takes no arguments") {
		t.Fatalf("err = %v, want an argument error", err)
	}
}

func TestMigrateRejectsUnknownCommand(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://user:pass@localhost:5432/cublyverse?sslmode=disable")

	_, _, err := execute(t, "migrate", "sideways")
	if err == nil || !strings.Contains(err.Error(), `unknown migrate command "sideways"`) {
		t.Fatalf("err = %v, want an unknown migrate command error", err)
	}
}

func TestMigrateHelp(t *testing.T) {
	_, _, err := execute(t, "migrate", "-h")
	if !errors.Is(err, flag.ErrHelp) {
		t.Fatalf("err = %v, want flag.ErrHelp", err)
	}
}
