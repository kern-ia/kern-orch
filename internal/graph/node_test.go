package graph

import (
	"context"
	"errors"
	"testing"
)

func TestToolNodeExecutesFuncAndMutatesState(t *testing.T) {
	n := NewToolNode("increment", func(_ context.Context, s *State) error {
		v, _ := s.Get("count")
		cur, _ := v.(int)
		s.Set("count", cur+1)
		return nil
	})
	if n.ID() != "increment" {
		t.Fatalf("ID() = %q; want increment", n.ID())
	}
	if n.Kind() != KindTool {
		t.Fatalf("Kind() = %v; want KindTool", n.Kind())
	}
	s := NewState()
	s.Set("count", 41)
	if err := n.Execute(context.Background(), s); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if v, _ := s.Get("count"); v != 42 {
		t.Fatalf("count = %v; want 42", v)
	}
}

func TestToolNodePropagatesError(t *testing.T) {
	sentinel := errors.New("boom")
	n := NewToolNode("fail", func(context.Context, *State) error { return sentinel })
	if err := n.Execute(context.Background(), NewState()); !errors.Is(err, sentinel) {
		t.Fatalf("Execute error = %v; want %v", err, sentinel)
	}
}

// fakeRunner is a test double for the AgentRunner port.
type fakeRunner struct {
	gotPrompt string
	result    map[string]any
	err       error
}

func (f *fakeRunner) Run(_ context.Context, req AgentRequest) (AgentResult, error) {
	f.gotPrompt = req.Prompt
	return AgentResult{Output: f.result}, f.err
}

func TestAgentNodeInvokesRunnerAndMergesOutput(t *testing.T) {
	r := &fakeRunner{result: map[string]any{"answer": "yes"}}
	n := NewAgentNode("planner", "decide: {{topic}}", r)
	if n.Kind() != KindAgent {
		t.Fatalf("Kind() = %v; want KindAgent", n.Kind())
	}
	s := NewState()
	s.Set("topic", "routing")
	if err := n.Execute(context.Background(), s); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if r.gotPrompt != "decide: {{topic}}" {
		t.Fatalf("runner got prompt %q", r.gotPrompt)
	}
	if v, _ := s.Get("answer"); v != "yes" {
		t.Fatalf("agent output not merged into state: %v", v)
	}
}

func TestAgentNodePropagatesRunnerError(t *testing.T) {
	sentinel := errors.New("runner down")
	n := NewAgentNode("a", "p", &fakeRunner{err: sentinel})
	if err := n.Execute(context.Background(), NewState()); !errors.Is(err, sentinel) {
		t.Fatalf("Execute error = %v; want %v", err, sentinel)
	}
}

func TestApprovalNodeRecordsTheDecisionUnderItsOwnKey(t *testing.T) {
	n := NewApprovalNode("confirm", func(_ context.Context, nodeID string) (Decision, error) {
		if nodeID != "confirm" {
			t.Errorf("wait called with nodeID = %q, want confirm", nodeID)
		}
		return Approved, nil
	})
	if n.Kind() != KindApproval {
		t.Fatalf("Kind() = %v; want KindApproval", n.Kind())
	}
	s := NewState()
	if err := n.Execute(context.Background(), s); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if v, _ := s.Get("decision:confirm"); v != "approve" {
		t.Fatalf("decision:confirm = %v; want approve", v)
	}
}

func TestApprovalNodeRecordsRefusal(t *testing.T) {
	n := NewApprovalNode("confirm", func(context.Context, string) (Decision, error) {
		return Refused, nil
	})
	s := NewState()
	if err := n.Execute(context.Background(), s); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if v, _ := s.Get("decision:confirm"); v != "refuse" {
		t.Fatalf("decision:confirm = %v; want refuse", v)
	}
}

// A cancelled context (a stop request) must propagate as an error, not hang forever or
// silently record a decision nobody made.
func TestApprovalNodePropagatesWaitError(t *testing.T) {
	sentinel := errors.New("stopped")
	n := NewApprovalNode("confirm", func(context.Context, string) (Decision, error) {
		return "", sentinel
	})
	if err := n.Execute(context.Background(), NewState()); !errors.Is(err, sentinel) {
		t.Fatalf("Execute error = %v; want %v", err, sentinel)
	}
}
