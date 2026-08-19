package cmd

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/yoann/kern-orch/internal/checkpoint"
	"github.com/yoann/kern-orch/internal/config"
	"github.com/yoann/kern-orch/internal/journal"
	"github.com/yoann/kern-orch/internal/journal/projection"
)

// seedDoubleGraph is a two-level run that actually mutates the state: seed writes n=3, then
// double turns it into 6. A graph of noops would let a projection that produced nothing pass.
const seedDoubleGraph = `
entry: seed
nodes:
  - id: seed
    type: tool
    func: seed
  - id: double
    type: tool
    func: double
edges:
  - from: seed
    to: [double]
`

// startSeedDoubleRun runs the real engine through the real daemon wiring and returns the
// store and the finished run's id. Everything below asserts on what a whole run left behind,
// not on a hand-built event list: the recorder, the adapter, the engine's own emission order
// and the transaction all have to agree for these to hold.
func startSeedDoubleRun(t *testing.T) (*checkpoint.SQLiteStore, string) {
	t.Helper()
	dir := t.TempDir()
	graphPath := filepath.Join(dir, "seed-double.yaml")
	if err := os.WriteFile(graphPath, []byte(seedDoubleGraph), 0o644); err != nil {
		t.Fatal(err)
	}
	store := openDaemonStore(t, dir)
	d := &daemonRunner{cfg: config.Config{}, store: store}

	runID, err := d.StartRun(context.Background(), graphPath, "yoann")
	if err != nil {
		t.Fatalf("StartRun: %v", err)
	}
	waitForRun(t, store, runID, checkpoint.StatusDone)
	return store, runID
}

// The acceptance criterion this whole issue turns on, asserted on a real run: the row's state
// is what Project makes of the run's stored events, not a marshalled copy of the live state
// the engine held. The two agree here — that is the point of the design — so the assertion is
// against the projection, which is the only one of the two that can be wrong silently.
func TestARealRunsProjectionRowEqualsTheProjectionOfItsJournal(t *testing.T) {
	store, runID := startSeedDoubleRun(t)
	ctx := context.Background()

	events, err := store.Read(ctx, runID)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if len(events) == 0 {
		t.Fatalf("the run wrote no events at all — nothing is wired to the engine's emitter")
	}
	want, err := projection.Project(events)
	if err != nil {
		t.Fatalf("Project the run's own journal: %v", err)
	}
	rec, ok, err := store.Latest(ctx, runID)
	if err != nil || !ok {
		t.Fatalf("Latest returned (ok %t, err %v), want (true, nil)", ok, err)
	}
	if recorderStateJSON(t, rec.State) != recorderStateJSON(t, want) {
		t.Fatalf("row state = %s, want Project(the run's events) = %s",
			recorderStateJSON(t, rec.State), recorderStateJSON(t, want))
	}
	if v, _ := rec.State.Get("n"); v != float64(6) {
		t.Fatalf("row state n = %v, want 6 — the run doubled the seed", v)
	}
}

// A run that completed normally still reads the way `status` and the daemon's run endpoint
// have always read it: highest step, done, with the graph path and requester that resume and
// the permission check depend on.
func TestARealRunStillReadsTheWayStatusAndTheRunEndpointExpect(t *testing.T) {
	store, runID := startSeedDoubleRun(t)
	ctx := context.Background()

	rec, ok, err := store.Latest(ctx, runID)
	if err != nil || !ok {
		t.Fatalf("Latest returned (ok %t, err %v), want (true, nil)", ok, err)
	}
	if rec.Step != 2 || rec.Status != checkpoint.StatusDone {
		t.Fatalf("row = step %d/%q, want 2/%q", rec.Step, rec.Status, checkpoint.StatusDone)
	}
	if len(rec.Frontier) != 0 {
		t.Fatalf("row frontier = %v, want empty on a finished run", rec.Frontier)
	}
	if rec.Requester != "yoann" || rec.GraphPath == "" {
		t.Fatalf("row provenance = %q/%q, want the requester and the graph path preserved", rec.Requester, rec.GraphPath)
	}

	sums, err := store.List(ctx)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	found := false
	for _, s := range sums {
		if s.RunID != runID {
			continue
		}
		found = true
		if s.LastStep != 2 || s.Status != checkpoint.StatusDone {
			t.Fatalf("summary = step %d/%q, want 2/%q", s.LastStep, s.Status, checkpoint.StatusDone)
		}
	}
	if !found {
		t.Fatalf("run %s is absent from List, which is what `status` prints", runID)
	}
}

// The run's journal is closed, not truncated at its last level. Issue 08 has to tell a run
// that finished from one whose process died mid-level, and a missing terminal event is
// exactly what those two would otherwise look like.
func TestARealRunsJournalIsOpenedAndClosed(t *testing.T) {
	store, runID := startSeedDoubleRun(t)

	events, err := store.Read(context.Background(), runID)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if _, ok := events[0].Payload.(journal.RunStarted); !ok {
		t.Fatalf("first event payload is %T, want journal.RunStarted", events[0].Payload)
	}
	last := events[len(events)-1]
	if _, ok := last.Payload.(journal.RunFinished); !ok {
		t.Fatalf("last event payload is %T, want journal.RunFinished", last.Payload)
	}
	for i, ev := range events {
		if want := checkpoint.FirstSeq + int64(i); ev.Seq != want {
			t.Fatalf("event %d has seq %d, want %d", i, ev.Seq, want)
		}
		if ev.RunID != runID {
			t.Fatalf("event %d belongs to run %q, want %q", i, ev.RunID, runID)
		}
	}
}

// The queued marker is written before any event exists, and a status query racing the run's
// first level must still find it. Reading it back on a run parked in its first node is the
// only way to prove the journal's own writes did not displace it.
func TestTheQueuedMarkerIsStillVisibleImmediatelyAfterAcceptance(t *testing.T) {
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
	t.Cleanup(func() { d.StopRun(context.Background(), runID, "") })

	rec, ok, err := store.Latest(context.Background(), runID)
	if err != nil || !ok {
		t.Fatalf("Latest returned (ok %t, err %v) right after acceptance, want (true, nil)", ok, err)
	}
	if rec.Step != checkpoint.QueuedStep || rec.Status != checkpoint.StatusQueued {
		t.Fatalf("row = step %d/%q, want %d/%q", rec.Step, rec.Status, checkpoint.QueuedStep, checkpoint.StatusQueued)
	}
}
