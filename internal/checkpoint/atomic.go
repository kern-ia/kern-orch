package checkpoint

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/yoann/kern-orch/internal/graph"
	"github.com/yoann/kern-orch/internal/journal"
	"github.com/yoann/kern-orch/internal/journal/projection"
)

// Projection is everything a checkpoint row holds that the journal does not: where the run
// is, where it is going, and the provenance a resume or a permission check needs.
//
// It is Record minus State, and the missing field is the whole point of this file. Record's
// State is supplied by the caller, which is what let the snapshot be written independently
// of the events it claims to summarise; here the state is never an argument at all — it is
// computed from the run's journal inside the transaction that writes the row, so the row
// cannot disagree with the events even if the caller's live state does.
type Projection struct {
	RunID     string
	Step      int
	Frontier  []string
	Status    string
	CreatedAt time.Time
	GraphPath string
	Requester string
	Dossier   string
}

// AppendAndProject appends a level's events and writes the projection row for (RunID, Step),
// both inside one transaction. A failure in either half rolls back the other: the events of
// a level whose row could not be written are not a record of anything a reader could act on,
// and a row for events that were not written is exactly the drift this replaces.
//
// The state is re-derived from the run's whole journal, read back through the same
// transaction rather than accumulated in memory. Two cheaper designs were rejected:
// marshalling the caller's live *graph.State makes the row an independent write that merely
// looks derived, and folding an in-memory running state forward makes the row correct only
// as long as that in-memory copy is — a second truth again, just one with a shorter life.
// Reading the journal back costs O(events) per level, which is the price of the row being a
// projection rather than a claim.
func (s *SQLiteStore) AppendAndProject(ctx context.Context, p Projection, events ...journal.Event) error {
	if p.RunID == "" {
		return ErrEmptyRunID
	}
	// Encoded before the transaction opens, for the reason Append documents: an event that
	// cannot cross JSON fails with nothing written and no write lock taken.
	rows, err := encodeBatch(p.RunID, events)
	if err != nil {
		return err
	}
	return s.inTx(ctx, p.RunID, func(tx *sql.Tx) error {
		if err := appendRowsTx(ctx, tx, p.RunID, rows); err != nil {
			return err
		}
		state, err := projectTx(ctx, tx, p.RunID)
		if err != nil {
			return err
		}
		return saveTx(ctx, tx, Record{
			RunID: p.RunID, Step: p.Step, Frontier: p.Frontier, State: state, Status: p.Status,
			CreatedAt: p.CreatedAt, GraphPath: p.GraphPath, Requester: p.Requester, Dossier: p.Dossier,
		})
	})
}

// AppendAndReproject appends events that belong to no new level and re-derives the state of
// the row the run already sits on, in one transaction.
//
// A run's terminal events (finished, failed) are emitted after the last level's hook has
// fired, so there is no StepInfo to build a Projection from and no new row to write. Writing
// them with no reprojection would be enough today — neither payload changes the state — but
// it would make "the row is Project(the journal)" true only by coincidence, and issue 06's
// nudge and freeze events, which do change the state, arrive through this same path.
//
// A run with no row yet is not an error: nothing is out of sync when nothing is stored. The
// CLI's `run` writes no queued marker, so its opening event legitimately arrives first.
func (s *SQLiteStore) AppendAndReproject(ctx context.Context, runID string, events ...journal.Event) error {
	if runID == "" {
		return ErrEmptyRunID
	}
	rows, err := encodeBatch(runID, events)
	if err != nil {
		return err
	}
	return s.inTx(ctx, runID, func(tx *sql.Tx) error {
		if err := appendRowsTx(ctx, tx, runID, rows); err != nil {
			return err
		}
		rec, ok, err := latestTx(ctx, tx, runID)
		if err != nil || !ok {
			return err
		}
		state, err := projectTx(ctx, tx, runID)
		if err != nil {
			return err
		}
		// Only the state is rewritten. Step, status, frontier and provenance describe where
		// the run is, which these events did not move — rewriting the status here is how the
		// queued marker would stop being findable right after acceptance.
		rec.State = state
		return saveTx(ctx, tx, rec)
	})
}

// NextSeq returns the sequence number the run's next appended event must carry. An emitter
// building a batch needs it before it has anything to append: a resumed run continues a
// journal that already exists, and starting its numbering at FirstSeq would be refused by
// Append for a reason the emitter could not have known.
func (s *SQLiteStore) NextSeq(ctx context.Context, runID string) (int64, error) {
	if runID == "" {
		return 0, ErrEmptyRunID
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, fmt.Errorf("checkpoint: begin next seq of run %q: %w", runID, err)
	}
	defer tx.Rollback()
	return nextSeqTx(ctx, tx, runID)
}

// inTx runs fn inside a transaction and commits only if it returns nil. Every atomic write
// in this file has the same shape, and having one place own the rollback is what keeps a
// later branch from returning early past a defer that was never registered.
func (s *SQLiteStore) inTx(ctx context.Context, runID string, fn func(*sql.Tx) error) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("checkpoint: begin transaction for run %q: %w", runID, err)
	}
	defer tx.Rollback()
	if err := fn(tx); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("checkpoint: commit transaction for run %q: %w", runID, err)
	}
	return nil
}

// appendRowsTx is appendTx with the empty batch allowed. A level that emitted nothing is a
// real case for the callers here — they still owe the row an up-to-date state — whereas
// appendTx reads rows[0] to check the sequence and has no meaning with none.
func appendRowsTx(ctx context.Context, tx *sql.Tx, runID string, rows []eventRow) error {
	if len(rows) == 0 {
		return nil
	}
	return appendTx(ctx, tx, runID, rows)
}

// projectTx replays every event of runID that tx can see — the ones this transaction just
// inserted included — and returns the state they describe. Reading through tx rather than
// through the pool is what makes the projection a function of what is about to be committed
// rather than of what was committed before.
func projectTx(ctx context.Context, tx *sql.Tx, runID string) (*graph.State, error) {
	events, err := readEventsFrom(ctx, tx, runID, FirstSeq)
	if err != nil {
		return nil, err
	}
	state, err := projection.Project(events)
	if err != nil {
		return nil, fmt.Errorf("checkpoint: project run %q: %w", runID, err)
	}
	return state, nil
}

// latestTx is Latest read through tx, so the row a reprojection is about to rewrite is the
// row this transaction will overwrite and not one another writer moved in between.
func latestTx(ctx context.Context, tx *sql.Tx, runID string) (Record, bool, error) {
	row := tx.QueryRowContext(ctx,
		`SELECT step, frontier, state, status, created_at, graph_path, requester, dossier
		 FROM checkpoints WHERE run_id = ? ORDER BY step DESC LIMIT 1`, runID)
	rec, ok, err := scanRecord(runID, row)
	if err != nil {
		return Record{}, false, err
	}
	return rec, ok, nil
}

// saveTx is the checkpoints upsert, shared by Save and by the transactions here. One
// statement in one place: a column added to Record must otherwise be remembered in two
// upserts that look alike enough for the second to be forgotten.
func saveTx(ctx context.Context, tx *sql.Tx, r Record) error {
	frontier, state, created, err := encodeRecord(r)
	if err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, upsertCheckpoint,
		r.RunID, r.Step, frontier, state, r.Status, created, r.GraphPath, r.Requester, r.Dossier); err != nil {
		return fmt.Errorf("checkpoint: save run %q at step %d: %w", r.RunID, r.Step, err)
	}
	return nil
}
