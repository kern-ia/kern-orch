package cmd

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/yoann/kern-orch/internal/checkpoint"
	"github.com/yoann/kern-orch/internal/graph"
	"github.com/yoann/kern-orch/internal/journal"
	"github.com/yoann/kern-orch/internal/journal/projection"
)

// openRecorderStore gives each test its own database, for the reason the journal's own tests
// give: every assertion here is about one run's stored sequence, and a shared file would let
// one test decide another's next-seq.
func openRecorderStore(t *testing.T) *checkpoint.SQLiteStore {
	t.Helper()
	st, err := checkpoint.OpenSQLite(filepath.Join(t.TempDir(), "recorder.db"))
	if err != nil {
		t.Fatalf("OpenSQLite: %v", err)
	}
	t.Cleanup(func() { st.Close() })
	return st
}

func newRecorder(t *testing.T, st *checkpoint.SQLiteStore, runID string) *journalRecorder {
	t.Helper()
	rec, err := newJournalRecorder(context.Background(), st, runID, "demo")
	if err != nil {
		t.Fatalf("newJournalRecorder: %v", err)
	}
	return rec
}

// A level's events are held until the level's own hook writes them with the row. Observing
// one without the other is what the atomicity is for, so the buffering is asserted directly
// rather than inferred from the end state.
func TestALevelsEventsAreNotStoredUntilItsCheckpointHookRuns(t *testing.T) {
	st := openRecorderStore(t)
	ctx := context.Background()
	rec := newRecorder(t, st, "run-1")

	for _, ev := range []graph.Event{
		{Kind: graph.EventLevelOpened, Frontier: []string{"collect"}},
		{Kind: graph.EventNodeStarted, NodeID: "collect"},
		{Kind: graph.EventNodeProduced, NodeID: "collect", Data: map[string]any{"dossier": "D-1"}},
		{Kind: graph.EventLevelClosed, Frontier: []string{"collect"}, Rule: graph.CombinationReplace},
	} {
		if err := rec.record(ctx, ev); err != nil {
			t.Fatalf("record %s: %v", ev.Kind, err)
		}
	}

	stored, err := st.Read(ctx, "run-1")
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if len(stored) != 0 {
		t.Fatalf("journal holds %d events before the level's hook ran, want none", len(stored))
	}
	if _, ok, err := st.Latest(ctx, "run-1"); err != nil || ok {
		t.Fatalf("Latest returned (ok %t, err %v) before the hook ran, want (false, nil)", ok, err)
	}

	hook := checkpointHook(rec, "/graphs/demo.yaml", "yoann", "D-1")
	if err := hook(ctx, graph.StepInfo{Step: 1, Frontier: []string{"refine"}}, graph.NewState()); err != nil {
		t.Fatalf("checkpointHook: %v", err)
	}

	stored, err = st.Read(ctx, "run-1")
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if len(stored) != 4 {
		t.Fatalf("journal holds %d events after the hook ran, want 4", len(stored))
	}
	saved, ok, err := st.Latest(ctx, "run-1")
	if err != nil || !ok {
		t.Fatalf("Latest returned (ok %t, err %v), want (true, nil)", ok, err)
	}
	if saved.Step != 1 || saved.Status != checkpoint.StatusRunning {
		t.Fatalf("row = step %d/%q, want 1/%q", saved.Step, saved.Status, checkpoint.StatusRunning)
	}
	if saved.GraphPath != "/graphs/demo.yaml" || saved.Requester != "yoann" || saved.Dossier != "D-1" {
		t.Fatalf("row provenance = %q/%q/%q, want /graphs/demo.yaml/yoann/D-1",
			saved.GraphPath, saved.Requester, saved.Dossier)
	}
}

// The state written by the hook is a projection of the journal, not of the live state the
// engine handed it. The live state here deliberately holds a key no event ever carried: if
// the row were marshalled from it, that key would be in the row.
func TestTheHookIgnoresTheLiveStateAndWritesTheProjectionOfTheEvents(t *testing.T) {
	st := openRecorderStore(t)
	ctx := context.Background()
	rec := newRecorder(t, st, "run-1")

	for _, ev := range []graph.Event{
		{Kind: graph.EventLevelOpened, Frontier: []string{"collect"}},
		{Kind: graph.EventNodeStarted, NodeID: "collect"},
		{Kind: graph.EventNodeProduced, NodeID: "collect", Data: map[string]any{"dossier": "D-1"}},
		{Kind: graph.EventLevelClosed, Frontier: []string{"collect"}, Rule: graph.CombinationReplace},
	} {
		if err := rec.record(ctx, ev); err != nil {
			t.Fatalf("record %s: %v", ev.Kind, err)
		}
	}

	live := graph.NewState()
	live.Set("dossier", "D-1")
	live.Set("never_emitted", "only in the live state")
	live.Step = 99

	hook := checkpointHook(rec, "/graphs/demo.yaml", "", "")
	if err := hook(ctx, graph.StepInfo{Step: 1, Frontier: nil}, live); err != nil {
		t.Fatalf("checkpointHook: %v", err)
	}

	saved, ok, err := st.Latest(ctx, "run-1")
	if err != nil || !ok {
		t.Fatalf("Latest returned (ok %t, err %v), want (true, nil)", ok, err)
	}
	if saved.State.Has("never_emitted") {
		t.Fatalf("row state holds %q, which no event carried — the row was marshalled from the live state", "never_emitted")
	}
	events, err := st.Read(ctx, "run-1")
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	want, err := projection.Project(events)
	if err != nil {
		t.Fatalf("Project: %v", err)
	}
	if recorderStateJSON(t, saved.State) != recorderStateJSON(t, want) {
		t.Fatalf("row state = %s, want Project(events) = %s",
			recorderStateJSON(t, saved.State), recorderStateJSON(t, want))
	}
}

// An empty frontier means the run reached its end, which the daemon and `status` read off
// the row's status and not off the journal.
func TestTheHookMarksTheRunDoneWhenTheFrontierEmpties(t *testing.T) {
	st := openRecorderStore(t)
	ctx := context.Background()
	rec := newRecorder(t, st, "run-1")

	hook := checkpointHook(rec, "/graphs/demo.yaml", "", "")
	if err := hook(ctx, graph.StepInfo{Step: 1, Frontier: nil}, graph.NewState()); err != nil {
		t.Fatalf("checkpointHook: %v", err)
	}
	saved, ok, err := st.Latest(ctx, "run-1")
	if err != nil || !ok {
		t.Fatalf("Latest returned (ok %t, err %v), want (true, nil)", ok, err)
	}
	if saved.Status != checkpoint.StatusDone {
		t.Fatalf("row status = %q, want %q", saved.Status, checkpoint.StatusDone)
	}
}

// A terminal event arrives after the last hook has run, so it writes itself. Buffering it
// would truncate the journal at the last level, which is exactly the tail issue 08 has to
// tell apart from a process that died.
func TestARunTerminalEventIsStoredWithoutWaitingForAHook(t *testing.T) {
	st := openRecorderStore(t)
	ctx := context.Background()
	rec := newRecorder(t, st, "run-1")

	if err := rec.record(ctx, graph.Event{Kind: graph.EventRunStarted}); err != nil {
		t.Fatalf("record run_started: %v", err)
	}
	if err := rec.record(ctx, graph.Event{Kind: graph.EventRunFinished}); err != nil {
		t.Fatalf("record run_finished: %v", err)
	}

	events, err := st.Read(ctx, "run-1")
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if len(events) != 2 {
		t.Fatalf("journal holds %d events, want 2", len(events))
	}
	started, ok := events[0].Payload.(journal.RunStarted)
	if !ok {
		t.Fatalf("first event payload is %T, want journal.RunStarted", events[0].Payload)
	}
	if started.Graph != "demo" {
		t.Fatalf("run_started graph = %q, want %q — the engine has no graph name, the adapter carries it", started.Graph, "demo")
	}
	if _, ok := events[1].Payload.(journal.RunFinished); !ok {
		t.Fatalf("second event payload is %T, want journal.RunFinished", events[1].Payload)
	}
}

// A level that failed emits no level_closed, so its buffered events would never be written
// by a hook that never runs. The run's terminal event flushes them, which is what makes the
// failed level visible in the journal at all.
func TestAFailedLevelsEventsAreFlushedByTheRunFailedEvent(t *testing.T) {
	st := openRecorderStore(t)
	ctx := context.Background()
	rec := newRecorder(t, st, "run-1")

	boom := errors.New("node exploded")
	for _, ev := range []graph.Event{
		{Kind: graph.EventRunStarted},
		{Kind: graph.EventLevelOpened, Frontier: []string{"collect"}},
		{Kind: graph.EventNodeStarted, NodeID: "collect"},
		{Kind: graph.EventNodeFailed, NodeID: "collect", Err: boom},
		{Kind: graph.EventRunFailed, Nodes: []string{"collect"}, Err: boom},
	} {
		if err := rec.record(ctx, ev); err != nil {
			t.Fatalf("record %s: %v", ev.Kind, err)
		}
	}

	events, err := st.Read(ctx, "run-1")
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if len(events) != 5 {
		t.Fatalf("journal holds %d events, want 5", len(events))
	}
	failed, ok := events[3].Payload.(journal.NodeFailed)
	if !ok {
		t.Fatalf("event 3 payload is %T, want journal.NodeFailed", events[3].Payload)
	}
	if !strings.Contains(failed.Message, "node exploded") {
		t.Fatalf("node_failed message = %q, want it to carry the node's error", failed.Message)
	}
	runFailed, ok := events[4].Payload.(journal.RunFailed)
	if !ok {
		t.Fatalf("event 4 payload is %T, want journal.RunFailed", events[4].Payload)
	}
	if len(runFailed.Nodes) != 1 || runFailed.Nodes[0] != "collect" {
		t.Fatalf("run_failed nodes = %v, want [collect]", runFailed.Nodes)
	}
}

// Sequence numbers are the journal's own invariant, and a recorder that started at FirstSeq
// on a run that already has events would be refused by the store for a reason it could not
// have known. Resume is issue 08's problem; not corrupting the sequence is this one's.
func TestARecorderContinuesTheSequenceOfARunThatAlreadyHasEvents(t *testing.T) {
	st := openRecorderStore(t)
	ctx := context.Background()

	first := newRecorder(t, st, "run-1")
	if err := first.record(ctx, graph.Event{Kind: graph.EventRunStarted}); err != nil {
		t.Fatalf("record: %v", err)
	}

	second := newRecorder(t, st, "run-1")
	if err := second.record(ctx, graph.Event{Kind: graph.EventRunFinished}); err != nil {
		t.Fatalf("record on the second recorder: %v", err)
	}
	events, err := st.Read(ctx, "run-1")
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if len(events) != 2 {
		t.Fatalf("journal holds %d events, want 2", len(events))
	}
	if events[1].Seq != events[0].Seq+1 {
		t.Fatalf("second event seq = %d, want %d", events[1].Seq, events[0].Seq+1)
	}
}

// graph.EventFunc says the hook is called from each node's own goroutine, so the recorder
// owns the locking. Without -race this test proves only that nothing is lost; with it, that
// nothing races.
func TestTheRecorderIsSafeForConcurrentNodeEvents(t *testing.T) {
	st := openRecorderStore(t)
	ctx := context.Background()
	rec := newRecorder(t, st, "run-1")

	frontier := []string{"a", "b", "c", "d"}
	if err := rec.record(ctx, graph.Event{Kind: graph.EventLevelOpened, Frontier: frontier}); err != nil {
		t.Fatalf("record level_opened: %v", err)
	}
	var wg sync.WaitGroup
	errs := make([]error, len(frontier))
	for i, id := range frontier {
		wg.Add(1)
		go func(i int, id string) {
			defer wg.Done()
			if err := rec.record(ctx, graph.Event{Kind: graph.EventNodeStarted, NodeID: id}); err != nil {
				errs[i] = err
				return
			}
			errs[i] = rec.record(ctx, graph.Event{
				Kind: graph.EventNodeProduced, NodeID: id, Data: map[string]any{id: i},
			})
		}(i, id)
	}
	wg.Wait()
	for i, err := range errs {
		if err != nil {
			t.Fatalf("node %s: %v", frontier[i], err)
		}
	}
	if err := rec.record(ctx, graph.Event{
		Kind: graph.EventLevelClosed, Frontier: frontier, Rule: graph.CombinationMerge,
	}); err != nil {
		t.Fatalf("record level_closed: %v", err)
	}
	hook := checkpointHook(rec, "/graphs/demo.yaml", "", "")
	if err := hook(ctx, graph.StepInfo{Step: 4, Frontier: nil}, graph.NewState()); err != nil {
		t.Fatalf("checkpointHook: %v", err)
	}

	events, err := st.Read(ctx, "run-1")
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if len(events) != 2+2*len(frontier) {
		t.Fatalf("journal holds %d events, want %d", len(events), 2+2*len(frontier))
	}
	for i, ev := range events {
		if want := int64(checkpoint.FirstSeq) + int64(i); ev.Seq != want {
			t.Fatalf("event %d has seq %d, want %d — the sequence gapped under concurrency", i, ev.Seq, want)
		}
	}
}

// The recorder buffers what the engine hands it, and the engine reuses its own frontier
// slice across a level. Holding a reference rather than a copy would let a later level
// rewrite an event already recorded for an earlier one.
func TestTheRecorderCopiesTheEnginesFrontierAndData(t *testing.T) {
	st := openRecorderStore(t)
	ctx := context.Background()
	rec := newRecorder(t, st, "run-1")

	frontier := []string{"collect"}
	data := map[string]any{"dossier": "D-1"}
	for _, ev := range []graph.Event{
		{Kind: graph.EventLevelOpened, Frontier: frontier},
		{Kind: graph.EventNodeStarted, NodeID: "collect"},
		{Kind: graph.EventNodeProduced, NodeID: "collect", Data: data},
		{Kind: graph.EventLevelClosed, Frontier: frontier, Rule: graph.CombinationReplace},
	} {
		if err := rec.record(ctx, ev); err != nil {
			t.Fatalf("record %s: %v", ev.Kind, err)
		}
	}
	frontier[0] = "rewritten"
	data["dossier"] = "rewritten"

	hook := checkpointHook(rec, "/graphs/demo.yaml", "", "")
	if err := hook(ctx, graph.StepInfo{Step: 1, Frontier: nil}, graph.NewState()); err != nil {
		t.Fatalf("checkpointHook: %v", err)
	}
	events, err := st.Read(ctx, "run-1")
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	opened, ok := events[0].Payload.(journal.LevelOpened)
	if !ok {
		t.Fatalf("event 0 payload is %T, want journal.LevelOpened", events[0].Payload)
	}
	if len(opened.Frontier) != 1 || opened.Frontier[0] != "collect" {
		t.Fatalf("level_opened frontier = %v, want [collect]", opened.Frontier)
	}
	produced, ok := events[2].Payload.(journal.NodeProduced)
	if !ok {
		t.Fatalf("event 2 payload is %T, want journal.NodeProduced", events[2].Payload)
	}
	if produced.Data["dossier"] != "D-1" {
		t.Fatalf("node_produced dossier = %v, want D-1", produced.Data["dossier"])
	}
}

// The two vocabularies mirror each other by convention, and this switch is the single place
// a divergence would show. A kind it does not know must stop the run rather than be dropped:
// a hook error aborts the run by design (graph.EventFunc), and a journal silently missing an
// event is what replay cannot detect.
func TestTheAdapterRefusesAnEventKindItDoesNotKnow(t *testing.T) {
	_, err := journalPayload(graph.Event{Kind: graph.EventKind("teleported")}, "demo")
	if err == nil {
		t.Fatalf("journalPayload returned nil error for an unknown kind, want a refusal")
	}
	if !strings.Contains(err.Error(), "teleported") {
		t.Fatalf("error %q does not name the offending kind", err.Error())
	}
}

func TestTheAdapterCarriesTheCombinationRuleAcross(t *testing.T) {
	for _, tc := range []struct {
		from graph.CombinationRule
		want journal.CombinationRule
	}{
		{graph.CombinationReplace, journal.CombinationReplace},
		{graph.CombinationMerge, journal.CombinationMerge},
	} {
		payload, err := journalPayload(graph.Event{
			Kind: graph.EventLevelClosed, Frontier: []string{"a"}, Rule: tc.from,
		}, "demo")
		if err != nil {
			t.Fatalf("journalPayload for rule %q: %v", tc.from, err)
		}
		closed, ok := payload.(journal.LevelClosed)
		if !ok {
			t.Fatalf("payload is %T, want journal.LevelClosed", payload)
		}
		if closed.Rule != tc.want {
			t.Fatalf("rule %q mapped to %q, want %q", tc.from, closed.Rule, tc.want)
		}
	}
}

func TestTheAdapterRefusesACombinationRuleItDoesNotKnow(t *testing.T) {
	_, err := journalPayload(graph.Event{
		Kind: graph.EventLevelClosed, Frontier: []string{"a"}, Rule: graph.CombinationRule("interleave"),
	}, "demo")
	if err == nil {
		t.Fatalf("journalPayload returned nil error for an unknown rule, want a refusal")
	}
	if !strings.Contains(err.Error(), "interleave") {
		t.Fatalf("error %q does not name the offending rule", err.Error())
	}
}

// The node's zone labels travel with its keys. Dropping them would make every key look
// persistent to replay, and a later freeze's carry-over would keep what the run dropped.
func TestTheAdapterCarriesANodesZoneLabels(t *testing.T) {
	payload, err := journalPayload(graph.Event{
		Kind: graph.EventNodeProduced, NodeID: "collect",
		Data:  map[string]any{"dossier": "D-1", "scratch": "notes"},
		Zones: map[string]string{"scratch": graph.ZoneEphemeral},
	}, "demo")
	if err != nil {
		t.Fatalf("journalPayload: %v", err)
	}
	produced, ok := payload.(journal.NodeProduced)
	if !ok {
		t.Fatalf("payload is %T, want journal.NodeProduced", payload)
	}
	if produced.Zones["scratch"] != graph.ZoneEphemeral {
		t.Fatalf("scratch zone = %q, want %q", produced.Zones["scratch"], graph.ZoneEphemeral)
	}
	if _, tagged := produced.Zones["dossier"]; tagged {
		t.Fatalf("dossier carries a zone label, want none — a key absent from Zones is persistent")
	}
}

func recorderStateJSON(t *testing.T, s *graph.State) string {
	t.Helper()
	b, err := json.Marshal(s)
	if err != nil {
		t.Fatalf("marshal state: %v", err)
	}
	return string(b)
}

// A nudge is an out-of-band write to the shared state applied between two levels, by the
// steer mailbox rather than by any node. It has to be recorded here even though nudge events
// are nominally issue 06's: the moment the row is a projection of the journal, a mutation
// the journal does not carry is a mutation the row loses. See the drift record for this
// branch.
func TestANudgeIsRecordedBeforeTheLevelItAppliesTo(t *testing.T) {
	st := openRecorderStore(t)
	ctx := context.Background()
	rec := newRecorder(t, st, "run-1")

	if err := rec.recordNudge(ctx, map[string]any{"probe": "hello"}); err != nil {
		t.Fatalf("recordNudge: %v", err)
	}
	for _, ev := range []graph.Event{
		{Kind: graph.EventLevelOpened, Frontier: []string{"collect"}},
		{Kind: graph.EventNodeStarted, NodeID: "collect"},
		{Kind: graph.EventNodeProduced, NodeID: "collect", Data: map[string]any{"dossier": "D-1"}},
		{Kind: graph.EventLevelClosed, Frontier: []string{"collect"}, Rule: graph.CombinationReplace},
	} {
		if err := rec.record(ctx, ev); err != nil {
			t.Fatalf("record %s: %v", ev.Kind, err)
		}
	}
	hook := checkpointHook(rec, "/graphs/demo.yaml", "", "")
	if err := hook(ctx, graph.StepInfo{Step: 1, Frontier: nil}, graph.NewState()); err != nil {
		t.Fatalf("checkpointHook: %v", err)
	}

	events, err := st.Read(ctx, "run-1")
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	nudged, ok := events[0].Payload.(journal.NudgeApplied)
	if !ok {
		t.Fatalf("event 0 payload is %T, want journal.NudgeApplied before the level opened", events[0].Payload)
	}
	if nudged.Origin == "" {
		t.Fatalf("nudge origin is empty, want the surface it came through")
	}
	saved, ok, err := st.Latest(ctx, "run-1")
	if err != nil || !ok {
		t.Fatalf("Latest returned (ok %t, err %v), want (true, nil)", ok, err)
	}
	if v, _ := saved.State.Get("probe"); v != "hello" {
		t.Fatalf("row state probe = %v, want hello — the nudge never reached the projection", v)
	}
}

// Nothing queued means nothing recorded: an empty nudge event would put an out-of-band write
// in the record for every level of every run that was never steered.
func TestNoNudgeRecordsNothing(t *testing.T) {
	st := openRecorderStore(t)
	ctx := context.Background()
	rec := newRecorder(t, st, "run-1")

	if err := rec.recordNudge(ctx, nil); err != nil {
		t.Fatalf("recordNudge: %v", err)
	}
	if err := rec.record(ctx, graph.Event{Kind: graph.EventRunFinished}); err != nil {
		t.Fatalf("record: %v", err)
	}
	events, err := st.Read(ctx, "run-1")
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("journal holds %d events, want the terminal one only", len(events))
	}
}
