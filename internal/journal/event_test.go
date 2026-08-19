package journal

import (
	"encoding/json"
	"reflect"
	"testing"
	"time"
)

func sampleTime() time.Time {
	return time.Date(2026, 8, 19, 12, 0, 0, 0, time.UTC)
}

// roundTrip marshals e and unmarshals the result into a fresh Event, returning it for the
// caller to inspect. Failing here means either the JSON codec lost a field or the two
// events plain do not compare equal, both of which are the round-trip contract breaking.
func roundTrip(t *testing.T, e Event) Event {
	t.Helper()
	b, err := json.Marshal(e)
	if err != nil {
		t.Fatalf("Marshal(%+v): %v", e, err)
	}
	var got Event
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatalf("Unmarshal(%s): %v", b, err)
	}
	return got
}

func TestRunStartedEventRoundTripsThroughJSON(t *testing.T) {
	want := Event{RunID: "run-1", Seq: 1, At: sampleTime(), Payload: RunStarted{Graph: "examples/hello.yaml"}}
	got := roundTrip(t, want)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("round trip = %+v, want %+v", got, want)
	}
}

func TestRunFinishedEventRoundTripsThroughJSON(t *testing.T) {
	want := Event{RunID: "run-1", Seq: 9, At: sampleTime(), Payload: RunFinished{}}
	got := roundTrip(t, want)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("round trip = %+v, want %+v", got, want)
	}
}

func TestRunFailedEventRoundTripsThroughJSON(t *testing.T) {
	want := Event{
		RunID: "run-1", Seq: 4, At: sampleTime(),
		Payload: RunFailed{Message: "node \"b\": boom", Nodes: []string{"a", "b"}},
	}
	got := roundTrip(t, want)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("round trip = %+v, want %+v", got, want)
	}
}

func TestRunInterruptedEventRoundTripsThroughJSON(t *testing.T) {
	want := Event{RunID: "run-1", Seq: 3, At: sampleTime(), Payload: RunInterrupted{Reason: "process killed mid-level"}}
	got := roundTrip(t, want)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("round trip = %+v, want %+v", got, want)
	}
}

func TestLevelOpenedEventRoundTripsThroughJSON(t *testing.T) {
	want := Event{RunID: "run-1", Seq: 2, At: sampleTime(), Payload: LevelOpened{Frontier: []string{"a", "b"}}}
	got := roundTrip(t, want)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("round trip = %+v, want %+v", got, want)
	}
}

// TestLevelClosedEventDistinguishesTheCombinationRuleAfterRoundTrip covers the acceptance
// criterion directly: a single-node frontier replaces the shared state, a fan-out merges it
// additively, and replay cannot re-derive which happened from the resulting keys alone —
// the event has to carry the rule as data.
func TestLevelClosedEventDistinguishesTheCombinationRuleAfterRoundTrip(t *testing.T) {
	replace := roundTrip(t, Event{
		RunID: "run-1", Seq: 3, At: sampleTime(),
		Payload: LevelClosed{Frontier: []string{"a"}, Rule: CombinationReplace},
	})
	merge := roundTrip(t, Event{
		RunID: "run-1", Seq: 3, At: sampleTime(),
		Payload: LevelClosed{Frontier: []string{"a", "b"}, Rule: CombinationMerge},
	})
	rp, ok := replace.Payload.(LevelClosed)
	if !ok {
		t.Fatalf("replace payload type = %T, want LevelClosed", replace.Payload)
	}
	mp, ok := merge.Payload.(LevelClosed)
	if !ok {
		t.Fatalf("merge payload type = %T, want LevelClosed", merge.Payload)
	}
	if rp.Rule != CombinationReplace {
		t.Fatalf("replace rule = %q, want %q", rp.Rule, CombinationReplace)
	}
	if mp.Rule != CombinationMerge {
		t.Fatalf("merge rule = %q, want %q", mp.Rule, CombinationMerge)
	}
	if rp.Rule == mp.Rule {
		t.Fatalf("replace and merge rules compare equal after round trip: %q", rp.Rule)
	}
}

func TestNodeStartedEventRoundTripsThroughJSON(t *testing.T) {
	want := Event{RunID: "run-1", Seq: 5, At: sampleTime(), Payload: NodeStarted{NodeID: "a"}}
	got := roundTrip(t, want)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("round trip = %+v, want %+v", got, want)
	}
}

func TestNodeProducedEventRoundTripsThroughJSON(t *testing.T) {
	want := Event{
		RunID: "run-1", Seq: 6, At: sampleTime(),
		Payload: NodeProduced{
			NodeID: "a",
			Data:   map[string]any{"x": float64(1), "y": "hello"},
			Zones:  map[string]string{"y": "ephemeral"},
		},
	}
	got := roundTrip(t, want)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("round trip = %+v, want %+v", got, want)
	}
}

func TestNodeFailedEventRoundTripsThroughJSON(t *testing.T) {
	want := Event{RunID: "run-1", Seq: 7, At: sampleTime(), Payload: NodeFailed{NodeID: "a", Message: "boom"}}
	got := roundTrip(t, want)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("round trip = %+v, want %+v", got, want)
	}
}

func TestNudgeAppliedEventRecordsItsOrigin(t *testing.T) {
	want := Event{
		RunID: "run-1", Seq: 8, At: sampleTime(),
		Payload: NudgeApplied{Origin: "operator:alice", Data: map[string]any{"note": "resume with cap raised"}},
	}
	got := roundTrip(t, want)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("round trip = %+v, want %+v", got, want)
	}
	np, ok := got.Payload.(NudgeApplied)
	if !ok {
		t.Fatalf("payload type = %T, want NudgeApplied", got.Payload)
	}
	if np.Origin != "operator:alice" {
		t.Fatalf("origin = %q, want %q", np.Origin, "operator:alice")
	}
}

// TestFreezeAppliedEventRecordsCarriedOverAndDroppedAsSeparateFields covers the acceptance
// criterion directly: State.Freeze is a wholesale replacement, not a set of key removals,
// so modelling the event as a delta would let replay reconstruct a state the run never had
// under any non-default carry-over. Carried-over and dropped are independent fields here.
func TestFreezeAppliedEventRecordsCarriedOverAndDroppedAsSeparateFields(t *testing.T) {
	want := Event{
		RunID: "run-1", Seq: 9, At: sampleTime(),
		Payload: FreezeApplied{
			CarriedOver: map[string]any{"summary": "kept"},
			Dropped:     []string{"scratch", "draft"},
		},
	}
	got := roundTrip(t, want)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("round trip = %+v, want %+v", got, want)
	}
	fp, ok := got.Payload.(FreezeApplied)
	if !ok {
		t.Fatalf("payload type = %T, want FreezeApplied", got.Payload)
	}
	if len(fp.CarriedOver) != 1 || fp.CarriedOver["summary"] != "kept" {
		t.Fatalf("carried-over = %+v, want {summary: kept}", fp.CarriedOver)
	}
	if !reflect.DeepEqual(fp.Dropped, []string{"scratch", "draft"}) {
		t.Fatalf("dropped = %+v, want [scratch draft]", fp.Dropped)
	}
}

func TestDecodingAnUnknownEventKindFailsLoud(t *testing.T) {
	raw := `{"run_id":"run-1","seq":1,"kind":"bogus_kind","at":"2026-08-19T12:00:00Z","payload":{}}`
	var got Event
	err := json.Unmarshal([]byte(raw), &got)
	if err == nil {
		t.Fatalf("Unmarshal(%s) = nil error, want a failure naming the unknown kind", raw)
	}
}

func TestDecodingAnEventWithNoKindFailsLoud(t *testing.T) {
	raw := `{"run_id":"run-1","seq":1,"at":"2026-08-19T12:00:00Z","payload":{}}`
	var got Event
	err := json.Unmarshal([]byte(raw), &got)
	if err == nil {
		t.Fatalf("Unmarshal(%s) = nil error, want a failure for the missing kind", raw)
	}
}
