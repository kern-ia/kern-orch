package graph

import (
	"context"
	"errors"
	"testing"
)

// buildChild returns a 2-node child graph that reads "seed" and writes "child_out".
func buildChild() *Graph {
	g := NewGraph()
	g.AddNode(NewToolNode("c1", func(_ context.Context, s *State) error {
		v, _ := s.Get("seed")
		n, _ := v.(int)
		s.Set("child_out", n*10)
		return nil
	}))
	g.SetEntry("c1")
	return g
}

func TestSubgraphNodeRunsNestedGraphAndBubblesResult(t *testing.T) {
	n := NewSubgraphNode("nested", buildChild())
	if n.Kind() != KindSubgraph {
		t.Fatalf("Kind() = %v; want KindSubgraph", n.Kind())
	}
	parent := NewState()
	parent.Set("seed", 4)
	if err := n.Execute(context.Background(), parent); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	// default input clones parent (child sees seed), default output merges child back.
	if v, _ := parent.Get("child_out"); v != 40 {
		t.Fatalf("child_out = %v; want 40 (result did not bubble up)", v)
	}
}

func TestSubgraphNodeIsolatesChildStateByDefault(t *testing.T) {
	child := NewGraph()
	child.AddNode(NewToolNode("only", func(_ context.Context, s *State) error {
		s.Set("seed", 999) // mutate a key that also exists in parent
		return nil
	}))
	child.SetEntry("only")

	// With a custom output that drops child keys, the parent must stay untouched.
	n := NewSubgraphNode("iso", child,
		WithOutput(func(parent, _ *State) { /* discard child result */ }))
	parent := NewState()
	parent.Set("seed", 1)
	if err := n.Execute(context.Background(), parent); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if v, _ := parent.Get("seed"); v != 1 {
		t.Fatalf("parent seed mutated to %v; child state leaked", v)
	}
}

func TestSubgraphCustomInputMapper(t *testing.T) {
	n := NewSubgraphNode("mapped", buildChild(),
		WithInput(func(parent *State) *State {
			c := NewState()
			pv, _ := parent.Get("outer")
			c.Set("seed", pv) // rename outer -> seed for the child
			return c
		}))
	parent := NewState()
	parent.Set("outer", 5)
	if err := n.Execute(context.Background(), parent); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if v, _ := parent.Get("child_out"); v != 50 {
		t.Fatalf("child_out = %v; want 50", v)
	}
}

func TestSubgraphPropagatesChildError(t *testing.T) {
	boom := errors.New("child failed")
	child := NewGraph()
	child.AddNode(NewToolNode("x", func(context.Context, *State) error { return boom }))
	child.SetEntry("x")
	n := NewSubgraphNode("bad", child)
	if err := n.Execute(context.Background(), NewState()); !errors.Is(err, boom) {
		t.Fatalf("Execute error = %v; want boom", err)
	}
}

func TestSubgraphInsideEngine(t *testing.T) {
	parentG := NewGraph()
	parentG.AddNode(NewToolNode("prep", func(_ context.Context, s *State) error {
		s.Set("seed", 3)
		return nil
	}))
	parentG.AddNode(NewSubgraphNode("sub", buildChild()))
	parentG.SetEntry("prep")
	parentG.AddEdge("prep", Static("sub"))

	s := NewState()
	if err := NewEngine(parentG).Run(context.Background(), s); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if v, _ := s.Get("child_out"); v != 30 {
		t.Fatalf("child_out = %v; want 30", v)
	}
}

// A nested run is invisible from outside: the parent sees one atomic step, so without a
// hook of its own the child's levels are never reported and a consumer can only draw the
// sub-agent as a single dot.
func TestSubgraphReportsItsOwnLevels(t *testing.T) {
	child := NewGraph()
	child.AddNode(NewToolNode("c1", func(context.Context, *State) error { return nil })).
		AddNode(NewToolNode("c2", func(context.Context, *State) error { return nil }))
	child.SetEntry("c1")
	child.AddEdge("c1", Static("c2"))

	var levels [][]string
	node := NewSubgraphNode("nested", child, WithChildStep(
		func(nodeID, _ string) StepFunc {
			if nodeID != "nested" {
				t.Errorf("the hook was built for %q, want the subgraph node id", nodeID)
			}
			return func(_ context.Context, info StepInfo, _ *State) error {
				levels = append(levels, info.Frontier)
				return nil
			}
		},
	))

	parent := NewGraph()
	parent.AddNode(node)
	parent.SetEntry("nested")

	if err := NewEngine(parent).Run(context.Background(), NewState()); err != nil {
		t.Fatalf("Run: %v", err)
	}

	// c1 completes announcing c2, then c2 completes announcing nothing.
	if len(levels) != 2 {
		t.Fatalf("the child reported %d levels, want 2: %v", len(levels), levels)
	}
	if len(levels[0]) != 1 || levels[0][0] != "c2" {
		t.Errorf("first level = %v, want [c2]", levels[0])
	}
	if len(levels[1]) != 0 {
		t.Errorf("last level = %v, want the empty frontier that closes the child", levels[1])
	}
}

// Without the option the child runs exactly as before: reporting is opt-in, and a graph
// built in Go without it must not gain a dependency on anything.
func TestASubgraphWithoutAHookStillRuns(t *testing.T) {
	child := NewGraph()
	child.AddNode(NewToolNode("c1", func(_ context.Context, s *State) error {
		s.Set("child_ran", true)
		return nil
	}))
	child.SetEntry("c1")

	parent := NewGraph()
	parent.AddNode(NewSubgraphNode("nested", child))
	parent.SetEntry("nested")

	s := NewState()
	if err := NewEngine(parent).Run(context.Background(), s); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if v, _ := s.Get("child_ran"); v != true {
		t.Error("the child did not run")
	}
}

// WithChildRun wires both hooks the builder returns onto the nested engine: a reporting
// StepFunc and a journalling EventFunc, so the child's own record and its wire report cover
// the same execution.
func TestSubgraphWithChildRunWiresBothHooks(t *testing.T) {
	child := NewGraph()
	child.AddNode(NewToolNode("c1", func(context.Context, *State) error { return nil }))
	child.SetEntry("c1")

	var steps int
	var kinds []EventKind
	node := NewSubgraphNode("nested", child, WithChildRun(
		func(nodeID, _ string) *ChildRunHooks {
			if nodeID != "nested" {
				t.Errorf("the builder was called for %q, want the subgraph node id", nodeID)
			}
			return &ChildRunHooks{
				Step: func(context.Context, StepInfo, *State) error { steps++; return nil },
				Event: func(_ context.Context, ev Event) error {
					kinds = append(kinds, ev.Kind)
					return nil
				},
			}
		},
	))

	if err := node.Execute(context.Background(), NewState()); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if steps == 0 {
		t.Error("the reporting hook never ran")
	}
	if len(kinds) == 0 || kinds[0] != EventRunStarted {
		t.Fatalf("event kinds = %v, want the first to be EventRunStarted", kinds)
	}
}

// The builder runs once per execution, and Execute defers Close until the nested engine has
// returned — a caller opening a resource per execution (cmd's own store connection) needs
// that resource to outlive the whole nested run, not just the call that opens it.
func TestSubgraphWithChildRunClosesAfterTheNestedEngineReturns(t *testing.T) {
	child := NewGraph()
	child.AddNode(NewToolNode("c1", func(context.Context, *State) error { return nil }))
	child.SetEntry("c1")

	var closedBeforeRunEnded bool
	var runEnded bool
	node := NewSubgraphNode("nested", child, WithChildRun(
		func(string, string) *ChildRunHooks {
			return &ChildRunHooks{
				Event: func(_ context.Context, ev Event) error {
					if ev.Kind == EventRunFinished {
						runEnded = true
					}
					return nil
				},
				Close: func() {
					if !runEnded {
						closedBeforeRunEnded = true
					}
				},
			}
		},
	))

	if err := node.Execute(context.Background(), NewState()); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if closedBeforeRunEnded {
		t.Error("Close ran before the nested run's own EventRunFinished — the resource closed too early")
	}
	if !runEnded {
		t.Fatal("the nested run never reported EventRunFinished")
	}
}

// The same node executed twice — a retry, a loop — must ask the builder twice, so a caller
// minting a fresh identity per call (cmd's nestedRuns) gets two distinct nested runs rather
// than one run journalled twice.
func TestSubgraphWithChildRunCallsTheBuilderOncePerExecution(t *testing.T) {
	child := NewGraph()
	child.AddNode(NewToolNode("c1", func(context.Context, *State) error { return nil }))
	child.SetEntry("c1")

	calls := 0
	node := NewSubgraphNode("nested", child, WithChildRun(
		func(string, string) *ChildRunHooks {
			calls++
			return &ChildRunHooks{}
		},
	))

	for i := 0; i < 2; i++ {
		if err := node.Execute(context.Background(), NewState()); err != nil {
			t.Fatalf("Execute %d: %v", i, err)
		}
	}
	if calls != 2 {
		t.Fatalf("the builder ran %d times over two executions, want 2", calls)
	}
}
