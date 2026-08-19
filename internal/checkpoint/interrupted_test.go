package checkpoint

import (
	"context"
	"testing"
	"time"

	"github.com/yoann/kern-orch/internal/journal"
)

// storeInterruptedTail writes a run whose first level closed and whose second level was
// opened, started, and then simply stops: no LevelClosed, no terminal event. That is what a
// process killed mid-level leaves behind — the record ends in the middle of a sentence.
func storeInterruptedTail(t *testing.T, st *SQLiteStore, runID string) {
	t.Helper()
	ctx := context.Background()
	first := levelEvents(runID, FirstSeq, "seed", map[string]any{"n": 3})
	if err := st.AppendAndProject(ctx, Projection{
		RunID: runID, Step: 1, Frontier: []string{"confirm"}, Status: StatusRunning,
		CreatedAt: time.Now().UTC(), GraphPath: "/graphs/demo.yaml", Requester: "yoann",
	}, first...); err != nil {
		t.Fatalf("AppendAndProject the first level: %v", err)
	}
	open := []journal.Event{
		eventAt(runID, FirstSeq+4, journal.LevelOpened{Frontier: []string{"confirm"}}),
		eventAt(runID, FirstSeq+5, journal.NodeStarted{NodeID: "confirm"}),
	}
	if err := st.Append(ctx, runID, open...); err != nil {
		t.Fatalf("Append the open level: %v", err)
	}
}

// rawEvents returns the stored JSON of a run's events exactly as the table holds it. Reading
// the blobs rather than the decoded events is the only way to assert that an untouched
// journal was not rewritten: a decode-and-re-encode round trip would hide a changed byte.
func rawEvents(t *testing.T, st *SQLiteStore, runID string) []string {
	t.Helper()
	rows, err := st.db.QueryContext(context.Background(),
		`SELECT event FROM events WHERE run_id = ? ORDER BY seq`, runID)
	if err != nil {
		t.Fatalf("query raw events: %v", err)
	}
	defer rows.Close()
	var blobs []string
	for rows.Next() {
		var blob string
		if err := rows.Scan(&blob); err != nil {
			t.Fatalf("scan raw event: %v", err)
		}
		blobs = append(blobs, blob)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("read raw events: %v", err)
	}
	return blobs
}

// The acceptance criterion: a run killed mid-level is picked up again, and the hole it left
// becomes explicit facts in its own journal — the node that started and never came back, and
// the run itself, both named rather than inferred by whoever reads the table next.
func TestResumingAnInterruptedRunClosesItsTailWithSyntheticEvents(t *testing.T) {
	st := openAtomic(t)
	ctx := context.Background()
	const runID = "run-1"
	storeInterruptedTail(t, st, runID)

	got, ok, err := st.ResumePoint(ctx, runID)
	if err != nil || !ok {
		t.Fatalf("ResumePoint returned (ok %t, err %v), want (true, nil)", ok, err)
	}
	if len(got.Frontier) != 1 || got.Frontier[0] != "confirm" {
		t.Fatalf("frontier = %v, want [confirm] — the level the journal left open", got.Frontier)
	}

	events, err := st.Read(ctx, runID)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if len(events) != 8 {
		t.Fatalf("journal holds %d events, want 8 — the six recorded plus a node failure and a run interruption", len(events))
	}
	failed, isFailure := events[6].Payload.(journal.NodeFailed)
	if !isFailure {
		t.Fatalf("event 7 is %T, want journal.NodeFailed for the node that never came back", events[6].Payload)
	}
	if failed.NodeID != "confirm" {
		t.Fatalf("the synthetic failure names node %q, want %q", failed.NodeID, "confirm")
	}
	if _, isInterrupted := events[7].Payload.(journal.RunInterrupted); !isInterrupted {
		t.Fatalf("event 8 is %T, want journal.RunInterrupted", events[7].Payload)
	}
}

// The marking, asserted on its own: a reader coming to this journal later must be able to
// tell which events were observed and which were reconstructed. Presence alone would not say
// it — a synthetic node failure reads exactly like a real one without the flag.
func TestTheEventsThatCloseAnInterruptedTailAreMarkedSynthetic(t *testing.T) {
	st := openAtomic(t)
	ctx := context.Background()
	const runID = "run-1"
	storeInterruptedTail(t, st, runID)

	if _, _, err := st.ResumePoint(ctx, runID); err != nil {
		t.Fatalf("ResumePoint: %v", err)
	}
	events, err := st.Read(ctx, runID)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	for i, ev := range events[:6] {
		if ev.Synthetic {
			t.Fatalf("event %d (%T) is marked synthetic, but it was observed as it happened", i+1, ev.Payload)
		}
	}
	for i, ev := range events[6:] {
		if !ev.Synthetic {
			t.Fatalf("event %d (%T) closes the tail but is not marked synthetic", i+7, ev.Payload)
		}
	}
}

// The state must stay one a real run could have held. The interrupted level combined nothing
// — the engine restarts a level as a whole — so closing the tail must not let the keys a node
// wrote before the process died leak into the shared state.
func TestClosingAnInterruptedTailDoesNotCreditTheLevelThatNeverCombined(t *testing.T) {
	st := openAtomic(t)
	ctx := context.Background()
	const runID = "run-1"
	storeInterruptedTail(t, st, runID)
	// The node got as far as producing, and only then did the process die: its output is in
	// the journal, but the level it belongs to never closed.
	if err := st.Append(ctx, runID, eventAt(runID, FirstSeq+6,
		journal.NodeProduced{NodeID: "confirm", Data: map[string]any{"half_done": true}})); err != nil {
		t.Fatalf("Append the partial output: %v", err)
	}

	got, _, err := st.ResumePoint(ctx, runID)
	if err != nil {
		t.Fatalf("ResumePoint: %v", err)
	}
	if _, present := got.State.Get("half_done"); present {
		t.Fatal("the resumed state carries a key from a level that never closed; the run never held it")
	}
	if v, _ := got.State.Get("n"); v != float64(3) {
		t.Fatalf("resumed state n = %v, want 3 from the level that did close", v)
	}

	events, err := st.Read(ctx, runID)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if _, isFailure := events[len(events)-2].Payload.(journal.NodeFailed); isFailure {
		t.Fatal("a node that had already produced was recorded as having failed; the tail invented a failure")
	}
}

// A journal that simply stops after a closed level is the shape a stopped run leaves today:
// its terminal events are written through the run's own context, which cancellation already
// refused. It resumes correctly from the row's frontier either way — but the record still has
// to say the run was interrupted, because "the record stops" and "the run ended" are not the
// same fact and nothing else in the table distinguishes them.
func TestATruncatedJournalIsMarkedInterruptedEvenWithNoLevelLeftOpen(t *testing.T) {
	st := openAtomic(t)
	ctx := context.Background()
	const runID = "run-1"
	if err := st.AppendAndProject(ctx, Projection{
		RunID: runID, Step: 1, Frontier: []string{"double"}, Status: StatusRunning,
		GraphPath: "/graphs/demo.yaml",
	}, levelEvents(runID, FirstSeq, "seed", map[string]any{"n": 3})...); err != nil {
		t.Fatalf("AppendAndProject: %v", err)
	}

	got, _, err := st.ResumePoint(ctx, runID)
	if err != nil {
		t.Fatalf("ResumePoint: %v", err)
	}
	if len(got.Frontier) != 1 || got.Frontier[0] != "double" {
		t.Fatalf("frontier = %v, want [double] from the row — the journal names none", got.Frontier)
	}

	events, err := st.Read(ctx, runID)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if len(events) != 5 {
		t.Fatalf("journal holds %d events, want 5 — the four recorded plus the interruption", len(events))
	}
	last := events[4]
	if _, isInterrupted := last.Payload.(journal.RunInterrupted); !isInterrupted {
		t.Fatalf("last event is %T, want journal.RunInterrupted", last.Payload)
	}
	if !last.Synthetic {
		t.Fatal("the interruption is not marked synthetic, so a reader would take it for an observed event")
	}
}

// A run that ended on a terminal event is a complete record, and completing it again would
// put a second ending in it. The bytes are compared, not the decoded events: rewriting a
// stored row to an equivalent encoding is still a rewrite of a record that is meant to be
// append-only.
func TestResumingARunWhoseJournalSaysItFailedLeavesTheBytesUntouched(t *testing.T) {
	st := openAtomic(t)
	ctx := context.Background()
	const runID = "run-1"
	storeSeedThenOpenConfirm(t, st, runID)
	before := rawEvents(t, st, runID)

	if _, _, err := st.ResumePoint(ctx, runID); err != nil {
		t.Fatalf("ResumePoint: %v", err)
	}

	after := rawEvents(t, st, runID)
	if len(after) != len(before) {
		t.Fatalf("journal grew from %d to %d events; a coherent record gained synthetic events", len(before), len(after))
	}
	for i := range before {
		if before[i] != after[i] {
			t.Fatalf("event %d was rewritten:\n before %s\n  after %s", i+1, before[i], after[i])
		}
	}
}

// The same, for the run that reached its end. Nothing about a finished run is missing, so
// resume must add nothing to it whatever it then does with the empty frontier.
func TestResumingAFinishedRunLeavesTheBytesUntouched(t *testing.T) {
	st := openAtomic(t)
	ctx := context.Background()
	const runID = "run-1"
	events := append(levelEvents(runID, FirstSeq, "seed", map[string]any{"n": 3}),
		eventAt(runID, FirstSeq+4, journal.RunFinished{}))
	if err := st.AppendAndProject(ctx, Projection{
		RunID: runID, Step: 1, Frontier: nil, Status: StatusDone,
	}, events...); err != nil {
		t.Fatalf("AppendAndProject: %v", err)
	}
	before := rawEvents(t, st, runID)

	if _, _, err := st.ResumePoint(ctx, runID); err != nil {
		t.Fatalf("ResumePoint: %v", err)
	}

	after := rawEvents(t, st, runID)
	if len(after) != len(before) {
		t.Fatalf("journal grew from %d to %d events; a finished run gained synthetic events", len(before), len(after))
	}
	for i := range before {
		if before[i] != after[i] {
			t.Fatalf("event %d was rewritten:\n before %s\n  after %s", i+1, before[i], after[i])
		}
	}
}

// Closing a tail must be a one-off, not something every look at the run repeats. Once the
// interruption is recorded the journal is closed, so the second call finds a complete record
// and adds nothing — otherwise a run inspected twice would grow an ending each time.
func TestClosingAnInterruptedTailHappensOnlyOnce(t *testing.T) {
	st := openAtomic(t)
	ctx := context.Background()
	const runID = "run-1"
	storeInterruptedTail(t, st, runID)

	if _, _, err := st.ResumePoint(ctx, runID); err != nil {
		t.Fatalf("first ResumePoint: %v", err)
	}
	closed := rawEvents(t, st, runID)
	if _, _, err := st.ResumePoint(ctx, runID); err != nil {
		t.Fatalf("second ResumePoint: %v", err)
	}
	again := rawEvents(t, st, runID)

	if len(again) != len(closed) {
		t.Fatalf("journal grew from %d to %d events on a second resume", len(closed), len(again))
	}
	for i := range closed {
		if closed[i] != again[i] {
			t.Fatalf("event %d was rewritten by the second resume:\n before %s\n  after %s", i+1, closed[i], again[i])
		}
	}
}

// The synthetic events go through Append's sequence contract, not around it: they continue
// the run's stored sequence exactly, contiguously, and the events already there keep the
// numbers they were written with.
func TestTheSyntheticTailContinuesTheStoredSequence(t *testing.T) {
	st := openAtomic(t)
	ctx := context.Background()
	const runID = "run-1"
	storeInterruptedTail(t, st, runID)

	if _, _, err := st.ResumePoint(ctx, runID); err != nil {
		t.Fatalf("ResumePoint: %v", err)
	}
	events, err := st.Read(ctx, runID)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	for i, ev := range events {
		if want := FirstSeq + int64(i); ev.Seq != want {
			t.Fatalf("event %d has seq %d, want %d — the sequence is not contiguous", i+1, ev.Seq, want)
		}
	}
	next, err := st.NextSeq(ctx, runID)
	if err != nil {
		t.Fatalf("NextSeq: %v", err)
	}
	if want := FirstSeq + int64(len(events)); next != want {
		t.Fatalf("next seq = %d, want %d", next, want)
	}
}
