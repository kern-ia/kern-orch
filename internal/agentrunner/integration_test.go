package agentrunner

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/yoann/kern-orch/internal/graph"
)

// TestEndToEndGraphWithTheClaudeCodeAdapter drives the full chain: Engine -> AgentNode ->
// ClaudeCode -> a real external process (a shell script emitting Claude Code's real
// stream-json shape) -> merged into state -> a downstream ToolNode consumes it.
func TestEndToEndGraphWithTheClaudeCodeAdapter(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the fake CLI is a POSIX shell script")
	}
	dir := t.TempDir()
	cli := filepath.Join(dir, "fake-claude.sh")
	script := "#!/bin/sh\n" +
		"cat >/dev/null\n" + // drain the stream-json request on stdin
		`printf '%s\n' '{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"provider-says-hi"}]}}'` + "\n" +
		`printf '%s\n' '{"type":"result","subtype":"success","is_error":false,"session_id":"s-e2e","result":"provider-says-hi"}'` + "\n"
	if err := os.WriteFile(cli, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}

	runner := &ClaudeCode{Path: cli}
	g := graph.NewGraph()
	g.AddNode(graph.NewAgentNode("ask", "what is up?", runner))
	g.AddNode(graph.NewToolNode("record", func(_ context.Context, s *State) error {
		if v, _ := s.Get("display:ask"); v == "provider-says-hi" {
			s.Set("recorded", true)
		}
		return nil
	}))
	g.SetEntry("ask")
	g.AddEdge("ask", graph.Static("record"))

	s := graph.NewState()
	if err := graph.NewEngine(g).Run(context.Background(), s); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if v, _ := s.Get("recorded"); v != true {
		t.Fatalf("end-to-end failed; state=%v", s.Keys())
	}
	if v, _ := s.Get("claude-code:session:ask"); v != "s-e2e" {
		t.Errorf("session provenance = %v, want it merged into the state", v)
	}
}

// State is aliased so the ToolNode closure above reads naturally.
type State = graph.State
