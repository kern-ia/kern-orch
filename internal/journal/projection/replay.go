package projection

import (
	"github.com/yoann/kern-orch/internal/graph"
	"github.com/yoann/kern-orch/internal/journal"
)

// Replayed is everything one pass over a journal establishes: the state the events project
// to, the frontier of a level they leave open, and the event that closed the run if one did.
//
// Project returns only the state, which is all a consumer inspecting a finished run needs.
// Resume needs the position too, and getting it from a second pass — or worse, from the
// checkpoint row — would let the state and the frontier come from two different readings of
// the same journal. They come from one here.
type Replayed struct {
	// State is what the events project to, exactly as Project returns it.
	State *graph.State
	// OpenFrontier is the frontier of a level that was opened and never closed, and nil
	// when every level closed. It is the level to restart: the engine aborts a level as a
	// whole, so a level that did not close contributed nothing to State.
	//
	// A journal whose last level closed cleanly names no frontier here, and that is a fact
	// rather than a gap: the engine computes the next frontier from the routes of the
	// branches it just combined, and a route's result is not an event. Only the checkpoint
	// row carries it, which is why the row is still read for it — a position, not a state.
	OpenFrontier []string
	// ClosedBy is the kind of the terminal event the run ended on, or empty when the
	// journal carries none. Empty means the record simply stops, which is not the same
	// fact as a run that finished, and callers must be able to tell them apart.
	ClosedBy journal.Kind
}

// Replay projects the events and reports where they leave the run. It is Project plus the
// position, and Project is defined in terms of it so the two can never disagree.
func Replay(events []journal.Event) (Replayed, error) {
	p := &projector{shared: graph.NewState()}
	for i, ev := range events {
		if err := p.apply(i, ev); err != nil {
			return Replayed{}, err
		}
	}
	out := Replayed{State: p.shared, ClosedBy: p.closedBy}
	if p.level != nil {
		out.OpenFrontier = append([]string(nil), p.level.frontier...)
	}
	return out, nil
}
