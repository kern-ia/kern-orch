package cmd

import (
	"context"
	"strings"
	"testing"

	"github.com/yoann/kern-orch/internal/graph"
	"github.com/yoann/kern-orch/internal/journal"
)

// equivalenceCheckHook returning nil when disabled is what lets multiStep skip it entirely —
// see the "no additional projection work" tests below, which rely on this rather than on a
// hook that runs every level but does nothing.
func TestEquivalenceCheckHookIsNilWhenDisabled(t *testing.T) {
	if hook := equivalenceCheckHook(false, nil, "run-1"); hook != nil {
		t.Fatalf("equivalenceCheckHook(false, ...) = non-nil hook, want nil")
	}
}

func TestEquivalenceCheckHookIsNonNilWhenEnabled(t *testing.T) {
	st := openRecorderStore(t)
	if hook := equivalenceCheckHook(true, st, "run-1"); hook == nil {
		t.Fatalf("equivalenceCheckHook(true, ...) = nil, want a hook")
	}
}

// With the check enabled, a projection deliberately made to diverge from the live state is
// detected, and the error names the key that diverged — the acceptance criterion this issue
// sets. The corruption is proxied by handing the hook a live state the journal never actually
// produced, since the only way a real engine run diverges from its own journal is exactly the
// class of bug (an emission-site or projection bug) this check exists to catch, not something
// this test can trigger honestly through the engine.
func TestEquivalenceCheckHookDetectsACorruptedProjectionAndNamesTheDivergentKey(t *testing.T) {
	st := openRecorderStore(t)
	ctx := context.Background()
	rec := newRecorder(t, st, "run-1")

	g := graph.NewGraph()
	g.AddNode(graph.NewToolNode("collect", func(_ context.Context, s *graph.State) error {
		s.Set("dossier", "D-1")
		s.Set("score", 42)
		return nil
	}))
	g.SetEntry("collect")

	live := graph.NewState()
	eng := graph.NewEngine(g).OnEvent(rec.record).OnStep(checkpointHook(rec, "/graphs/demo.yaml", "", ""))
	if err := eng.Run(ctx, live); err != nil {
		t.Fatalf("Run: %v", err)
	}

	corrupted := live.Clone()
	corrupted.Set("dossier", "D-1-TAMPERED")

	hook := equivalenceCheckHook(true, st, "run-1")
	err := hook(ctx, graph.StepInfo{Step: 1}, corrupted)
	if err == nil {
		t.Fatalf("hook = nil error, want one naming the divergent key")
	}
	if !strings.Contains(err.Error(), "data.dossier") {
		t.Fatalf("error = %q, want it to name data.dossier", err.Error())
	}
	if strings.Contains(err.Error(), "data.score") {
		t.Fatalf("error = %q, want it to not name the key that did not diverge", err.Error())
	}
}

// A run that does not diverge from its journal passes the check silently — the complement of
// the corruption case above, proving the comparison does not merely always fail.
func TestEquivalenceCheckHookPassesAnUncorruptedRun(t *testing.T) {
	st := openRecorderStore(t)
	ctx := context.Background()
	rec := newRecorder(t, st, "run-1")

	g := graph.NewGraph()
	g.AddNode(graph.NewToolNode("collect", func(_ context.Context, s *graph.State) error {
		s.Set("dossier", "D-1")
		return nil
	}))
	g.SetEntry("collect")

	hook := multiStep(
		checkpointHook(rec, "/graphs/demo.yaml", "", ""),
		equivalenceCheckHook(true, st, "run-1"),
	)
	eng := graph.NewEngine(g).OnEvent(rec.record).OnStep(hook)
	if err := eng.Run(ctx, graph.NewState()); err != nil {
		t.Fatalf("Run: %v, want the equivalence check to pass silently", err)
	}
}

// With the check disabled, no additional projection work happens per level — asserted by
// instrumenting equivalenceProject's call count rather than by timing, over the same
// multiStep chain serve.go wires in production.
func TestDisabledEquivalenceCheckCallsProjectionZeroTimesPerLevel(t *testing.T) {
	calls := 0
	orig := equivalenceProject
	equivalenceProject = func(events []journal.Event) (*graph.State, error) {
		calls++
		return orig(events)
	}
	t.Cleanup(func() { equivalenceProject = orig })

	st := openRecorderStore(t)
	ctx := context.Background()
	rec := newRecorder(t, st, "run-1")

	g := graph.NewGraph()
	g.AddNode(graph.NewToolNode("collect", func(_ context.Context, s *graph.State) error {
		s.Set("dossier", "D-1")
		return nil
	}))
	g.SetEntry("collect")

	hook := multiStep(
		checkpointHook(rec, "/graphs/demo.yaml", "", ""),
		equivalenceCheckHook(false, st, "run-1"),
	)
	eng := graph.NewEngine(g).OnEvent(rec.record).OnStep(hook)
	if err := eng.Run(ctx, graph.NewState()); err != nil {
		t.Fatalf("Run: %v", err)
	}

	if calls != 0 {
		t.Fatalf("equivalenceProject calls = %d, want 0 when the check is disabled", calls)
	}
}

// The enabled counterpart: the same one-level run calls the projection exactly once, proving
// the zero above is because the check is off, not because nothing ever calls it.
func TestEnabledEquivalenceCheckCallsProjectionOncePerLevel(t *testing.T) {
	calls := 0
	orig := equivalenceProject
	equivalenceProject = func(events []journal.Event) (*graph.State, error) {
		calls++
		return orig(events)
	}
	t.Cleanup(func() { equivalenceProject = orig })

	st := openRecorderStore(t)
	ctx := context.Background()
	rec := newRecorder(t, st, "run-1")

	g := graph.NewGraph()
	g.AddNode(graph.NewToolNode("collect", func(_ context.Context, s *graph.State) error {
		s.Set("dossier", "D-1")
		return nil
	}))
	g.SetEntry("collect")

	hook := multiStep(
		checkpointHook(rec, "/graphs/demo.yaml", "", ""),
		equivalenceCheckHook(true, st, "run-1"),
	)
	eng := graph.NewEngine(g).OnEvent(rec.record).OnStep(hook)
	if err := eng.Run(ctx, graph.NewState()); err != nil {
		t.Fatalf("Run: %v", err)
	}

	if calls != 1 {
		t.Fatalf("equivalenceProject calls = %d, want 1 for a single-level run", calls)
	}
}
