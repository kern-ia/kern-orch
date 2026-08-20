package agentrunner

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/yoann/kern-orch/internal/graph"
)

var _ graph.AgentRunner = (*ClaudeCode)(nil)

// Each call is a fresh, self-contained process, so the pair is a no-op — but declaring it
// keeps every adapter answering the same question, instead of the call site having to know
// which ones opted in.
var _ Lifecycle = (*ClaudeCode)(nil)

const (
	fixtureSessionPlainText = "ce7af633-c564-4c2f-af29-8bbec6fc9f71"
	fixtureSessionToolCall  = "7ef0d022-0e9e-49e5-b231-74a00e161acf"
	fixtureResultPlainText  = "PONG"
	fixtureResultToolCall   = "Fait : `hello-from-fixture-capture` affiché."
)

// realCapture returns the lines of issue 01's verbatim capture belonging to one invocation.
// The committed file concatenates two back-to-back runs, and a single `claude` process only
// ever emits one of them; sessionID is what separates them without editing the fixture.
func realCapture(t *testing.T, sessionID string) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("testdata", "claude-code-stream.jsonl"))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	var kept []string
	for _, line := range strings.Split(strings.TrimSpace(string(raw)), "\n") {
		if strings.Contains(line, sessionID) {
			kept = append(kept, line)
		}
	}
	if len(kept) == 0 {
		t.Fatalf("fixture carries no line for session %s", sessionID)
	}
	return strings.Join(kept, "\n") + "\n"
}

func TestClaudeCodeTranslatesTheRealPlainTextCapture(t *testing.T) {
	var sink bytes.Buffer
	res, err := translateClaudeStream(strings.NewReader(realCapture(t, fixtureSessionPlainText)), "n1", &sink)
	if err != nil {
		t.Fatalf("translateClaudeStream: %v", err)
	}
	if got := res.Output["display:n1"]; got != fixtureResultPlainText {
		t.Errorf("display:n1 = %v, want %q", got, fixtureResultPlainText)
	}
	if got := res.Output["claude-code:session:n1"]; got != fixtureSessionPlainText {
		t.Errorf("claude-code:session:n1 = %v, want %q", got, fixtureSessionPlainText)
	}
	if len(res.Output) != 2 {
		t.Errorf("Output = %v, want exactly the display and session keys", res.Output)
	}
	if sink.String() != fixtureResultPlainText {
		t.Errorf("token sink = %q, want %q", sink.String(), fixtureResultPlainText)
	}
}

// The tool-call capture is the second shape a translator must survive: an assistant message
// whose only content block is a `tool_use`, then a `user` message carrying the `tool_result`.
// Neither is model prose, so neither reaches the token sink — the sink is the answer stream,
// not a trace of the CLI's internal activity.
func TestClaudeCodeKeepsToolTrafficOutOfTheTokenStream(t *testing.T) {
	var sink bytes.Buffer
	res, err := translateClaudeStream(strings.NewReader(realCapture(t, fixtureSessionToolCall)), "n1", &sink)
	if err != nil {
		t.Fatalf("translateClaudeStream: %v", err)
	}
	if got := res.Output["display:n1"]; got != fixtureResultToolCall {
		t.Errorf("display:n1 = %v, want %q", got, fixtureResultToolCall)
	}
	if sink.String() != fixtureResultToolCall {
		t.Errorf("token sink = %q, want only the assistant's final text", sink.String())
	}
	if strings.Contains(sink.String(), "echo hello-from-fixture-capture") {
		t.Error("the Bash tool_use input leaked into the token sink")
	}
}

// The committed fixture is two runs concatenated. Feeding it whole is the closest thing to a
// stream carrying more than one result, and pins the rule: the last result wins.
func TestClaudeCodeKeepsTheLastResultWhenAStreamCarriesSeveral(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("testdata", "claude-code-stream.jsonl"))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	var sink bytes.Buffer
	res, err := translateClaudeStream(bytes.NewReader(raw), "n1", &sink)
	if err != nil {
		t.Fatalf("translateClaudeStream: %v", err)
	}
	if got := res.Output["display:n1"]; got != fixtureResultToolCall {
		t.Errorf("display:n1 = %v, want the last result %q", got, fixtureResultToolCall)
	}
	want := fixtureResultPlainText + fixtureResultToolCall
	if sink.String() != want {
		t.Errorf("token sink = %q, want %q", sink.String(), want)
	}
}

func TestClaudeCodeErrorsWhenTheStreamCarriesNoResultMessage(t *testing.T) {
	stream := `{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"half"}]}}` + "\n"
	_, err := translateClaudeStream(strings.NewReader(stream), "n1", &bytes.Buffer{})
	if err == nil {
		t.Fatal("expected an error when the CLI emitted no result message")
	}
	if !strings.Contains(err.Error(), "claude-code") {
		t.Errorf("error = %q, want it to name the adapter", err)
	}
}

func TestClaudeCodeReportsAnErroredResultAsAnError(t *testing.T) {
	stream := `{"type":"result","subtype":"error_during_execution","is_error":true,"session_id":"s","result":"credit balance too low"}` + "\n"
	_, err := translateClaudeStream(strings.NewReader(stream), "n1", &bytes.Buffer{})
	if err == nil {
		t.Fatal("expected an error from a result with is_error=true")
	}
	if !strings.Contains(err.Error(), "credit balance too low") {
		t.Errorf("error = %q, want it to carry the CLI's own message", err)
	}
}

// fakeClaude writes a POSIX script standing in for the `claude` binary and returns a runner
// bound to it. body is appended after the stdin drain, so a test decides what the fake emits
// and with which exit status.
func fakeClaude(t *testing.T, body string) *ClaudeCode {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the fake CLI is a POSIX shell script")
	}
	path := filepath.Join(t.TempDir(), "fake-claude.sh")
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"+body), 0o755); err != nil {
		t.Fatal(err)
	}
	return &ClaudeCode{Path: path}
}

const fakeClaudeSuccess = `cat >/dev/null
printf '%s\n' '{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"answered"}]}}'
printf '%s\n' '{"type":"result","subtype":"success","is_error":false,"session_id":"s-1","result":"answered"}'
`

func TestClaudeCodeBracketsActivityAroundASuccessfulCall(t *testing.T) {
	r := fakeClaude(t, fakeClaudeSuccess)
	var calls []string
	r.OnActivity = func(nodeID string, generating bool, message string) {
		calls = append(calls, nodeID+"|"+boolText(generating)+"|"+message)
	}

	if _, err := r.Run(context.Background(), graph.AgentRequest{NodeID: "n1", Prompt: "p", State: graph.NewState()}); err != nil {
		t.Fatalf("Run: %v", err)
	}

	want := []string{"n1|true|", "n1|false|answered"}
	if len(calls) != 2 || calls[0] != want[0] || calls[1] != want[1] {
		t.Errorf("OnActivity calls = %v, want %v", calls, want)
	}
}

// The stop half of the bracket must fire on the error paths too: a beacon left lit on a
// broken run is worse than no beacon. Nothing is narrated, because nothing was produced.
func TestClaudeCodeBracketsActivityAroundAFailedCall(t *testing.T) {
	r := fakeClaude(t, "cat >/dev/null\nexit 3\n")
	var calls []string
	r.OnActivity = func(nodeID string, generating bool, message string) {
		calls = append(calls, nodeID+"|"+boolText(generating)+"|"+message)
	}

	if _, err := r.Run(context.Background(), graph.AgentRequest{NodeID: "n1", Prompt: "p", State: graph.NewState()}); err == nil {
		t.Fatal("expected an error from a CLI that exited 3 with no result")
	}

	want := []string{"n1|true|", "n1|false|"}
	if len(calls) != 2 || calls[0] != want[0] || calls[1] != want[1] {
		t.Errorf("OnActivity calls = %v, want %v", calls, want)
	}
}

func TestClaudeCodeReportsANonZeroExitWithNoResult(t *testing.T) {
	r := fakeClaude(t, "cat >/dev/null\nexit 3\n")
	_, err := r.Run(context.Background(), graph.AgentRequest{NodeID: "n1", Prompt: "p", State: graph.NewState()})
	if err == nil {
		t.Fatal("expected an error")
	}
	if !strings.Contains(err.Error(), "claude-code") {
		t.Errorf("error = %q, want it to name the adapter", err)
	}
}

// The input side is the half issue 01's capture does not show directly; the shape asserted
// here is the one its README documents as having been piped in to produce that very output.
func TestClaudeCodeWritesTheDocumentedStreamJSONInputOnStdin(t *testing.T) {
	captured := filepath.Join(t.TempDir(), "stdin.json")
	r := fakeClaude(t, "cat > '"+captured+"'\n"+
		`printf '%s\n' '{"type":"result","subtype":"success","is_error":false,"session_id":"s-1","result":"ok"}'`+"\n")

	st := graph.NewState()
	st.Set("topic", "routing")
	if _, err := r.Run(context.Background(), graph.AgentRequest{NodeID: "n1", Prompt: "summarize", State: st}); err != nil {
		t.Fatalf("Run: %v", err)
	}

	raw, err := os.ReadFile(captured)
	if err != nil {
		t.Fatalf("read captured stdin: %v", err)
	}
	var in struct {
		Type    string `json:"type"`
		Message struct {
			Role    string `json:"role"`
			Content []struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"content"`
		} `json:"message"`
	}
	if err := json.Unmarshal(raw, &in); err != nil {
		t.Fatalf("stdin is not the documented stream-json object: %v (%s)", err, raw)
	}
	if in.Type != "user" || in.Message.Role != "user" || len(in.Message.Content) != 1 || in.Message.Content[0].Type != "text" {
		t.Fatalf("stdin = %s, want one user/text block", raw)
	}
	text := in.Message.Content[0].Text
	if !strings.Contains(text, "summarize") {
		t.Errorf("input text = %q, want the node's prompt", text)
	}
	if !strings.Contains(text, "routing") {
		t.Errorf("input text = %q, want the run state folded in", text)
	}
}

func boolText(b bool) string {
	if b {
		return "true"
	}
	return "false"
}
