package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/maksimkurb/cublyverse/internal/store"
	"github.com/maksimkurb/cublyverse/internal/store/testdb"
)

// legacyExportFile writes a synthetic export of the old n8n table and returns
// its path. One row is valid, one has an unknown time zone.
const legacyExportFile = `reminder_id,user_id,chat_id,message,timezone,days,time_hhmm,repeat_minutes,enabled,deleted
l-1,1001,1001,"Полить цветы 🌱",Europe/Moscow,"1,4",08:15,10,true,false
l-2,1001,1001,"Сломанный пояс",Mars/Olympus,1,09:00,10,true,false
`

func writeLegacyExport(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "telegram_reminders_v2.csv")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write export: %v", err)
	}
	return path
}

// TestImportN8NRejectsBadArguments covers the input validation of the command.
func TestImportN8NRejectsBadArguments(t *testing.T) {
	path := writeLegacyExport(t, legacyExportFile)
	t.Setenv("DATABASE_URL", "")

	if _, _, err := execute(t, "import-n8n"); err == nil || !strings.Contains(err.Error(), "exactly one file") {
		t.Fatalf("err = %v, want a missing file error", err)
	}
	if _, _, err := execute(t, "import-n8n", "--format", "xlsx", path); err == nil || !strings.Contains(err.Error(), "unknown --format") {
		t.Fatalf("err = %v, want an unknown format error", err)
	}
	// Without any database URL an import (not a dry run) cannot start.
	if _, _, err := execute(t, "import-n8n", path); err == nil || !strings.Contains(err.Error(), "database url is required") {
		t.Fatalf("err = %v, want a database url error", err)
	}
	if _, _, err := execute(t, "import-n8n", filepath.Join(t.TempDir(), "missing.csv")); err == nil {
		t.Fatal("a missing file returned no error")
	}
}

// TestImportN8NDryRunWithoutDatabase proves a dry run needs no database and
// still validates every row.
func TestImportN8NDryRunWithoutDatabase(t *testing.T) {
	path := writeLegacyExport(t, legacyExportFile)
	t.Setenv("DATABASE_URL", "")

	stdout, stderr, err := execute(t, "import-n8n", path, "--dry-run")
	if err != nil {
		t.Fatalf("dry run: %v", err)
	}
	for _, want := range []string{
		"rows: 2",
		"imported: 0",
		"invalid: 1",
		"row 3: Неизвестный часовой пояс",
		"dry run: nothing was written",
	} {
		if !strings.Contains(stdout, want) {
			t.Errorf("stdout %q does not contain %q", stdout, want)
		}
	}
	if !strings.Contains(stderr, "duplicates are not checked") {
		t.Errorf("stderr %q does not warn about the missing database", stderr)
	}
}

// TestImportN8NDryRunWritesNoSchema proves a dry run does not even apply the
// migrations: on a database that never saw the import it reports zero duplicates
// and leaves no table behind.
func TestImportN8NDryRunWritesNoSchema(t *testing.T) {
	path := writeLegacyExport(t, legacyExportFile)
	t.Setenv("DATABASE_URL", "")
	databaseURL := testdb.Start(t)

	stdout, _, err := execute(t, "import-n8n", "--database-url", databaseURL, "--dry-run", path)
	if err != nil {
		t.Fatalf("dry run: %v", err)
	}
	for _, want := range []string{"already imported: 0", "waiting for a sign-in: 0", "dry run: nothing was written"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("stdout %q does not contain %q", stdout, want)
		}
	}

	ctx := context.Background()
	st, err := store.Open(ctx, databaseURL)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer st.Close()
	var exists bool
	if err := st.Pool().QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM information_schema.tables
			WHERE table_schema = 'public' AND table_name = 'legacy_reminders_import'
		)`).Scan(&exists); err != nil {
		t.Fatalf("check the table: %v", err)
	}
	if exists {
		t.Fatal("a dry run created legacy_reminders_import")
	}
}

// TestImportN8NImportsAndIsIdempotent runs the command against a real
// PostgreSQL: the first run stores the valid row, the second reports it as a
// duplicate and a dry run writes nothing.
func TestImportN8NImportsAndIsIdempotent(t *testing.T) {
	path := writeLegacyExport(t, legacyExportFile)
	// The flag must win over a wrong environment value.
	t.Setenv("DATABASE_URL", "")
	databaseURL := testdb.Start(t)

	stdout, stderr, err := execute(t, "import-n8n", "--database-url", databaseURL, path)
	if err != nil {
		t.Fatalf("import: %v (stderr %s)", err, stderr)
	}
	for _, want := range []string{"imported: 1", "already imported: 0", "invalid: 1", "attached to already bound users: 0"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("stdout %q does not contain %q", stdout, want)
		}
	}

	stdout, stderr, err = execute(t, "import-n8n", "--database-url", databaseURL, path)
	if err != nil {
		t.Fatalf("second import: %v (stderr %s)", err, stderr)
	}
	for _, want := range []string{"imported: 0", "already imported: 1", "waiting for a sign-in: 1"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("second run stdout %q does not contain %q", stdout, want)
		}
	}

	stdout, _, err = execute(t, "import-n8n", "--database-url", databaseURL, "--dry-run", path)
	if err != nil {
		t.Fatalf("dry run: %v", err)
	}
	if !strings.Contains(stdout, "already imported: 1") || !strings.Contains(stdout, "dry run: nothing was written") {
		t.Errorf("dry run stdout = %q", stdout)
	}
}
