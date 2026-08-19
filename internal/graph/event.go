package graph

import (
	"context"
	"reflect"
	"sort"
)

// EventKind names one fact the engine reports as it executes. The set is closed and
// deliberately mirrors, name for name, the run-lifecycle / level-boundary / node subset of
// the internal journal's vocabulary — so the adapter that turns these into journal events is
// one flat switch with nothing to infer.
//
// It mirrors rather than reuses because of the dependency direction (see EventFunc): the
// engine may not import the journal, so the two vocabularies stay in sync by convention, and
// the adapter is the single place a divergence would show up.
type EventKind string

const (
	// EventRunStarted opens a run, once RunFrom has validated the graph.
	EventRunStarted EventKind = "run_started"
	// EventRunFinished closes a run whose frontier emptied with no error.
	EventRunFinished EventKind = "run_finished"
	// EventRunFailed closes a run that stopped on an error; Err carries it and Nodes names
	// every node that failed when the error came from a level.
	EventRunFailed EventKind = "run_failed"
	// EventLevelOpened reports the frontier a level is about to execute.
	EventLevelOpened EventKind = "level_opened"
	// EventLevelClosed reports a level whose branches were folded back into the shared
	// state, and the CombinationRule that folded them.
	EventLevelClosed EventKind = "level_closed"
	// EventNodeStarted reports a node beginning to execute, from that node's own goroutine.
	EventNodeStarted EventKind = "node_started"
	// EventNodeProduced reports a node that completed, with the keys it wrote on its branch.
	EventNodeProduced EventKind = "node_produced"
	// EventNodeFailed reports a node whose Execute returned an error.
	EventNodeFailed EventKind = "node_failed"
	// EventFreezeApplied reports a node whose Execute called State.Freeze: the branch's
	// Frozen counter moved, so what it wrote is a wholesale replacement, not a diff — see
	// CarriedOver/Dropped on Event and runLevel's use of freezeDiff.
	EventFreezeApplied EventKind = "freeze_applied"
)

// CombinationRule names how a level's branches were folded back into the shared state.
// It is reported as data rather than left to be re-derived: a single-node frontier and a
// fan-out that happens to touch the same keys leave an identical key set behind, despite one
// replacing the state wholesale and the other merging into it.
type CombinationRule string

const (
	// CombinationReplace is applied by a single-node frontier — the branch replaces the
	// shared state, so deletions and Freeze propagate. See Engine.runLevel.
	CombinationReplace CombinationRule = "replace"
	// CombinationMerge is applied by a fan-out — each branch's keys are overlaid additively
	// onto the shared state. See Engine.runLevel.
	CombinationMerge CombinationRule = "merge"
)

// Event is one fact the engine reports, in the shape StepInfo already established for
// StepFunc: a single struct the hook receives by value, rather than one method per fact.
// A method-per-fact port would have to grow a method — and break every implementation — each
// time a later issue adds a kind; a Kind-tagged struct grows by a constant, which is why
// nudge and freeze can be added downstream without touching anyone who already implements
// this port.
//
// Only the fields a Kind defines are set; the rest are zero. Which is which:
//
//	EventRunStarted    — nothing (the run's identity and graph name belong to the caller,
//	                     which is what names the run; the engine has neither).
//	EventRunFinished   — nothing.
//	EventRunFailed     — Err, and Nodes when the error came from a level.
//	EventLevelOpened   — Frontier.
//	EventLevelClosed   — Frontier, Rule.
//	EventNodeStarted   — NodeID.
//	EventNodeProduced  — NodeID, Data, Zones.
//	EventNodeFailed    — NodeID, Err.
//	EventFreezeApplied — NodeID, CarriedOver, Dropped. NodeID is engine-side attribution
//	                     only (runLevel refuses a freeze outside a single-node frontier, so
//	                     it always names that one node); journal.FreezeApplied itself
//	                     carries no node id, so the adapter drops it on the way across.
type Event struct {
	Kind EventKind

	// Frontier is the level's node IDs, in execution order.
	Frontier []string
	// Rule is the combination rule a closed level applied.
	Rule CombinationRule
	// NodeID names the node a per-node event is about.
	NodeID string
	// Data holds the keys a node wrote on its branch — only those, never the whole state:
	// the shared state is reconstructible from the per-node writes plus the level's rule,
	// and shipping a full snapshot per node would make the record grow with the state
	// rather than with what actually happened.
	Data map[string]any
	// Zones holds the non-persistent zone labels of the keys in Data, following State.Zone's
	// own convention: a key absent from Zones is persistent.
	Zones map[string]string
	// Nodes names every node that failed in the level that ended the run (see LevelError).
	Nodes []string
	// Err is the failure a node or a run reported, wrapped as the engine wraps it.
	Err error
	// CarriedOver holds every key/value a Freeze kept — the branch's entire contents after
	// Execute returned, not a diff against what the level started with. State.Freeze
	// replaces the branch wholesale, so a diff (like Data above) would silently lose every
	// key the carry-over dropped; a downstream replay only sees this event, never the
	// branch it came from.
	CarriedOver map[string]any
	// Dropped names the keys the level held before this node ran that are absent from
	// CarriedOver — audit information only. Replay does not need it to reconstruct the
	// state (CarriedOver alone determines that); it is what lets a human or a report say
	// what a Freeze discarded, not just what it kept.
	Dropped []string
}

// EventFunc is the port through which the engine reports what a run did. It follows the
// existing hooks exactly — a func type declared by graph, taking a graph-owned struct, with
// a nil value meaning "no-op" (compare StepFunc and NudgeFunc). Returning an error aborts
// the run: whoever records these events is the run's record, and continuing a run whose
// record refused an event would produce a journal that is silently incomplete.
//
// # Dependency direction
//
// The obvious port would have been func(context.Context, journal.Event) error, and it is
// deliberately not that. CONVENTIONS.md fixes the direction — graph declares the ports,
// infrastructure implements them, never the reverse — and here the rule has teeth beyond
// style: the journal's projection rebuilds a *graph.State from an event slice, so the
// journal package imports graph. Had graph imported journal for this port, that pair would
// be an import cycle the compiler refuses, and the fix under deadline pressure would have
// been to move State — the engine's core type — to wherever the cycle stopped hurting.
//
// So the port is declared over graph-owned types (Event, EventKind, CombinationRule) whose
// names mirror the journal's. The cost is a vocabulary kept in sync by convention plus one
// mapping switch in the adapter; the alternative cost was the engine's own types drifting to
// wherever an import cycle pushed them. The mapping belongs to the adapter anyway: the
// engine has no run ID, no sequence number and no graph name — all three are the caller's,
// and all three are journal.Event fields this port has no business inventing.
//
// An implementation MUST be safe for concurrent use: runLevel emits node events from each
// node's own goroutine, so a hook that appends to a slice without a lock is a data race.
type EventFunc func(ctx context.Context, ev Event) error

// emit hands ev to the registered hook, or does nothing when none is registered. The nil
// check lives here rather than at each call site so the "no emitter" path stays free: with
// no hook to escape into, ev never leaves the stack and the call allocates nothing.
func (e *Engine) emit(ctx context.Context, ev Event) error {
	if e.onEvent == nil {
		return nil
	}
	return e.onEvent(ctx, ev)
}

// producedKeys reports what a node wrote on its branch: every key whose value or zone
// differs from the state the level started with.
//
// Both halves matter. Comparing values alone would miss SetZoned(zone, k, v) re-tagging a
// key it did not otherwise change — a real mutation, and one a later Freeze's carry-over
// acts on. Comparing with reflect.DeepEqual rather than == is what lets a node write a map
// or a slice without the engine panicking on an uncomparable type.
//
// Keys the node deleted are not reported here: a deletion only survives the level under the
// replace rule, which the level-closed event already carries, and inventing a second way to
// express it would give a consumer two sources to reconcile.
func producedKeys(before, after *State) (map[string]any, map[string]string) {
	var data map[string]any
	var zones map[string]string
	for k, v := range after.data {
		if old, ok := before.data[k]; ok && before.Zone(k) == after.Zone(k) && reflect.DeepEqual(old, v) {
			continue
		}
		if data == nil {
			data = make(map[string]any, 4)
		}
		data[k] = v
		if z := after.Zone(k); z != ZonePersistent {
			if zones == nil {
				zones = make(map[string]string, 2)
			}
			zones[k] = z
		}
	}
	return data, zones
}

// freezeDiff reports what a Freeze inside a node's Execute did: the branch's entire
// contents afterwards (CarriedOver — everything the freeze kept, plus anything the node
// wrote after freezing, since both end up indistinguishable in the branch and both must
// survive replay), and the keys the level held before this node ran that are gone from it
// now (Dropped).
//
// Unlike producedKeys this is not a diff of changed keys: State.Freeze discards the
// branch's contents wholesale, so "only what changed" would silently drop every carried-over
// key whose value happened to survive unchanged, and a replay that only sees this event
// would rebuild a state missing them.
func freezeDiff(before, after *State) (map[string]any, []string) {
	carried := make(map[string]any, len(after.data))
	for k, v := range after.data {
		carried[k] = v
	}
	var dropped []string
	for k := range before.data {
		if _, ok := after.data[k]; !ok {
			dropped = append(dropped, k)
		}
	}
	sort.Strings(dropped)
	return carried, dropped
}
