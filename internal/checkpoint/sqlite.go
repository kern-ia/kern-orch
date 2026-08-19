package checkpoint

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/yoann/kern-orch/internal/graph"
	_ "modernc.org/sqlite"
)

// SchemaVersion is the schema this build understands. It only ever grows: every change to
// the tables below bumps it, and a database carrying any other value is refused rather than
// reinterpreted. Nothing migrates a database from one version to another — a mismatch is a
// dead end on purpose, because guessing at the difference is how a run silently reads rows
// written under rules that no longer hold.
// Version 2 added the events table (the run journal, issue 03 of the event-journal epic).
const SchemaVersion = 2

// unversionedSchema is what a database created before this mechanism reports. It is a real
// version, not a missing one: those files exist, they were written under different rules,
// and 0 makes them fall through the same refusal as any other mismatch instead of needing a
// special case that would inevitably end in adopting them.
const unversionedSchema = 0

// ErrSchemaVersion reports a database this build must not touch. Callers get it wrapped in a
// message naming the file, so the operator knows which database to move aside.
var ErrSchemaVersion = errors.New("checkpoint: unsupported schema version")

// schema is every table this build understands, created as one unit under SchemaVersion.
//
// events holds one row per journal entry. The whole journal.Event travels in a single JSON
// column rather than a column per field, so internal/journal stays the only owner of the
// encoding: a payload type added there needs no migration here, and there is no second
// decoder that could drift from the first. run_id, seq and at are lifted out because they
// are what queries and human inspection actually filter and order on; nothing reads them
// back into an Event, so the duplication cannot go stale in a way that changes a read.
const schema = `
CREATE TABLE IF NOT EXISTS schema_meta (
	id      INTEGER PRIMARY KEY CHECK (id = 1),
	version INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS checkpoints (
	run_id     TEXT    NOT NULL,
	step       INTEGER NOT NULL,
	frontier   TEXT    NOT NULL,
	state      TEXT    NOT NULL,
	status     TEXT    NOT NULL,
	created_at TEXT    NOT NULL,
	graph_path TEXT    NOT NULL DEFAULT '',
	requester  TEXT    NOT NULL DEFAULT '',
	dossier    TEXT    NOT NULL DEFAULT '',
	PRIMARY KEY (run_id, step)
);

CREATE TABLE IF NOT EXISTS events (
	run_id TEXT    NOT NULL,
	seq    INTEGER NOT NULL,
	at     TEXT    NOT NULL,
	event  TEXT    NOT NULL,
	PRIMARY KEY (run_id, seq)
);`

// SQLiteStore is a Store backed by modernc.org/sqlite (pure Go, no cgo).
type SQLiteStore struct {
	db *sql.DB
}

// busyTimeoutMS bounds how long a writer/reader waits on SQLITE_BUSY instead of failing
// immediately. C6 made this load-bearing rather than theoretical: StopRun/Nudge/Decide
// read the latest checkpoint (for the requester check) at the same time a live run's own
// goroutine may be writing one — a real concurrent access this store never had before.
const busyTimeoutMS = 5000

// OpenSQLite opens the checkpoint database at path, creating and stamping it when the file
// holds nothing yet, and refuses any existing database whose stamp is not SchemaVersion.
func OpenSQLite(path string) (*SQLiteStore, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("checkpoint: open %q: %w", path, err)
	}
	// SQLite's busy_timeout is per-connection state, and database/sql may otherwise open
	// several. Pinning the pool to one connection is what makes the pragma below actually
	// apply to every access rather than to whichever connection happened to be first.
	db.SetMaxOpenConns(1)
	if _, err := db.Exec(fmt.Sprintf("PRAGMA busy_timeout=%d;", busyTimeoutMS)); err != nil {
		db.Close()
		return nil, fmt.Errorf("checkpoint: set busy_timeout: %w", err)
	}
	if err := ensureSchema(db, path); err != nil {
		db.Close()
		return nil, err
	}
	return &SQLiteStore{db: db}, nil
}

// ensureSchema creates and stamps an empty database, and otherwise only checks the stamp.
// The check comes before any statement that could write, so a database this build does not
// understand is left exactly as its own build left it.
func ensureSchema(db *sql.DB, path string) error {
	empty, err := isEmpty(db)
	if err != nil {
		return err
	}
	if !empty {
		version, err := storedSchemaVersion(db)
		if err != nil {
			return err
		}
		if version != SchemaVersion {
			return fmt.Errorf("checkpoint: open %q: database at schema version %d, this build understands %d: %w",
				path, version, SchemaVersion, ErrSchemaVersion)
		}
		// The stamp already says what this build would write, so nothing is written: an
		// unchanged file is what lets an operator (or a reader-only process) open the
		// database without touching its mtime or taking a write lock.
		return nil
	}
	// Tables and stamp go in one transaction. Split apart, a crash between them would leave
	// a database with tables and no stamp — indistinguishable from a pre-versioning file,
	// and therefore refused forever.
	tx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("checkpoint: begin schema creation: %w", err)
	}
	defer tx.Rollback()
	if _, err := tx.Exec(schema); err != nil {
		return fmt.Errorf("checkpoint: schema: %w", err)
	}
	if _, err := tx.Exec(`INSERT OR REPLACE INTO schema_meta (id, version) VALUES (1, ?)`, SchemaVersion); err != nil {
		return fmt.Errorf("checkpoint: stamp schema version: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("checkpoint: commit schema creation: %w", err)
	}
	return nil
}

// isEmpty reports whether the file holds no user table yet, which is the one case where this
// build may create a schema. Any other content — an older checkpoint database, or some other
// application's file pointed at by a mistyped path — belongs to the version check.
func isEmpty(db *sql.DB) (bool, error) {
	var n int
	if err := db.QueryRow(`SELECT count(*) FROM sqlite_master WHERE type = 'table' AND name NOT LIKE 'sqlite_%'`).Scan(&n); err != nil {
		return false, fmt.Errorf("checkpoint: inspect schema: %w", err)
	}
	return n == 0, nil
}

// storedSchemaVersion reads the stamp of a non-empty database. A missing schema_meta table is
// not an error to report upwards but the answer itself: such a database predates the stamp.
func storedSchemaVersion(db *sql.DB) (int, error) {
	var version int
	err := db.QueryRow(`SELECT version FROM schema_meta WHERE id = 1`).Scan(&version)
	switch {
	case err == nil:
		return version, nil
	case errors.Is(err, sql.ErrNoRows):
		return unversionedSchema, nil
	case isMissingSchemaMeta(err):
		return unversionedSchema, nil
	default:
		return 0, fmt.Errorf("checkpoint: read schema version: %w", err)
	}
}

// isMissingSchemaMeta recognises the driver's "no such table" failure. The driver exposes no
// typed error for it, so the message is all there is to match on; the query is fixed and
// literal, so it can only ever be this one table that is missing.
func isMissingSchemaMeta(err error) bool {
	return strings.Contains(err.Error(), "no such table: schema_meta")
}

// Save upserts the checkpoint for (RunID, Step).
func (s *SQLiteStore) Save(ctx context.Context, r Record) error {
	if r.RunID == "" {
		return ErrEmptyRunID
	}
	frontier, err := json.Marshal(r.Frontier)
	if err != nil {
		return fmt.Errorf("checkpoint: marshal frontier: %w", err)
	}
	state, err := json.Marshal(r.State)
	if err != nil {
		return fmt.Errorf("checkpoint: marshal state: %w", err)
	}
	created := r.CreatedAt
	if created.IsZero() {
		created = time.Now().UTC()
	}
	_, err = s.db.ExecContext(ctx,
		`INSERT INTO checkpoints (run_id, step, frontier, state, status, created_at, graph_path, requester, dossier)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
		 ON CONFLICT(run_id, step) DO UPDATE SET
		   frontier=excluded.frontier, state=excluded.state,
		   status=excluded.status, created_at=excluded.created_at, graph_path=excluded.graph_path,
		   requester=excluded.requester, dossier=excluded.dossier`,
		r.RunID, r.Step, string(frontier), string(state), r.Status, created.Format(time.RFC3339Nano), r.GraphPath, r.Requester, r.Dossier)
	if err != nil {
		return fmt.Errorf("checkpoint: save: %w", err)
	}
	return nil
}

// Latest returns the highest-step checkpoint for runID; ok is false if none exists.
func (s *SQLiteStore) Latest(ctx context.Context, runID string) (Record, bool, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT step, frontier, state, status, created_at, graph_path, requester, dossier
		 FROM checkpoints WHERE run_id = ? ORDER BY step DESC LIMIT 1`, runID)
	var (
		step                                          int
		frontier, state                               string
		status, createdStr, gpath, requester, dossier string
	)
	switch err := row.Scan(&step, &frontier, &state, &status, &createdStr, &gpath, &requester, &dossier); err {
	case sql.ErrNoRows:
		return Record{}, false, nil
	case nil:
		// fallthrough below
	default:
		return Record{}, false, fmt.Errorf("checkpoint: latest: %w", err)
	}

	rec := Record{RunID: runID, Step: step, Status: status, State: graph.NewState(), GraphPath: gpath, Requester: requester, Dossier: dossier}
	if err := json.Unmarshal([]byte(frontier), &rec.Frontier); err != nil {
		return Record{}, false, fmt.Errorf("checkpoint: decode frontier: %w", err)
	}
	if err := json.Unmarshal([]byte(state), rec.State); err != nil {
		return Record{}, false, fmt.Errorf("checkpoint: decode state: %w", err)
	}
	rec.CreatedAt, _ = time.Parse(time.RFC3339Nano, createdStr)
	return rec, true, nil
}

// List returns one Summary per run, taking each run's highest-step row.
func (s *SQLiteStore) List(ctx context.Context) ([]Summary, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT c.run_id, c.step, c.status, c.created_at
		 FROM checkpoints c
		 JOIN (SELECT run_id, MAX(step) AS step FROM checkpoints GROUP BY run_id) m
		   ON c.run_id = m.run_id AND c.step = m.step
		 ORDER BY c.created_at DESC`)
	if err != nil {
		return nil, fmt.Errorf("checkpoint: list: %w", err)
	}
	defer rows.Close()

	var out []Summary
	for rows.Next() {
		var (
			sum        Summary
			createdStr string
		)
		if err := rows.Scan(&sum.RunID, &sum.LastStep, &sum.Status, &createdStr); err != nil {
			return nil, fmt.Errorf("checkpoint: scan summary: %w", err)
		}
		sum.UpdatedAt, _ = time.Parse(time.RFC3339Nano, createdStr)
		out = append(out, sum)
	}
	return out, rows.Err()
}

// Close releases the database handle.
func (s *SQLiteStore) Close() error { return s.db.Close() }
