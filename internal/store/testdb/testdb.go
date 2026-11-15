// Package testdb provides throwaway PostgreSQL instances for integration
// tests.
//
// Tests that need a real database call Start and are skipped when Docker is
// not available, so `go test ./...` stays green on machines without it.
package testdb

import (
	"context"
	"testing"

	"github.com/testcontainers/testcontainers-go"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
)

// Image is the PostgreSQL image used by integration tests.
const Image = "postgres:17"

// Database, Username and Password are the credentials of the throwaway
// database.
const (
	Database = "cublyverse"
	Username = "cublyverse"
	Password = "cublyverse"
)

// Start launches a PostgreSQL container, registers its cleanup and returns the
// connection URL. The test is skipped when Docker is unavailable or when tests
// run in short mode.
func Start(t *testing.T) string {
	t.Helper()

	if testing.Short() {
		t.Skip("testdb: integration test disabled by -short")
	}
	// The provider health check both skips the test on Docker-less machines and
	// reports a usable Docker endpoint.
	testcontainers.SkipIfProviderIsNotHealthy(t)

	ctx := context.Background()
	container, err := tcpostgres.Run(ctx, Image,
		tcpostgres.WithDatabase(Database),
		tcpostgres.WithUsername(Username),
		tcpostgres.WithPassword(Password),
		tcpostgres.BasicWaitStrategies(),
	)
	if err != nil {
		t.Fatalf("testdb: start %s container: %v", Image, err)
	}
	t.Cleanup(func() {
		if err := container.Terminate(context.Background()); err != nil {
			t.Logf("testdb: terminate container: %v", err)
		}
	})

	url, err := container.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		t.Fatalf("testdb: build connection string: %v", err)
	}
	return url
}
