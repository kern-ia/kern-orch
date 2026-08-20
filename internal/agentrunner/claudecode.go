package agentrunner

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os/exec"

	"github.com/yoann/kern-orch/internal/config"
	"github.com/yoann/kern-orch/internal/graph"
)

// claudeCodeProtocolArgs are the flags that put `claude` in the mode this adapter speaks.
// They are fixed, not configurable: the whole adapter is the translation of *this* protocol,
// so an operator able to drop --output-format would only be able to break it.
//
// --verbose is not decoration. `claude` refuses outright with "When using --print,
// --output-format=stream-json requires --verbose" (verified against 2.1.237), so the three
// documented flags alone do not produce a working invocation.
var claudeCodeProtocolArgs = []string{
	"-p",
	"--input-format=stream-json",
	"--output-format=stream-json",
	"--verbose",
}

// ClaudeCode is the graph.AgentRunner speaking Claude Code's real stream-json CLI protocol.
// One call is one fresh `claude -p` process: the node's request goes in on stdin as a single
// stream-json user message, the CLI's typed messages come back on stdout one JSON object per
// line, and the run's last result message is what becomes the node's output.
type ClaudeCode struct {
	Path      string    // the `claude` binary
	Args      []string  // extra args appended after the protocol flags
	Env       []string  // child environment (nil => inherit parent)
	Stderr    io.Writer // child stderr (nil => discarded)
	TokenSink io.Writer // assistant text, written as each message arrives (nil => discarded)

	// OnActivity, when set, brackets the window during which the model is working on this
	// node: true once the child is running, false when it is done, whatever the outcome.
	//
	// The bracket opens at spawn rather than at the first token on purpose: a turn answered
	// in one piece streams nothing incremental at all, and waiting for a token would report
	// such a node as never having thought.
	//
	// message, on the stop call only, is the node's own state["display:<nodeID>"] output —
	// here, the CLI's final result text. Empty on the start call and on every error path:
	// a run that failed has nothing to narrate.
	//
	// It must not block: it is called on the run's own thread.
	OnActivity func(nodeID string, generating bool, message string)
}

// Start satisfies Lifecycle and does nothing. Claude Code in -p mode owns no resource that
// outlives a node — each Run spawns and reaps its own process — but declaring the pair keeps
// every adapter answering the same question rather than making the call site special-case
// which ones opted in.
func (r *ClaudeCode) Start(context.Context) error { return nil }

// Close satisfies Lifecycle and does nothing. See Start.
func (r *ClaudeCode) Close() error { return nil }

// newClaudeCode is the registry constructor for config.AgentKindClaudeCode.
func newClaudeCode(cfg config.Config, opts Options) (graph.AgentRunner, error) {
	return &ClaudeCode{
		Path:       cfg.AgentCLI,
		Stderr:     opts.Stderr,
		TokenSink:  opts.TokenSink,
		OnActivity: opts.OnActivity,
	}, nil
}

// Run implements graph.AgentRunner by driving one `claude -p` process to completion.
func (r *ClaudeCode) Run(ctx context.Context, req graph.AgentRequest) (graph.AgentResult, error) {
	payload, err := claudeCodeInput(req)
	if err != nil {
		return graph.AgentResult{}, err
	}

	cmd := exec.CommandContext(ctx, r.Path, append(append([]string{}, claudeCodeProtocolArgs...), r.Args...)...)
	cmd.Env = r.Env
	cmd.Stderr = r.Stderr
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return graph.AgentResult{}, fmt.Errorf("agentrunner: claude-code: stdin: %w", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return graph.AgentResult{}, fmt.Errorf("agentrunner: claude-code: stdout: %w", err)
	}
	if err := cmd.Start(); err != nil {
		return graph.AgentResult{}, fmt.Errorf("agentrunner: claude-code: start %q: %w", r.Path, err)
	}

	// Deferred so the bracket closes on every path out of here, including the error ones. A
	// stop that only fired on success would leave a beacon lit on a broken run. result is read
	// by the closure at call time, not captured now, so an error path — which leaves it zero —
	// narrates nothing rather than a stale value.
	var result graph.AgentResult
	r.activity(req.NodeID, true, "")
	defer func() { r.activity(req.NodeID, false, displayMessage(req.NodeID, result)) }()

	// One stream-json user message, then stdin is closed: -p mode reads until EOF, and a
	// stdin left open would hang the child waiting for a turn that never comes.
	if _, err := stdin.Write(payload); err != nil {
		_ = cmd.Wait()
		return graph.AgentResult{}, fmt.Errorf("agentrunner: claude-code: write request: %w", err)
	}
	_ = stdin.Close()

	sink := r.TokenSink
	if sink == nil {
		sink = io.Discard
	}
	result, err = translateClaudeStream(stdout, req.NodeID, sink)
	if waitErr := cmd.Wait(); waitErr != nil && err == nil {
		// A complete, successful stream followed by a non-zero exit is still a failed call.
		// Clearing result keeps the deferred stop from narrating an answer the caller never
		// receives.
		result = graph.AgentResult{}
		return graph.AgentResult{}, fmt.Errorf("agentrunner: claude-code: %q exited: %w", r.Path, waitErr)
	}
	return result, err
}

// activity fires the OnActivity hook when one is set.
func (r *ClaudeCode) activity(nodeID string, generating bool, message string) {
	if r.OnActivity != nil {
		r.OnActivity(nodeID, generating, message)
	}
}

// claudeCodeInput builds the single stream-json object written to the child's stdin.
//
// The shape is the one issue 01 documented as having been piped in to produce the committed
// capture, and matches --input-format=stream-json's contract: a `user` message whose content
// is a list of typed blocks. The protocol carries no field for the run's state, so the state
// travels inside the text block, appended under a heading — the prompt is the only channel
// Claude Code offers, and hiding the state in a side file would make a node's context
// invisible in the CLI's own transcript.
func claudeCodeInput(req graph.AgentRequest) ([]byte, error) {
	text := req.Prompt
	if req.State != nil && len(req.State.Keys()) > 0 {
		stateJSON, err := json.Marshal(req.State)
		if err != nil {
			return nil, fmt.Errorf("agentrunner: claude-code: marshal state: %w", err)
		}
		text += "\n\n" + claudeCodeStateHeading + "\n" + string(stateJSON)
	}
	payload, err := json.Marshal(claudeCodeInputMessage{
		Type: "user",
		Message: claudeCodeUserMessage{
			Role:    "user",
			Content: []claudeCodeTextBlock{{Type: "text", Text: text}},
		},
	})
	if err != nil {
		return nil, fmt.Errorf("agentrunner: claude-code: marshal request: %w", err)
	}
	return append(payload, '\n'), nil
}

const claudeCodeStateHeading = "Current run state (JSON):"

type claudeCodeInputMessage struct {
	Type    string                `json:"type"`
	Message claudeCodeUserMessage `json:"message"`
}

type claudeCodeUserMessage struct {
	Role    string                `json:"role"`
	Content []claudeCodeTextBlock `json:"content"`
}

type claudeCodeTextBlock struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

// claudeCodeMessage is the subset of one stdout line this adapter reads. Claude Code's
// messages carry far more (usage, cost, rate limits, the full tool inventory); decoding only
// what is translated keeps an added upstream field from breaking the parse, and keeps the
// mapping between the wire and the node's output readable in one screen.
type claudeCodeMessage struct {
	Type      string `json:"type"`
	Subtype   string `json:"subtype"`
	SessionID string `json:"session_id"`
	// Message is set on assistant and user messages; its content blocks carry the prose.
	Message struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
	} `json:"message"`
	// IsError and Result are set on the terminal result message only.
	IsError bool   `json:"is_error"`
	Result  string `json:"result"`
}

// translateClaudeStream reads Claude Code's stream-json stdout and produces the node's
// result, forwarding assistant prose to sink as each message arrives.
//
// What maps to what, and why:
//   - assistant messages' `text` content blocks -> sink. They are the model's prose. tool_use
//     blocks and the `user` messages carrying tool_result are deliberately skipped: the sink
//     is the answer stream a caller renders, not a trace of the CLI's internal work.
//   - the result message's `result` -> Output["display:<nodeID>"], the convention this repo
//     already uses for "a node's own output, in plain language", so the final answer is both
//     what a downstream node reads and what OnActivity narrates on stop.
//   - the result message's `session_id` -> Output["claude-code:session:<nodeID>"], the
//     provenance a run needs to be matched against Claude Code's own transcript afterwards.
//     It is the CLI's identifier, carried as-is; nothing here interprets it.
//
// rate_limit_event and system/init messages are read and ignored: they describe the CLI's own
// state, not the node's work.
//
// The last result message wins. A stream with no result at all, or one whose result is
// flagged as an error, is an error naming this adapter.
func translateClaudeStream(stdout io.Reader, nodeID string, sink io.Writer) (graph.AgentResult, error) {
	sc := bufio.NewScanner(stdout)
	// Claude Code's system/init message alone runs past 8 KiB (the tool and slash-command
	// inventories), and a single assistant message can be far larger.
	sc.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)

	var last *claudeCodeMessage
	for sc.Scan() {
		line := sc.Bytes()
		if len(line) == 0 {
			continue
		}
		var msg claudeCodeMessage
		if err := json.Unmarshal(line, &msg); err != nil {
			return graph.AgentResult{}, fmt.Errorf("agentrunner: claude-code: bad stream message %q: %w", line, err)
		}
		switch msg.Type {
		case claudeCodeTypeAssistant:
			for _, block := range msg.Message.Content {
				if block.Type == claudeCodeBlockText {
					_, _ = io.WriteString(sink, block.Text)
				}
			}
		case claudeCodeTypeResult:
			kept := msg
			last = &kept
		}
	}
	if err := sc.Err(); err != nil {
		return graph.AgentResult{}, fmt.Errorf("agentrunner: claude-code: read stdout: %w", err)
	}
	if last == nil {
		return graph.AgentResult{}, fmt.Errorf("agentrunner: claude-code: the CLI produced no result message")
	}
	if last.IsError || (last.Subtype != "" && last.Subtype != claudeCodeSubtypeSuccess) {
		return graph.AgentResult{}, fmt.Errorf("agentrunner: claude-code: run failed (%s): %s", last.Subtype, last.Result)
	}

	output := map[string]any{}
	if last.Result != "" {
		output["display:"+nodeID] = last.Result
	}
	if last.SessionID != "" {
		output["claude-code:session:"+nodeID] = last.SessionID
	}
	return graph.AgentResult{Output: output}, nil
}

const (
	claudeCodeTypeAssistant  = "assistant"
	claudeCodeTypeResult     = "result"
	claudeCodeBlockText      = "text"
	claudeCodeSubtypeSuccess = "success"
)
