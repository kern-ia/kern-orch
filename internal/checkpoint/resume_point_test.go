package checkpoint

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/yoann/kern-orch/internal/graph"
	"github.com/yoann/kern-orch/internal/journal"
)

// seedThenOpenConfirm is the journal of a run that completed one level and then opened a
// second one it never closed — what a run interrupted or failed inside a level leaves behind.
func seedThenOpenConfirm(runID string) []journal.Event {
	events := levelEvents(runID, FirstSeq, "seed", map[string]any{"n": 3})
	return append(events,
		eventAt(runID, FirstSeq+4, journal.LevelOpened{Frontier: []string{"confirm"}}),
		eventAt(runID, FirstSeq+5, journal.NodeStarted{NodeID: "confirm"}),
		eventAt(runID, FirstSeq+6, journal.NodeFailed{NodeID: "confirm", Message: "context canceled"}),
		eventAt(runID, FirstSeq+7, journal.RunFailed{Message: "context canceled", Nodes: []string{"confirm"}}),
	)
}

// storeSeedThenOpenConfirm writes that run: the first level through the atomic path, so the
// row is a real projection row, then the aborted level's events, which no row summarises —
// exactly what the engine leaves when a level fails.
func storeSeedThenOpenConfirm(t *testing.T, st *SQLiteStore, runID string) {
	t.Helper()
	ctx := context.Background()
	all := seedThenOpenConfirm(runID)
	err := st.AppendAndProject(ctx, Projection{
		RunID: runID, Step: 1, Frontier: []string{"confirm"}, Status: StatusRunning,
		CreatedAt: time.Now().UTC(), GraphPath: "/graphs/demo.yaml", Requester: "yoann", Dossier: "D-1",
	}, all[:4]...)
	if err != nil {
		t.Fatalf("AppendAndProject: %v", err)
	}
	if err := st.Append(ctx, runID, all[4:]...); err != nil {
		t.Fatalf("Append the aborted level: %v", err)
	}
}

// The criterion the issue turns on. The row is a cache, so it is allowed to be wrong; what
// resume runs from must not be. Here it is made wrong on purpose — a state no run ever had,
// written over the row through the store's own Save — and the answer must not move.
func TestCorruptingTheCachedRowDoesNotChangeWhatResumeReconstructs(t *testing.T) {
	st := openAtomic(t)
	ctx := context.Background()
	const runID = "run-1"
	storeSeedThenOpenConfirm(t, st, runID)

	clean, ok, err := st.ResumePoint(ctx, runID)
	if err != nil || !ok {
		t.Fatalf("ResumePoint before corruption returned (ok %t, err %v), want (true, nil)", ok, err)
	}

	rec, _, err := st.Latest(ctx, runID)
	if err != nil {
		t.Fatalf("Latest: %v", err)
	}
	lie := graph.NewState()
	lie.Set("n", 999)
	lie.Set("never_happened", true)
	rec.State = lie
	if err := st.Save(ctx, rec); err != nil {
		t.Fatalf("Save the corrupted row: %v", err)
	}

	got, ok, err := st.ResumePoint(ctx, runID)
	if err != nil || !ok {
		t.Fatalf("ResumePoint after corruption returned (ok %t, err %v), want (true, nil)", ok, err)
	}
	if v, _ := got.State.Get("n"); v != float64(3) {
		t.Fatalf("resume state n = %v, want 3 — the journal's value, not the row's 999", v)
	}
	if _, present := got.State.Get("never_happened"); present {
		t.Fatal("resume state carries a key only the corrupted row held; the row's state was read")
	}
	if stateJSON(t, got.State) != stateJSON(t, clean.State) {
		t.Fatalf("corrupting the row moved the reconstructed state from %s to %s",
			stateJSON(t, clean.State), stateJSON(t, got.State))
	}
}

// A level that was opened and never closed is the level to restart, and the journal is where
// that frontier is written: no row is ever saved for a level that did not close, so the row
// still names the frontier of the level before it.
func TestResumePointRestartsTheLevelTheJournalLeftOpen(t *testing.T) {
	st := openAtomic(t)
	ctx := context.Background()
	const runID = "run-1"
	storeSeedThenOpenConfirm(t, st, runID)

	got, ok, err := st.ResumePoint(ctx, runID)
	if err != nil || !ok {
		t.Fatalf("ResumePoint returned (ok %t, err %v), want (true, nil)", ok, err)
	}
	if len(got.Frontier) != 1 || got.Frontier[0] != "confirm" {
		t.Fatalf("frontier = %v, want [confirm]", got.Frontier)
	}
	if got.GraphPath != "/graphs/demo.yaml" || got.Requester != "yoann" || got.Dossier != "D-1" {
		t.Fatalf("provenance = (%q, %q, %q), want (/graphs/demo.yaml, yoann, D-1)",
			got.GraphPath, got.Requester, got.Dossier)
	}
	if got.Step != 1 {
		t.Fatalf("step = %d, want 1 — the step the replayed state carries", got.Step)
	}
}

// A run killed between two levels closed every level it opened, so the journal names no
// frontier to restart. The engine derives the next frontier from the routes of the branches
// it combined, and a route's result is not an event — the row is the only record of it, and
// it is a position rather than a state.
func TestResumePointFallsBackToTheRowsFrontierWhenNoLevelIsOpen(t *testing.T) {
	st := openAtomic(t)
	ctx := context.Background()
	const runID = "run-1"
	err := st.AppendAndProject(ctx, Projection{
		RunID: runID, Step: 1, Frontier: []string{"double"}, Status: StatusRunning,
		GraphPath: "/graphs/demo.yaml",
	}, levelEvents(runID, FirstSeq, "seed", map[string]any{"n": 3})...)
	if err != nil {
		t.Fatalf("AppendAndProject: %v", err)
	}

	got, ok, err := st.ResumePoint(ctx, runID)
	if err != nil || !ok {
		t.Fatalf("ResumePoint returned (ok %t, err %v), want (true, nil)", ok, err)
	}
	if len(got.Frontier) != 1 || got.Frontier[0] != "double" {
		t.Fatalf("frontier = %v, want [double] from the row", got.Frontier)
	}
	if v, _ := got.State.Get("n"); v != float64(3) {
		t.Fatalf("state n = %v, want 3 from the journal", v)
	}
}

// A run whose journal says it finished has nothing left to run, whatever the row says. The
// row is a cache and may lag; the journal is the record, so it decides.
func TestResumePointReportsNoFrontierWhenTheJournalSaysTheRunFinished(t *testing.T) {
	st := openAtomic(t)
	ctx := context.Background()
	const runID = "run-1"
	events := append(levelEvents(runID, FirstSeq, "seed", map[string]any{"n": 3}),
		eventAt(runID, FirstSeq+4, journal.RunFinished{}))
	// The row deliberately still names a frontier: a stale cache is the case being tested.
	if err := st.AppendAndProject(ctx, Projection{
		RunID: runID, Step: 1, Frontier: []string{"double"}, Status: StatusRunning,
	}, events...); err != nil {
		t.Fatalf("AppendAndProject: %v", err)
	}

	got, ok, err := st.ResumePoint(ctx, runID)
	if err != nil || !ok {
		t.Fatalf("ResumePoint returned (ok %t, err %v), want (true, nil)", ok, err)
	}
	if len(got.Frontier) != 0 {
		t.Fatalf("frontier = %v, want none — the journal records the run as finished", got.Frontier)
	}
}

// A run checkpointed before this repo had a journal has a row and no events at all. Replaying
// it projects an empty state, which is not the state that run held — it is the absence of a
// record. Resuming from it would restart the run's remaining frontier against a state with
// nothing in it, and every node downstream would read blanks where a value stood. Refusing
// names the run, so the operator can decide; the one thing that must never happen is that it
// resumes quietly.
func TestResumeRefusesARunThatHasACheckpointButNoJournal(t *testing.T) {
	st := openAtomic(t)
	ctx := context.Background()
	const runID = "legacy-run"
	old := graph.NewState()
	old.Set("n", 3)
	if err := st.Save(ctx, Record{
		RunID: runID, Step: 1, Frontier: []string{"double"}, State: old,
		Status: StatusRunning, GraphPath: "/graphs/demo.yaml",
	}); err != nil {
		t.Fatalf("Save: %v", err)
	}

	_, _, err := st.ResumePoint(ctx, runID)
	if !errors.Is(err, ErrNoJournal) {
		t.Fatalf("ResumePoint error = %v, want ErrNoJournal", err)
	}
	if !strings.Contains(err.Error(), runID) {
		t.Fatalf("error %q does not name the run %q", err, runID)
	}
}

// A run nothing was ever recorded for is not an error: `resume` on an unknown id has always
// answered "no checkpoint", and that message is about the id, not about the journal.
func TestResumePointReportsAnUnknownRunAsAbsent(t *testing.T) {
	st := openAtomic(t)
	_, ok, err := st.ResumePoint(context.Background(), "never-existed")
	if err != nil {
		t.Fatalf("ResumePoint on an unknown run returned error %v, want nil", err)
	}
	if ok {
		t.Fatal("ResumePoint reported an unknown run as resumable")
	}
}
