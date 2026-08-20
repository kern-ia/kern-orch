// Package agentrunner provides the implementations of the graph.AgentRunner port: a
// deterministic Stub (so the harness runs with no LLM configured) and one adapter per
// supported agent CLI, selected by config.AgentKind through New.
//
// Each adapter owns its translation privately. There is deliberately no shared intermediate
// event type between them: the CLIs speak genuinely different protocols, and the surface they
// must all reach — graph.AgentResult, the TokenSink io.Writer, the OnActivity bracket — is
// already narrow and CLI-agnostic. What lives here is only what every adapter honours
// identically, so that two adapters cannot drift on the same contract.
package agentrunner

import "github.com/yoann/kern-orch/internal/graph"

// displayMessage reads the node's own state["display:<nodeID>"] output, if it set one — the
// convention this repo already uses for "a node's output, in plain language", and what an
// adapter narrates on the stop half of its OnActivity bracket. Not every node opts in, and an
// empty result is a normal, silent outcome, not an error.
func displayMessage(nodeID string, result graph.AgentResult) string {
	v, ok := result.Output["display:"+nodeID]
	if !ok {
		return ""
	}
	s, _ := v.(string)
	return s
}
