package checkpoint

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/yoann/kern-orch/internal/graph"
	"github.com/yoann/kern-orch/internal/journal"
	"github.com/yoann/kern-orch/internal/journal/projection"
)

// openAtomic gives each test its own database. Every assertion here is about what one run's
// tables hold after a partial failure, so a shared file would let one test's rollback be
// read as another test's write.
func openAtomic(t *testing.T) *SQLiteStore {
	t.Helper()
	st, err := OpenSQLite(filepath.Join(t.TempDir(), "atomic.db"))
	if err != nil {
		t.Fatalf("OpenSQLite: %v", err)
	}
	t.Cleanup(func() { st.Close() })
	return st
}

// levelEvents is one complete level of a run, at the sequence numbers a journal starting at
// from would assign. Building them from a helper rather than by hand keeps every test here
// describing what the level did instead of counting sequence numbers.
func levelEvents(runID string, from int64, node string, data map[string]any) []journal.Event {
	return []journal.Event{
		eventAt(runID, from, journal.LevelOpened{Frontier: []string{node}}),
		eventAt(runID, from+1, journal.NodeStarted{NodeID: node}),
		eventAt(runID, from+2, journal.NodeProduced{NodeID: node, Data: data}),
		eventAt(runID, from+3, journal.LevelClosed{Frontier: []string{node}, Rule: journal.CombinationReplace}),
	}
}

func TestAppendAndProjectStoresTheEventsAndTheRowTogether(t *testing.T) {
	st := openAtomic(t)
	ctx := context.Background()
	events := levelEvents("run-1", FirstSeq, "collect", map[string]any{"dossier": "D-1"})

	err := st.AppendAndProject(ctx, Projection{
		RunID: "run-1", Step: 1, Frontier: []string{"next"}, Status: StatusRunning,
	}, events...)
	if err != nil {
		t.Fatalf("AppendAndProject returned error %v, want nil", err)
	}

	stored, err := st.Read(ctx, "run-1")
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if len(stored) != len(events) {
		t.Fatalf("journal holds %d events, want %d", len(stored), len(events))
	}
	rec, ok, err := st.Latest(ctx, "run-1")
	if err != nil || !ok {
		t.Fatalf("Latest returned (ok %t, err %v), want (true, nil)", ok, err)
	}
	if rec.Step != 1 {
		t.Fatalf("row step = %d, want 1", rec.Step)
	}
	if v, ok := rec.State.Get("dossier"); !ok || v != "D-1" {
		t.Fatalf("row state dossier = %v (present %t), want D-1 (present true)", v, ok)
	}
}

// The criterion the whole issue turns on: the row's state is what Project makes of the
// events, not a marshalled copy of whatever live state the caller happened to hold. The
// Projection argument carries no state field at all, so this is asserted against the
// journal read back from the table rather than against anything the test passed in.
func TestTheStoredRowStateEqualsTheProjectionOfTheRunsEvents(t *testing.T) {
	st := openAtomic(t)
	ctx := context.Background()

	first := levelEvents("run-1", FirstSeq, "collect", map[string]any{"dossier": "D-1", "draft": "v1"})
	if err := st.AppendAndProject(ctx, Projection{
		RunID: "run-1", Step: 1, Frontier: []string{"refine"}, Status: StatusRunning,
	}, first...); err != nil {
		t.Fatalf("AppendAndProject level 1: %v", err)
	}
	second := levelEvents("run-1", FirstSeq+4, "refine", map[string]any{"draft": "v2"})
	if err := st.AppendAndProject(ctx, Projection{
		RunID: "run-1", Step: 2, Frontier: nil, Status: StatusDone,
	}, second...); err != nil {
		t.Fatalf("AppendAndProject level 2: %v", err)
	}

	events, err := st.Read(ctx, "run-1")
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	want, err := projection.Project(events)
	if err != nil {
		t.Fatalf("Project: %v", err)
	}
	rec, ok, err := st.Latest(ctx, "run-1")
	if err != nil || !ok {
		t.Fatalf("Latest returned (ok %t, err %v), want (true, nil)", ok, err)
	}
	if stateJSON(t, rec.State) != stateJSON(t, want) {
		t.Fatalf("row state = %s, want Project(events) = %s", stateJSON(t, rec.State), stateJSON(t, want))
	}
}

// A batch whose sequence does not continue the run's stored sequence is refused by the
// journal half — and must take the projection row down with it, or the row would advance to
// a level whose events were never written.
func TestAFailedEventAppendLeavesThePreviousRowIntact(t *testing.T) {
	st := openAtomic(t)
	ctx := context.Background()

	first := levelEvents("run-1", FirstSeq, "collect", map[string]any{"dossier": "D-1"})
	if err := st.AppendAndProject(ctx, Projection{
		RunID: "run-1", Step: 1, Frontier: []string{"refine"}, Status: StatusRunning,
	}, first...); err != nil {
		t.Fatalf("AppendAndProject level 1: %v", err)
	}

	// Starts at a seq the run already used, which is exactly the hole-or-collision the
	// journal refuses.
	gapped := levelEvents("run-1", FirstSeq, "refine", map[string]any{"draft": "v2"})
	err := st.AppendAndProject(ctx, Projection{
		RunID: "run-1", Step: 2, Frontier: nil, Status: StatusDone,
	}, gapped...)
	if !errors.Is(err, ErrSeqMismatch) {
		t.Fatalf("AppendAndProject returned %v, want ErrSeqMismatch", err)
	}

	rec, ok, err := st.Latest(ctx, "run-1")
	if err != nil || !ok {
		t.Fatalf("Latest returned (ok %t, err %v), want (true, nil)", ok, err)
	}
	if rec.Step != 1 || rec.Status != StatusRunning {
		t.Fatalf("row = step %d/%q, want the untouched step 1/%q", rec.Step, rec.Status, StatusRunning)
	}
	events, err := st.Read(ctx, "run-1")
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if len(events) != len(first) {
		t.Fatalf("journal holds %d events, want the %d of the first level only", len(events), len(first))
	}
}

// The other half of the same guarantee: an upsert that cannot happen must leave the level's
// events unwritten. A run id that is not a run id fails the row write only — the events
// encode and their sequence is valid — so it isolates the projection half of the
// transaction.
func TestAFailedProjectionUpsertLeavesNoEventsForThatLevel(t *testing.T) {
	st := openAtomic(t)
	ctx := context.Background()

	// An event sequence the projection refuses to replay: two levels opened without the
	// first being closed. The append half is well-formed, so only the projection can fail.
	events := []journal.Event{
		eventAt("run-1", FirstSeq, journal.LevelOpened{Frontier: []string{"a"}}),
		eventAt("run-1", FirstSeq+1, journal.LevelOpened{Frontier: []string{"b"}}),
	}
	err := st.AppendAndProject(ctx, Projection{
		RunID: "run-1", Step: 1, Frontier: nil, Status: StatusRunning,
	}, events...)
	if err == nil {
		t.Fatalf("AppendAndProject returned nil error, want the projection's refusal")
	}
	if !strings.Contains(err.Error(), "checkpoint:") {
		t.Fatalf("error %q is not prefixed by the package name", err.Error())
	}

	stored, err := st.Read(ctx, "run-1")
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if len(stored) != 0 {
		t.Fatalf("journal holds %d events after a failed projection, want none", len(stored))
	}
	if _, ok, err := st.Latest(ctx, "run-1"); err != nil || ok {
		t.Fatalf("Latest returned (ok %t, err %v), want (false, nil)", ok, err)
	}
}

// The queued marker is written before any event exists, by plain Save. Reprojecting the
// run's terminal events must not rewrite it into something a status query cannot read: the
// row keeps its status and its step, only its state is re-derived.
func TestReprojectKeepsTheQueuedMarkerReadable(t *testing.T) {
	st := openAtomic(t)
	ctx := context.Background()

	if err := st.Save(ctx, Record{
		RunID: "run-1", Step: QueuedStep, State: graph.NewState(),
		Status: StatusQueued, GraphPath: "/graphs/demo.yaml",
	}); err != nil {
		t.Fatalf("Save queued marker: %v", err)
	}
	if err := st.AppendAndReproject(ctx, "run-1",
		eventAt("run-1", FirstSeq, journal.RunStarted{Graph: "demo"})); err != nil {
		t.Fatalf("AppendAndReproject: %v", err)
	}

	rec, ok, err := st.Latest(ctx, "run-1")
	if err != nil || !ok {
		t.Fatalf("Latest returned (ok %t, err %v), want (true, nil)", ok, err)
	}
	if rec.Step != QueuedStep || rec.Status != StatusQueued {
		t.Fatalf("row = step %d/%q, want %d/%q", rec.Step, rec.Status, QueuedStep, StatusQueued)
	}
	if rec.GraphPath != "/graphs/demo.yaml" {
		t.Fatalf("row graph path = %q, want it preserved", rec.GraphPath)
	}
}

// A terminal event arrives after the last level's row was written, with no StepInfo to
// carry a new one. It appends to the journal and re-derives the row that already exists,
// which is what keeps "the row equals Project(events)" true at the end of a run too.
func TestAppendAndReprojectRederivesTheHighestStepRow(t *testing.T) {
	st := openAtomic(t)
	ctx := context.Background()

	first := levelEvents("run-1", FirstSeq, "collect", map[string]any{"dossier": "D-1"})
	if err := st.AppendAndProject(ctx, Projection{
		RunID: "run-1", Step: 1, Frontier: nil, Status: StatusDone,
	}, first...); err != nil {
		t.Fatalf("AppendAndProject: %v", err)
	}
	if err := st.AppendAndReproject(ctx, "run-1",
		eventAt("run-1", FirstSeq+4, journal.RunFinished{})); err != nil {
		t.Fatalf("AppendAndReproject: %v", err)
	}

	events, err := st.Read(ctx, "run-1")
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if len(events) != 5 {
		t.Fatalf("journal holds %d events, want 5", len(events))
	}
	want, err := projection.Project(events)
	if err != nil {
		t.Fatalf("Project: %v", err)
	}
	rec, ok, err := st.Latest(ctx, "run-1")
	if err != nil || !ok {
		t.Fatalf("Latest returned (ok %t, err %v), want (true, nil)", ok, err)
	}
	if rec.Step != 1 || rec.Status != StatusDone {
		t.Fatalf("row = step %d/%q, want 1/%q", rec.Step, rec.Status, StatusDone)
	}
	if stateJSON(t, rec.State) != stateJSON(t, want) {
		t.Fatalf("row state = %s, want Project(events) = %s", stateJSON(t, rec.State), stateJSON(t, want))
	}
}

// A run with no row yet has nothing to keep in sync, so the events still land. Refusing
// here would make the run's opening event depend on a marker only the daemon writes, and
// the CLI's `run` writes none.
func TestAppendAndReprojectOnARunWithNoRowStillWritesTheEvents(t *testing.T) {
	st := openAtomic(t)
	ctx := context.Background()

	if err := st.AppendAndReproject(ctx, "run-1",
		eventAt("run-1", FirstSeq, journal.RunStarted{Graph: "demo"})); err != nil {
		t.Fatalf("AppendAndReproject: %v", err)
	}
	events, err := st.Read(ctx, "run-1")
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("journal holds %d events, want 1", len(events))
	}
}

func TestNextSeqAnswersFirstSeqForARunWithNoEvents(t *testing.T) {
	st := openAtomic(t)
	got, err := st.NextSeq(context.Background(), "run-1")
	if err != nil {
		t.Fatalf("NextSeq: %v", err)
	}
	if got != FirstSeq {
		t.Fatalf("NextSeq = %d, want %d", got, FirstSeq)
	}
}

func TestNextSeqContinuesARunThatAlreadyHasEvents(t *testing.T) {
	st := openAtomic(t)
	ctx := context.Background()
	if err := st.Append(ctx, "run-1", levelEvents("run-1", FirstSeq, "collect", nil)...); err != nil {
		t.Fatalf("Append: %v", err)
	}
	got, err := st.NextSeq(ctx, "run-1")
	if err != nil {
		t.Fatalf("NextSeq: %v", err)
	}
	if got != FirstSeq+4 {
		t.Fatalf("NextSeq = %d, want %d", got, FirstSeq+4)
	}
}

func TestAppendAndProjectRefusesAnEmptyRunID(t *testing.T) {
	st := openAtomic(t)
	err := st.AppendAndProject(context.Background(), Projection{Step: 0, Status: StatusRunning})
	if !errors.Is(err, ErrEmptyRunID) {
		t.Fatalf("AppendAndProject returned %v, want ErrEmptyRunID", err)
	}
}

// stateJSON renders a state through its own MarshalJSON, which carries step, frozen, data
// and zones. Comparing that one string rather than a few keys is what makes a zone label
// lost between the journal and the row a failure instead of an unasserted detail.
func stateJSON(t *testing.T, s *graph.State) string {
	t.Helper()
	b, err := json.Marshal(s)
	if err != nil {
		t.Fatalf("marshal state: %v", err)
	}
	return string(b)
}
