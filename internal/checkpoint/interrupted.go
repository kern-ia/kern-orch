package checkpoint

import (
	"context"
	"fmt"
	"time"

	"github.com/yoann/kern-orch/internal/journal"
	"github.com/yoann/kern-orch/internal/journal/projection"
)

// InterruptedRunReason and InterruptedNodeReason are the messages the synthetic tail carries.
// They are exported because they are the only text an operator reading a closed journal has
// to explain why an event nobody witnessed is in the record.
const (
	InterruptedRunReason  = "the record stops without a terminal event: the process did not close this run"
	InterruptedNodeReason = "the record stops while this node was running: it neither produced nor failed"
)

// closeInterruptedTail records that a run whose journal carries no terminal event was
// interrupted, and returns the journal as it stands afterwards.
//
// # Which shapes are closed, and why both
//
// The discriminator is the absence of a terminal event, not the shape of the tail. Two
// different accidents produce it:
//
//   - a process killed mid-level, which leaves a LevelOpened with no LevelClosed and nodes
//     that started and never came back;
//   - a run stopped through its own context, whose terminal events are written on that
//     already-cancelled context and are therefore refused — its journal simply stops after
//     the last level that closed.
//
// The second is the common one in this codebase today and it looks tidy, which is exactly
// why it needs closing too: "the record stops" and "the run ended" are different facts, and
// nothing else in the table tells them apart. A reader of the second shape cannot say
// whether the run ended, is still live, or died — and a run that resumes and then really
// finishes would leave a journal holding two attempts with only one visible boundary.
//
// A journal that already ends on run_finished, run_failed or run_interrupted is complete and
// is not touched at all: not one row is rewritten, and no event is appended.
//
// # What is NOT synthesised
//
// No synthetic LevelClosed. Closing the open level would make replay fold its branches into
// the shared state, and the engine aborts a level as a whole — that state is one the run
// never held. The level stays open on purpose: it is what tells resume which level to
// restart, and it contributes nothing until it closes for real.
//
// A node that started and never resolved does get a synthetic NodeFailed, because the run
// really did stop with that node unfinished, and the alternative — a NodeStarted nothing
// ever answers — is the ambiguity this whole issue exists to remove. A node that had already
// produced is left alone: recording a failure it did not have would be an invention, and its
// output is harmless anyway since the level never combined.
func (s *SQLiteStore) closeInterruptedTail(ctx context.Context, runID string, events []journal.Event, replayed projection.Replayed) ([]journal.Event, error) {
	tail := syntheticTail(runID, events, replayed, time.Now().UTC())
	// Through AppendAndReproject, not around it: the batch satisfies the same sequence
	// contract every other append does, and the row stays Project(the journal) because it
	// is re-derived inside the very transaction that writes these events.
	if err := s.AppendAndReproject(ctx, runID, tail...); err != nil {
		return nil, fmt.Errorf("checkpoint: close the interrupted tail of run %q: %w", runID, err)
	}
	// Read back rather than appending to the slice in memory: the frontier resume answers
	// must come from the journal as it now stands. A run that is in fact still live has its
	// own appender moving the sequence, and this append is then refused rather than
	// interleaved — the refusal is the right outcome, and it happens above.
	return s.Read(ctx, runID)
}

// syntheticTail is the batch that closes an unterminated journal: one NodeFailed per node the
// open level started and never resolved, in frontier order, then the RunInterrupted that
// closes the run. It is a pure function of the events, so what gets written can be reasoned
// about without a database.
//
// The sequence continues the last stored event's, which is what Append enforces anyway; at
// is the moment the interruption is *recorded*, not the moment it happened. Those two are
// far apart, which is what the events' Synthetic marking exists to warn a reader about.
func syntheticTail(runID string, events []journal.Event, replayed projection.Replayed, at time.Time) []journal.Event {
	seq := events[len(events)-1].Seq
	var tail []journal.Event
	for _, id := range unresolvedNodes(events, replayed.OpenFrontier) {
		seq++
		tail = append(tail, journal.Event{
			RunID: runID, Seq: seq, At: at, Synthetic: true,
			Payload: journal.NodeFailed{NodeID: id, Message: InterruptedNodeReason},
		})
	}
	seq++
	return append(tail, journal.Event{
		RunID: runID, Seq: seq, At: at, Synthetic: true,
		Payload: journal.RunInterrupted{Reason: InterruptedRunReason},
	})
}

// unresolvedNodes names the nodes of the still-open level that started and neither produced
// nor failed, in frontier order.
//
// It walks backwards to that level's own LevelOpened rather than replaying forwards from the
// start: frontier already comes from the replay, so which level is open is settled, and a
// second forward pass would be a second opinion on it, free to drift from the projector's.
// A node of the frontier that never started gets nothing — the journal makes no claim about
// it that needs answering, and replay refuses a failure for a node it never saw start.
func unresolvedNodes(events []journal.Event, frontier []string) []string {
	if len(frontier) == 0 {
		return nil
	}
	started := make(map[string]bool, len(frontier))
	resolved := make(map[string]bool, len(frontier))
scan:
	for i := len(events) - 1; i >= 0; i-- {
		switch payload := events[i].Payload.(type) {
		case journal.LevelOpened:
			break scan
		case journal.NodeStarted:
			started[payload.NodeID] = true
		case journal.NodeProduced:
			resolved[payload.NodeID] = true
		case journal.NodeFailed:
			resolved[payload.NodeID] = true
		case journal.FreezeApplied:
			// journal.FreezeApplied carries no node id: the engine refuses a freeze it
			// cannot attribute to a single branch, so inside a one-node level it can only
			// be that node's, and inside a fan-out it cannot exist.
			if len(frontier) == 1 {
				resolved[frontier[0]] = true
			}
		}
	}
	var unresolved []string
	for _, id := range frontier {
		if started[id] && !resolved[id] {
			unresolved = append(unresolved, id)
		}
	}
	return unresolved
}
