package cmd

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/yoann/kern-orch/internal/checkpoint"
	"github.com/yoann/kern-orch/internal/config"
	"github.com/yoann/kern-orch/internal/daemon"
	"github.com/yoann/kern-orch/internal/graph"
)

const waitGraph = `
entry: hold
nodes:
  - id: hold
    type: tool
    func: wait
`

const pauseGraph = `
entry: pause
nodes:
  - id: pause
    type: tool
    func: pause
  - id: finish
    type: tool
    func: noop
edges:
  - from: pause
    to: [finish]
`

const confirmGraph = `
entry: confirm
nodes:
  - id: confirm
    type: approval
  - id: approved
    type: tool
    func: noop
  - id: refused
    type: tool
    func: noop
edges:
  - from: confirm
    router: onConfirmDecision
`

func openDaemonStore(t *testing.T, dir string) *checkpoint.SQLiteStore {
	t.Helper()
	st, err := checkpoint.OpenSQLite(filepath.Join(dir, "cp.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	return st
}

func waitForRun(t *testing.T, store *checkpoint.SQLiteStore, runID string, want string) checkpoint.Record {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		rec, ok, err := store.Latest(context.Background(), runID)
		if err != nil {
			t.Fatalf("Latest: %v", err)
		}
		if ok && rec.Status == want {
			return rec
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("run %s never reached status %q", runID, want)
	return checkpoint.Record{}
}

// The whole point of the daemon: a run starts in the background and the caller gets its id
// back immediately, without waiting for it to finish.
func TestDaemonRunnerStartsARunInTheBackground(t *testing.T) {
	dir := t.TempDir()
	graphPath := filepath.Join(dir, "g.yaml")
	if err := os.WriteFile(graphPath, []byte(exampleGraph), 0o644); err != nil {
		t.Fatal(err)
	}
	store := openDaemonStore(t, dir)
	d := &daemonRunner{cfg: config.Config{}, store: store}

	runID, err := d.StartRun(context.Background(), graphPath, "")
	if err != nil {
		t.Fatalf("StartRun: %v", err)
	}
	if runID == "" {
		t.Fatal("StartRun returned no id")
	}

	waitForRun(t, store, runID, checkpoint.StatusDone)
}

// A queued marker must exist the instant StartRun returns, before the engine has produced
// anything: a status query landing between the two calls must not 404.
func TestDaemonRunnerMarksARunQueuedImmediately(t *testing.T) {
	dir := t.TempDir()
	graphPath := filepath.Join(dir, "g.yaml")
	if err := os.WriteFile(graphPath, []byte(exampleGraph), 0o644); err != nil {
		t.Fatal(err)
	}
	store := openDaemonStore(t, dir)
	d := &daemonRunner{cfg: config.Config{}, store: store}

	runID, err := d.StartRun(context.Background(), graphPath, "")
	if err != nil {
		t.Fatalf("StartRun: %v", err)
	}

	rec, ok, err := store.Latest(context.Background(), runID)
	if err != nil || !ok {
		t.Fatalf("Latest immediately after StartRun: ok=%v err=%v", ok, err)
	}
	if rec.Status != checkpoint.StatusQueued && rec.Status != checkpoint.StatusRunning && rec.Status != checkpoint.StatusDone {
		t.Errorf("status = %q right after StartRun, want it to already exist", rec.Status)
	}
}

// A bad graph must fail synchronously: a caller deserves to know immediately rather than
// poll for a run that will never appear.
func TestDaemonRunnerFailsFastOnABadGraph(t *testing.T) {
	dir := t.TempDir()
	graphPath := filepath.Join(dir, "bad.yaml")
	if err := os.WriteFile(graphPath, []byte("entry: a\nnodes:\n  - id: a\n    type: tool\n    func: ghost\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	store := openDaemonStore(t, dir)
	d := &daemonRunner{cfg: config.Config{}, store: store}

	if _, err := d.StartRun(context.Background(), graphPath, ""); err == nil {
		t.Fatal("StartRun accepted a graph with an unknown tool func")
	}
}

// A missing file is the same story: synchronous, not a goroutine that silently never
// reports anything.
func TestDaemonRunnerFailsFastOnAMissingGraph(t *testing.T) {
	dir := t.TempDir()
	store := openDaemonStore(t, dir)
	d := &daemonRunner{cfg: config.Config{}, store: store}

	if _, err := d.StartRun(context.Background(), filepath.Join(dir, "absent.yaml"), ""); err == nil {
		t.Fatal("StartRun accepted a graph file that does not exist")
	}
}

func TestDaemonRunnerListsAndGetsRuns(t *testing.T) {
	dir := t.TempDir()
	graphPath := filepath.Join(dir, "g.yaml")
	if err := os.WriteFile(graphPath, []byte(exampleGraph), 0o644); err != nil {
		t.Fatal(err)
	}
	store := openDaemonStore(t, dir)
	d := &daemonRunner{cfg: config.Config{}, store: store}

	runID, err := d.StartRun(context.Background(), graphPath, "")
	if err != nil {
		t.Fatalf("StartRun: %v", err)
	}
	waitForRun(t, store, runID, checkpoint.StatusDone)

	runs, err := d.ListRuns(context.Background())
	if err != nil || len(runs) != 1 || runs[0].RunID != runID {
		t.Fatalf("ListRuns = %v, %v; want the one run", runs, err)
	}

	rec, ok, err := d.GetRun(context.Background(), runID)
	if err != nil || !ok || rec.RunID != runID {
		t.Fatalf("GetRun = %+v, %v, %v; want the run", rec, ok, err)
	}

	if _, ok, err := d.GetRun(context.Background(), "jamais"); err != nil || ok {
		t.Errorf("GetRun on an unknown id = ok=%v err=%v, want ok=false err=nil", ok, err)
	}
}

// Resuming an already-complete run is a no-op: there is nothing left to do, and that is not
// an error condition — the CLI's `resume` says "already complete" for the same case.
func TestDaemonRunnerResumeOnACompleteRunIsANoop(t *testing.T) {
	dir := t.TempDir()
	graphPath := filepath.Join(dir, "g.yaml")
	if err := os.WriteFile(graphPath, []byte(exampleGraph), 0o644); err != nil {
		t.Fatal(err)
	}
	store := openDaemonStore(t, dir)
	d := &daemonRunner{cfg: config.Config{}, store: store}

	runID, err := d.StartRun(context.Background(), graphPath, "")
	if err != nil {
		t.Fatalf("StartRun: %v", err)
	}
	waitForRun(t, store, runID, checkpoint.StatusDone)

	if err := d.ResumeRun(context.Background(), runID); err != nil {
		t.Errorf("ResumeRun on a complete run = %v, want nil", err)
	}
}

func TestDaemonRunnerResumeOnAnUnknownRunIsErrUnknownRun(t *testing.T) {
	dir := t.TempDir()
	store := openDaemonStore(t, dir)
	d := &daemonRunner{cfg: config.Config{}, store: store}

	err := d.ResumeRun(context.Background(), "jamais")
	if !errors.Is(err, daemon.ErrUnknownRun) {
		t.Errorf("ResumeRun on an unknown id = %v, want daemon.ErrUnknownRun", err)
	}
}

// The real proof stop works: a node genuinely blocked on its context, actually
// interrupted by a person rather than by the run finishing on its own.
func TestDaemonRunnerStopInterruptsALiveNode(t *testing.T) {
	dir := t.TempDir()
	graphPath := filepath.Join(dir, "wait.yaml")
	if err := os.WriteFile(graphPath, []byte(waitGraph), 0o644); err != nil {
		t.Fatal(err)
	}
	store := openDaemonStore(t, dir)
	d := &daemonRunner{cfg: config.Config{}, store: store}

	runID, err := d.StartRun(context.Background(), graphPath, "")
	if err != nil {
		t.Fatalf("StartRun: %v", err)
	}
	time.Sleep(30 * time.Millisecond) // let it actually reach the blocking node

	if err := d.StopRun(context.Background(), runID, ""); err != nil {
		t.Fatalf("StopRun: %v", err)
	}

	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if _, ok := d.mailboxFor(runID); !ok {
			return // the run's own goroutine exited and cleaned up: stop worked
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("run did not stop within 1s of StopRun")
}

func TestDaemonRunnerStopOnAnUnknownRunIsErrUnknownRun(t *testing.T) {
	dir := t.TempDir()
	store := openDaemonStore(t, dir)
	d := &daemonRunner{cfg: config.Config{}, store: store}

	if err := d.StopRun(context.Background(), "jamais", ""); !errors.Is(err, daemon.ErrUnknownRun) {
		t.Errorf("StopRun on an unknown id = %v, want daemon.ErrUnknownRun", err)
	}
}

func TestDaemonRunnerStopRefusesTheWrongActor(t *testing.T) {
	dir := t.TempDir()
	graphPath := filepath.Join(dir, "wait.yaml")
	if err := os.WriteFile(graphPath, []byte(waitGraph), 0o644); err != nil {
		t.Fatal(err)
	}
	store := openDaemonStore(t, dir)
	d := &daemonRunner{cfg: config.Config{}, store: store}

	runID, err := d.StartRun(context.Background(), graphPath, "yoann")
	if err != nil {
		t.Fatalf("StartRun: %v", err)
	}
	time.Sleep(30 * time.Millisecond)

	if err := d.StopRun(context.Background(), runID, "pas-yoann"); !errors.Is(err, daemon.ErrForbidden) {
		t.Errorf("StopRun by a different actor = %v, want daemon.ErrForbidden", err)
	}
	// Clean up: stop it for real so the test doesn't leak a blocked goroutine.
	_ = d.StopRun(context.Background(), runID, "yoann")
}

// The real proof nudge works: a value sent mid-run reaches the state a later node reads,
// not just an in-memory queue nobody drains.
func TestDaemonRunnerNudgeAppliesToTheNextLevel(t *testing.T) {
	dir := t.TempDir()
	graphPath := filepath.Join(dir, "pause.yaml")
	if err := os.WriteFile(graphPath, []byte(pauseGraph), 0o644); err != nil {
		t.Fatal(err)
	}
	store := openDaemonStore(t, dir)
	d := &daemonRunner{cfg: config.Config{}, store: store}

	runID, err := d.StartRun(context.Background(), graphPath, "")
	if err != nil {
		t.Fatalf("StartRun: %v", err)
	}
	time.Sleep(30 * time.Millisecond) // still inside the pause node's 150ms sleep

	if err := d.Nudge(context.Background(), runID, "", "probe", "hello"); err != nil {
		t.Fatalf("Nudge: %v", err)
	}
	waitForRun(t, store, runID, checkpoint.StatusDone)

	rec, _, err := d.GetRun(context.Background(), runID)
	if err != nil {
		t.Fatalf("GetRun: %v", err)
	}
	if v, _ := rec.State.Get("probe"); v != "hello" {
		t.Errorf("state[probe] = %v, want hello — the nudge never reached the run", v)
	}
}

func TestDaemonRunnerNudgeOnAnUnknownRunIsErrUnknownRun(t *testing.T) {
	dir := t.TempDir()
	store := openDaemonStore(t, dir)
	d := &daemonRunner{cfg: config.Config{}, store: store}

	if err := d.Nudge(context.Background(), "jamais", "", "k", "v"); !errors.Is(err, daemon.ErrUnknownRun) {
		t.Errorf("Nudge on an unknown id = %v, want daemon.ErrUnknownRun", err)
	}
}

// The real proof decide works: an approval node genuinely blocked, then genuinely routed
// down the branch the decision picked.
func TestDaemonRunnerDecideUnblocksAnApprovalNode(t *testing.T) {
	dir := t.TempDir()
	graphPath := filepath.Join(dir, "confirm.yaml")
	if err := os.WriteFile(graphPath, []byte(confirmGraph), 0o644); err != nil {
		t.Fatal(err)
	}
	store := openDaemonStore(t, dir)
	d := &daemonRunner{cfg: config.Config{}, store: store}

	runID, err := d.StartRun(context.Background(), graphPath, "")
	if err != nil {
		t.Fatalf("StartRun: %v", err)
	}
	time.Sleep(30 * time.Millisecond) // let it reach the approval node and start waiting

	if err := d.Decide(context.Background(), runID, "confirm", "", string(graph.Approved)); err != nil {
		t.Fatalf("Decide: %v", err)
	}
	waitForRun(t, store, runID, checkpoint.StatusDone)

	rec, _, err := d.GetRun(context.Background(), runID)
	if err != nil {
		t.Fatalf("GetRun: %v", err)
	}
	if v, _ := rec.State.Get(graph.DecisionKey("confirm")); v != string(graph.Approved) {
		t.Errorf("decision:confirm = %v, want approve", v)
	}
	if len(rec.Frontier) != 0 {
		t.Errorf("frontier = %v, want empty — the run should have completed via the approved branch", rec.Frontier)
	}
}

func TestDaemonRunnerDecideOnAnUnknownRunIsErrUnknownRun(t *testing.T) {
	dir := t.TempDir()
	store := openDaemonStore(t, dir)
	d := &daemonRunner{cfg: config.Config{}, store: store}

	err := d.Decide(context.Background(), "jamais", "confirm", "", string(graph.Approved))
	if !errors.Is(err, daemon.ErrUnknownRun) {
		t.Errorf("Decide on an unknown run = %v, want daemon.ErrUnknownRun", err)
	}
}

func TestDaemonRunnerDecideRejectsAnInvalidDecisionValue(t *testing.T) {
	dir := t.TempDir()
	graphPath := filepath.Join(dir, "confirm.yaml")
	if err := os.WriteFile(graphPath, []byte(confirmGraph), 0o644); err != nil {
		t.Fatal(err)
	}
	store := openDaemonStore(t, dir)
	d := &daemonRunner{cfg: config.Config{}, store: store}

	runID, err := d.StartRun(context.Background(), graphPath, "")
	if err != nil {
		t.Fatalf("StartRun: %v", err)
	}
	time.Sleep(30 * time.Millisecond)

	if err := d.Decide(context.Background(), runID, "confirm", "", "maybe"); err == nil {
		t.Error("Decide accepted an invalid decision value")
	}
	// Clean up: answer for real so the test doesn't leak a blocked goroutine.
	_ = d.Decide(context.Background(), runID, "confirm", "", string(graph.Approved))
}

func TestDaemonRunnerDecideOnANodeNotWaitingIsErrUnknownNode(t *testing.T) {
	dir := t.TempDir()
	graphPath := filepath.Join(dir, "confirm.yaml")
	if err := os.WriteFile(graphPath, []byte(confirmGraph), 0o644); err != nil {
		t.Fatal(err)
	}
	store := openDaemonStore(t, dir)
	d := &daemonRunner{cfg: config.Config{}, store: store}

	runID, err := d.StartRun(context.Background(), graphPath, "")
	if err != nil {
		t.Fatalf("StartRun: %v", err)
	}
	time.Sleep(30 * time.Millisecond)

	err = d.Decide(context.Background(), runID, "not-confirm", "", string(graph.Approved))
	if !errors.Is(err, daemon.ErrUnknownNode) {
		t.Errorf("Decide on a node not waiting = %v, want daemon.ErrUnknownNode", err)
	}
	// Clean up.
	_ = d.Decide(context.Background(), runID, "confirm", "", string(graph.Approved))
}

// Without this, a run parked on an approval node reports nothing at all until a level
// completes — which never happens until someone decides it. kern-ui would have no way to
// show a human the very decision they are meant to make. This proves the activity signal
// fires the moment the node starts waiting, not only once it is answered.
func TestDaemonRunnerReportsActivityWhileAnApprovalNodeWaits(t *testing.T) {
	dir := t.TempDir()
	graphPath := filepath.Join(dir, "confirm.yaml")
	if err := os.WriteFile(graphPath, []byte(confirmGraph), 0o644); err != nil {
		t.Fatal(err)
	}
	store := openDaemonStore(t, dir)

	var mu sync.Mutex
	var signals []struct {
		NodeID     string `json:"node_id"`
		Generating bool   `json:"generating"`
	}
	sink := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var sig struct {
			NodeID     string `json:"node_id"`
			Generating bool   `json:"generating"`
		}
		_ = json.NewDecoder(r.Body).Decode(&sig)
		mu.Lock()
		signals = append(signals, sig)
		mu.Unlock()
		w.WriteHeader(http.StatusAccepted)
	}))
	defer sink.Close()

	d := &daemonRunner{cfg: config.Config{ActivityReportURL: sink.URL}, store: store}
	runID, err := d.StartRun(context.Background(), graphPath, "")
	if err != nil {
		t.Fatalf("StartRun: %v", err)
	}

	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		mu.Lock()
		n := len(signals)
		mu.Unlock()
		if n >= 1 {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}

	mu.Lock()
	got := append([]struct {
		NodeID     string `json:"node_id"`
		Generating bool   `json:"generating"`
	}{}, signals...)
	mu.Unlock()
	if len(got) == 0 || got[0].NodeID != "confirm" || !got[0].Generating {
		t.Fatalf("first activity signal = %+v, want confirm/generating=true before any decision", got)
	}

	if err := d.Decide(context.Background(), runID, "confirm", "", string(graph.Approved)); err != nil {
		t.Fatalf("Decide: %v", err)
	}
	waitForRun(t, store, runID, checkpoint.StatusDone)

	mu.Lock()
	got = append([]struct {
		NodeID     string `json:"node_id"`
		Generating bool   `json:"generating"`
	}{}, signals...)
	mu.Unlock()
	found := false
	for _, s := range got {
		if s.NodeID == "confirm" && !s.Generating {
			found = true
		}
	}
	if !found {
		t.Errorf("no generating=false signal for confirm after it was decided: %+v", got)
	}
}
