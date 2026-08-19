package cmd

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/yoann/kern-orch/internal/graph"
	"github.com/yoann/kern-orch/internal/report"
)

// wireBodySink records every JSON body posted to it, in arrival order — the whole payload,
// not just the state, because the equivalence test below compares every field the wire
// contract carries.
type wireBodySink struct {
	srv *httptest.Server
	mu  sync.Mutex
	got []map[string]any
}

func newWireBodySink(t *testing.T) *wireBodySink {
	t.Helper()
	s := &wireBodySink{}
	s.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		s.mu.Lock()
		s.got = append(s.got, body)
		s.mu.Unlock()
		w.WriteHeader(http.StatusAccepted)
	}))
	t.Cleanup(s.srv.Close)
	return s
}

func (s *wireBodySink) all() []map[string]any {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]map[string]any(nil), s.got...)
}

// buildEquivalenceGraph is a small two-level graph: seed writes a key on a single-node
// frontier (a replace level), then a fan-out of two nodes each write their own key (a merge
// level). Both combination rules are exercised so the equivalence test below is not only
// proven for the trivial case a single-node run would give it.
func buildEquivalenceGraph() *graph.Graph {
	seed := graph.NewToolNode("seed", func(_ context.Context, s *graph.State) error {
		s.Set("n", 1)
		return nil
	})
	left := graph.NewToolNode("left", func(_ context.Context, s *graph.State) error {
		s.Set("left", "L")
		return nil
	})
	right := graph.NewToolNode("right", func(_ context.Context, s *graph.State) error {
		s.Set("right", "R")
		return nil
	})
	return graph.NewGraph().
		SetEntry("seed").
		AddNode(seed).AddNode(left).AddNode(right).
		AddEdge("seed", graph.Static("left", "right"))
}

// TestReportedSequenceIsIdenticalBeforeAndAfterTheJournalRewiring is issue 11's central
// proof. internal/report itself is untouched by this issue — not one line under
// internal/report moved — so "before" and "after" cannot be two versions of Hook; they are
// the same Hook fed two different states: the engine's own live *graph.State (what every
// caller handed it before this issue, wired straight into OnStep) versus the journal's
// projection of the run (what reportHook, this issue's addition to internal/cmd, hands it
// now, chained after the checkpoint hook the same way serve.go wires it).
//
// It runs the same deterministic graph twice — once each way — rather than replaying one
// run's captured states after the fact: reportHook reads the journal *as it stands at the
// moment each level closes*, so it has to be driven in step with the engine, in the same
// hook chain a real run uses, or a later level's already-written events would leak into an
// earlier level's projection.
func TestReportedSequenceIsIdenticalBeforeAndAfterTheJournalRewiring(t *testing.T) {
	ctx := context.Background()
	topo := &report.Topology{Entry: "seed"}

	// Before: the reporter's hook wired straight into OnStep, exactly as every call site did
	// before this issue — the engine's own live state, nothing from the journal.
	before := newWireBodySink(t)
	beforeReporter := report.NewHTTP(before.srv.URL)
	beforeReporter.Requester = "yoann"
	beforeReporter.Dossier = "D-1"
	beforeEngine := graph.NewEngine(buildEquivalenceGraph()).
		OnStep(beforeReporter.Hook("run-equivalence", "equivalence", topo))
	if err := beforeEngine.Run(ctx, graph.NewState()); err != nil {
		t.Fatalf("before Run: %v", err)
	}
	beforeReporter.Flush()

	// After: checkpointHook writes the level's journal events first, then reportHook reads
	// them back and projects — the same order serve.go's run() chains them in.
	st := openRecorderStore(t)
	rec := newRecorder(t, st, "run-equivalence")
	after := newWireBodySink(t)
	afterReporter := report.NewHTTP(after.srv.URL)
	afterReporter.Requester = "yoann"
	afterReporter.Dossier = "D-1"
	afterHook := multiStep(
		checkpointHook(rec, "/graphs/equivalence.yaml", "", ""),
		reportHook(st, "run-equivalence", afterReporter.Hook("run-equivalence", "equivalence", topo)),
	)
	afterEngine := graph.NewEngine(buildEquivalenceGraph()).
		OnStep(afterHook).
		OnEvent(multiEvent(rec.record))
	if err := afterEngine.Run(ctx, graph.NewState()); err != nil {
		t.Fatalf("after Run: %v", err)
	}
	afterReporter.Flush()

	beforeBodies, afterBodies := before.all(), after.all()
	if len(beforeBodies) < 2 {
		t.Fatalf("the live-state path sent %d events, want at least 2 (the seed level and the fan-out level)", len(beforeBodies))
	}
	if len(beforeBodies) != len(afterBodies) {
		t.Fatalf("the live-state path sent %d events, the journal path sent %d", len(beforeBodies), len(afterBodies))
	}
	for i := range beforeBodies {
		// "at" is the one field expected to differ: it is the wall-clock moment each of the
		// two separate calls happened to run, not part of what the rewiring is supposed to
		// preserve.
		delete(beforeBodies[i], "at")
		delete(afterBodies[i], "at")
		bb, err := json.Marshal(beforeBodies[i])
		if err != nil {
			t.Fatalf("marshal before[%d]: %v", i, err)
		}
		ab, err := json.Marshal(afterBodies[i])
		if err != nil {
			t.Fatalf("marshal after[%d]: %v", i, err)
		}
		if string(bb) != string(ab) {
			t.Fatalf("event %d differs between the live-state path and the journal path:\n live-state = %s\n journal    = %s", i, bb, ab)
		}
	}
}
