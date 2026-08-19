package cmd

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/yoann/kern-orch/internal/checkpoint"
	"github.com/yoann/kern-orch/internal/config"
	"github.com/yoann/kern-orch/internal/graph"
	"github.com/yoann/kern-orch/internal/journal"
)

// seedConfirmGraph produces state (n=3) in its first level and then parks on an approval, so
// a run can be interrupted with one level's worth of state already recorded and carried on
// later. A graph that produced nothing would let a resume from an empty state pass.
const seedConfirmGraph = `
entry: seed
nodes:
  - id: seed
    type: tool
    func: seed
  - id: confirm
    type: approval
  - id: approved
    type: tool
    func: noop
  - id: refused
    type: tool
    func: noop
edges:
  - from: seed
    to: [confirm]
  - from: confirm
    router: onConfirmDecision
`

// stepSink collects what the reporter posts after every level. That payload carries the
// *live* state the engine is running with, which is the only way to observe what resume
// actually continued from — the checkpoint rows written afterwards are reprojections of the
// journal and would look right even if the engine had resumed from a corrupted row.
type stepSink struct {
	srv *httptest.Server
	mu  sync.Mutex
	got []map[string]any
}

func newStepSink(t *testing.T) *stepSink {
	t.Helper()
	s := &stepSink{}
	s.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var ev struct {
			State map[string]any `json:"state"`
		}
		_ = json.NewDecoder(r.Body).Decode(&ev)
		s.mu.Lock()
		s.got = append(s.got, ev.State)
		s.mu.Unlock()
		w.WriteHeader(http.StatusAccepted)
	}))
	t.Cleanup(s.srv.Close)
	return s
}

func (s *stepSink) states() []map[string]any {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]map[string]any(nil), s.got...)
}

// waitForFrontier blocks until the run's row names the given frontier, which is how a test
// knows a level closed without racing the engine.
func waitForFrontier(t *testing.T, store *checkpoint.SQLiteStore, runID, node string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		rec, ok, err := store.Latest(context.Background(), runID)
		if err != nil {
			t.Fatalf("Latest: %v", err)
		}
		if ok && len(rec.Frontier) == 1 && rec.Frontier[0] == node {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("run %s never reached frontier [%s]", runID, node)
}

// waitUntilNotLive blocks until the run's goroutine has ended, so a resume starts from a
// record nothing is still writing to.
//
// It does not wait for a terminal event: a stopped run's own closing events are written with
// the run's cancelled context, so the write is refused and the journal simply stops after the
// last level that closed. That silence is what resume turns into a recorded interruption
// (checkpoint.closeInterruptedTail); the callers here only need the record to be still.
func waitUntilNotLive(t *testing.T, d *daemonRunner, runID string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if _, live := d.mailboxFor(runID); !live {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("run %s was still live two seconds after being stopped", runID)
}

// The acceptance criterion, end to end and through the very call POST /api/v1/runs/{id}/resume
// makes: a run is interrupted, its cached row is then filled with a state no run ever had,
// and the resumed engine must still run from what the journal says. The corrupted value (999)
// would show up in the live state the reporter posts if the row were read.
func TestResumeIgnoresACorruptedRowAndReplaysTheJournal(t *testing.T) {
	dir := t.TempDir()
	graphPath := filepath.Join(dir, "seed-confirm.yaml")
	if err := os.WriteFile(graphPath, []byte(seedConfirmGraph), 0o644); err != nil {
		t.Fatal(err)
	}
	sink := newStepSink(t)
	store := openDaemonStore(t, dir)
	ctx := context.Background()
	d := &daemonRunner{cfg: config.Config{StepReportURL: sink.srv.URL}, store: store}

	runID, err := d.StartRun(ctx, graphPath, "yoann")
	if err != nil {
		t.Fatalf("StartRun: %v", err)
	}
	waitForFrontier(t, store, runID, "confirm")
	if err := d.StopRun(ctx, runID, "yoann"); err != nil {
		t.Fatalf("StopRun: %v", err)
	}
	waitUntilNotLive(t, d, runID)

	// The corruption: the row keeps its provenance — resume still needs the graph path from
	// it — and gets a state that contradicts the journal outright.
	rec, ok, err := store.Latest(ctx, runID)
	if err != nil || !ok {
		t.Fatalf("Latest returned (ok %t, err %v), want (true, nil)", ok, err)
	}
	lie := graph.NewState()
	lie.Set("n", 999)
	rec.State = lie
	if err := store.Save(ctx, rec); err != nil {
		t.Fatalf("Save the corrupted row: %v", err)
	}

	if err := d.ResumeRun(ctx, runID); err != nil {
		t.Fatalf("ResumeRun: %v", err)
	}
	decideWhenWaiting(t, d, runID)
	waitForRun(t, store, runID, checkpoint.StatusDone)

	decided := false
	for _, state := range sink.states() {
		if state["n"] == float64(999) {
			t.Fatalf("a level ran with n=999, the corrupted row's value: %v", state)
		}
		if _, ok := state[graph.DecisionKey("confirm")]; ok {
			decided = true
			if state["n"] != float64(3) {
				t.Fatalf("the level after resume ran with n=%v, want 3 from the journal", state["n"])
			}
		}
	}
	if !decided {
		t.Fatalf("no level ran after the resume; states seen: %v", sink.states())
	}
}

// The interruption made explicit, on a real run rather than a hand-built journal: a run is
// started, stopped in flight, and resumed. Its own terminal events were never written — they
// go through the run's cancelled context, which refuses them — so the record ends mid-story,
// and resume is what turns that silence into a fact. The whole point is that the fact is
// identifiable afterwards: an event nobody witnessed must not read like one that was.
func TestResumingAStoppedRunRecordsTheInterruptionInItsJournal(t *testing.T) {
	dir := t.TempDir()
	graphPath := filepath.Join(dir, "seed-confirm.yaml")
	if err := os.WriteFile(graphPath, []byte(seedConfirmGraph), 0o644); err != nil {
		t.Fatal(err)
	}
	store := openDaemonStore(t, dir)
	ctx := context.Background()
	d := &daemonRunner{cfg: config.Config{}, store: store}

	runID, err := d.StartRun(ctx, graphPath, "yoann")
	if err != nil {
		t.Fatalf("StartRun: %v", err)
	}
	waitForFrontier(t, store, runID, "confirm")
	if err := d.StopRun(ctx, runID, "yoann"); err != nil {
		t.Fatalf("StopRun: %v", err)
	}
	waitUntilNotLive(t, d, runID)

	stopped, err := store.Read(ctx, runID)
	if err != nil {
		t.Fatalf("Read the stopped run's journal: %v", err)
	}
	for i, ev := range stopped {
		switch ev.Payload.(type) {
		case journal.RunFinished, journal.RunFailed, journal.RunInterrupted:
			t.Fatalf("event %d (%T) closes the stopped run's journal; the premise of this test is that nothing did", i+1, ev.Payload)
		}
	}

	if err := d.ResumeRun(ctx, runID); err != nil {
		t.Fatalf("ResumeRun: %v", err)
	}
	decideWhenWaiting(t, d, runID)
	waitForRun(t, store, runID, checkpoint.StatusDone)

	events, err := store.Read(ctx, runID)
	if err != nil {
		t.Fatalf("Read the resumed run's journal: %v", err)
	}
	interruption := events[len(stopped)]
	if _, ok := interruption.Payload.(journal.RunInterrupted); !ok {
		t.Fatalf("event %d is %T, want journal.RunInterrupted right where the stopped record ended", len(stopped)+1, interruption.Payload)
	}
	if !interruption.Synthetic {
		t.Fatal("the interruption is not marked synthetic; a reader would take it for an event the run itself emitted")
	}
	for i, ev := range events[:len(stopped)] {
		if ev.Synthetic {
			t.Fatalf("event %d (%T) was recorded as it happened but is marked synthetic", i+1, ev.Payload)
		}
	}
	if _, ok := events[len(events)-1].Payload.(journal.RunFinished); !ok {
		t.Fatalf("last event is %T, want journal.RunFinished — the resumed attempt ran to its end on the reopened record",
			events[len(events)-1].Payload)
	}
}

// decideWhenWaiting answers the approval as soon as the resumed run is parked on it. The
// resumed engine takes a moment to reach the node, and Decide before then is refused.
func decideWhenWaiting(t *testing.T, d *daemonRunner, runID string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if err := d.Decide(context.Background(), runID, "confirm", "yoann", string(graph.Approved)); err == nil {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("run %s never parked on the approval after being resumed", runID)
}

// A run recorded before this repo had a journal has a checkpoint and no events. Replaying it
// yields an empty state, so resuming would silently continue from a state that never existed
// — the one outcome the whole issue exists to prevent. Both entry points refuse it, and the
// message names the run so an operator knows which one to look at.
func TestResumeRefusesARunWhoseJournalIsAbsent(t *testing.T) {
	dir := t.TempDir()
	graphPath := filepath.Join(dir, "g.yaml")
	if err := os.WriteFile(graphPath, []byte(exampleGraph), 0o644); err != nil {
		t.Fatal(err)
	}
	store := openDaemonStore(t, dir)
	ctx := context.Background()
	const runID = "legacy-run"
	old := graph.NewState()
	old.Set("n", 3)
	if err := store.Save(ctx, checkpoint.Record{
		RunID: runID, Step: 1, Frontier: []string{"double"}, State: old,
		Status: checkpoint.StatusRunning, GraphPath: graphPath,
	}); err != nil {
		t.Fatalf("Save: %v", err)
	}

	d := &daemonRunner{cfg: config.Config{}, store: store}
	err := d.ResumeRun(ctx, runID)
	if err == nil {
		t.Fatal("ResumeRun accepted a run with no journal; it would have resumed from an empty state")
	}
	if !strings.Contains(err.Error(), runID) {
		t.Fatalf("ResumeRun error %q does not name the run %q", err, runID)
	}

	t.Setenv("KERN_CHECKPOINT_DB", filepath.Join(dir, "cp.db"))
	out, cliErr := execute(t, "resume", runID)
	if cliErr == nil {
		t.Fatalf("CLI resume accepted a run with no journal: %s", out)
	}
	if !strings.Contains(cliErr.Error(), runID) {
		t.Fatalf("CLI resume error %q does not name the run %q", cliErr, runID)
	}
}
