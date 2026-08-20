package agentrunner

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/yoann/kern-orch/internal/graph"
)

// The OpenCode wire types below are decoded from issue 01's real capture
// (testdata/opencode-message.json), not from the OpenAPI document. Every field named here
// was observed in a response body of a running server; anything the server sends that this
// harness has no use for is deliberately absent, so a shape change in an unused corner of
// the API cannot break a run.
//
// They are private to this adapter on purpose (epic 2, decision 05): OpenCode's
// { info, parts } transcript has no structural overlap with Claude Code's stream-json
// envelope, and forcing a shared vocabulary onto the two would invent a third protocol
// neither CLI speaks.

// transcriptMessage is one element of the GET /session/:id/message array: a message's
// metadata plus its ordered parts.
type transcriptMessage struct {
	Info  messageInfo   `json:"info"`
	Parts []messagePart `json:"parts"`
}

// messageInfo is the message envelope. Finish and Error are only ever set on assistant
// messages.
type messageInfo struct {
	ID        string        `json:"id"`
	SessionID string        `json:"sessionID"`
	Role      string        `json:"role"`
	Finish    string        `json:"finish"`
	Error     *messageError `json:"error"`
}

// messageError is the common shape of every error variant the API documents
// (ProviderAuthError, APIError, ContextOverflowError, …): they differ in their extra data
// fields but all carry a name and a human-readable message, which is all a failing run needs.
type messageError struct {
	Name string `json:"name"`
	Data struct {
		Message string `json:"message"`
	} `json:"data"`
}

// messagePart is one part of a message. The parts of one assistant turn interleave
// step-start/reasoning/tool/text/step-finish; only text and tool carry anything a caller of
// this harness can act on.
type messagePart struct {
	Type string `json:"type"`
	Text string `json:"text"`
	// Tool and Name are the same field under two names. The captured server (opencode
	// 1.18.19) puts the tool's name in "tool"; the OpenAPI document that same server serves
	// calls it "name" in SessionMessageAssistantTool. Reading both means the adapter follows
	// whichever the server actually sends rather than betting on the document being current.
	Tool string `json:"tool"`
	Name string `json:"name"`
}

const (
	roleAssistant = "assistant"
	partTypeText  = "text"
	partTypeTool  = "tool"
)

// decodeTranscript parses a GET /session/:id/message response body.
func decodeTranscript(body []byte) ([]transcriptMessage, error) {
	var msgs []transcriptMessage
	if err := json.Unmarshal(body, &msgs); err != nil {
		return nil, fmt.Errorf("agentrunner: opencode: decode transcript: %w", err)
	}
	return msgs, nil
}

// translateTranscript turns a session transcript into the node's AgentResult.
//
// Every key is suffixed with the node's own ID rather than being a flat name. Two agent
// nodes on the same graph level write into one shared state, so a bare "opencode:session"
// would have each fan-out branch silently overwrite its sibling's provenance — the exact
// failure issue 06 goes on to exercise concurrently. "display:<nodeID>" already worked this
// way, and the other three follow it rather than inventing a second rule.
//
// What is kept, and why each earns its place in the state:
//
//   - display:<nodeID> — the assistant's text parts, joined. This is the answer, and it goes
//     under the display convention rather than an invented domain key because the adapter has
//     no idea what the node was asked to produce; the display key is the one name a consumer
//     and OnActivity already read (see Subprocess.OnActivity).
//   - opencode:session:<nodeID> — the server-side session ID. A run that produced a wrong
//     answer is diagnosed against the server's own transcript, and that ID is the only handle
//     to it; it is not reconstructable after the fact.
//   - opencode:tools:<nodeID> — the tools the model actually ran, in call order. The whole
//     reason issue 01 captured a transcript instead of the bare POST body was to keep the
//     tool-call shape; dropping it here would throw away what the fixture exists to prove.
//   - opencode:finish:<nodeID> — the last assistant message's finish reason, which is how a
//     truncated answer ("length") is told apart from a complete one ("stop").
//
// Deliberately dropped: reasoning parts (not output — they are the model thinking aloud, and
// persisting them into a checkpointed state would carry them into every downstream prompt),
// and cost/token counts (real, but this harness emits contracts for sinks that ask for them,
// and no sink asks).
func translateTranscript(nodeID string, msgs []transcriptMessage) (graph.AgentResult, error) {
	var (
		texts     []string
		tools     []string
		sessionID string
		finish    string
	)
	for _, m := range msgs {
		if m.Info.Role != roleAssistant {
			continue
		}
		if e := m.Info.Error; e != nil {
			return graph.AgentResult{}, fmt.Errorf("agentrunner: opencode: %s: %s", e.Name, e.Data.Message)
		}
		sessionID = m.Info.SessionID
		finish = m.Info.Finish
		for _, p := range m.Parts {
			switch p.Type {
			case partTypeText:
				texts = append(texts, p.Text)
			case partTypeTool:
				tools = append(tools, toolName(p))
			}
		}
	}

	// An assistant turn whose parts carry no text at all is not a silent success: the node
	// was asked a question and the state would gain nothing to route on. Reported here rather
	// than downstream, where the missing key reads as "some other node forgot to set it".
	if len(texts) == 0 {
		return graph.AgentResult{}, fmt.Errorf(
			"agentrunner: opencode: session produced no assistant text (finish %q)", finish)
	}

	// Joined with a blank line: OpenCode emits one text part per model step, so a multi-step
	// answer is several paragraphs, not one run-on sentence.
	out := map[string]any{
		"display:" + nodeID:          strings.Join(texts, "\n\n"),
		"opencode:session:" + nodeID: sessionID,
		"opencode:finish:" + nodeID:  finish,
	}
	if tools != nil {
		out["opencode:tools:"+nodeID] = tools
	}
	return graph.AgentResult{Output: out}, nil
}

// toolName reads whichever of the two spellings the server used; see messagePart.Tool.
func toolName(p messagePart) string {
	if p.Tool != "" {
		return p.Tool
	}
	return p.Name
}
