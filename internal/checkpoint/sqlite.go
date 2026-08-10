package checkpoint

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/yoann/kern-orch/internal/graph"
	_ "modernc.org/sqlite"
)

const schema = `
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

// OpenSQLite opens (creating if needed) the checkpoint database at path and ensures
// the schema exists.
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
	if _, err := db.Exec(schema); err != nil {
		db.Close()
		return nil, fmt.Errorf("checkpoint: schema: %w", err)
	}
	// CREATE TABLE IF NOT EXISTS does nothing for a database that already existed before
	// this column was added — no migration mechanism exists yet in this project's dev
	// stage, so this one ALTER TABLE covers the gap. Errors are ignored: the only failure
	// mode against this fixed schema is "column already exists" on every run after the
	// first, which is not a fault.
	_, _ = db.Exec(`ALTER TABLE checkpoints ADD COLUMN dossier TEXT NOT NULL DEFAULT ''`)
	return &SQLiteStore{db: db}, nil
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
