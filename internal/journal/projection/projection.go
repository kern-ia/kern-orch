// Package projection rebuilds a run's graph.State by replaying its journal. It is the
// function that makes the state a projection of the journal rather than a record kept
// alongside it: if this is wrong, "the journal is the source of truth" is a slogan.
//
// It lives beside internal/journal rather than inside it because the engine will emit
// events (issue 05), which makes internal/graph depend on internal/journal — a projection
// living in internal/journal and importing internal/graph would close that loop into an
// import cycle. A sibling package depending on both directions' leaves keeps the one-way
// dependency CONVENTIONS.md requires (graph defines the ports; nothing graph imports may
// import graph back).
//
// Project is pure: it takes an ordered slice and returns a state. Reading events out of
// SQLite is the caller's job (issue 07) and closing an interrupted tail is issue 12's; a
// projection that also did I/O could not be exhaustively tested before either exists.
//
// # What replay cannot re-derive, and therefore reads off the events
//
//   - The combination rule. graph.Engine.runLevel REPLACES the shared state with the branch
//     when the frontier holds one node and MERGES additively when it holds several. Both can
//     leave the same key set behind, so the rule is taken from LevelClosed.Rule and never
//     inferred from how many nodes this function happens to see.
//   - What a Freeze kept. graph.State.Freeze replaces the state's contents wholesale with
//     the carry-over's result and resets the zone map, so the projection installs
//     FreezeApplied.CarriedOver as the new contents. FreezeApplied.Dropped is deliberately
//     unused here: reconstructing "the prior state minus Dropped" is the delta model the
//     epic's Notes warn about — it agrees with graph.DefaultCarryOver by accident and is
//     wrong for every carry-over that re-zones, rewrites or synthesises a value.
package projection

import (
	"fmt"

	"github.com/yoann/kern-orch/internal/graph"
	"github.com/yoann/kern-orch/internal/journal"
)

// Project replays events in order and returns the graph.State the run held after the last
// event that could be applied. It fails rather than guessing on an incoherent sequence: a
// projection that quietly skipped an event it did not understand would produce a state no
// run ever had, and nothing downstream could tell.
func Project(events []journal.Event) (*graph.State, error) {
	replayed, err := Replay(events)
	if err != nil {
		return nil, err
	}
	return replayed.State, nil
}

// projector holds the replay's running position: the shared state so far, and the level
// currently open with one branch per frontier node. Branches are materialised at
// LevelOpened rather than lazily at NodeStarted so that combination always has a state per
// node in frontier order, even for a node that produced nothing.
type projector struct {
	shared   *graph.State
	level    *openLevel
	runID    string
	lastSeq  int64
	seenAny  bool
	closedBy journal.Kind
}

type openLevel struct {
	frontier   []string
	branches   map[string]*graph.State
	started    map[string]bool
	failed     map[string]bool
	stepAtOpen int
}

func (p *projector) apply(index int, ev journal.Event) error {
	if p.seenAny {
		if ev.Seq <= p.lastSeq {
			return fmt.Errorf("projection: event %d has seq %d, which is not after the previous event's seq %d", index, ev.Seq, p.lastSeq)
		}
		if ev.RunID != p.runID {
			return fmt.Errorf("projection: event %d belongs to run %q, but the replay started on run %q", index, ev.RunID, p.runID)
		}
	} else {
		p.runID = ev.RunID
	}
	p.seenAny = true
	p.lastSeq = ev.Seq

	if p.closedBy != "" {
		// A resumed run continues the journal it already has, so its RunStarted is the one
		// event that legitimately follows a terminal one: it opens a new attempt on the
		// same record. Everything else after a close is a record contradicting itself, and
		// stays refused — relaxing the check wholesale would make a lost LevelClosed
		// indistinguishable from a run that was genuinely picked up again.
		if _, reopens := ev.Payload.(journal.RunStarted); !reopens {
			return fmt.Errorf("projection: event %d (seq %d) arrives after the run was already closed by %s", index, ev.Seq, p.closedBy)
		}
		p.closedBy = ""
		// The previous attempt's unclosed level is dropped rather than carried into the new
		// one. The engine aborts and restarts a level as a whole, so that level combined
		// nothing into the shared state; keeping it open would make the resumed attempt's
		// own LevelOpened look like a second level opened inside the first.
		p.level = nil
	}

	switch payload := ev.Payload.(type) {
	case journal.RunStarted:
		// Nothing to apply: the graph's name is provenance, not state. The event is still
		// accepted rather than rejected as unknown, so a full journal replays as-is.
		return nil
	case journal.RunFinished:
		p.closedBy = journal.KindRunFinished
		return nil
	case journal.RunFailed:
		p.closedBy = journal.KindRunFailed
		return nil
	case journal.RunInterrupted:
		p.closedBy = journal.KindRunInterrupted
		return nil
	case journal.LevelOpened:
		return p.openLevel(index, payload)
	case journal.LevelClosed:
		return p.closeLevel(index, payload)
	case journal.NodeStarted:
		return p.startNode(index, payload)
	case journal.NodeProduced:
		return p.produce(index, payload)
	case journal.NodeFailed:
		return p.failNode(index, payload)
	case journal.NudgeApplied:
		return p.nudge(index, payload)
	case journal.FreezeApplied:
		return p.freeze(index, payload)
	default:
		// journal.Payload is a closed union, so this is unreachable today. It stays an
		// error rather than a silent no-op: a payload type added to the vocabulary without
		// a case here must stop replay, not be replayed as nothing.
		return fmt.Errorf("projection: event %d carries payload type %T, which replay does not handle", index, ev.Payload)
	}
}

func (p *projector) openLevel(index int, ev journal.LevelOpened) error {
	if p.level != nil {
		return fmt.Errorf("projection: event %d opens a level while the level with frontier %v is still open", index, p.level.frontier)
	}
	lvl := &openLevel{
		frontier:   append([]string(nil), ev.Frontier...),
		branches:   make(map[string]*graph.State, len(ev.Frontier)),
		started:    make(map[string]bool, len(ev.Frontier)),
		failed:     make(map[string]bool),
		stepAtOpen: p.shared.Step,
	}
	for _, id := range lvl.frontier {
		if _, dup := lvl.branches[id]; dup {
			return fmt.Errorf("projection: event %d opens a level whose frontier lists node %q twice", index, id)
		}
		// Each branch starts as a clone of the shared state, exactly as runLevel hands one
		// to each goroutine — which is why a merge can be additive and still be complete.
		lvl.branches[id] = p.shared.Clone()
	}
	p.level = lvl
	return nil
}

func (p *projector) closeLevel(index int, ev journal.LevelClosed) error {
	lvl := p.level
	if lvl == nil {
		return fmt.Errorf("projection: event %d closes a level with frontier %v that was never opened", index, ev.Frontier)
	}
	if !sameFrontier(lvl.frontier, ev.Frontier) {
		return fmt.Errorf("projection: event %d closes a level with frontier %v, but the open level's frontier is %v", index, ev.Frontier, lvl.frontier)
	}

	// Frontier order, not journal order: the engine folds results in the order the frontier
	// lists them, so that is who wins a contested key. Node goroutines finish in whatever
	// order they finish, so the journal's order is not the run's.
	var contributing []*graph.State
	for _, id := range lvl.frontier {
		if lvl.failed[id] {
			continue
		}
		contributing = append(contributing, lvl.branches[id])
	}

	switch ev.Rule {
	case journal.CombinationReplace:
		// The rule comes from the event; only its well-definedness is checked here. A
		// replace names one branch to adopt, so more (or none) has no meaning to resolve.
		if len(contributing) != 1 {
			return fmt.Errorf("projection: event %d closes a level with rule %q over %d branches, which names no single branch to adopt", index, journal.CombinationReplace, len(contributing))
		}
		// Adopting the branch wholesale is graph.State.replaceWith: it honors the keys the
		// branch no longer holds, which is how a Freeze propagates out of a single-node level.
		p.shared = contributing[0]
	case journal.CombinationMerge:
		for _, branch := range contributing {
			p.shared.Merge(branch)
		}
	default:
		return fmt.Errorf("projection: event %d closes a level with unknown combination rule %q", index, string(ev.Rule))
	}

	// runLevel advances Step once per frontier node, whatever the rule and whatever each
	// node wrote — including under replace, where adopting the branch would otherwise have
	// rewound Step to its value when the branch was cloned.
	p.shared.Step = lvl.stepAtOpen + len(lvl.frontier)
	p.level = nil
	return nil
}

func (p *projector) startNode(index int, ev journal.NodeStarted) error {
	lvl := p.level
	if lvl == nil {
		return fmt.Errorf("projection: event %d starts node %q while no level is open", index, ev.NodeID)
	}
	if _, ok := lvl.branches[ev.NodeID]; !ok {
		return fmt.Errorf("projection: event %d starts node %q, which is not in the open level's frontier %v", index, ev.NodeID, lvl.frontier)
	}
	if lvl.started[ev.NodeID] {
		return fmt.Errorf("projection: event %d starts node %q, which already started in this level", index, ev.NodeID)
	}
	lvl.started[ev.NodeID] = true
	return nil
}

func (p *projector) produce(index int, ev journal.NodeProduced) error {
	lvl := p.level
	if lvl == nil {
		return fmt.Errorf("projection: event %d has node %q produce while no level is open", index, ev.NodeID)
	}
	if !lvl.started[ev.NodeID] {
		return fmt.Errorf("projection: event %d has node %q produce, but it never started in this level", index, ev.NodeID)
	}
	branch := lvl.branches[ev.NodeID]
	for key, value := range ev.Data {
		// Written key by key rather than by adopting Data: the projection must own its map,
		// or a later mutation of the state would reach back into the caller's events.
		// A key absent from Zones is persistent, mirroring graph.State.Zone.
		branch.SetZoned(ev.Zones[key], key, value)
	}
	return nil
}

func (p *projector) failNode(index int, ev journal.NodeFailed) error {
	lvl := p.level
	if lvl == nil {
		return fmt.Errorf("projection: event %d fails node %q while no level is open", index, ev.NodeID)
	}
	if !lvl.started[ev.NodeID] {
		return fmt.Errorf("projection: event %d fails node %q, but it never started in this level", index, ev.NodeID)
	}
	// The engine drops a failed node's branch and aborts the level, so its partial writes
	// never reached the shared state. Marking it here keeps that true if the level closes.
	lvl.failed[ev.NodeID] = true
	return nil
}

func (p *projector) nudge(index int, ev journal.NudgeApplied) error {
	if p.level != nil {
		// A nudge is an out-of-band write applied by Engine.OnBeforeLevel, between two
		// levels. Inside an open level every branch has already been cloned, so there is no
		// state it could truthfully land on — the run could not have produced this.
		return fmt.Errorf("projection: event %d applies a nudge from %q while the level with frontier %v is open", index, ev.Origin, p.level.frontier)
	}
	for key, value := range ev.Data {
		// A nudge carries no zone labels, and graph.State.Set resets a key to persistent —
		// which is what an out-of-band write to the shared state does.
		p.shared.Set(key, value)
	}
	return nil
}

func (p *projector) freeze(index int, ev journal.FreezeApplied) error {
	target := p.shared
	inBranch := ""
	if p.level != nil {
		if len(p.level.frontier) != 1 {
			// A freeze inside a fan-out is unattributable — FreezeApplied carries no node
			// id — and the engine loses it anyway: Merge carries neither key removals nor
			// the Frozen counter. Refusing it beats applying it to an arbitrary branch.
			return fmt.Errorf("projection: event %d applies a freeze inside the level with frontier %v, where it cannot be attributed to a single branch", index, p.level.frontier)
		}
		inBranch = p.level.frontier[0]
		target = p.level.branches[inBranch]
	}

	// Replacement, not deletion: graph.State.Freeze discards the contents and the zone map
	// and repopulates from the carry-over's result, all in the persistent zone. Building a
	// fresh state reproduces that for any carry-over, including ones that re-zone a key,
	// rewrite a value, or synthesise a key the prior state never had.
	frozen := graph.NewState()
	for key, value := range ev.CarriedOver {
		frozen.Set(key, value)
	}
	frozen.Step = target.Step
	frozen.Frozen = target.Frozen + 1

	if inBranch == "" {
		p.shared = frozen
		return nil
	}
	p.level.branches[inBranch] = frozen
	return nil
}

// sameFrontier reports whether two frontiers are the same list in the same order. Order is
// part of the identity, not an incidental detail: it decides who wins a contested key when
// the level's branches are merged.
func sameFrontier(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
