package graph

import (
	"context"
	"errors"
	"sync"
	"testing"
)

// eventRecorder collects everything the engine emits. It locks because runLevel emits from
// the per-node goroutines: a recorder that appended without a mutex would make every test
// here a race the detector reports, hiding the engine's own behaviour behind the test's bug.
type eventRecorder struct {
	mu     sync.Mutex
	events []Event
}

func (r *eventRecorder) hook(_ context.Context, ev Event) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.events = append(r.events, ev)
	return nil
}

func (r *eventRecorder) kinds() []EventKind {
	r.mu.Lock()
	defer r.mu.Unlock()
	ks := make([]EventKind, 0, len(r.events))
	for _, ev := range r.events {
		ks = append(ks, ev.Kind)
	}
	return ks
}

// node returns the single event of that kind naming nodeID, and whether exactly one exists.
func (r *eventRecorder) node(kind EventKind, nodeID string) (Event, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var found Event
	n := 0
	for _, ev := range r.events {
		if ev.Kind == kind && ev.NodeID == nodeID {
			found = ev
			n++
		}
	}
	return found, n == 1
}

func (r *eventRecorder) first(kind EventKind) (Event, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, ev := range r.events {
		if ev.Kind == kind {
			return ev, true
		}
	}
	return Event{}, false
}

func (r *eventRecorder) count(kind EventKind) int {
	r.mu.Lock()
	defer r.mu.Unlock()
	n := 0
	for _, ev := range r.events {
		if ev.Kind == kind {
			n++
		}
	}
	return n
}

func kindsEqual(got []EventKind, want ...EventKind) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}

// failingNode returns a node whose Execute always fails with err.
func failingNode(id string, err error) Node {
	return NewToolNode(id, func(_ context.Context, _ *State) error { return err })
}

func TestASingleNodeLevelEmitsOpenStartProduceCloseInThatOrder(t *testing.T) {
	g := NewGraph()
	g.AddNode(appendNode("a"))
	g.SetEntry("a")
	rec := &eventRecorder{}
	if err := NewEngine(g).OnEvent(rec.hook).Run(context.Background(), NewState()); err != nil {
		t.Fatalf("Run: %v", err)
	}
	got := rec.kinds()
	want := []EventKind{
		EventRunStarted, EventLevelOpened, EventNodeStarted, EventNodeProduced,
		EventLevelClosed, EventRunFinished,
	}
	if !kindsEqual(got, want...) {
		t.Fatalf("kinds = %v; want %v", got, want)
	}
}

func TestASingleNodeLevelClosesWithTheReplaceRule(t *testing.T) {
	g := NewGraph()
	g.AddNode(appendNode("a"))
	g.SetEntry("a")
	rec := &eventRecorder{}
	if err := NewEngine(g).OnEvent(rec.hook).Run(context.Background(), NewState()); err != nil {
		t.Fatalf("Run: %v", err)
	}
	closed, ok := rec.first(EventLevelClosed)
	if !ok {
		t.Fatalf("no level-closed event emitted; kinds = %v", rec.kinds())
	}
	if closed.Rule != CombinationReplace {
		t.Fatalf("Rule = %q; want %q", closed.Rule, CombinationReplace)
	}
	if len(closed.Frontier) != 1 || closed.Frontier[0] != "a" {
		t.Fatalf("Frontier = %v; want [a]", closed.Frontier)
	}
}

func TestALevelOpensWithTheFrontierItIsAboutToRun(t *testing.T) {
	g := NewGraph()
	g.AddNode(appendNode("root")).AddNode(appendNode("x")).AddNode(appendNode("y"))
	g.SetEntry("root")
	g.AddEdge("root", Static("x", "y"))
	rec := &eventRecorder{}
	if err := NewEngine(g).OnEvent(rec.hook).Run(context.Background(), NewState()); err != nil {
		t.Fatalf("Run: %v", err)
	}
	rec.mu.Lock()
	var opened []Event
	for _, ev := range rec.events {
		if ev.Kind == EventLevelOpened {
			opened = append(opened, ev)
		}
	}
	rec.mu.Unlock()
	if len(opened) != 2 {
		t.Fatalf("level-opened count = %d; want 2", len(opened))
	}
	if len(opened[0].Frontier) != 1 || opened[0].Frontier[0] != "root" {
		t.Fatalf("first frontier = %v; want [root]", opened[0].Frontier)
	}
	if len(opened[1].Frontier) != 2 || opened[1].Frontier[0] != "x" || opened[1].Frontier[1] != "y" {
		t.Fatalf("second frontier = %v; want [x y]", opened[1].Frontier)
	}
}

func TestAFanOutLevelEmitsOneStartAndOneProducePerNodeAndClosesWithMerge(t *testing.T) {
	g := NewGraph()
	g.AddNode(appendNode("root")).AddNode(appendNode("x")).AddNode(appendNode("y"))
	g.SetEntry("root")
	g.AddEdge("root", Static("x", "y"))
	rec := &eventRecorder{}
	if err := NewEngine(g).OnEvent(rec.hook).Run(context.Background(), NewState()); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if _, ok := rec.node(EventNodeStarted, "x"); !ok {
		t.Fatalf("want exactly one node-started for x; kinds = %v", rec.kinds())
	}
	if _, ok := rec.node(EventNodeStarted, "y"); !ok {
		t.Fatalf("want exactly one node-started for y; kinds = %v", rec.kinds())
	}
	if _, ok := rec.node(EventNodeProduced, "x"); !ok {
		t.Fatalf("want exactly one node-produced for x; kinds = %v", rec.kinds())
	}
	if _, ok := rec.node(EventNodeProduced, "y"); !ok {
		t.Fatalf("want exactly one node-produced for y; kinds = %v", rec.kinds())
	}
	rec.mu.Lock()
	var fanOut Event
	for _, ev := range rec.events {
		if ev.Kind == EventLevelClosed && len(ev.Frontier) == 2 {
			fanOut = ev
		}
	}
	rec.mu.Unlock()
	if fanOut.Rule != CombinationMerge {
		t.Fatalf("fan-out Rule = %q; want %q", fanOut.Rule, CombinationMerge)
	}
}

func TestALevelWhereOneNodeFailsStillReportsTheNodeThatCompleted(t *testing.T) {
	boom := errors.New("boom")
	g := NewGraph()
	g.AddNode(appendNode("root")).AddNode(failingNode("x", boom)).AddNode(appendNode("y"))
	g.SetEntry("root")
	g.AddEdge("root", Static("x", "y"))
	rec := &eventRecorder{}
	err := NewEngine(g).OnEvent(rec.hook).Run(context.Background(), NewState())
	if err == nil {
		t.Fatalf("Run: want the level error, got nil")
	}
	failed, ok := rec.node(EventNodeFailed, "x")
	if !ok {
		t.Fatalf("want exactly one node-failed for x; kinds = %v", rec.kinds())
	}
	if !errors.Is(failed.Err, boom) {
		t.Fatalf("node-failed Err = %v; want it to wrap %v", failed.Err, boom)
	}
	if _, ok := rec.node(EventNodeProduced, "y"); !ok {
		t.Fatalf("want exactly one node-produced for y; kinds = %v", rec.kinds())
	}
	if _, ok := rec.node(EventNodeProduced, "x"); ok {
		t.Fatalf("x failed; it must not be reported as having produced anything")
	}
}

func TestAFailedLevelIsNotClosedBecauseNoCombinationRuleWasApplied(t *testing.T) {
	g := NewGraph()
	g.AddNode(failingNode("a", errors.New("boom")))
	g.SetEntry("a")
	rec := &eventRecorder{}
	if err := NewEngine(g).OnEvent(rec.hook).Run(context.Background(), NewState()); err == nil {
		t.Fatalf("Run: want the level error, got nil")
	}
	if n := rec.count(EventLevelClosed); n != 0 {
		t.Fatalf("level-closed count = %d; want 0 on a level that never combined its branches", n)
	}
}

func TestNodeProducedCarriesOnlyTheKeysTheNodeSet(t *testing.T) {
	g := NewGraph()
	g.AddNode(NewToolNode("a", func(_ context.Context, s *State) error {
		s.Set("written", 1)
		return nil
	}))
	g.SetEntry("a")
	rec := &eventRecorder{}
	s := NewState()
	s.Set("pre-existing", "untouched")
	if err := NewEngine(g).OnEvent(rec.hook).Run(context.Background(), s); err != nil {
		t.Fatalf("Run: %v", err)
	}
	produced, ok := rec.node(EventNodeProduced, "a")
	if !ok {
		t.Fatalf("want exactly one node-produced for a; kinds = %v", rec.kinds())
	}
	if len(produced.Data) != 1 {
		t.Fatalf("Data = %v; want only the key the node set", produced.Data)
	}
	if produced.Data["written"] != 1 {
		t.Fatalf("Data[written] = %v; want 1", produced.Data["written"])
	}
	if _, ok := produced.Data["pre-existing"]; ok {
		t.Fatalf("Data carries a key the node never touched: %v", produced.Data)
	}
}

func TestNodeProducedCarriesTheZoneOfAKeyTheNodeTagged(t *testing.T) {
	g := NewGraph()
	g.AddNode(NewToolNode("a", func(_ context.Context, s *State) error {
		s.SetZoned(ZoneEphemeral, "scratch", "tmp")
		s.Set("kept", "yes")
		return nil
	}))
	g.SetEntry("a")
	rec := &eventRecorder{}
	if err := NewEngine(g).OnEvent(rec.hook).Run(context.Background(), NewState()); err != nil {
		t.Fatalf("Run: %v", err)
	}
	produced, ok := rec.node(EventNodeProduced, "a")
	if !ok {
		t.Fatalf("want exactly one node-produced for a; kinds = %v", rec.kinds())
	}
	if produced.Zones["scratch"] != ZoneEphemeral {
		t.Fatalf("Zones[scratch] = %q; want %q", produced.Zones["scratch"], ZoneEphemeral)
	}
	if _, ok := produced.Zones["kept"]; ok {
		t.Fatalf("Zones names a persistent key: %v", produced.Zones)
	}
}

func TestNodeProducedReportsAKeyWhoseValueIsUnchangedButZoneIsNot(t *testing.T) {
	g := NewGraph()
	g.AddNode(NewToolNode("a", func(_ context.Context, s *State) error {
		s.SetZoned(ZoneEphemeral, "k", "same")
		return nil
	}))
	g.SetEntry("a")
	rec := &eventRecorder{}
	s := NewState()
	s.Set("k", "same")
	if err := NewEngine(g).OnEvent(rec.hook).Run(context.Background(), s); err != nil {
		t.Fatalf("Run: %v", err)
	}
	produced, ok := rec.node(EventNodeProduced, "a")
	if !ok {
		t.Fatalf("want exactly one node-produced for a; kinds = %v", rec.kinds())
	}
	if produced.Zones["k"] != ZoneEphemeral {
		t.Fatalf("Zones[k] = %q; want %q — the value did not move but the zone did", produced.Zones["k"], ZoneEphemeral)
	}
}

func TestANodeThatWroteNothingStillProduces(t *testing.T) {
	g := NewGraph()
	g.AddNode(NewToolNode("a", func(_ context.Context, _ *State) error { return nil }))
	g.SetEntry("a")
	rec := &eventRecorder{}
	if err := NewEngine(g).OnEvent(rec.hook).Run(context.Background(), NewState()); err != nil {
		t.Fatalf("Run: %v", err)
	}
	produced, ok := rec.node(EventNodeProduced, "a")
	if !ok {
		t.Fatalf("a completed and must be reported as such; kinds = %v", rec.kinds())
	}
	if len(produced.Data) != 0 {
		t.Fatalf("Data = %v; want empty", produced.Data)
	}
}

func TestAFailedRunEndsWithRunFailedNamingEveryNodeThatFailed(t *testing.T) {
	boom := errors.New("boom")
	g := NewGraph()
	g.AddNode(appendNode("root")).AddNode(failingNode("x", boom)).AddNode(failingNode("y", boom))
	g.SetEntry("root")
	g.AddEdge("root", Static("x", "y"))
	rec := &eventRecorder{}
	if err := NewEngine(g).OnEvent(rec.hook).Run(context.Background(), NewState()); err == nil {
		t.Fatalf("Run: want the level error, got nil")
	}
	ks := rec.kinds()
	if len(ks) == 0 || ks[len(ks)-1] != EventRunFailed {
		t.Fatalf("last kind = %v; want run-failed to close the journal", ks)
	}
	if n := rec.count(EventRunFinished); n != 0 {
		t.Fatalf("run-finished count = %d; want 0 on a run that failed", n)
	}
	failed, _ := rec.first(EventRunFailed)
	if len(failed.Nodes) != 2 || failed.Nodes[0] != "x" || failed.Nodes[1] != "y" {
		t.Fatalf("Nodes = %v; want [x y]", failed.Nodes)
	}
	if !errors.Is(failed.Err, boom) {
		t.Fatalf("run-failed Err = %v; want it to wrap %v", failed.Err, boom)
	}
}

func TestARunThatNeverStartsEmitsNothing(t *testing.T) {
	g := NewGraph() // no entry set: Validate refuses it
	rec := &eventRecorder{}
	if err := NewEngine(g).OnEvent(rec.hook).Run(context.Background(), NewState()); err == nil {
		t.Fatalf("Run: want the validation error, got nil")
	}
	if n := len(rec.kinds()); n != 0 {
		t.Fatalf("emitted %d events; want none — a graph that never validated never ran", n)
	}
}

func TestANilEmitterLeavesTheRunUnchanged(t *testing.T) {
	g := NewGraph()
	g.AddNode(appendNode("a")).AddNode(appendNode("b"))
	g.SetEntry("a")
	g.AddEdge("a", Static("b"))
	s := NewState()
	if err := NewEngine(g).Run(context.Background(), s); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !s.Has("visited_a") || !s.Has("visited_b") {
		t.Fatalf("both nodes should have run: %v", s.Keys())
	}
	if s.Step != 2 {
		t.Fatalf("Step = %d; want 2", s.Step)
	}
}

func TestANilEmitterAllocatesNothingWhenTheEngineEmits(t *testing.T) {
	e := NewEngine(NewGraph())
	ctx := context.Background()
	frontier := []string{"a", "b"}
	allocs := testing.AllocsPerRun(100, func() {
		_ = e.emit(ctx, Event{Kind: EventLevelOpened, Frontier: frontier})
	})
	if allocs != 0 {
		t.Fatalf("allocs per emit = %v; want 0 when no emitter is registered", allocs)
	}
}

func TestAnEmitterErrorAbortsTheRun(t *testing.T) {
	refuse := errors.New("sink refused")
	g := NewGraph()
	g.AddNode(appendNode("a")).AddNode(appendNode("b"))
	g.SetEntry("a")
	g.AddEdge("a", Static("b"))
	ran := 0
	emit := func(_ context.Context, ev Event) error {
		if ev.Kind == EventNodeProduced {
			ran++
			return refuse
		}
		return nil
	}
	err := NewEngine(g).OnEvent(emit).Run(context.Background(), NewState())
	if !errors.Is(err, refuse) {
		t.Fatalf("Run error = %v; want it to wrap %v", err, refuse)
	}
	if ran != 1 {
		t.Fatalf("node-produced emitted %d times; want 1 — the run must stop at the first refusal", ran)
	}
}

func TestEveryNodeOfAWideFanOutIsReportedWhenEmittingFromItsOwnGoroutine(t *testing.T) {
	ids := []string{"n0", "n1", "n2", "n3", "n4", "n5", "n6", "n7"}
	g := NewGraph()
	g.AddNode(appendNode("root"))
	for _, id := range ids {
		g.AddNode(appendNode(id))
	}
	g.SetEntry("root")
	g.AddEdge("root", Static(ids...))
	rec := &eventRecorder{}
	if err := NewEngine(g).OnEvent(rec.hook).Run(context.Background(), NewState()); err != nil {
		t.Fatalf("Run: %v", err)
	}
	for _, id := range ids {
		if _, ok := rec.node(EventNodeStarted, id); !ok {
			t.Fatalf("no node-started for %s; kinds = %v", id, rec.kinds())
		}
		if _, ok := rec.node(EventNodeProduced, id); !ok {
			t.Fatalf("no node-produced for %s; kinds = %v", id, rec.kinds())
		}
	}
}

// A node that calls State.Freeze on a single-node frontier reports a freeze_applied event
// instead of node_produced: the branch's Frozen counter moved, so what it wrote is a
// wholesale replacement (CarriedOver), not the diff node_produced would otherwise carry.
func TestASingleNodeFreezeEmitsFreezeAppliedInsteadOfNodeProduced(t *testing.T) {
	g := NewGraph()
	g.AddNode(NewToolNode("freeze", func(_ context.Context, s *State) error {
		s.Freeze(nil) // default carry-over: keeps persistent, drops ephemeral
		return nil
	}))
	g.SetEntry("freeze")
	rec := &eventRecorder{}
	s := NewState()
	s.Set("goal", "ship")                       // persistent, survives
	s.SetZoned(ZoneEphemeral, "scratch", "tmp") // ephemeral, dropped
	if err := NewEngine(g).OnEvent(rec.hook).Run(context.Background(), s); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if _, ok := rec.node(EventNodeProduced, "freeze"); ok {
		t.Fatalf("a node that froze must not also report node_produced; kinds = %v", rec.kinds())
	}
	frozen, ok := rec.node(EventFreezeApplied, "freeze")
	if !ok {
		t.Fatalf("want exactly one freeze_applied for freeze; kinds = %v", rec.kinds())
	}
	if frozen.CarriedOver["goal"] != "ship" {
		t.Fatalf("CarriedOver = %v; want goal=ship", frozen.CarriedOver)
	}
	if _, kept := frozen.CarriedOver["scratch"]; kept {
		t.Fatalf("CarriedOver keeps ephemeral key scratch: %v", frozen.CarriedOver)
	}
	if len(frozen.Dropped) != 1 || frozen.Dropped[0] != "scratch" {
		t.Fatalf("Dropped = %v; want [scratch]", frozen.Dropped)
	}
}

// A freeze inside a fan-out has no single branch to attribute it to (runLevel.Merge does
// not fold Frozen or dropped keys), and projection.Project already refuses to replay one.
// The engine must refuse it live, at the same node, rather than accept a run that produces a
// journal replay cannot reconstruct.
func TestAFreezeInsideAFanOutFailsTheLevel(t *testing.T) {
	g := NewGraph()
	g.AddNode(appendNode("root"))
	g.AddNode(NewToolNode("freezer", func(_ context.Context, s *State) error {
		s.Freeze(nil)
		return nil
	}))
	g.AddNode(appendNode("plain"))
	g.SetEntry("root")
	g.AddEdge("root", Static("freezer", "plain"))
	rec := &eventRecorder{}
	err := NewEngine(g).OnEvent(rec.hook).Run(context.Background(), NewState())
	if err == nil {
		t.Fatalf("Run: want an error for a freeze inside a fan-out, got nil")
	}
	if _, ok := rec.node(EventFreezeApplied, "freezer"); ok {
		t.Fatalf("a rejected freeze must not still be reported as applied; kinds = %v", rec.kinds())
	}
	if _, ok := rec.node(EventNodeFailed, "freezer"); !ok {
		t.Fatalf("want node_failed for freezer; kinds = %v", rec.kinds())
	}
}

func TestNodeStartedPrecedesTheNodesOwnOutcome(t *testing.T) {
	g := NewGraph()
	g.AddNode(appendNode("root")).AddNode(appendNode("x")).AddNode(appendNode("y"))
	g.SetEntry("root")
	g.AddEdge("root", Static("x", "y"))
	rec := &eventRecorder{}
	if err := NewEngine(g).OnEvent(rec.hook).Run(context.Background(), NewState()); err != nil {
		t.Fatalf("Run: %v", err)
	}
	rec.mu.Lock()
	defer rec.mu.Unlock()
	started := map[string]int{}
	for i, ev := range rec.events {
		switch ev.Kind {
		case EventNodeStarted:
			started[ev.NodeID] = i
		case EventNodeProduced:
			at, ok := started[ev.NodeID]
			if !ok {
				t.Fatalf("%s produced before it started", ev.NodeID)
			}
			if at > i {
				t.Fatalf("%s: started at %d, produced at %d", ev.NodeID, at, i)
			}
		}
	}
}
