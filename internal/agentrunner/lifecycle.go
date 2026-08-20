package agentrunner

import "context"

// Lifecycle is the optional half of the adapter contract: an adapter owning a resource
// that outlives a single node — a server process, a pooled connection, a session — declares
// it here, and the run's setup/teardown honours it.
//
// It is deliberately NOT folded into graph.AgentRunner. Stub answers from a map and has
// nothing to start or stop; ClaudeCode spawns its child inside Run, which is exactly right
// for a one-shot CLI, and declares a no-op pair only so the call site reads one way for every
// adapter. Widening the port every graph depends on would force that on Stub and on every
// future test double too, to satisfy a need they do not have. Callers type-assert instead:
//
//	if lc, ok := runner.(agentrunner.Lifecycle); ok { ... }
//
// Scope of a Start/Close pair is one run, never one node. That is the whole point: an
// HTTP-backed adapter must serve every node of a graph from the same already-running
// server, not restart one per level.
type Lifecycle interface {
	// Start prepares the adapter for the run about to execute. ctx is the run's own
	// context — cancelled by a stop request — so a Start that waits for readiness aborts
	// with it. A non-nil error aborts the run before any node executes.
	Start(ctx context.Context) error

	// Close releases what Start acquired. It is called exactly once per run that started,
	// on every exit path, and takes no context: teardown must still happen when the run's
	// context is precisely what got cancelled.
	Close() error
}
