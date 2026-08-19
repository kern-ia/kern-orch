package projection

import (
	"testing"

	"github.com/yoann/kern-orch/internal/graph"
	"github.com/yoann/kern-orch/internal/journal"
)

// A run that failed inside a level left that level opened and never closed. That frontier is
// the one resume has to restart, and it is written in the journal — nowhere else, since no
// checkpoint row is ever written for a level that did not close.
func TestReplayNamesTheFrontierOfALevelThatWasOpenedAndNeverClosed(t *testing.T) {
	var s seq
	got, err := Replay([]journal.Event{
		s.ev(journal.RunStarted{Graph: "demo"}),
		s.ev(journal.LevelOpened{Frontier: []string{"seed"}}),
		s.ev(journal.NodeStarted{NodeID: "seed"}),
		s.ev(journal.NodeProduced{NodeID: "seed", Data: map[string]any{"n": 3}}),
		s.ev(journal.LevelClosed{Frontier: []string{"seed"}, Rule: journal.CombinationReplace}),
		s.ev(journal.LevelOpened{Frontier: []string{"confirm"}}),
		s.ev(journal.NodeStarted{NodeID: "confirm"}),
		s.ev(journal.NodeFailed{NodeID: "confirm", Message: "context canceled"}),
		s.ev(journal.RunFailed{Message: "context canceled", Nodes: []string{"confirm"}}),
	})
	if err != nil {
		t.Fatalf("Replay returned error %v, want nil", err)
	}
	if len(got.OpenFrontier) != 1 || got.OpenFrontier[0] != "confirm" {
		t.Fatalf("OpenFrontier = %v, want [confirm]", got.OpenFrontier)
	}
	if got.ClosedBy != journal.KindRunFailed {
		t.Fatalf("ClosedBy = %q, want %q", got.ClosedBy, journal.KindRunFailed)
	}
	if v, _ := got.State.Get("n"); v != 3 {
		t.Fatalf("replayed state n = %v, want 3", v)
	}
}

// A journal whose last level closed names no frontier to restart: the engine computes the
// next frontier from the routes of the branches it just combined, and those routes are not
// in the journal. Answering the last closed level's own frontier would re-run a level that
// already ran, which is the one wrong answer that looks plausible.
func TestReplayNamesNoOpenFrontierWhenEveryLevelClosed(t *testing.T) {
	var s seq
	got, err := Replay([]journal.Event{
		s.ev(journal.RunStarted{Graph: "demo"}),
		s.ev(journal.LevelOpened{Frontier: []string{"seed"}}),
		s.ev(journal.NodeStarted{NodeID: "seed"}),
		s.ev(journal.LevelClosed{Frontier: []string{"seed"}, Rule: journal.CombinationReplace}),
	})
	if err != nil {
		t.Fatalf("Replay returned error %v, want nil", err)
	}
	if got.OpenFrontier != nil {
		t.Fatalf("OpenFrontier = %v, want nil", got.OpenFrontier)
	}
	if got.ClosedBy != "" {
		t.Fatalf("ClosedBy = %q, want empty — no terminal event was recorded", got.ClosedBy)
	}
}

// The state and the position come out of one traversal, so a caller cannot get a state from
// one reading of the journal and a frontier from another.
func TestReplayAndProjectAgreeOnTheState(t *testing.T) {
	var s seq
	events := []journal.Event{
		s.ev(journal.LevelOpened{Frontier: []string{"a", "b"}}),
		s.ev(journal.NodeStarted{NodeID: "a"}),
		s.ev(journal.NodeProduced{NodeID: "a", Data: map[string]any{"x": 1}}),
		s.ev(journal.NodeStarted{NodeID: "b"}),
		s.ev(journal.NodeProduced{NodeID: "b", Data: map[string]any{"y": 2},
			Zones: map[string]string{"y": graph.ZoneEphemeral}}),
		s.ev(journal.LevelClosed{Frontier: []string{"a", "b"}, Rule: journal.CombinationMerge}),
		s.ev(journal.RunFinished{}),
	}
	replayed, err := Replay(events)
	if err != nil {
		t.Fatalf("Replay returned error %v, want nil", err)
	}
	projected, err := Project(events)
	if err != nil {
		t.Fatalf("Project returned error %v, want nil", err)
	}
	if stateJSON(t, replayed.State) != stateJSON(t, projected) {
		t.Fatalf("Replay state = %s, Project state = %s; want identical",
			stateJSON(t, replayed.State), stateJSON(t, projected))
	}
	if replayed.ClosedBy != journal.KindRunFinished {
		t.Fatalf("ClosedBy = %q, want %q", replayed.ClosedBy, journal.KindRunFinished)
	}
}
