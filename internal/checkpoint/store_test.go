package checkpoint

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/yoann/kern-orch/internal/graph"
)

var _ Store = (*SQLiteStore)(nil)

func openTemp(t *testing.T) *SQLiteStore {
	t.Helper()
	st, err := OpenSQLite(filepath.Join(t.TempDir(), "cp.db"))
	if err != nil {
		t.Fatalf("OpenSQLite: %v", err)
	}
	t.Cleanup(func() { st.Close() })
	return st
}

func stateWith(k string, v any, step int) *graph.State {
	s := graph.NewState()
	s.Set(k, v)
	s.Step = step
	return s
}

func TestSaveAndLatestReturnsHighestStep(t *testing.T) {
	st := openTemp(t)
	ctx := context.Background()
	if err := st.Save(ctx, Record{RunID: "r1", Step: 1, Frontier: []string{"b"}, State: stateWith("x", 1, 1), Status: StatusRunning}); err != nil {
		t.Fatal(err)
	}
	if err := st.Save(ctx, Record{RunID: "r1", Step: 2, Frontier: []string{"c"}, State: stateWith("x", 2, 2), Status: StatusRunning}); err != nil {
		t.Fatal(err)
	}
	rec, ok, err := st.Latest(ctx, "r1")
	if err != nil || !ok {
		t.Fatalf("Latest: ok=%v err=%v", ok, err)
	}
	if rec.Step != 2 {
		t.Fatalf("latest step = %d; want 2", rec.Step)
	}
	if v, _ := rec.State.Get("x"); v != float64(2) { // JSON numbers decode to float64
		t.Fatalf("state x = %v; want 2", v)
	}
	if len(rec.Frontier) != 1 || rec.Frontier[0] != "c" {
		t.Fatalf("frontier = %v; want [c]", rec.Frontier)
	}
}

func TestLatestMissingRun(t *testing.T) {
	st := openTemp(t)
	_, ok, err := st.Latest(context.Background(), "nope")
	if err != nil {
		t.Fatalf("err = %v; want nil", err)
	}
	if ok {
		t.Fatal("expected ok=false for missing run")
	}
}

func TestSaveIsIdempotentPerStep(t *testing.T) {
	st := openTemp(t)
	ctx := context.Background()
	r := Record{RunID: "r", Step: 1, Frontier: []string{"b"}, State: stateWith("x", 1, 1), Status: StatusRunning}
	if err := st.Save(ctx, r); err != nil {
		t.Fatal(err)
	}
	r.Status = StatusDone // overwrite same (run,step)
	if err := st.Save(ctx, r); err != nil {
		t.Fatalf("re-save: %v", err)
	}
	rec, _, _ := st.Latest(ctx, "r")
	if rec.Status != StatusDone {
		t.Fatalf("status = %q; want done (upsert should overwrite)", rec.Status)
	}
}

func TestListSummarizesRuns(t *testing.T) {
	st := openTemp(t)
	ctx := context.Background()
	st.Save(ctx, Record{RunID: "a", Step: 1, Frontier: []string{"n"}, State: graph.NewState(), Status: StatusRunning})
	st.Save(ctx, Record{RunID: "a", Step: 2, Frontier: nil, State: graph.NewState(), Status: StatusDone})
	st.Save(ctx, Record{RunID: "b", Step: 1, Frontier: nil, State: graph.NewState(), Status: StatusFailed})
	got, err := st.List(ctx)
	if err != nil {
		t.Fatal(err)
	}
	byID := map[string]Summary{}
	for _, s := range got {
		byID[s.RunID] = s
	}
	if byID["a"].LastStep != 2 || byID["a"].Status != StatusDone {
		t.Fatalf("run a summary = %+v", byID["a"])
	}
	if byID["b"].Status != StatusFailed {
		t.Fatalf("run b summary = %+v", byID["b"])
	}
}

func TestSaveRejectsEmptyRunID(t *testing.T) {
	st := openTemp(t)
	err := st.Save(context.Background(), Record{Step: 1, State: graph.NewState()})
	if !errors.Is(err, ErrEmptyRunID) {
		t.Fatalf("err = %v; want ErrEmptyRunID", err)
	}
}

// A daemon writes a queued marker the instant a run is accepted, before the engine has
// produced its first checkpoint. It must be visible immediately, and must stop being the
// answer the moment a real checkpoint exists.
func TestAQueuedMarkerYieldsToTheFirstRealCheckpoint(t *testing.T) {
	st := openTemp(t)
	ctx := context.Background()

	if err := st.Save(ctx, Record{
		RunID: "r1", Step: QueuedStep, Frontier: nil, State: graph.NewState(),
		Status: StatusQueued, GraphPath: "g.yaml",
	}); err != nil {
		t.Fatal(err)
	}

	rec, ok, err := st.Latest(ctx, "r1")
	if err != nil || !ok {
		t.Fatalf("Latest: ok=%v err=%v", ok, err)
	}
	if rec.Status != StatusQueued {
		t.Fatalf("status = %q, want %q before any real checkpoint", rec.Status, StatusQueued)
	}

	if err := st.Save(ctx, Record{
		RunID: "r1", Step: 1, Frontier: []string{"b"}, State: stateWith("x", 1, 1),
		Status: StatusRunning, GraphPath: "g.yaml",
	}); err != nil {
		t.Fatal(err)
	}

	rec, ok, err = st.Latest(ctx, "r1")
	if err != nil || !ok {
		t.Fatalf("Latest: ok=%v err=%v", ok, err)
	}
	if rec.Status != StatusRunning || rec.Step != 1 {
		t.Fatalf("Latest = %+v, want the real checkpoint once one exists", rec)
	}
}

// List must surface a run that has only ever been queued: a run somebody just started must
// appear before its first level completes, not only after.
func TestListSurfacesAQueuedRun(t *testing.T) {
	st := openTemp(t)
	ctx := context.Background()
	if err := st.Save(ctx, Record{
		RunID: "r1", Step: QueuedStep, State: graph.NewState(), Status: StatusQueued,
	}); err != nil {
		t.Fatal(err)
	}

	runs, err := st.List(ctx)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(runs) != 1 || runs[0].Status != StatusQueued {
		t.Fatalf("List = %+v, want the queued run", runs)
	}
}
