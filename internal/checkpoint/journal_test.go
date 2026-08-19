package checkpoint

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/yoann/kern-orch/internal/journal"
)

// The SQLite store is the only Journal there is today, and the interface exists so issue
// 04's projection and the reporter can depend on reading events without depending on the
// checkpoint half. Asserting it here keeps the two from drifting silently.
var _ Journal = (*SQLiteStore)(nil)

// openJournal gives each test its own database file. The journal's whole contract is about
// what is already stored for a run, so a shared file would let one test's sequence decide
// another test's next-seq.
func openJournal(t *testing.T) *SQLiteStore {
	t.Helper()
	st, err := OpenSQLite(filepath.Join(t.TempDir(), "journal.db"))
	if err != nil {
		t.Fatalf("OpenSQLite: %v", err)
	}
	t.Cleanup(func() { st.Close() })
	return st
}

// eventAt builds one event of a run at seq. The payload varies with seq so an assertion on
// ordering cannot pass by accident on a table full of identical rows.
func eventAt(runID string, seq int64, p journal.Payload) journal.Event {
	return journal.Event{
		RunID:   runID,
		Seq:     seq,
		At:      time.Date(2026, 8, 19, 12, 0, int(seq), 0, time.UTC),
		Payload: p,
	}
}

func TestAppendThenReadReturnsTheExactEventsInSequenceOrder(t *testing.T) {
	st := openJournal(t)
	ctx := context.Background()

	want := []journal.Event{
		eventAt("run-a", 1, journal.RunStarted{Graph: "pipeline.yaml"}),
		eventAt("run-a", 2, journal.LevelOpened{Frontier: []string{"extract", "classify"}}),
		eventAt("run-a", 3, journal.NodeProduced{
			NodeID: "extract",
			Data:   map[string]any{"count": float64(3)},
			Zones:  map[string]string{"count": "level"},
		}),
		eventAt("run-a", 4, journal.LevelClosed{Frontier: []string{"extract", "classify"}, Rule: journal.CombinationMerge}),
		eventAt("run-a", 5, journal.RunFinished{}),
	}
	if err := st.Append(ctx, "run-a", want...); err != nil {
		t.Fatalf("Append: %v", err)
	}

	got, err := st.Read(ctx, "run-a")
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if len(got) != len(want) {
		t.Fatalf("Read returned %d events; want %d", len(got), len(want))
	}
	for i := range want {
		if got[i].Seq != want[i].Seq {
			t.Fatalf("event %d seq = %d; want %d", i, got[i].Seq, want[i].Seq)
		}
		if got[i].RunID != want[i].RunID {
			t.Fatalf("event %d run id = %q; want %q", i, got[i].RunID, want[i].RunID)
		}
		if !got[i].At.Equal(want[i].At) {
			t.Fatalf("event %d at = %v; want %v", i, got[i].At, want[i].At)
		}
	}
	produced, ok := got[2].Payload.(journal.NodeProduced)
	if !ok {
		t.Fatalf("event 2 payload = %T; want journal.NodeProduced", got[2].Payload)
	}
	if produced.NodeID != "extract" || produced.Zones["count"] != "level" {
		t.Fatalf("event 2 payload = %+v; want node extract with zone level on count", produced)
	}
	closed, ok := got[3].Payload.(journal.LevelClosed)
	if !ok {
		t.Fatalf("event 3 payload = %T; want journal.LevelClosed", got[3].Payload)
	}
	if closed.Rule != journal.CombinationMerge {
		t.Fatalf("event 3 rule = %q; want %q", closed.Rule, journal.CombinationMerge)
	}
}

func TestAppendAcceptsSuccessiveBatchesThatContinueTheSequence(t *testing.T) {
	st := openJournal(t)
	ctx := context.Background()

	if err := st.Append(ctx, "run-a", eventAt("run-a", 1, journal.RunStarted{Graph: "g"})); err != nil {
		t.Fatalf("Append (first batch): %v", err)
	}
	if err := st.Append(ctx, "run-a",
		eventAt("run-a", 2, journal.NodeStarted{NodeID: "n1"}),
		eventAt("run-a", 3, journal.NodeFailed{NodeID: "n1", Message: "boom"}),
	); err != nil {
		t.Fatalf("Append (second batch): %v", err)
	}

	got, err := st.Read(ctx, "run-a")
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("Read returned %d events; want 3", len(got))
	}
	if got[0].Seq != 1 || got[1].Seq != 2 || got[2].Seq != 3 {
		t.Fatalf("sequence = %d,%d,%d; want 1,2,3", got[0].Seq, got[1].Seq, got[2].Seq)
	}
}

func TestTheFirstEventOfARunMustCarrySeqOne(t *testing.T) {
	st := openJournal(t)
	ctx := context.Background()

	// Seq 0 is Go's zero value: an event whose Seq was simply never set must not look like
	// a valid first event, which is why the sequence starts at FirstSeq rather than at 0.
	err := st.Append(ctx, "run-a", eventAt("run-a", 0, journal.RunStarted{Graph: "g"}))
	if err == nil {
		t.Fatal("Append of a zero-seq first event = nil error; want refusal")
	}
	if !errors.Is(err, ErrSeqMismatch) {
		t.Fatalf("error = %v; want one matching ErrSeqMismatch", err)
	}
}

func TestAppendRejectsABatchThatSkipsSeqAndLeavesTheTableUnchanged(t *testing.T) {
	st := openJournal(t)
	ctx := context.Background()

	if err := st.Append(ctx, "run-a", eventAt("run-a", 1, journal.RunStarted{Graph: "g"})); err != nil {
		t.Fatalf("Append (first batch): %v", err)
	}
	// Seq 3 with 2 never written: accepting this would leave a hole nothing downstream
	// could distinguish from an event that simply has not been read yet.
	err := st.Append(ctx, "run-a",
		eventAt("run-a", 3, journal.NodeStarted{NodeID: "n1"}),
		eventAt("run-a", 4, journal.NodeStarted{NodeID: "n2"}),
	)
	if err == nil {
		t.Fatal("Append of a gapped batch = nil error; want refusal")
	}
	if !errors.Is(err, ErrSeqMismatch) {
		t.Fatalf("error = %v; want one matching ErrSeqMismatch", err)
	}

	got, err := st.Read(ctx, "run-a")
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("Read returned %d events after the refused batch; want 1 (the table unchanged)", len(got))
	}
}

func TestAppendRejectsAReplayOfAlreadyStoredSeqs(t *testing.T) {
	st := openJournal(t)
	ctx := context.Background()

	if err := st.Append(ctx, "run-a",
		eventAt("run-a", 1, journal.RunStarted{Graph: "g"}),
		eventAt("run-a", 2, journal.NodeStarted{NodeID: "n1"}),
	); err != nil {
		t.Fatalf("Append (first batch): %v", err)
	}
	err := st.Append(ctx, "run-a", eventAt("run-a", 2, journal.NodeStarted{NodeID: "n1"}))
	if err == nil {
		t.Fatal("Append replaying seq 2 = nil error; want refusal")
	}
	if !errors.Is(err, ErrSeqMismatch) {
		t.Fatalf("error = %v; want one matching ErrSeqMismatch", err)
	}

	got, err := st.Read(ctx, "run-a")
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("Read returned %d events after the refused replay; want 2", len(got))
	}
}

func TestAppendRejectsABatchThatIsNotContiguousWithinItself(t *testing.T) {
	st := openJournal(t)
	ctx := context.Background()

	// The first seq is correct here, so only an in-batch check catches the hole: without
	// one, a batch could pass the stored next-seq test and still store a gap.
	err := st.Append(ctx, "run-a",
		eventAt("run-a", 1, journal.RunStarted{Graph: "g"}),
		eventAt("run-a", 3, journal.RunFinished{}),
	)
	if err == nil {
		t.Fatal("Append of an internally gapped batch = nil error; want refusal")
	}
	if !errors.Is(err, ErrSeqMismatch) {
		t.Fatalf("error = %v; want one matching ErrSeqMismatch", err)
	}

	got, err := st.Read(ctx, "run-a")
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("Read returned %d events after the refused batch; want 0", len(got))
	}
}

func TestAppendRejectsAnEventBelongingToAnotherRun(t *testing.T) {
	st := openJournal(t)
	ctx := context.Background()

	err := st.Append(ctx, "run-a", eventAt("run-b", 1, journal.RunStarted{Graph: "g"}))
	if err == nil {
		t.Fatal("Append of an event carrying another run id = nil error; want refusal")
	}
	if !errors.Is(err, ErrRunIDMismatch) {
		t.Fatalf("error = %v; want one matching ErrRunIDMismatch", err)
	}
}

func TestAppendFillsAnUnsetRunIDFromTheRunBeingAppendedTo(t *testing.T) {
	st := openJournal(t)
	ctx := context.Background()

	// An empty run id carries no claim that could contradict the argument, so filling it
	// cannot silently redirect an event the way overwriting a wrong one would.
	if err := st.Append(ctx, "run-a", eventAt("", 1, journal.RunStarted{Graph: "g"})); err != nil {
		t.Fatalf("Append: %v", err)
	}
	got, err := st.Read(ctx, "run-a")
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("Read returned %d events; want 1", len(got))
	}
	if got[0].RunID != "run-a" {
		t.Fatalf("stored run id = %q; want %q", got[0].RunID, "run-a")
	}
}

func TestAppendRejectsAnEmptyRunID(t *testing.T) {
	st := openJournal(t)

	err := st.Append(context.Background(), "", eventAt("", 1, journal.RunFinished{}))
	if !errors.Is(err, ErrEmptyRunID) {
		t.Fatalf("error = %v; want one matching ErrEmptyRunID", err)
	}
}

func TestAppendOfAnEmptyBatchIsANoOp(t *testing.T) {
	st := openJournal(t)
	ctx := context.Background()

	if err := st.Append(ctx, "run-a"); err != nil {
		t.Fatalf("Append of no events: %v", err)
	}
	got, err := st.Read(ctx, "run-a")
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("Read returned %d events; want 0", len(got))
	}
}

// unserializable is a payload whose Data holds a channel, which encoding/json refuses. It
// stands in for any value a caller puts in NodeProduced.Data that cannot cross JSON — the
// realistic failure, since Data is a map[string]any filled from agent output.
func unserializablePayload() journal.Payload {
	return journal.NodeProduced{NodeID: "n1", Data: map[string]any{"ch": make(chan int)}}
}

func TestAppendRejectsAnUnserializablePayloadNamingTheOffendingType(t *testing.T) {
	st := openJournal(t)
	ctx := context.Background()

	err := st.Append(ctx, "run-a", eventAt("run-a", 1, unserializablePayload()))
	if err == nil {
		t.Fatal("Append of an unserializable payload = nil error; want refusal")
	}
	if !strings.Contains(err.Error(), "chan int") {
		t.Fatalf("error = %q; want it to name the offending type %q", err.Error(), "chan int")
	}

	got, err := st.Read(ctx, "run-a")
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("Read returned %d events after the refused append; want 0 (no broken row stored)", len(got))
	}
}

func TestAppendStoresNothingWhenALaterEventOfTheBatchCannotBeEncoded(t *testing.T) {
	st := openJournal(t)
	ctx := context.Background()

	// Encoding happens for the whole batch before anything is written, so the good first
	// event must not survive the bad second one. Otherwise the run's next-seq would move
	// on an append the caller was told had failed.
	err := st.Append(ctx, "run-a",
		eventAt("run-a", 1, journal.RunStarted{Graph: "g"}),
		eventAt("run-a", 2, unserializablePayload()),
	)
	if err == nil {
		t.Fatal("Append of a batch with an unserializable event = nil error; want refusal")
	}
	got, err := st.Read(ctx, "run-a")
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("Read returned %d events; want 0", len(got))
	}
}

func TestReadOfARunWithNoEventsReturnsNothingRatherThanAnError(t *testing.T) {
	st := openJournal(t)

	got, err := st.Read(context.Background(), "never-ran")
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("Read returned %d events; want 0", len(got))
	}
}

func TestReadFromReturnsOnlyTheSuffixAtOrAfterTheGivenSeq(t *testing.T) {
	st := openJournal(t)
	ctx := context.Background()

	if err := st.Append(ctx, "run-a",
		eventAt("run-a", 1, journal.RunStarted{Graph: "g"}),
		eventAt("run-a", 2, journal.NodeStarted{NodeID: "n1"}),
		eventAt("run-a", 3, journal.NodeStarted{NodeID: "n2"}),
		eventAt("run-a", 4, journal.RunFinished{}),
	); err != nil {
		t.Fatalf("Append: %v", err)
	}

	got, err := st.ReadFrom(ctx, "run-a", 3)
	if err != nil {
		t.Fatalf("ReadFrom: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("ReadFrom(3) returned %d events; want 2", len(got))
	}
	if got[0].Seq != 3 || got[1].Seq != 4 {
		t.Fatalf("ReadFrom(3) sequence = %d,%d; want 3,4", got[0].Seq, got[1].Seq)
	}
}

func TestReadFromPastTheEndReturnsAnEmptySliceRatherThanAnError(t *testing.T) {
	st := openJournal(t)
	ctx := context.Background()

	if err := st.Append(ctx, "run-a", eventAt("run-a", 1, journal.RunStarted{Graph: "g"})); err != nil {
		t.Fatalf("Append: %v", err)
	}

	// A consumer that already applied the whole journal asks for the suffix after it; an
	// error there would make "nothing new" indistinguishable from a real failure.
	for _, from := range []int64{2, 99} {
		got, err := st.ReadFrom(ctx, "run-a", from)
		if err != nil {
			t.Fatalf("ReadFrom(%d): %v", from, err)
		}
		if len(got) != 0 {
			t.Fatalf("ReadFrom(%d) returned %d events; want 0", from, len(got))
		}
	}
}

func TestReadFromANegativeSeqIsRefused(t *testing.T) {
	st := openJournal(t)

	got, err := st.ReadFrom(context.Background(), "run-a", -1)
	if err == nil {
		t.Fatalf("ReadFrom(-1) = %d events, nil error; want refusal", len(got))
	}
	if !errors.Is(err, ErrInvalidSeq) {
		t.Fatalf("error = %v; want one matching ErrInvalidSeq", err)
	}
}

func TestTwoRunsInterleavingAppendsKeepIndependentSequences(t *testing.T) {
	st := openJournal(t)
	ctx := context.Background()

	if err := st.Append(ctx, "run-a", eventAt("run-a", 1, journal.RunStarted{Graph: "a"})); err != nil {
		t.Fatalf("Append run-a 1: %v", err)
	}
	if err := st.Append(ctx, "run-b", eventAt("run-b", 1, journal.RunStarted{Graph: "b"})); err != nil {
		t.Fatalf("Append run-b 1: %v", err)
	}
	if err := st.Append(ctx, "run-a", eventAt("run-a", 2, journal.RunFinished{})); err != nil {
		t.Fatalf("Append run-a 2: %v", err)
	}
	if err := st.Append(ctx, "run-b", eventAt("run-b", 2, journal.RunFinished{})); err != nil {
		t.Fatalf("Append run-b 2: %v", err)
	}

	for _, runID := range []string{"run-a", "run-b"} {
		got, err := st.Read(ctx, runID)
		if err != nil {
			t.Fatalf("Read %s: %v", runID, err)
		}
		if len(got) != 2 {
			t.Fatalf("Read %s returned %d events; want 2", runID, len(got))
		}
		if got[0].Seq != 1 || got[1].Seq != 2 {
			t.Fatalf("Read %s sequence = %d,%d; want 1,2", runID, got[0].Seq, got[1].Seq)
		}
		if got[0].RunID != runID {
			t.Fatalf("Read %s returned an event of run %q", runID, got[0].RunID)
		}
	}
}

func TestConcurrentRunsAppendWithoutBorrowingEachOthersSequences(t *testing.T) {
	st := openJournal(t)
	ctx := context.Background()

	const perRun = 40
	runIDs := []string{"run-a", "run-b", "run-c"}

	var wg sync.WaitGroup
	errs := make(chan error, len(runIDs)*perRun)
	for _, runID := range runIDs {
		wg.Add(1)
		go func(runID string) {
			defer wg.Done()
			for seq := int64(1); seq <= perRun; seq++ {
				if err := st.Append(ctx, runID, eventAt(runID, seq, journal.NodeStarted{NodeID: "n"})); err != nil {
					errs <- err
					return
				}
			}
		}(runID)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatalf("concurrent Append: %v", err)
	}

	for _, runID := range runIDs {
		got, err := st.Read(ctx, runID)
		if err != nil {
			t.Fatalf("Read %s: %v", runID, err)
		}
		if len(got) != perRun {
			t.Fatalf("Read %s returned %d events; want %d", runID, len(got), perRun)
		}
		for i, ev := range got {
			if ev.Seq != int64(i+1) {
				t.Fatalf("Read %s event %d seq = %d; want %d", runID, i, ev.Seq, i+1)
			}
			if ev.RunID != runID {
				t.Fatalf("Read %s event %d run id = %q; want %q", runID, i, ev.RunID, runID)
			}
		}
	}
}

func TestReadingOneRunWhileAnotherGoroutineAppendsSeesOnlyDensePrefixes(t *testing.T) {
	st := openJournal(t)
	ctx := context.Background()

	// The acceptance criterion this test exists for is `go test -race`: OpenSQLite pins the
	// pool to a single connection, and until this issue nothing in the repo ever exercised
	// two goroutines against that pin at once. What is asserted on top of the race detector
	// is that a reader never observes a hole — a partially committed batch would show up as
	// a prefix whose seqs skip.
	const total = 60
	done := make(chan error, 1)
	go func() {
		for seq := int64(1); seq <= total; seq++ {
			if err := st.Append(ctx, "run-a", eventAt("run-a", seq, journal.NodeStarted{NodeID: "n"})); err != nil {
				done <- err
				return
			}
		}
		done <- nil
	}()

	for {
		got, err := st.Read(ctx, "run-a")
		if err != nil {
			t.Fatalf("concurrent Read: %v", err)
		}
		for i, ev := range got {
			if ev.Seq != int64(i+1) {
				t.Fatalf("concurrent Read saw event %d at seq %d; want a dense prefix starting at 1", i, ev.Seq)
			}
		}
		select {
		case err := <-done:
			if err != nil {
				t.Fatalf("concurrent Append: %v", err)
			}
			final, err := st.Read(ctx, "run-a")
			if err != nil {
				t.Fatalf("final Read: %v", err)
			}
			if len(final) != total {
				t.Fatalf("final Read returned %d events; want %d", len(final), total)
			}
			return
		default:
		}
	}
}

func TestAppendSurvivesReopeningTheDatabase(t *testing.T) {
	path := filepath.Join(t.TempDir(), "reopen.db")
	ctx := context.Background()

	st, err := OpenSQLite(path)
	if err != nil {
		t.Fatalf("OpenSQLite (first): %v", err)
	}
	if err := st.Append(ctx, "run-a",
		eventAt("run-a", 1, journal.RunStarted{Graph: "g"}),
		eventAt("run-a", 2, journal.NudgeApplied{Origin: "operator", Data: map[string]any{"k": "v"}}),
	); err != nil {
		t.Fatalf("Append: %v", err)
	}
	st.Close()

	// The next-seq check reads the table, not in-memory state: a fresh process resuming a
	// run must continue the stored sequence rather than restart it.
	st2, err := OpenSQLite(path)
	if err != nil {
		t.Fatalf("OpenSQLite (second): %v", err)
	}
	defer st2.Close()
	if err := st2.Append(ctx, "run-a", eventAt("run-a", 3, journal.RunFinished{})); err != nil {
		t.Fatalf("Append after reopen: %v", err)
	}
	if err := st2.Append(ctx, "run-a", eventAt("run-a", 1, journal.RunFinished{})); !errors.Is(err, ErrSeqMismatch) {
		t.Fatalf("Append restarting the sequence after reopen = %v; want one matching ErrSeqMismatch", err)
	}

	got, err := st2.Read(ctx, "run-a")
	if err != nil {
		t.Fatalf("Read after reopen: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("Read after reopen returned %d events; want 3", len(got))
	}
	nudge, ok := got[1].Payload.(journal.NudgeApplied)
	if !ok {
		t.Fatalf("event 1 payload = %T; want journal.NudgeApplied", got[1].Payload)
	}
	if nudge.Origin != "operator" {
		t.Fatalf("nudge origin after reopen = %q; want %q", nudge.Origin, "operator")
	}
}
