package store_test

import (
	"context"
	"io"
	"log/slog"
	"testing"

	"github.com/maksimkurb/cublyverse/internal/store"
	"github.com/maksimkurb/cublyverse/internal/store/db"
	"github.com/maksimkurb/cublyverse/internal/store/testdb"
)

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// TestMigrateAndUsers is the M0 integration test: it starts a real PostgreSQL,
// applies the embedded migrations and round-trips a user through sqlc.
func TestMigrateAndUsers(t *testing.T) {
	url := testdb.Start(t)
	ctx := context.Background()
	logger := testLogger()

	if err := store.Migrate(ctx, url, logger); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	// Migrations must be safe to re-apply on the next start.
	if err := store.Migrate(ctx, url, logger); err != nil {
		t.Fatalf("Migrate (second run): %v", err)
	}

	st, err := store.Open(ctx, url)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(st.Close)

	if err := st.Ping(ctx); err != nil {
		t.Fatalf("Ping: %v", err)
	}

	email := "user@example.com"
	name := "Test User"
	created, err := st.Queries().CreateUser(ctx, db.CreateUserParams{
		OidcIssuer:  "https://id.example.com",
		OidcSubject: "subject-1",
		Email:       &email,
		DisplayName: &name,
	})
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	if !created.ID.Valid {
		t.Fatal("CreateUser returned an invalid id")
	}
	if created.Timezone != "UTC" {
		t.Errorf("Timezone = %q, want %q (schema default)", created.Timezone, "UTC")
	}
	if created.CreatedAt.IsZero() || created.UpdatedAt.IsZero() {
		t.Errorf("timestamps not set: created_at=%v updated_at=%v", created.CreatedAt, created.UpdatedAt)
	}

	got, err := st.Queries().GetUserByID(ctx, created.ID)
	if err != nil {
		t.Fatalf("GetUserByID: %v", err)
	}
	if got.ID != created.ID {
		t.Errorf("ID = %v, want %v", got.ID, created.ID)
	}
	if got.OidcIssuer != "https://id.example.com" || got.OidcSubject != "subject-1" {
		t.Errorf("identity = (%q, %q), want (%q, %q)", got.OidcIssuer, got.OidcSubject, "https://id.example.com", "subject-1")
	}
	if got.Email == nil || *got.Email != email {
		t.Errorf("Email = %v, want %q", got.Email, email)
	}
	if got.DisplayName == nil || *got.DisplayName != name {
		t.Errorf("DisplayName = %v, want %q", got.DisplayName, name)
	}

	byOIDC, err := st.Queries().GetUserByOIDC(ctx, db.GetUserByOIDCParams{
		OidcIssuer:  "https://id.example.com",
		OidcSubject: "subject-1",
	})
	if err != nil {
		t.Fatalf("GetUserByOIDC: %v", err)
	}
	if byOIDC.ID != created.ID {
		t.Errorf("GetUserByOIDC returned %v, want %v", byOIDC.ID, created.ID)
	}

	// The identity pair must stay unique, otherwise a second SSO account could
	// hijack an existing user row.
	if _, err := st.Queries().CreateUser(ctx, db.CreateUserParams{
		OidcIssuer:  "https://id.example.com",
		OidcSubject: "subject-1",
	}); err == nil {
		t.Error("CreateUser with a duplicate (oidc_issuer, oidc_subject) succeeded, want a unique violation")
	}

	statuses, err := store.MigrationStatus(ctx, url, logger)
	if err != nil {
		t.Fatalf("MigrationStatus: %v", err)
	}
	if len(statuses) != 1 {
		t.Fatalf("MigrationStatus returned %d migrations, want 1: %+v", len(statuses), statuses)
	}
	if statuses[0].State != "applied" {
		t.Errorf("migration state = %q, want %q", statuses[0].State, "applied")
	}
}
