package agentrunner

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"strings"
	"testing"

	"github.com/yoann/kern-orch/internal/config"
	"github.com/yoann/kern-orch/internal/graph"
)

// The one test this adapter cannot fully trust without it: a real `claude` process answering
// a real turn. Every other test here drives a fake CLI, which proves the translation but not
// that the flags, the input shape and the stream framing are the ones the real binary
// accepts — the exact class of mistake the placeholder protocol was.
//
// Gated on the repo's own two variables rather than on a credential env var, because Claude
// Code authenticates from an inherited session as readily as from ANTHROPIC_API_KEY (issue
// 01's capture ran with apiKeySource "none"), so no env var can prove a credential exists.
// Requiring the caller to point KERN_AGENT_CLI at a binary keeps a billed model call from
// ever happening implicitly during `go test ./...`.
//
// Run with:
//
//	KERN_AGENT_CLI=$(command -v claude) KERN_AGENT_KIND=claude-code \
//	  go test ./internal/agentrunner/ -run TestClaudeCodeRoundTripsAgainstTheRealCLI -v
func TestClaudeCodeRoundTripsAgainstTheRealCLI(t *testing.T) {
	path := os.Getenv(config.EnvAgentCLI)
	if path == "" || os.Getenv(config.EnvAgentKind) != config.AgentKindClaudeCode {
		t.Skipf("set %s to a real claude binary and %s=%s to run this against the real CLI",
			config.EnvAgentCLI, config.EnvAgentKind, config.AgentKindClaudeCode)
	}
	if _, err := exec.LookPath(path); err != nil {
		t.Skipf("%s=%q is not an executable: %v", config.EnvAgentCLI, path, err)
	}

	// Built through the registry, not by hand: the e2e must exercise the same construction a
	// real run takes, kind selection included.
	var sink bytes.Buffer
	runner, err := New(
		config.Config{AgentCLI: path, AgentKind: config.AgentKindClaudeCode},
		Options{TokenSink: &sink, Stderr: os.Stderr},
	)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	res, err := runner.Run(context.Background(), graph.AgentRequest{
		NodeID: "e2e",
		Prompt: "Reply with exactly the single word: PONG",
		State:  graph.NewState(),
	})
	if err != nil {
		t.Fatalf("Run against the real claude CLI: %v", err)
	}

	answer, _ := res.Output["display:e2e"].(string)
	if answer == "" {
		t.Fatalf("Output = %v, want a non-empty display:e2e answer", res.Output)
	}
	if !strings.Contains(answer, "PONG") {
		t.Errorf("answer = %q, want it to contain PONG", answer)
	}
	if res.Output["claude-code:session:e2e"] == "" {
		t.Error("the real CLI's session id did not reach the node's output")
	}
	if sink.Len() == 0 {
		t.Error("the token sink stayed empty on a real turn")
	}
}
