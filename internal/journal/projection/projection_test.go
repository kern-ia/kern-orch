package projection

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/yoann/kern-orch/internal/graph"
	"github.com/yoann/kern-orch/internal/journal"
)

// seq hands out the monotonic sequence numbers a real journal would have assigned, so a
// test can describe a run as a readable list of payloads instead of hand-numbering every
// event and silently breaking the ordering rule Project enforces.
type seq struct{ n int64 }

func (s *seq) ev(p journal.Payload) journal.Event {
	s.n++
	return journal.Event{RunID: "run-1", Seq: s.n, At: time.Unix(1_700_000_000+s.n, 0).UTC(), Payload: p}
}

// stateJSON renders a state through its own MarshalJSON, which carries step, frozen, data
// and zones. Comparing that single string rather than field by field is what makes a zone
// label silently lost during projection a test failure instead of an unasserted detail.
func stateJSON(t *testing.T, s *graph.State) string {
	t.Helper()
	b, err := json.Marshal(s)
	if err != nil {
		t.Fatalf("marshal state: %v", err)
	}
	return string(b)
}

func TestProjectingNoEventsYieldsAnEmptyState(t *testing.T) {
	got, err := Project(nil)
	if err != nil {
		t.Fatalf("Project(nil) returned error %v, want nil", err)
	}
	if len(got.Keys()) != 0 {
		t.Fatalf("Project(nil) keys = %v, want none", got.Keys())
	}
	if got.Step != 0 || got.Frozen != 0 {
		t.Fatalf("Project(nil) step/frozen = %d/%d, want 0/0", got.Step, got.Frozen)
	}
}

func TestProjectingASingleNodeLevelYieldsTheBranchState(t *testing.T) {
	var s seq
	events := []journal.Event{
		s.ev(journal.RunStarted{Graph: "demo"}),
		s.ev(journal.LevelOpened{Frontier: []string{"collect"}}),
		s.ev(journal.NodeStarted{NodeID: "collect"}),
		s.ev(journal.NodeProduced{
			NodeID: "collect",
			Data:   map[string]any{"dossier": "D-1", "scratch": "notes"},
			Zones:  map[string]string{"scratch": graph.ZoneEphemeral},
		}),
		s.ev(journal.LevelClosed{Frontier: []string{"collect"}, Rule: journal.CombinationReplace}),
		s.ev(journal.RunFinished{}),
	}

	got, err := Project(events)
	if err != nil {
		t.Fatalf("Project returned error %v, want nil", err)
	}

	want := graph.NewState()
	want.Set("dossier", "D-1")
	want.SetZoned(graph.ZoneEphemeral, "scratch", "notes")
	want.Step = 1
	if stateJSON(t, got) != stateJSON(t, want) {
		t.Fatalf("Project state = %s, want %s", stateJSON(t, got), stateJSON(t, want))
	}
}

// A key the branch no longer holds must disappear from the shared state when the level's
// recorded rule is replace. Freeze is the only way State's exported API drops a key, so it
// is the only way a run can produce this shape — and a projection that merged the branch
// instead of adopting it would keep the dropped key alive forever.
func TestProjectingAReplaceLevelDropsKeysTheBranchNoLongerHolds(t *testing.T) {
	var s seq
	events := []journal.Event{
		s.ev(journal.LevelOpened{Frontier: []string{"seed"}}),
		s.ev(journal.NodeStarted{NodeID: "seed"}),
		s.ev(journal.NodeProduced{NodeID: "seed", Data: map[string]any{"keep": 1, "drop": 2}}),
		s.ev(journal.LevelClosed{Frontier: []string{"seed"}, Rule: journal.CombinationReplace}),
		s.ev(journal.LevelOpened{Frontier: []string{"compact"}}),
		s.ev(journal.NodeStarted{NodeID: "compact"}),
		s.ev(journal.FreezeApplied{CarriedOver: map[string]any{"keep": 1}, Dropped: []string{"drop"}}),
		s.ev(journal.LevelClosed{Frontier: []string{"compact"}, Rule: journal.CombinationReplace}),
	}

	got, err := Project(events)
	if err != nil {
		t.Fatalf("Project returned error %v, want nil", err)
	}
	if got.Has("drop") {
		t.Fatalf("Project state still holds %q, want it dropped by the replace", "drop")
	}
	if v, ok := got.Get("keep"); !ok || v != 1 {
		t.Fatalf("Project state keep = %v (present %t), want 1 (present true)", v, ok)
	}
	if got.Frozen != 1 {
		t.Fatalf("Project state frozen = %d, want 1", got.Frozen)
	}
}

// The engine folds a fan-out's branches in frontier order, so the last node of the frontier
// wins a contested key. The journal here emits "a" before "b" while the frontier lists "b"
// before "a": a projection folding in journal order would leave b's value behind.
func TestProjectingAFanOutLevelMergesEveryBranchInFrontierOrder(t *testing.T) {
	var s seq
	events := []journal.Event{
		s.ev(journal.LevelOpened{Frontier: []string{"b", "a"}}),
		s.ev(journal.NodeStarted{NodeID: "a"}),
		s.ev(journal.NodeStarted{NodeID: "b"}),
		s.ev(journal.NodeProduced{NodeID: "a", Data: map[string]any{"shared": "from-a", "only_a": 1}}),
		s.ev(journal.NodeProduced{NodeID: "b", Data: map[string]any{"shared": "from-b", "only_b": 2}}),
		s.ev(journal.LevelClosed{Frontier: []string{"b", "a"}, Rule: journal.CombinationMerge}),
	}

	got, err := Project(events)
	if err != nil {
		t.Fatalf("Project returned error %v, want nil", err)
	}

	want := graph.NewState()
	want.Set("only_b", 2)
	want.Set("only_a", 1)
	want.Set("shared", "from-a")
	want.Step = 2
	if stateJSON(t, got) != stateJSON(t, want) {
		t.Fatalf("Project state = %s, want %s", stateJSON(t, got), stateJSON(t, want))
	}
}

// A freeze inside a fan-out branch is lost by the engine (Merge carries neither deletions
// nor the Frozen counter), so a projection must not represent one. Refusing it is honest;
// applying it to one arbitrary branch would invent a state the run never had.
func TestProjectingRejectsAFreezeInsideAFanOutLevel(t *testing.T) {
	var s seq
	events := []journal.Event{
		s.ev(journal.LevelOpened{Frontier: []string{"a", "b"}}),
		s.ev(journal.FreezeApplied{CarriedOver: map[string]any{"keep": 1}}),
	}

	_, err := Project(events)
	if err == nil {
		t.Fatalf("Project returned nil error, want a freeze-inside-fan-out error")
	}
	if !strings.Contains(err.Error(), "freeze") {
		t.Fatalf("Project error = %q, want it to name the freeze", err.Error())
	}
}

// The acceptance criterion the epic's Notes single out: State.Freeze replaces the state's
// contents wholesale, so the projection is checked against a real graph.State frozen with a
// carry-over that is NOT DefaultCarryOver. This carry-over does three things a delta-shaped
// projection ("keep everything except Dropped") cannot reproduce: it keeps an EPHEMERAL key
// (which the default would have dropped, and whose zone label must be reset to persistent),
// it drops a PERSISTENT key, and it synthesises a key that never existed in the prior state.
func TestProjectingAFreezeWithANonDefaultCarryOverYieldsExactlyWhatItKept(t *testing.T) {
	// The state the run really held when the freeze happened, rebuilt with graph's own API.
	live := graph.NewState()
	live.Set("dossier", "D-1")
	live.Set("bulky", "a very long transcript")
	live.SetZoned(graph.ZoneEphemeral, "scratch", "notes")
	live.Step = 1

	carry := func(s *graph.State) map[string]any {
		scratch, _ := s.Get("scratch")
		return map[string]any{"scratch": scratch, "summary": "1 dossier"}
	}

	want := live.Clone()
	want.Freeze(carry)
	if want.Zone("scratch") != graph.ZonePersistent {
		t.Fatalf("graph.Freeze left scratch in zone %q, want %q", want.Zone("scratch"), graph.ZonePersistent)
	}

	var s seq
	events := []journal.Event{
		s.ev(journal.LevelOpened{Frontier: []string{"seed"}}),
		s.ev(journal.NodeStarted{NodeID: "seed"}),
		s.ev(journal.NodeProduced{
			NodeID: "seed",
			Data:   map[string]any{"dossier": "D-1", "bulky": "a very long transcript", "scratch": "notes"},
			Zones:  map[string]string{"scratch": graph.ZoneEphemeral},
		}),
		s.ev(journal.LevelClosed{Frontier: []string{"seed"}, Rule: journal.CombinationReplace}),
		s.ev(journal.FreezeApplied{
			CarriedOver: carry(live),
			Dropped:     []string{"bulky", "dossier"},
		}),
	}

	got, err := Project(events)
	if err != nil {
		t.Fatalf("Project returned error %v, want nil", err)
	}
	if stateJSON(t, got) != stateJSON(t, want) {
		t.Fatalf("Project state = %s, want %s", stateJSON(t, got), stateJSON(t, want))
	}
}

func TestProjectingAFreezeUnderTheDefaultCarryOverDropsTheEphemeralZone(t *testing.T) {
	live := graph.NewState()
	live.Set("dossier", "D-1")
	live.SetZoned(graph.ZoneEphemeral, "scratch", "notes")
	live.Step = 1

	want := live.Clone()
	want.Freeze(graph.DefaultCarryOver)

	var s seq
	events := []journal.Event{
		s.ev(journal.LevelOpened{Frontier: []string{"seed"}}),
		s.ev(journal.NodeStarted{NodeID: "seed"}),
		s.ev(journal.NodeProduced{
			NodeID: "seed",
			Data:   map[string]any{"dossier": "D-1", "scratch": "notes"},
			Zones:  map[string]string{"scratch": graph.ZoneEphemeral},
		}),
		s.ev(journal.LevelClosed{Frontier: []string{"seed"}, Rule: journal.CombinationReplace}),
		s.ev(journal.FreezeApplied{
			CarriedOver: graph.DefaultCarryOver(live),
			Dropped:     []string{"scratch"},
		}),
	}

	got, err := Project(events)
	if err != nil {
		t.Fatalf("Project returned error %v, want nil", err)
	}
	if stateJSON(t, got) != stateJSON(t, want) {
		t.Fatalf("Project state = %s, want %s", stateJSON(t, got), stateJSON(t, want))
	}
}

func TestProjectingANudgeYieldsTheNudgedValue(t *testing.T) {
	var s seq
	events := []journal.Event{
		s.ev(journal.LevelOpened{Frontier: []string{"seed"}}),
		s.ev(journal.NodeStarted{NodeID: "seed"}),
		s.ev(journal.NodeProduced{NodeID: "seed", Data: map[string]any{"instruction": "original"}}),
		s.ev(journal.LevelClosed{Frontier: []string{"seed"}, Rule: journal.CombinationReplace}),
		s.ev(journal.NudgeApplied{Origin: "steering", Data: map[string]any{"instruction": "nudged", "priority": "high"}}),
	}

	got, err := Project(events)
	if err != nil {
		t.Fatalf("Project returned error %v, want nil", err)
	}
	if v, _ := got.Get("instruction"); v != "nudged" {
		t.Fatalf("Project state instruction = %v, want %q", v, "nudged")
	}
	if v, _ := got.Get("priority"); v != "high" {
		t.Fatalf("Project state priority = %v, want %q", v, "high")
	}
}

// A nudge is a non-node mutation of the shared state, applied by the engine between two
// levels. Inside an open level the branches were already cloned, so there is no state it
// could truthfully apply to.
func TestProjectingRejectsANudgeInsideAnOpenLevel(t *testing.T) {
	var s seq
	events := []journal.Event{
		s.ev(journal.LevelOpened{Frontier: []string{"seed"}}),
		s.ev(journal.NudgeApplied{Origin: "steering", Data: map[string]any{"k": 1}}),
	}

	_, err := Project(events)
	if err == nil {
		t.Fatalf("Project returned nil error, want a nudge-inside-open-level error")
	}
	if !strings.Contains(err.Error(), "nudge") {
		t.Fatalf("Project error = %q, want it to name the nudge", err.Error())
	}
}

func TestZonesAndStepSurviveProjection(t *testing.T) {
	var s seq
	events := []journal.Event{
		s.ev(journal.LevelOpened{Frontier: []string{"seed"}}),
		s.ev(journal.NodeStarted{NodeID: "seed"}),
		s.ev(journal.NodeProduced{
			NodeID: "seed",
			Data:   map[string]any{"kept": "k", "scratch": "s"},
			Zones:  map[string]string{"scratch": graph.ZoneEphemeral},
		}),
		s.ev(journal.LevelClosed{Frontier: []string{"seed"}, Rule: journal.CombinationReplace}),
		s.ev(journal.LevelOpened{Frontier: []string{"left", "right"}}),
		s.ev(journal.NodeStarted{NodeID: "left"}),
		s.ev(journal.NodeStarted{NodeID: "right"}),
		s.ev(journal.NodeProduced{
			NodeID: "left",
			Data:   map[string]any{"draft": "d"},
			Zones:  map[string]string{"draft": graph.ZoneEphemeral},
		}),
		s.ev(journal.NodeProduced{NodeID: "right", Data: map[string]any{"final": "f"}}),
		s.ev(journal.LevelClosed{Frontier: []string{"left", "right"}, Rule: journal.CombinationMerge}),
	}

	got, err := Project(events)
	if err != nil {
		t.Fatalf("Project returned error %v, want nil", err)
	}
	if got.Zone("scratch") != graph.ZoneEphemeral {
		t.Fatalf("Project zone of scratch = %q, want %q", got.Zone("scratch"), graph.ZoneEphemeral)
	}
	if got.Zone("draft") != graph.ZoneEphemeral {
		t.Fatalf("Project zone of draft = %q, want %q", got.Zone("draft"), graph.ZoneEphemeral)
	}
	if got.Zone("kept") != graph.ZonePersistent {
		t.Fatalf("Project zone of kept = %q, want %q", got.Zone("kept"), graph.ZonePersistent)
	}
	// One step per node of every closed level, exactly as Engine.runLevel counts them.
	if got.Step != 3 {
		t.Fatalf("Project step = %d, want 3", got.Step)
	}
}

// An interrupted run's last level never closed, so its branches were never folded back. The
// projection stops at the last combined state — closing that tail is issue 08's job, not
// this function's, and inventing a combination here would pre-empt it.
func TestProjectingAnInterruptedTailKeepsTheLastCombinedState(t *testing.T) {
	var s seq
	events := []journal.Event{
		s.ev(journal.LevelOpened{Frontier: []string{"seed"}}),
		s.ev(journal.NodeStarted{NodeID: "seed"}),
		s.ev(journal.NodeProduced{NodeID: "seed", Data: map[string]any{"done": true}}),
		s.ev(journal.LevelClosed{Frontier: []string{"seed"}, Rule: journal.CombinationReplace}),
		s.ev(journal.LevelOpened{Frontier: []string{"next"}}),
		s.ev(journal.NodeStarted{NodeID: "next"}),
		s.ev(journal.NodeProduced{NodeID: "next", Data: map[string]any{"partial": true}}),
		s.ev(journal.RunInterrupted{Reason: "process killed"}),
	}

	got, err := Project(events)
	if err != nil {
		t.Fatalf("Project returned error %v, want nil", err)
	}
	if got.Has("partial") {
		t.Fatalf("Project state holds %q, want the uncombined level to be ignored", "partial")
	}
	if !got.Has("done") {
		t.Fatalf("Project state lacks %q, want the last combined level to survive", "done")
	}
	if got.Step != 1 {
		t.Fatalf("Project step = %d, want 1", got.Step)
	}
}

func TestProjectingRejectsALevelClosedThatWasNeverOpened(t *testing.T) {
	var s seq
	events := []journal.Event{
		s.ev(journal.LevelClosed{Frontier: []string{"seed"}, Rule: journal.CombinationReplace}),
	}

	_, err := Project(events)
	if err == nil {
		t.Fatalf("Project returned nil error, want a level-never-opened error")
	}
	if !strings.Contains(err.Error(), "never opened") {
		t.Fatalf("Project error = %q, want it to say the level was never opened", err.Error())
	}
}

func TestProjectingRejectsANodeProducedThatNeverStarted(t *testing.T) {
	var s seq
	events := []journal.Event{
		s.ev(journal.LevelOpened{Frontier: []string{"seed"}}),
		s.ev(journal.NodeProduced{NodeID: "seed", Data: map[string]any{"k": 1}}),
	}

	_, err := Project(events)
	if err == nil {
		t.Fatalf("Project returned nil error, want a produced-before-started error")
	}
	if !strings.Contains(err.Error(), "seed") {
		t.Fatalf("Project error = %q, want it to name node %q", err.Error(), "seed")
	}
}

func TestProjectingRejectsANodeOutsideTheOpenFrontier(t *testing.T) {
	var s seq
	events := []journal.Event{
		s.ev(journal.LevelOpened{Frontier: []string{"seed"}}),
		s.ev(journal.NodeStarted{NodeID: "intruder"}),
	}

	_, err := Project(events)
	if err == nil {
		t.Fatalf("Project returned nil error, want an unknown-node error")
	}
	if !strings.Contains(err.Error(), "intruder") {
		t.Fatalf("Project error = %q, want it to name node %q", err.Error(), "intruder")
	}
}

func TestProjectingRejectsOutOfOrderSequenceNumbers(t *testing.T) {
	events := []journal.Event{
		{RunID: "run-1", Seq: 7, Payload: journal.RunStarted{Graph: "demo"}},
		{RunID: "run-1", Seq: 3, Payload: journal.RunFinished{}},
	}

	_, err := Project(events)
	if err == nil {
		t.Fatalf("Project returned nil error, want an out-of-order error")
	}
	if !strings.Contains(err.Error(), "seq") {
		t.Fatalf("Project error = %q, want it to name the sequence number", err.Error())
	}
}

func TestProjectingRejectsEventsFromMoreThanOneRun(t *testing.T) {
	events := []journal.Event{
		{RunID: "run-1", Seq: 1, Payload: journal.RunStarted{Graph: "demo"}},
		{RunID: "run-2", Seq: 2, Payload: journal.RunFinished{}},
	}

	_, err := Project(events)
	if err == nil {
		t.Fatalf("Project returned nil error, want a mixed-run error")
	}
	if !strings.Contains(err.Error(), "run-2") {
		t.Fatalf("Project error = %q, want it to name run %q", err.Error(), "run-2")
	}
}

func TestProjectingRejectsALevelOpenedWhileAnotherIsOpen(t *testing.T) {
	var s seq
	events := []journal.Event{
		s.ev(journal.LevelOpened{Frontier: []string{"a"}}),
		s.ev(journal.LevelOpened{Frontier: []string{"b"}}),
	}

	_, err := Project(events)
	if err == nil {
		t.Fatalf("Project returned nil error, want a level-already-open error")
	}
	if !strings.Contains(err.Error(), "still open") {
		t.Fatalf("Project error = %q, want it to say a level is still open", err.Error())
	}
}

func TestProjectingRejectsAFrontierThatChangedBetweenOpenAndClose(t *testing.T) {
	var s seq
	events := []journal.Event{
		s.ev(journal.LevelOpened{Frontier: []string{"a", "b"}}),
		s.ev(journal.LevelClosed{Frontier: []string{"a"}, Rule: journal.CombinationReplace}),
	}

	_, err := Project(events)
	if err == nil {
		t.Fatalf("Project returned nil error, want a frontier-mismatch error")
	}
	if !strings.Contains(err.Error(), "frontier") {
		t.Fatalf("Project error = %q, want it to name the frontier", err.Error())
	}
}

// The rule is read off the event, never inferred from the node count — but a replace over
// more than one branch has no defined meaning, so it is refused rather than resolved by
// picking a branch. This is the one place the two numbers are compared, and it fails loud.
func TestProjectingRejectsAReplaceRuleOverMoreThanOneBranch(t *testing.T) {
	var s seq
	events := []journal.Event{
		s.ev(journal.LevelOpened{Frontier: []string{"a", "b"}}),
		s.ev(journal.LevelClosed{Frontier: []string{"a", "b"}, Rule: journal.CombinationReplace}),
	}

	_, err := Project(events)
	if err == nil {
		t.Fatalf("Project returned nil error, want a replace-over-many-branches error")
	}
	if !strings.Contains(err.Error(), string(journal.CombinationReplace)) {
		t.Fatalf("Project error = %q, want it to name the %q rule", err.Error(), journal.CombinationReplace)
	}
}

func TestProjectingRejectsAnUnknownCombinationRule(t *testing.T) {
	var s seq
	events := []journal.Event{
		s.ev(journal.LevelOpened{Frontier: []string{"a"}}),
		s.ev(journal.LevelClosed{Frontier: []string{"a"}, Rule: journal.CombinationRule("interleave")}),
	}

	_, err := Project(events)
	if err == nil {
		t.Fatalf("Project returned nil error, want an unknown-rule error")
	}
	if !strings.Contains(err.Error(), "interleave") {
		t.Fatalf("Project error = %q, want it to name the unknown rule", err.Error())
	}
}

func TestProjectingRejectsEventsAfterTheRunWasClosed(t *testing.T) {
	var s seq
	events := []journal.Event{
		s.ev(journal.RunFinished{}),
		s.ev(journal.LevelOpened{Frontier: []string{"a"}}),
	}

	_, err := Project(events)
	if err == nil {
		t.Fatalf("Project returned nil error, want a run-already-closed error")
	}
	if !strings.Contains(err.Error(), "closed") {
		t.Fatalf("Project error = %q, want it to say the run was already closed", err.Error())
	}
}

// A failed node's branch is discarded by the engine, which aborts the level. Should a level
// still close after one, the failed branch must not contribute keys the run never adopted.
func TestProjectingExcludesAFailedNodesBranchFromTheCombination(t *testing.T) {
	var s seq
	events := []journal.Event{
		s.ev(journal.LevelOpened{Frontier: []string{"ok", "broken"}}),
		s.ev(journal.NodeStarted{NodeID: "ok"}),
		s.ev(journal.NodeStarted{NodeID: "broken"}),
		s.ev(journal.NodeProduced{NodeID: "ok", Data: map[string]any{"good": 1}}),
		s.ev(journal.NodeProduced{NodeID: "broken", Data: map[string]any{"partial": 1}}),
		s.ev(journal.NodeFailed{NodeID: "broken", Message: "boom"}),
		s.ev(journal.LevelClosed{Frontier: []string{"ok", "broken"}, Rule: journal.CombinationMerge}),
	}

	got, err := Project(events)
	if err != nil {
		t.Fatalf("Project returned error %v, want nil", err)
	}
	if got.Has("partial") {
		t.Fatalf("Project state holds %q, want the failed branch discarded", "partial")
	}
	if !got.Has("good") {
		t.Fatalf("Project state lacks %q, want the successful branch merged", "good")
	}
}

func TestProjectDoesNotMutateTheEventsItReplays(t *testing.T) {
	produced := journal.NodeProduced{NodeID: "seed", Data: map[string]any{"k": "v"}}
	var s seq
	events := []journal.Event{
		s.ev(journal.LevelOpened{Frontier: []string{"seed"}}),
		s.ev(journal.NodeStarted{NodeID: "seed"}),
		s.ev(produced),
		s.ev(journal.LevelClosed{Frontier: []string{"seed"}, Rule: journal.CombinationReplace}),
	}

	got, err := Project(events)
	if err != nil {
		t.Fatalf("Project returned error %v, want nil", err)
	}
	got.Set("k", "mutated")
	if produced.Data["k"] != "v" {
		t.Fatalf("event data k = %v after mutating the projection, want %q", produced.Data["k"], "v")
	}
}
