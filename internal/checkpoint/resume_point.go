package checkpoint

import (
	"context"
	"errors"
	"fmt"

	"github.com/yoann/kern-orch/internal/graph"
	"github.com/yoann/kern-orch/internal/journal"
	"github.com/yoann/kern-orch/internal/journal/projection"
)

// ErrNoJournal reports a run that has a checkpoint row but no events at all — a run
// checkpointed before this repo had a journal. Replay projects it to an empty state, which
// is not a state that run ever held: it is the absence of a record. Resuming from it would
// run the remaining frontier against nothing, and every node downstream would read blanks
// where a value stood, with no error anywhere to say so. Distinct from an unknown run, which
// is simply absent, and from an interrupted tail, which is a journal that stops early rather
// than one that never existed (issue 12).
var ErrNoJournal = errors.New("checkpoint: run has no journal")

// ResumePoint is where a run is picked up again: the state to continue from, the frontier to
// execute, and the provenance the run was launched with.
//
// It is Record minus the row's own State, and the missing field is the point. Record.State
// is the cached projection, which decision 01 allows to be stale or absent; resume is the one
// path where continuing from a state the run never had is unrecoverable, because nothing
// downstream can tell. There is deliberately no field here through which that cached state
// could travel, so the resume path cannot read it even by accident — the same shape, and the
// same reason, as Projection on the write side.
type ResumePoint struct {
	RunID string
	// Step is the replayed state's own step count, not the row's. Both should say the same
	// thing; if they ever disagree, the one derived from the events is the true one.
	Step int
	// Frontier is the level to execute next. It comes from the journal whenever the journal
	// names one — see resumeFrontier for the case where only the row does.
	Frontier []string
	// State is Project(the run's events). Never the row's state.
	State     *graph.State
	GraphPath string
	Requester string
	Dossier   string
}

// ResumePoint rebuilds where runID stands by replaying its journal. It reports (_, false, nil)
// for a run nothing was ever recorded for, and fails loud rather than guessing for a run whose
// record cannot be replayed.
//
// The row is still read, for the provenance the journal does not carry — the graph path above
// all, which is what lets `resume <run-id>` take no graph argument — and for the next frontier
// in the one case the journal cannot name it. Its state is read by nothing.
func (s *SQLiteStore) ResumePoint(ctx context.Context, runID string) (ResumePoint, bool, error) {
	rec, ok, err := s.Latest(ctx, runID)
	if err != nil || !ok {
		return ResumePoint{}, false, err
	}
	events, err := s.Read(ctx, runID)
	if err != nil {
		return ResumePoint{}, false, err
	}
	if len(events) == 0 {
		return ResumePoint{}, false, fmt.Errorf(
			"checkpoint: run %q has a checkpoint at step %d but no events, so its state cannot be replayed"+
				" (it predates the journal); start it again rather than resuming it: %w",
			runID, rec.Step, ErrNoJournal)
	}
	replayed, err := projection.Replay(events)
	if err != nil {
		return ResumePoint{}, false, fmt.Errorf("checkpoint: replay run %q to resume it: %w", runID, err)
	}
	return ResumePoint{
		RunID:     runID,
		Step:      replayed.State.Step,
		Frontier:  resumeFrontier(replayed, rec.Frontier),
		State:     replayed.State,
		GraphPath: rec.GraphPath,
		Requester: rec.Requester,
		Dossier:   rec.Dossier,
	}, true, nil
}

// resumeFrontier decides which level a resumed run executes first.
//
// A level the journal opened and never closed is the level to restart: the engine aborts a
// level as a whole, so it contributed nothing to the replayed state, and no row was ever
// written for it — the row still names the frontier of the level before it, which would
// re-run a level that already ran.
//
// When every level closed, the journal names no frontier at all, and that is not a hole in
// it: the engine computes the next frontier from the routes of the branches it just
// combined, and a route's result is not an event. Re-evaluating the routes here was rejected
// — a route is called on the node's own branch, and after a fan-out is merged those branches
// no longer exist, so the answer would agree only for single-node levels. The row is the only
// record of that frontier, and taking it from there keeps the row a position rather than a
// state.
//
// A journal that records the run as finished overrides the row outright: a cache may lag
// behind the record, never the other way round.
func resumeFrontier(replayed projection.Replayed, cached []string) []string {
	switch {
	case replayed.OpenFrontier != nil:
		return replayed.OpenFrontier
	case replayed.ClosedBy == journal.KindRunFinished:
		return nil
	default:
		return cached
	}
}
