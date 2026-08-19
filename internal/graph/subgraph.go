package graph

import (
	"context"
	"fmt"
)

// SubgraphNode runs a nested graph as a single node — the sub-agent of spec §3. The
// child runs with its own State (seeded from the parent) and its result is merged back
// into the parent. From the parent's checkpoint view the whole sub-run is one atomic
// step (spec §6.3: checkpoint at sub-graph boundaries).
type SubgraphNode struct {
	id       string
	sub      *Graph
	graphRef string
	input    func(parent *State) *State
	output   func(parent, child *State)

	// childStep builds the hook the nested engine runs, or is nil when nobody is watching.
	// It is a factory rather than a hook because each execution is a distinct nested run and
	// the builder needs to know which node it belongs to.
	childStep func(nodeID, graphRef string) StepFunc

	// childRun builds both hooks a nested execution reports and journals through, bound to
	// the same run identity. Kept separate from childStep rather than folding one into the
	// other: a caller that only wants reporting (WithChildStep, and every existing test)
	// keeps working unchanged, while a caller that wants both — cmd's nestedRuns — gets a
	// single factory call per execution instead of two independent ones. Two independent
	// factories could not be trusted to agree on a run id without a shared, synchronized
	// place to mint it, and childStep's build runs concurrently across a level's subgraph
	// nodes, so that place would need locking two callers should not have to know about.
	childRun func(nodeID, graphRef string) *ChildRunHooks
}

// ChildRunHooks are the engine hooks one nested execution reports and journals through,
// built together so both describe the same run. Either Step or Event may be nil: a caller
// wiring only reporting, or only journalling, leaves the other seam untouched.
type ChildRunHooks struct {
	Step  StepFunc
	Event EventFunc

	// Close releases whatever the builder opened for this one execution — a store
	// connection, typically — once the nested engine has returned, win or lose. Scoped to
	// the execution rather than left for the caller to reclaim later: a builder that opens
	// a resource per subgraph node and never hears back would have no correct moment to
	// close it. Nil when there is nothing to release.
	Close func()
}

// WithGraphRef records the file the nested graph came from, so a caller can describe its
// shape later. Purely informational: the engine never reads it.
func WithGraphRef(ref string) SubgraphOption {
	return func(n *SubgraphNode) { n.graphRef = ref }
}

// SubgraphOption customizes how state flows in and out of the nested graph.
type SubgraphOption func(*SubgraphNode)

// WithInput overrides how the child's initial state is derived from the parent.
// Default: a Clone of the parent (the child sees the parent's context).
func WithInput(fn func(parent *State) *State) SubgraphOption {
	return func(n *SubgraphNode) { n.input = fn }
}

// WithOutput overrides how the finished child state is folded back into the parent.
// Default: Merge every child key into the parent.
func WithOutput(fn func(parent, child *State)) SubgraphOption {
	return func(n *SubgraphNode) { n.output = fn }
}

// WithChildStep makes the nested run report its own levels.
//
// Without it the child is invisible: the parent sees one atomic step, so a consumer can
// only ever draw the sub-agent as a single dot with no idea what happens inside. Opt-in,
// so a graph built in Go without observability keeps running exactly as before.
func WithChildStep(build func(nodeID, graphRef string) StepFunc) SubgraphOption {
	return func(n *SubgraphNode) { n.childStep = build }
}

// WithChildRun makes the nested run journal its own record, and report through it, as one
// run: the durable record and the wire report describe the same execution, identified the
// same way. Opt-in, mirroring WithChildStep: a graph built in Go without a recorder keeps
// running exactly as before, and silently — the parent's checkpoint view still sees the
// whole sub-run as one atomic step (see the type doc).
func WithChildRun(build func(nodeID, graphRef string) *ChildRunHooks) SubgraphOption {
	return func(n *SubgraphNode) { n.childRun = build }
}

// GraphRef returns the file the nested graph was loaded from, or "" when it was built in
// Go. A caller describing the child's shape needs it; the engine never does.
func (n *SubgraphNode) GraphRef() string { return n.graphRef }

// NewSubgraphNode builds a subgraph node wrapping sub.
func NewSubgraphNode(id string, sub *Graph, opts ...SubgraphOption) *SubgraphNode {
	n := &SubgraphNode{
		id:     id,
		sub:    sub,
		input:  func(parent *State) *State { return parent.Clone() },
		output: func(parent, child *State) { parent.Merge(child) },
	}
	for _, o := range opts {
		o(n)
	}
	return n
}

func (n *SubgraphNode) ID() string { return n.id }
func (n *SubgraphNode) Kind() Kind { return KindSubgraph }

// Execute seeds the child state, runs the nested graph to completion, then bubbles the
// result back into the parent.
func (n *SubgraphNode) Execute(ctx context.Context, s *State) error {
	child := n.input(s)

	engine := NewEngine(n.sub)
	if n.childStep != nil {
		if hook := n.childStep(n.id, n.graphRef); hook != nil {
			engine.OnStep(hook)
		}
	}
	// One call, not two: see childRun's field doc on why the run id it mints must reach
	// both hooks together.
	if n.childRun != nil {
		if hooks := n.childRun(n.id, n.graphRef); hooks != nil {
			if hooks.Close != nil {
				defer hooks.Close()
			}
			if hooks.Step != nil {
				engine.OnStep(hooks.Step)
			}
			if hooks.Event != nil {
				engine.OnEvent(hooks.Event)
			}
		}
	}

	if err := engine.Run(ctx, child); err != nil {
		return fmt.Errorf("subgraph %q: %w", n.id, err)
	}
	n.output(s, child)
	return nil
}
