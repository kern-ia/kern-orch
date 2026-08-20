package agentrunner

import (
	"fmt"
	"io"

	"github.com/yoann/kern-orch/internal/config"
	"github.com/yoann/kern-orch/internal/graph"
)

// Options carries the wiring an adapter needs but cannot read from config: the caller's
// streams and its activity hook. They are passed as one struct rather than as arguments so
// issues 04 and 05 can add what their adapter needs without re-touching every call site.
type Options struct {
	// Stderr receives the child CLI's own diagnostics; nil discards them.
	Stderr io.Writer
	// TokenSink receives the incremental token stream; nil discards it.
	TokenSink io.Writer
	// OnActivity brackets the window during which the model is working on a node. See
	// ClaudeCode.OnActivity for the contract every adapter honours.
	OnActivity func(nodeID string, generating bool, message string)
}

// constructor builds one adapter. The registry is a map of these rather than a switch so
// adding a CLI is adding an entry, and so the set of supported kinds is one readable list
// instead of a control-flow shape spread over the function.
type constructor func(cfg config.Config, opts Options) (graph.AgentRunner, error)

// adapters maps a config.AgentKind* value to the adapter that speaks that CLI's protocol.
//
// Every kind maps to a real adapter now that issues 04 and 05 have both landed.
var adapters = map[string]constructor{
	config.AgentKindClaudeCode: newClaudeCode,
	config.AgentKindOpenCode:   newOpenCode,
}

// newOpenCode builds the HTTP-backed OpenCode adapter. It also satisfies Lifecycle, which is
// what makes serve.go start its server once for the whole run instead of per node.
func newOpenCode(cfg config.Config, opts Options) (graph.AgentRunner, error) {
	return &OpenCode{
		Path:       cfg.AgentCLI,
		Stderr:     opts.Stderr,
		TokenSink:  opts.TokenSink,
		OnActivity: opts.OnActivity,
	}, nil
}

// New selects the AgentRunner for cfg. No AgentCLI configured is the harness's LLM-less mode
// and yields the deterministic Stub, unchanged from before this registry existed; anything
// else dispatches on AgentKind.
//
// The stub decision is keyed on AgentCLI rather than on AgentKind because AgentCLI is the
// field that says whether an external process exists at all — a Config assembled in Go can
// carry a kind with no path, and that is still the stub's case, not an error.
func New(cfg config.Config, opts Options) (graph.AgentRunner, error) {
	if cfg.AgentCLI == "" {
		return &Stub{}, nil
	}
	build, ok := adapters[cfg.AgentKind]
	if !ok {
		return nil, fmt.Errorf("agentrunner: %s: unknown agent kind %q: want %q or %q",
			config.EnvAgentKind, cfg.AgentKind, config.AgentKindClaudeCode, config.AgentKindOpenCode)
	}
	return build(cfg, opts)
}
