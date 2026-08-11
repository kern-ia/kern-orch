package agentrunner

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"

	"github.com/yoann/kern-orch/internal/graph"
)

// EnvCLIPath is the environment variable naming the external CLI binary.
const EnvCLIPath = "KERN_AGENT_CLI"

// Subprocess is the real AgentRunner: it spawns the external multi-provider CLI and
// exchanges the JSON-lines protocol documented in protocol.go.
type Subprocess struct {
	Path      string    // CLI binary path
	Args      []string  // extra args passed to the binary
	Env       []string  // child environment (nil => inherit parent)
	Stderr    io.Writer // child stderr (nil => discarded)
	TokenSink io.Writer // incremental token stream (nil => discarded)

	// OnActivity, when set, is called with true once the child is running and with false
	// when it is done, whatever the outcome. It brackets the window during which a model
	// is working on this node.
	//
	// The bracket opens at spawn rather than at the first token on purpose: a provider that
	// answers in one piece streams no token at all, and waiting for one would report such a
	// node as never having thought. What a caller learns is "the model is working", which
	// is the coarse fact this hook exists to carry.
	//
	// message, on the stop call only, is the node's own state["display:<nodeID>"] output
	// when it set one — the same convention kern-ui's hive panel already reads for a
	// node's output, reused here rather than inventing a second way for a skill to narrate
	// itself. Empty when the node set none, or on the start call (nothing has run yet).
	//
	// It must not block: it is called on the run's own thread.
	OnActivity func(nodeID string, generating bool, message string)
}

// NewSubprocessFromEnv builds a runner from KERN_AGENT_CLI. The bool is false when the
// variable is unset, signalling the caller to fall back to the Stub.
func NewSubprocessFromEnv() (*Subprocess, bool) {
	path := os.Getenv(EnvCLIPath)
	if path == "" {
		return nil, false
	}
	return &Subprocess{Path: path}, true
}

// Run implements graph.AgentRunner by streaming the request through the child process.
func (r *Subprocess) Run(ctx context.Context, req graph.AgentRequest) (graph.AgentResult, error) {
	stateJSON, err := json.Marshal(req.State)
	if err != nil {
		return graph.AgentResult{}, fmt.Errorf("agentrunner: marshal state: %w", err)
	}
	payload, err := json.Marshal(Request{NodeID: req.NodeID, Prompt: req.Prompt, State: stateJSON})
	if err != nil {
		return graph.AgentResult{}, fmt.Errorf("agentrunner: marshal request: %w", err)
	}

	cmd := exec.CommandContext(ctx, r.Path, r.Args...)
	cmd.Env = r.Env
	cmd.Stderr = r.Stderr
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return graph.AgentResult{}, fmt.Errorf("agentrunner: stdin: %w", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return graph.AgentResult{}, fmt.Errorf("agentrunner: stdout: %w", err)
	}
	if err := cmd.Start(); err != nil {
		return graph.AgentResult{}, fmt.Errorf("agentrunner: start %q: %w", r.Path, err)
	}

	// Deferred so the bracket closes on every path out of here, including the error ones.
	// A stop that only fired on success would leave a beacon lit on a broken run. result is
	// read by the closure at call time, not captured now, so a stop reported after an error
	// path (where it never got set) narrates nothing rather than a stale value.
	var result graph.AgentResult
	r.activity(req.NodeID, true, "")
	defer func() { r.activity(req.NodeID, false, displayMessage(req.NodeID, result)) }()

	// Send the single request object, then close stdin so the child knows we're done.
	if _, err := stdin.Write(payload); err != nil {
		_ = cmd.Wait()
		return graph.AgentResult{}, fmt.Errorf("agentrunner: write request: %w", err)
	}
	_ = stdin.Close()

	var runErr error
	result, runErr = r.consume(stdout)
	if waitErr := cmd.Wait(); waitErr != nil && runErr == nil {
		return graph.AgentResult{}, fmt.Errorf("agentrunner: %q exited: %w", r.Path, waitErr)
	}
	return result, runErr
}

// displayMessage reads the node's own state["display:<nodeID>"] output, if it set one —
// see OnActivity's doc comment. Not every node opts in, and an empty result is a normal,
// silent outcome, not an error.
func displayMessage(nodeID string, result graph.AgentResult) string {
	v, ok := result.Output["display:"+nodeID]
	if !ok {
		return ""
	}
	s, _ := v.(string)
	return s
}

// consume scans the child's stdout line-by-line, forwarding tokens and capturing the
// last result. It returns an error on an error event or if no result was produced.
func (r *Subprocess) consume(stdout io.Reader) (graph.AgentResult, error) {
	sink := r.TokenSink
	if sink == nil {
		sink = io.Discard
	}
	sc := bufio.NewScanner(stdout)
	sc.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)

	var result *graph.AgentResult
	for sc.Scan() {
		line := sc.Bytes()
		if len(line) == 0 {
			continue
		}
		var ev Event
		if err := json.Unmarshal(line, &ev); err != nil {
			return graph.AgentResult{}, fmt.Errorf("agentrunner: bad event %q: %w", line, err)
		}
		switch ev.Type {
		case eventToken:
			_, _ = io.WriteString(sink, ev.Text)
		case eventResult:
			result = &graph.AgentResult{Output: ev.Output}
		case eventError:
			return graph.AgentResult{}, fmt.Errorf("agentrunner: agent error: %s", ev.Message)
		}
	}
	if err := sc.Err(); err != nil {
		return graph.AgentResult{}, fmt.Errorf("agentrunner: read stdout: %w", err)
	}
	if result == nil {
		return graph.AgentResult{}, fmt.Errorf("agentrunner: child produced no result event")
	}
	return *result, nil
}

// activity fires the OnActivity hook when one is set.
func (r *Subprocess) activity(nodeID string, generating bool, message string) {
	if r.OnActivity != nil {
		r.OnActivity(nodeID, generating, message)
	}
}
