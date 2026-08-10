package graph

import (
	"context"
	"errors"
	"slices"
	"sort"
	"strings"
	"testing"
)

// appendNode records its id into a "trace" slice and sets a per-node key.
func appendNode(id string) Node {
	return NewToolNode(id, func(_ context.Context, s *State) error {
		tr, _ := s.Get("trace")
		list, _ := tr.([]string)
		s.Set("trace", append(list, id))
		s.Set("visited_"+id, true)
		return nil
	})
}

func TestEngineLinearPath(t *testing.T) {
	g := NewGraph()
	g.AddNode(appendNode("a")).AddNode(appendNode("b")).AddNode(appendNode("c"))
	g.SetEntry("a")
	g.AddEdge("a", Static("b"))
	g.AddEdge("b", Static("c"))
	// c has no edge => terminal
	if err := g.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}
	s := NewState()
	if err := NewEngine(g).Run(context.Background(), s); err != nil {
		t.Fatalf("Run: %v", err)
	}
	tr, _ := s.Get("trace")
	got, _ := tr.([]string)
	want := []string{"a", "b", "c"}
	if len(got) != 3 || got[0] != want[0] || got[1] != want[1] || got[2] != want[2] {
		t.Fatalf("trace = %v; want %v", got, want)
	}
	if s.Step != 3 {
		t.Fatalf("Step = %d; want 3", s.Step)
	}
}

func TestEngineConditionalRouting(t *testing.T) {
	g := NewGraph()
	g.AddNode(appendNode("start")).AddNode(appendNode("left")).AddNode(appendNode("right"))
	g.SetEntry("start")
	g.AddEdge("start", Conditional(func(s *State) []string {
		if v, _ := s.Get("go"); v == "L" {
			return []string{"left"}
		}
		return []string{"right"}
	}))
	s := NewState()
	s.Set("go", "L")
	if err := NewEngine(g).Run(context.Background(), s); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !s.Has("visited_left") || s.Has("visited_right") {
		t.Fatalf("wrong branch taken: %v", s.Keys())
	}
}

func TestEngineFanOutRunsBranchesAndMerges(t *testing.T) {
	g := NewGraph()
	g.AddNode(appendNode("root")).AddNode(appendNode("x")).AddNode(appendNode("y"))
	g.SetEntry("root")
	g.AddEdge("root", Static("x", "y")) // fan-out
	s := NewState()
	if err := NewEngine(g).Run(context.Background(), s); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !s.Has("visited_x") || !s.Has("visited_y") {
		t.Fatalf("both branches should have run: %v", s.Keys())
	}
}

func TestValidateRejectsUnknownEntryAndEdges(t *testing.T) {
	g := NewGraph()
	g.AddNode(appendNode("a"))
	g.SetEntry("missing")
	if err := g.Validate(); err == nil {
		t.Fatal("expected error for unknown entry")
	}
	g.SetEntry("a")
	g.AddEdge("a", Static("ghost"))
	if err := g.Validate(); err == nil {
		t.Fatal("expected error for edge to unknown node")
	}
}

func TestEngineCycleGuard(t *testing.T) {
	g := NewGraph()
	g.AddNode(appendNode("loop"))
	g.SetEntry("loop")
	g.AddEdge("loop", Static("loop"))
	err := NewEngine(g).WithMaxSteps(5).Run(context.Background(), NewState())
	if err == nil {
		t.Fatal("expected cycle-guard error")
	}
}

func TestStaticRouteIsStable(t *testing.T) {
	r := Static("b", "a", "c")
	got := r(NewState())
	cp := append([]string(nil), got...)
	sort.Strings(cp)
	if got[0] != "b" || got[1] != "a" || got[2] != "c" {
		t.Fatalf("Static should preserve given order, got %v", got)
	}
}

// The engine knows exactly which nodes broke: it waits for the whole level before it
// returns. Flattening that into a string threw the knowledge away, and a consumer drawing
// the graph could then only mark the whole frontier and hope.
func TestALevelErrorNamesEveryNodeThatFailed(t *testing.T) {
	failing := func(id, msg string) Node {
		return NewToolNode(id, func(context.Context, *State) error { return errors.New(msg) })
	}

	g := NewGraph()
	g.AddNode(appendNode("start")).
		AddNode(appendNode("ok")).
		AddNode(failing("boom", "exploded")).
		AddNode(failing("crash", "also exploded"))
	g.SetEntry("start")
	g.AddEdge("start", Static("ok", "boom", "crash"))

	err := NewEngine(g).Run(context.Background(), NewState())
	if err == nil {
		t.Fatal("the run was reported as a success")
	}

	var lvl *LevelError
	if !errors.As(err, &lvl) {
		t.Fatalf("error = %T (%v), want a *LevelError a caller can inspect", err, err)
	}

	want := []string{"boom", "crash"}
	if !slices.Equal(lvl.Nodes, want) {
		t.Errorf("Nodes = %v, want %v — sorted, and every failure named", lvl.Nodes, want)
	}
	// A node absent from the list completed. That is what lets a consumer colour the rest
	// of the frontier instead of blaming all of it.
	if slices.Contains(lvl.Nodes, "ok") {
		t.Error("a node that succeeded was named among the failures")
	}
}

// The message is the contract with humans and with every existing test: naming the node
// reads exactly as it did before.
func TestALevelErrorStillReadsAsBefore(t *testing.T) {
	g := NewGraph()
	g.AddNode(NewToolNode("boom", func(context.Context, *State) error {
		return errors.New("exploded")
	}))
	g.SetEntry("boom")

	err := NewEngine(g).Run(context.Background(), NewState())

	if got, want := err.Error(), `node "boom": exploded`; got != want {
		t.Errorf("Error() = %q, want %q", got, want)
	}
	if !strings.Contains(err.Error(), "exploded") {
		t.Error("the underlying cause was lost")
	}
}

// A nudge applies between levels, before the next one starts — this is the seam C6's
// steering channel injects a live instruction through.
func TestOnBeforeLevelAppliesBeforeEachLevel(t *testing.T) {
	g := NewGraph()
	g.AddNode(appendNode("a")).AddNode(appendNode("b"))
	g.SetEntry("a")
	g.AddEdge("a", Static("b"))

	var calls int
	e := NewEngine(g).OnBeforeLevel(func(_ context.Context, s *State) error {
		calls++
		s.Set("nudge_seen_at_level", calls)
		return nil
	})

	s := NewState()
	if err := e.Run(context.Background(), s); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if calls != 2 {
		t.Fatalf("OnBeforeLevel called %d times, want 2 (one per level)", calls)
	}
}

// The hook's own state mutation must reach the nodes about to run, not just sit unused —
// that is the entire point of a nudge.
func TestOnBeforeLevelStateIsVisibleToTheNode(t *testing.T) {
	g := NewGraph()
	g.AddNode(NewToolNode("read", func(_ context.Context, s *State) error {
		v, _ := s.Get("nudge")
		s.Set("saw", v)
		return nil
	}))
	g.SetEntry("read")

	e := NewEngine(g).OnBeforeLevel(func(_ context.Context, s *State) error {
		s.Set("nudge", "hello")
		return nil
	})

	s := NewState()
	if err := e.Run(context.Background(), s); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if v, _ := s.Get("saw"); v != "hello" {
		t.Fatalf("saw = %v, want hello", v)
	}
}

// A hook that errors must abort the run rather than being swallowed silently.
func TestOnBeforeLevelErrorAbortsTheRun(t *testing.T) {
	g := NewGraph()
	g.AddNode(appendNode("a"))
	g.SetEntry("a")

	sentinel := errors.New("nudge source unreachable")
	e := NewEngine(g).OnBeforeLevel(func(context.Context, *State) error { return sentinel })

	if err := e.Run(context.Background(), NewState()); !errors.Is(err, sentinel) {
		t.Fatalf("Run error = %v, want %v", err, sentinel)
	}
}

// No hook registered must behave exactly as before — nil is a no-op, not a nil-pointer
// panic waiting to happen.
func TestOnBeforeLevelUnsetIsANoOp(t *testing.T) {
	g := NewGraph()
	g.AddNode(appendNode("a"))
	g.SetEntry("a")
	if err := NewEngine(g).Run(context.Background(), NewState()); err != nil {
		t.Fatalf("Run: %v", err)
	}
}
