package topology

import (
	"context"
	"strings"
	"testing"

	"github.com/yoann/kern-orch/internal/agentrunner"
	"github.com/yoann/kern-orch/internal/graph"
)

func testRegistry() *Registry {
	reg := NewRegistry(&agentrunner.Stub{})
	reg.Tool("mark", func(_ context.Context, s *graph.State) error {
		s.Set("marked", true)
		return nil
	})
	reg.Router("toEnd", func(*graph.State) []string { return nil })
	return reg
}

const goodYAML = `
entry: start
nodes:
  - id: start
    type: tool
    func: mark
  - id: think
    type: agent
    skill: planner
    prompt: "plan {{goal}}"
edges:
  - from: start
    to: [think]
  - from: think
    router: toEnd
`

func TestLoadBuildsRunnableGraph(t *testing.T) {
	g, err := Load([]byte(goodYAML), testRegistry())
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if err := g.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}
	s := graph.NewState()
	if err := graph.NewEngine(g).Run(context.Background(), s); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if v, _ := s.Get("marked"); v != true {
		t.Fatalf("tool node did not run: %v", s.Keys())
	}
	// agent node used the stub, which echoes the prompt under "echo"
	if v, _ := s.Get("echo"); v != "plan {{goal}}" {
		t.Fatalf("agent node output missing: %v", s.Keys())
	}
}

func TestLoadUnknownToolFuncErrors(t *testing.T) {
	y := "entry: a\nnodes:\n  - id: a\n    type: tool\n    func: nope\n"
	_, err := Load([]byte(y), testRegistry())
	if err == nil || !strings.Contains(err.Error(), "nope") {
		t.Fatalf("err = %v; want unknown func nope", err)
	}
}

func TestLoadUnknownRouterErrors(t *testing.T) {
	y := "entry: a\nnodes:\n  - id: a\n    type: tool\n    func: mark\nedges:\n  - from: a\n    router: ghost\n"
	_, err := Load([]byte(y), testRegistry())
	if err == nil || !strings.Contains(err.Error(), "ghost") {
		t.Fatalf("err = %v; want unknown router ghost", err)
	}
}

func TestLoadInvalidNodeTypeErrors(t *testing.T) {
	y := "entry: a\nnodes:\n  - id: a\n    type: wizard\n"
	if _, err := Load([]byte(y), testRegistry()); err == nil {
		t.Fatal("expected error for invalid node type")
	}
}

func TestLoadEdgeWithBothToAndRouterErrors(t *testing.T) {
	y := "entry: a\nnodes:\n  - id: a\n    type: tool\n    func: mark\nedges:\n  - from: a\n    to: [a]\n    router: toEnd\n"
	if _, err := Load([]byte(y), testRegistry()); err == nil {
		t.Fatal("expected error: edge has both to and router")
	}
}

func TestLoadAgentNodeWithoutRunnerErrors(t *testing.T) {
	reg := NewRegistry(nil) // no runner configured
	y := "entry: a\nnodes:\n  - id: a\n    type: agent\n    prompt: hi\n"
	if _, err := Load([]byte(y), reg); err == nil {
		t.Fatal("expected error: agent node needs a runner")
	}
}

func TestLoadApprovalNodeUsesTheRegisteredWaitFunc(t *testing.T) {
	reg := testRegistry()
	var gotNodeID string
	reg.OnApproval(func(_ context.Context, nodeID string) (graph.Decision, error) {
		gotNodeID = nodeID
		return graph.Approved, nil
	})
	y := "entry: confirm\nnodes:\n  - id: confirm\n    type: approval\n"

	g, err := Load([]byte(y), reg)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	s := graph.NewState()
	if err := graph.NewEngine(g).Run(context.Background(), s); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if gotNodeID != "confirm" {
		t.Errorf("wait func called with %q, want confirm", gotNodeID)
	}
	if v, _ := s.Get(graph.DecisionKey("confirm")); v != "approve" {
		t.Errorf("decision:confirm = %v, want approve", v)
	}
}

// A graph run through the bare CLI (no daemon, no mailbox) must refuse an approval node
// clearly rather than hang forever with nothing configured to answer it.
func TestLoadApprovalNodeWithoutAWaitFuncErrors(t *testing.T) {
	reg := testRegistry() // OnApproval never called
	y := "entry: confirm\nnodes:\n  - id: confirm\n    type: approval\n"
	if _, err := Load([]byte(y), reg); err == nil {
		t.Fatal("expected error: approval node needs a decision source")
	}
}
