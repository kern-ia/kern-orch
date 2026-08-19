package checkpoint

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/yoann/kern-orch/internal/journal"
)

// FirstSeq is the sequence number of a run's first event. It is 1, not 0, so that Go's zero
// value cannot pass for a valid first event: an Event whose Seq was simply never assigned
// would otherwise be accepted as the opening of a journal, and every later append would
// build a sequence one short of what the emitter believed it wrote.
const FirstSeq int64 = 1

// eventTimeLayout formats the events table's at column. It matches what Save writes into
// checkpoints.created_at, so the two tables of one database sort and compare the same way
// under a plain SELECT — the only interface an operator inspecting a run actually has.
const eventTimeLayout = time.RFC3339Nano

// ErrSeqMismatch reports an append whose sequence numbers do not continue the run's stored
// sequence exactly. Both directions are refused: a batch starting past the next seq would
// leave a hole, and one starting before it would collide with events already written. A
// journal that can gap is a journal replay cannot be trusted to reproduce, which is the
// whole reason the sequence is enforced here rather than assumed from the emitter.
var ErrSeqMismatch = errors.New("checkpoint: event sequence mismatch")

// ErrRunIDMismatch reports an event whose own RunID names a run other than the one being
// appended to. Rewriting it to match would silently move an event between journals, so it
// is refused instead.
var ErrRunIDMismatch = errors.New("checkpoint: event belongs to another run")

// ErrInvalidSeq reports a read starting before the first possible sequence number. Distinct
// from ErrSeqMismatch: nothing about the stored journal is wrong here, the argument is.
var ErrInvalidSeq = errors.New("checkpoint: invalid sequence number")

// Journal is the append-only half of the run record: events go in, and come back out in the
// order they went in. It is a separate port from Store on purpose — a consumer that replays
// a run (issue 04's projection, the reporter) needs to read events and has no business
// writing checkpoints.
//
// Concurrency: implementations are safe for use from several goroutines against *different*
// runs. A single run must have exactly one appender, because Seq is assigned by the caller
// and only checked here; two goroutines appending to one run would compute the same next
// seq and one of them would get ErrSeqMismatch. That refusal is the intended outcome, not a
// limitation to work around by renumbering — silently reassigning a seq would break the
// emitter's own idea of what it wrote.
type Journal interface {
	Append(ctx context.Context, runID string, events ...journal.Event) error
	Read(ctx context.Context, runID string) ([]journal.Event, error)
	ReadFrom(ctx context.Context, runID string, fromSeq int64) ([]journal.Event, error)
}

// Append writes events as one batch, in one transaction, and refuses the whole batch unless
// its sequence continues the run's stored sequence exactly.
//
// The events are encoded before the transaction opens, so an event that cannot cross JSON
// fails with nothing written and the run's next-seq unmoved — the alternative, encoding row
// by row inside the transaction, would be correct too but only because of the rollback, and
// would report the failure after having already taken a write lock on a shared database.
//
// Note on atomicity for issue 07: the actual work lives in appendTx, which takes a *sql.Tx.
// That is what lets a later caller write the journal and a derived projection in one
// transaction without this method's own transaction boundary getting in the way.
func (s *SQLiteStore) Append(ctx context.Context, runID string, events ...journal.Event) error {
	if runID == "" {
		return ErrEmptyRunID
	}
	if len(events) == 0 {
		// An empty batch is not a gap and not an error: callers assemble batches from a
		// level's worth of activity, and a level that produced nothing must not have to
		// special-case the call.
		return nil
	}
	rows, err := encodeBatch(runID, events)
	if err != nil {
		return err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("checkpoint: begin append to run %q: %w", runID, err)
	}
	defer tx.Rollback()
	if err := appendTx(ctx, tx, runID, rows); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("checkpoint: commit append to run %q: %w", runID, err)
	}
	return nil
}

// eventRow is one encoded event on its way to the table: the indexed columns plus the whole
// journal.Event as the driver wrote it. Storing the event's own JSON rather than a column
// per field keeps internal/journal the single owner of the encoding — a payload type added
// there needs no change here, and no second decoder can drift from the first.
type eventRow struct {
	seq  int64
	at   string
	blob string
}

// encodeBatch validates the batch's shape and marshals it. Validation and encoding share a
// pass because both must complete before anything is written: reporting "event 3 is
// unserializable" after events 1 and 2 landed would move the run's next-seq on an append
// the caller was told had failed.
func encodeBatch(runID string, events []journal.Event) ([]eventRow, error) {
	rows := make([]eventRow, 0, len(events))
	for i, ev := range events {
		switch ev.RunID {
		case runID:
		case "":
			// An unset run id carries no claim that could contradict runID, so filling it
			// cannot redirect an event; a non-empty mismatch is a real contradiction and
			// falls through to the refusal below.
			ev.RunID = runID
		default:
			return nil, fmt.Errorf("checkpoint: append to run %q: event %d carries run id %q: %w",
				runID, i, ev.RunID, ErrRunIDMismatch)
		}
		if i > 0 && ev.Seq != events[i-1].Seq+1 {
			return nil, fmt.Errorf("checkpoint: append to run %q: event %d has seq %d, expected %d to follow the previous event: %w",
				runID, i, ev.Seq, events[i-1].Seq+1, ErrSeqMismatch)
		}
		blob, err := json.Marshal(ev)
		if err != nil {
			return nil, fmt.Errorf("checkpoint: append to run %q: encode event at seq %d: %w", runID, ev.Seq, err)
		}
		rows = append(rows, eventRow{seq: ev.Seq, at: ev.At.UTC().Format(eventTimeLayout), blob: string(blob)})
	}
	return rows, nil
}

// appendTx checks the run's next sequence number and inserts the batch, both inside tx.
//
// The check and the inserts have to share a transaction, otherwise two appends could each
// read the same next-seq before either wrote. In this store they cannot: OpenSQLite pins the
// pool to a single connection, so a transaction holds the only connection there is and every
// other access waits. That pin is load-bearing but invisible from here, which is why the
// table's PRIMARY KEY (run_id, seq) is the actual guarantee — a second writer that somehow
// got past the check still cannot overwrite an existing event, it gets a constraint error.
func appendTx(ctx context.Context, tx *sql.Tx, runID string, rows []eventRow) error {
	next, err := nextSeqTx(ctx, tx, runID)
	if err != nil {
		return err
	}
	if rows[0].seq != next {
		return fmt.Errorf("checkpoint: append to run %q: batch starts at seq %d, next seq is %d: %w",
			runID, rows[0].seq, next, ErrSeqMismatch)
	}
	stmt, err := tx.PrepareContext(ctx, `INSERT INTO events (run_id, seq, at, event) VALUES (?, ?, ?, ?)`)
	if err != nil {
		return fmt.Errorf("checkpoint: prepare append to run %q: %w", runID, err)
	}
	defer stmt.Close()
	for _, row := range rows {
		if _, err := stmt.ExecContext(ctx, runID, row.seq, row.at, row.blob); err != nil {
			return fmt.Errorf("checkpoint: append event at seq %d to run %q: %w", row.seq, runID, err)
		}
	}
	return nil
}

// nextSeqTx returns the sequence number the run's next event must carry. A run with no
// events yet answers FirstSeq, so the caller has one rule to satisfy rather than a special
// case for the first append.
func nextSeqTx(ctx context.Context, tx *sql.Tx, runID string) (int64, error) {
	// MAX over a NULL-able aggregate needs a nullable destination: an empty run has no rows
	// and SQLite answers NULL rather than 0, which would be indistinguishable from a real
	// seq if the sequence started at 0.
	var max sql.NullInt64
	if err := tx.QueryRowContext(ctx, `SELECT MAX(seq) FROM events WHERE run_id = ?`, runID).Scan(&max); err != nil {
		return 0, fmt.Errorf("checkpoint: read next seq of run %q: %w", runID, err)
	}
	if !max.Valid {
		return FirstSeq, nil
	}
	return max.Int64 + 1, nil
}

// Read returns every event of runID in sequence order. A run with no events is not an error:
// a run that was accepted but has not emitted yet is a normal state, and a caller
// distinguishing "nothing yet" from "failed to read" on an error value would get it wrong.
func (s *SQLiteStore) Read(ctx context.Context, runID string) ([]journal.Event, error) {
	return s.ReadFrom(ctx, runID, FirstSeq)
}

// ReadFrom returns the events of runID at or after fromSeq, in sequence order. A fromSeq past
// the end returns no events and no error — that is exactly what a consumer which has already
// applied the whole journal asks for, and an error there would make "nothing new" look like
// a failure.
func (s *SQLiteStore) ReadFrom(ctx context.Context, runID string, fromSeq int64) ([]journal.Event, error) {
	if fromSeq < 0 {
		return nil, fmt.Errorf("checkpoint: read run %q from seq %d, sequence numbers start at %d: %w",
			runID, fromSeq, FirstSeq, ErrInvalidSeq)
	}
	return readEventsFrom(ctx, s.db, runID, fromSeq)
}

// eventQuerier is what readEventsFrom needs of its source: the connection pool, or an open
// transaction. Both satisfy it, which is what lets the projection written together with a
// level's events (atomic.go) replay the rows that transaction just inserted — rows no other
// reader can see yet — through the same decoding as a plain read rather than a second copy
// of it that could drift.
type eventQuerier interface {
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
}

func readEventsFrom(ctx context.Context, q eventQuerier, runID string, fromSeq int64) ([]journal.Event, error) {
	rows, err := q.QueryContext(ctx,
		`SELECT event FROM events WHERE run_id = ? AND seq >= ? ORDER BY seq`, runID, fromSeq)
	if err != nil {
		return nil, fmt.Errorf("checkpoint: read run %q: %w", runID, err)
	}
	defer rows.Close()

	events := []journal.Event{}
	for rows.Next() {
		var blob string
		if err := rows.Scan(&blob); err != nil {
			return nil, fmt.Errorf("checkpoint: scan event of run %q: %w", runID, err)
		}
		var ev journal.Event
		if err := json.Unmarshal([]byte(blob), &ev); err != nil {
			return nil, fmt.Errorf("checkpoint: decode event of run %q: %w", runID, err)
		}
		events = append(events, ev)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("checkpoint: read run %q: %w", runID, err)
	}
	return events, nil
}
