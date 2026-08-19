package checkpoint

import (
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// storedVersion reads the stamp back through a second handle rather than through the
// store, so the assertions observe what is actually on disk instead of whatever the
// store happens to hold in memory.
func storedVersion(t *testing.T, path string) int {
	t.Helper()
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("open %s: %v", path, err)
	}
	defer db.Close()
	var version int
	if err := db.QueryRow(`SELECT version FROM schema_meta WHERE id = 1`).Scan(&version); err != nil {
		t.Fatalf("read schema_meta from %s: %v", path, err)
	}
	return version
}

// stampWith writes a database carrying the current tables but an arbitrary version,
// which is the only way to stand in for a build other than this one.
func stampWith(t *testing.T, path string, version int) {
	t.Helper()
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("open %s: %v", path, err)
	}
	defer db.Close()
	if _, err := db.Exec(schema); err != nil {
		t.Fatalf("create schema in %s: %v", path, err)
	}
	if _, err := db.Exec(`INSERT OR REPLACE INTO schema_meta (id, version) VALUES (1, ?)`, version); err != nil {
		t.Fatalf("stamp %s with %d: %v", path, version, err)
	}
}

func TestAFreshDatabaseIsStampedWithTheCurrentSchemaVersion(t *testing.T) {
	path := filepath.Join(t.TempDir(), "fresh.db")
	st, err := OpenSQLite(path)
	if err != nil {
		t.Fatalf("OpenSQLite: %v", err)
	}
	st.Close()

	if got := storedVersion(t, path); got != SchemaVersion {
		t.Fatalf("stored schema version = %d; want %d", got, SchemaVersion)
	}
}

func TestADatabaseAtTheCurrentVersionOpensWithoutRewritingItsStamp(t *testing.T) {
	path := filepath.Join(t.TempDir(), "current.db")
	st, err := OpenSQLite(path)
	if err != nil {
		t.Fatalf("OpenSQLite (first): %v", err)
	}
	st.Close()

	// A rewrite is invisible when it writes the same value back, so the file is made
	// read-only: SQLite opens a non-writable file read-only and fails any statement that
	// tries to modify it. Reopening successfully is therefore proof that the second open
	// wrote nothing at all.
	if err := os.Chmod(path, 0o444); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	st2, err := OpenSQLite(path)
	if err != nil {
		t.Fatalf("OpenSQLite (second, read-only file): %v", err)
	}
	st2.Close()
	if err := os.Chmod(path, 0o644); err != nil {
		t.Fatalf("chmod back: %v", err)
	}

	if got := storedVersion(t, path); got != SchemaVersion {
		t.Fatalf("stored schema version after reopen = %d; want %d", got, SchemaVersion)
	}
}

func TestADatabaseStampedWithAnUnknownVersionIsRefusedByFilePath(t *testing.T) {
	path := filepath.Join(t.TempDir(), "from-the-future.db")
	stampWith(t, path, SchemaVersion+1)

	st, err := OpenSQLite(path)
	if err == nil {
		st.Close()
		t.Fatalf("OpenSQLite on version %d = nil error; want refusal", SchemaVersion+1)
	}
	if !errors.Is(err, ErrSchemaVersion) {
		t.Fatalf("error = %v; want one matching ErrSchemaVersion", err)
	}
	if !strings.Contains(err.Error(), path) {
		t.Fatalf("error message = %q; want it to name the database file %q", err.Error(), path)
	}
}

func TestADatabaseStampedWithAnOlderVersionIsRefusedRatherThanMigrated(t *testing.T) {
	path := filepath.Join(t.TempDir(), "from-the-past.db")
	stampWith(t, path, SchemaVersion-1)

	// Older is refused on exactly the same footing as newer: nothing here migrates, so a
	// database written before the events table would otherwise be read by statements that
	// expect that table to exist.
	st, err := OpenSQLite(path)
	if err == nil {
		st.Close()
		t.Fatalf("OpenSQLite on version %d = nil error; want refusal", SchemaVersion-1)
	}
	if !errors.Is(err, ErrSchemaVersion) {
		t.Fatalf("error = %v; want one matching ErrSchemaVersion", err)
	}
	if !strings.Contains(err.Error(), path) {
		t.Fatalf("error message = %q; want it to name the database file %q", err.Error(), path)
	}
}

func TestTheSchemaVersionIsAtLeastTwoNowThatTheJournalTableExists(t *testing.T) {
	// The version is what refuses a pre-journal database; leaving it at 1 while adding a
	// table is exactly the silent reinterpretation issue 01 exists to prevent.
	if SchemaVersion < 2 {
		t.Fatalf("SchemaVersion = %d; want at least 2 now that the events table is part of the schema", SchemaVersion)
	}
}

func TestAFreshDatabaseCarriesTheEventsTable(t *testing.T) {
	path := filepath.Join(t.TempDir(), "tables.db")
	st, err := OpenSQLite(path)
	if err != nil {
		t.Fatalf("OpenSQLite: %v", err)
	}
	defer st.Close()

	// The journal table is created by the same stamped transaction as the rest, not lazily
	// on first append: a table that appears later would make one stamp describe two
	// different databases depending on whether a run had ever written an event.
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("open %s: %v", path, err)
	}
	defer db.Close()
	var n int
	if err := db.QueryRow(`SELECT count(*) FROM sqlite_master WHERE type = 'table' AND name = 'events'`).Scan(&n); err != nil {
		t.Fatalf("inspect sqlite_master: %v", err)
	}
	if n != 1 {
		t.Fatalf("events tables in a fresh database = %d; want 1", n)
	}
}

func TestADatabaseWrittenBeforeTheStampExistedIsRefusedRatherThanAdopted(t *testing.T) {
	path := filepath.Join(t.TempDir(), "legacy.db")
	// Exactly what a pre-versioning build left behind: the checkpoints table as it was
	// before the dossier column, and no schema_meta. Adopting it as the current version
	// is the failure this issue exists to prevent — the columns would not match what the
	// current statements read and write.
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("open %s: %v", path, err)
	}
	if _, err := db.Exec(`CREATE TABLE checkpoints (
		run_id     TEXT    NOT NULL,
		step       INTEGER NOT NULL,
		frontier   TEXT    NOT NULL,
		state      TEXT    NOT NULL,
		status     TEXT    NOT NULL,
		created_at TEXT    NOT NULL,
		PRIMARY KEY (run_id, step)
	);`); err != nil {
		t.Fatalf("create legacy table: %v", err)
	}
	db.Close()

	st, err := OpenSQLite(path)
	if err == nil {
		st.Close()
		t.Fatal("OpenSQLite on an unstamped database = nil error; want refusal")
	}
	if !errors.Is(err, ErrSchemaVersion) {
		t.Fatalf("error = %v; want one matching ErrSchemaVersion", err)
	}
	if !strings.Contains(err.Error(), path) {
		t.Fatalf("error message = %q; want it to name the database file %q", err.Error(), path)
	}
}
