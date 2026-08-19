package cmd

import (
	"context"
	"testing"

	"github.com/yoann/kern-orch/internal/checkpoint"
	"github.com/yoann/kern-orch/internal/graph"
	"github.com/yoann/kern-orch/internal/steer"
)

// runThroughRecorder wires a graph.Engine exactly the way a real run does — OnEvent feeding
// the journal recorder, OnStep closing each level's row, OnBeforeLevel draining any queued
// nudge — and returns the store the run wrote to. This is the seam issue 07's drift record
// used to prove nudge was lost before it went through the recorder; the same seam is what
// this issue's acceptance criteria need proven for freeze.
//
// It fails the test when the run does. A case that expects the engine to refuse the run
// calls recordRun instead, so that refusal stays observable rather than fatal.
func runThroughRecorder(t *testing.T, g *graph.Graph, s *graph.State, mailbox *steer.Mailbox) *checkpoint.SQLiteStore {
	t.Helper()
	st, err := recordRun(t, g, s, mailbox)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	return st
}

// recordRun is runThroughRecorder without the verdict: it hands back the run's error rather
// than failing on it, because "the engine refuses this run" is itself one of the facts the
// equivalence suite asserts (see the freeze-inside-a-fan-out case).
func recordRun(t *testing.T, g *graph.Graph, s *graph.State, mailbox *steer.Mailbox) (*checkpoint.SQLiteStore, error) {
	t.Helper()
	st := openRecorderStore(t)
	ctx := context.Background()
	rec := newRecorder(t, st, "run-1")

	eng := graph.NewEngine(g).
		OnEvent(rec.record).
		OnStep(checkpointHook(rec, "/graphs/demo.yaml", "", ""))
	if mailbox != nil {
		// Mirrors serve.go's own wiring (OnBeforeLevel -> mailbox.DrainNudges ->
		// recorder.recordNudge) without importing serve.go's HTTP surface: what this test
		// needs is that exact seam, not the daemon around it.
		eng.OnBeforeLevel(func(ctx context.Context, s *graph.State) error {
			return rec.recordNudge(ctx, mailbox.DrainNudges(s))
		})
	}
	return st, eng.Run(ctx, s)
}

// A run that freezes with the default carry-over — drops ephemeral, keeps persistent —
// replays to exactly the state the live run ended with.
func TestAFreezeWithTheDefaultCarryOverReplaysExactly(t *testing.T) {
	g := graph.NewGraph()
	g.AddNode(graph.NewToolNode("work", func(_ context.Context, s *graph.State) error {
		s.Set("goal", "ship")
		s.SetZoned(graph.ZoneEphemeral, "scratch", "temp notes")
		return nil
	}))
	g.AddNode(graph.NewToolNode("freeze", func(_ context.Context, s *graph.State) error {
		s.Freeze(nil)
		return nil
	}))
	g.SetEntry("work")
	g.AddEdge("work", graph.Static("freeze"))

	s := graph.NewState()
	st := runThroughRecorder(t, g, s, nil)

	if s.Has("scratch") {
		t.Fatalf("live state kept the ephemeral key past the freeze: %v", s.Keys())
	}
	assertReplayEquivalent(t, st, "run-1", s)
}

// The acceptance criterion the epic's Notes single out by name: a run that freezes with a
// carry-over other than DefaultCarryOver still replays to exactly the live state. A carry
// that only drops-the-rest-keep-persistent would pass by coincidence even under a delta
// model (it agrees with the default); this one also renames a key's value and invents one
// the prior state never had, so only "install CarriedOver wholesale" can reproduce it.
func TestAFreezeWithANonDefaultCarryOverReplaysExactly(t *testing.T) {
	nonDefaultCarry := func(st *graph.State) map[string]any {
		return map[string]any{
			// Rewritten from what the state actually held ("draft"), not passed through.
			"status": "finalized",
			// Synthesized: never a key in the state the node started with.
			"digest": "checksum-123",
			// Kept even though it is ephemeral — DefaultCarryOver would have dropped it.
			"scratch": "kept on purpose",
		}
	}
	g := graph.NewGraph()
	g.AddNode(graph.NewToolNode("work", func(_ context.Context, s *graph.State) error {
		s.Set("status", "draft")
		s.Set("irrelevant", "dropped by the custom carry")
		s.SetZoned(graph.ZoneEphemeral, "scratch", "buffer")
		return nil
	}))
	g.AddNode(graph.NewToolNode("freeze", func(_ context.Context, s *graph.State) error {
		s.Freeze(nonDefaultCarry)
		return nil
	}))
	g.SetEntry("work")
	g.AddEdge("work", graph.Static("freeze"))

	s := graph.NewState()
	st := runThroughRecorder(t, g, s, nil)

	if v, _ := s.Get("status"); v != "finalized" {
		t.Fatalf("live state status = %v, want finalized", v)
	}
	if v, _ := s.Get("digest"); v != "checksum-123" {
		t.Fatalf("live state digest = %v, want checksum-123", v)
	}
	if s.Has("irrelevant") {
		t.Fatalf("live state kept a key the custom carry-over never named: %v", s.Keys())
	}
	assertReplayEquivalent(t, st, "run-1", s)
}

// A run that was nudged between two levels replays to exactly the live state — the
// acceptance criterion this issue inherits from issue 07's drift fix, now proven through the
// same engine+recorder seam the freeze tests above use rather than through recordNudge alone.
func TestARunThatWasNudgedReplaysExactly(t *testing.T) {
	g := graph.NewGraph()
	g.AddNode(graph.NewToolNode("collect", func(_ context.Context, s *graph.State) error {
		s.Set("dossier", "D-1")
		return nil
	}))
	g.AddNode(graph.NewToolNode("refine", func(_ context.Context, s *graph.State) error {
		return nil
	}))
	g.SetEntry("collect")
	g.AddEdge("collect", graph.Static("refine"))

	mailbox := steer.NewMailbox(nil)
	mailbox.Nudge("probe", "hello")

	s := graph.NewState()
	st := runThroughRecorder(t, g, s, mailbox)

	if v, _ := s.Get("probe"); v != "hello" {
		t.Fatalf("live state probe = %v, want hello — the nudge never reached the run", v)
	}
	assertReplayEquivalent(t, st, "run-1", s)
}
