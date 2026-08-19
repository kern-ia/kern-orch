package cmd

import (
	"context"
	"strings"
	"testing"

	"github.com/yoann/kern-orch/internal/checkpoint"
	"github.com/yoann/kern-orch/internal/graph"
	"github.com/yoann/kern-orch/internal/journal"
	"github.com/yoann/kern-orch/internal/journal/projection"
)

// This file is the epic's central invariant, executed rather than commented: for a real
// engine run, Project(its journal) is the state the run actually ended with. It is asserted
// here — in internal/cmd — because this is the only package that holds all three pieces at
// once: the engine, the adapter that turns graph events into journal events, and the store
// they land in. A unit test over a hand-built event slice would prove the projection
// self-consistent while saying nothing about the emission sites, which is where a missing
// event actually comes from.
//
// The cases are spread across this file and freeze_journal_test.go (freeze, nudge) rather
// than gathered into one table: each one is a different graph shape with its own live-state
// precondition, and a table would hide precisely the thing that makes each case discriminate
// — see CONVENTIONS.md on table-driven tests.

// projectedState replays a run's stored journal and returns the state it describes.
func projectedState(t *testing.T, st *checkpoint.SQLiteStore, runID string) *graph.State {
	t.Helper()
	events, err := st.Read(context.Background(), runID)
	if err != nil {
		t.Fatalf("Read %q: %v", runID, err)
	}
	replayed, err := projection.Project(events)
	if err != nil {
		t.Fatalf("Project %q: %v", runID, err)
	}
	return replayed
}

// assertReplayEquivalent is the invariant itself. It compares the two states through their
// JSON encoding rather than field by field, for two reasons: State.MarshalJSON is the one
// place that already renders every part of it (data, zones, Step, Frozen), so a field added
// later is compared without this helper being touched; and the journal round-trips through
// JSON, so a value a node wrote as an int comes back as a float64 — encoding both sides
// makes the comparison about what the state holds rather than about which Go type carried it.
func assertReplayEquivalent(t *testing.T, st *checkpoint.SQLiteStore, runID string, live *graph.State) {
	t.Helper()
	got := recorderStateJSON(t, projectedState(t, st, runID))
	want := recorderStateJSON(t, live)
	if got != want {
		t.Fatalf("Project(journal of %q) = %s, want the live state %s", runID, got, want)
	}
}

// A chain of single-node levels replays exactly. Each of its levels closes under the replace
// rule, and the run carries the three things a replay has to reconstruct rather than guess:
// a key rewritten by a later level, a key that keeps its ephemeral zone label to the end, and
// a Step that counts nodes rather than levels.
//
// The other half of what replace guards — that a key the branch no longer holds stays gone —
// rides on the freeze cases in freeze_journal_test.go, since Freeze is the only way a node
// can remove a key from the state at all.
func TestASingleNodeFrontierRunReplaysExactly(t *testing.T) {
	g := graph.NewGraph()
	g.AddNode(graph.NewToolNode("collect", func(_ context.Context, s *graph.State) error {
		s.Set("dossier", "D-1")
		s.SetZoned(graph.ZoneEphemeral, "scratch", "raw pages")
		return nil
	}))
	g.AddNode(graph.NewToolNode("refine", func(_ context.Context, s *graph.State) error {
		s.Set("dossier", "D-1 (refined)")
		s.Set("score", 42)
		return nil
	}))
	g.SetEntry("collect")
	g.AddEdge("collect", graph.Static("refine"))

	s := graph.NewState()
	st := runThroughRecorder(t, g, s, nil)

	if v, _ := s.Get("dossier"); v != "D-1 (refined)" {
		t.Fatalf("live state dossier = %v, want the second level's rewrite", v)
	}
	if z := s.Zone("scratch"); z != graph.ZoneEphemeral {
		t.Fatalf("live state zone of scratch = %q, want %q", z, graph.ZoneEphemeral)
	}
	if s.Step != 2 {
		t.Fatalf("live state Step = %d, want 2 — one per executed node", s.Step)
	}
	assertReplayEquivalent(t, st, "run-1", s)
}

// A fan-out replays exactly, including who wins a key both branches wrote. The engine folds
// the branches in frontier order, so the last node of the frontier wins; the journal records
// that order in LevelClosed.Frontier and nowhere else. Both branches writing "owner" is what
// makes the case discriminate: a replay that folded them in any other order would rebuild a
// state the run never had, while still holding exactly the same key set.
func TestAFanOutRunReplaysExactly(t *testing.T) {
	g := graph.NewGraph()
	g.AddNode(graph.NewToolNode("split", func(_ context.Context, s *graph.State) error {
		s.Set("owner", "split")
		return nil
	}))
	g.AddNode(graph.NewToolNode("alpha", func(_ context.Context, s *graph.State) error {
		s.Set("owner", "alpha")
		s.Set("from-alpha", true)
		return nil
	}))
	g.AddNode(graph.NewToolNode("omega", func(_ context.Context, s *graph.State) error {
		s.Set("owner", "omega")
		s.SetZoned(graph.ZoneEphemeral, "from-omega", "scratch")
		return nil
	}))
	g.SetEntry("split")
	// Static sorts nothing; the engine sorts the next frontier itself, so the level runs
	// alpha before omega and omega is the branch that wins "owner".
	g.AddEdge("split", graph.Static("alpha", "omega"))

	s := graph.NewState()
	st := runThroughRecorder(t, g, s, nil)

	if v, _ := s.Get("owner"); v != "omega" {
		t.Fatalf("live state owner = %v, want omega — the last branch in frontier order", v)
	}
	if !s.Has("from-alpha") {
		t.Fatalf("live state lost alpha's own key, so the merge was not additive: %v", s.Keys())
	}
	assertReplayEquivalent(t, st, "run-1", s)
}

// An approval's answer is an ordinary state key (graph.DecisionKey), so it reaches the
// journal on the node-produced event like any other write, and a run that routes on it
// replays exactly. Nothing in the vocabulary knows what an approval is, and this is the test
// that says that is enough.
func TestAnApprovalRunReplaysExactly(t *testing.T) {
	g := graph.NewGraph()
	g.AddNode(graph.NewApprovalNode("confirm", func(_ context.Context, _ string) (graph.Decision, error) {
		return graph.Approved, nil
	}))
	g.AddNode(graph.NewToolNode("act", func(_ context.Context, s *graph.State) error {
		s.Set("acted", true)
		return nil
	}))
	g.AddNode(graph.NewToolNode("abort", func(_ context.Context, s *graph.State) error {
		s.Set("aborted", true)
		return nil
	}))
	g.SetEntry("confirm")
	g.AddEdge("confirm", graph.Conditional(func(s *graph.State) []string {
		if v, _ := s.Get(graph.DecisionKey("confirm")); v == string(graph.Approved) {
			return []string{"act"}
		}
		return []string{"abort"}
	}))

	s := graph.NewState()
	st := runThroughRecorder(t, g, s, nil)

	if v, _ := s.Get(graph.DecisionKey("confirm")); v != string(graph.Approved) {
		t.Fatalf("live state %s = %v, want %q", graph.DecisionKey("confirm"), v, graph.Approved)
	}
	if !s.Has("acted") {
		t.Fatalf("the run did not route on the decision: %v", s.Keys())
	}
	assertReplayEquivalent(t, st, "run-1", s)
}

// A run picked up again after a failure replays exactly, over the single journal both
// attempts wrote into. This is the case issue 12's reopen rule exists for: the second
// RunStarted reopens the record and drops the level the first attempt left open, and without
// that the resumed attempt's very first level would be refused at replay. The equivalence is
// what says the dropped level contributed nothing — the engine abandons a level as a whole,
// so the failed attempt's partial writes must be absent from the replayed state too.
func TestAResumedRunReplaysExactlyOverBothAttempts(t *testing.T) {
	fail := true
	newGraph := func() *graph.Graph {
		g := graph.NewGraph()
		g.AddNode(graph.NewToolNode("seed", func(_ context.Context, s *graph.State) error {
			s.Set("n", 3)
			return nil
		}))
		g.AddNode(graph.NewToolNode("flaky", func(_ context.Context, s *graph.State) error {
			// Written before the failure on purpose: the branch is dropped with the level,
			// so this key must be missing from the live state and from the replay alike.
			s.Set("half-done", true)
			if fail {
				return errNotYet
			}
			s.Set("done", true)
			return nil
		}))
		g.SetEntry("seed")
		g.AddEdge("seed", graph.Static("flaky"))
		return g
	}

	st := openRecorderStore(t)
	ctx := context.Background()
	live := graph.NewState()

	first := newRecorder(t, st, "run-1")
	firstEngine := graph.NewEngine(newGraph()).
		OnEvent(first.record).
		OnStep(checkpointHook(first, "/graphs/demo.yaml", "", ""))
	if err := firstEngine.Run(ctx, live); err == nil {
		t.Fatal("the first attempt succeeded, so there is no interrupted run to resume")
	}

	// Exactly what resume does: the journal, not the cached row, says where the run stands.
	events, err := st.Read(ctx, "run-1")
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	replayed, err := projection.Replay(events)
	if err != nil {
		t.Fatalf("Replay: %v", err)
	}
	if len(replayed.OpenFrontier) != 1 || replayed.OpenFrontier[0] != "flaky" {
		t.Fatalf("OpenFrontier = %v, want [flaky] — the level the failed attempt left open", replayed.OpenFrontier)
	}
	if replayed.State.Has("half-done") {
		t.Fatalf("the failed level's partial write reached the replayed state: %v", replayed.State.Keys())
	}

	fail = false
	live = replayed.State
	second := newRecorder(t, st, "run-1")
	secondEngine := graph.NewEngine(newGraph()).
		OnEvent(second.record).
		OnStep(checkpointHook(second, "/graphs/demo.yaml", "", ""))
	if err := secondEngine.RunFrom(ctx, live, replayed.OpenFrontier); err != nil {
		t.Fatalf("RunFrom: %v", err)
	}

	if !live.Has("done") {
		t.Fatalf("the resumed attempt did not finish the level it restarted: %v", live.Keys())
	}
	assertReplayEquivalent(t, st, "run-1", live)
}

// errNotYet is the failure the resumed run's first attempt returns. A package-level value
// rather than an inline fmt.Errorf so the two attempts share one identity for it.
var errNotYet = errNotYetType{}

type errNotYetType struct{}

func (errNotYetType) Error() string { return "not ready on the first attempt" }

// The case that must not be written as an equivalence: a Freeze inside a fan-out. Merge
// carries neither key removals nor the Frozen counter, so there is no state such a level
// could combine into — which is why the engine fails the level live (issue 06) instead of
// recording an event replay would later refuse. The invariant this asserts is therefore not
// "it replays exactly" but "the run is refused, and what the journal did record still
// replays": a run kept alive here would leave behind a journal only a replay could discover
// was broken.
func TestAFreezeInsideAFanOutIsRefusedAndLeavesAReplayableJournal(t *testing.T) {
	g := graph.NewGraph()
	g.AddNode(graph.NewToolNode("split", func(_ context.Context, s *graph.State) error {
		s.Set("goal", "ship")
		return nil
	}))
	g.AddNode(graph.NewToolNode("alpha", func(_ context.Context, s *graph.State) error {
		s.Set("from-alpha", true)
		return nil
	}))
	g.AddNode(graph.NewToolNode("omega", func(_ context.Context, s *graph.State) error {
		s.Freeze(nil)
		return nil
	}))
	g.SetEntry("split")
	g.AddEdge("split", graph.Static("alpha", "omega"))

	s := graph.NewState()
	st, err := recordRun(t, g, s, nil)
	if err == nil {
		t.Fatal("the run succeeded, although a node froze inside a fan-out level")
	}
	if !strings.Contains(err.Error(), "fan-out") {
		t.Fatalf("Run error = %v, want the fan-out freeze refusal", err)
	}

	events, readErr := st.Read(context.Background(), "run-1")
	if readErr != nil {
		t.Fatalf("Read: %v", readErr)
	}
	replayed, projErr := projection.Replay(events)
	if projErr != nil {
		t.Fatalf("Replay of the refused run's journal: %v — a run the engine refuses must still leave a journal that replays", projErr)
	}
	if replayed.ClosedBy != journal.KindRunFailed {
		t.Fatalf("ClosedBy = %q, want %q", replayed.ClosedBy, journal.KindRunFailed)
	}
	if replayed.State.Frozen != 0 {
		t.Fatalf("replayed Frozen = %d, want 0 — the refused level combined nothing", replayed.State.Frozen)
	}
}
