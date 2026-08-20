package agentrunner

import (
	"context"
	"testing"

	"github.com/yoann/kern-orch/internal/graph"
)

// startCloseRunner is the shape an adapter with a long-lived process takes: it satisfies
// graph.AgentRunner and, in addition, Lifecycle.
type startCloseRunner struct{}

func (startCloseRunner) Run(context.Context, graph.AgentRequest) (graph.AgentResult, error) {
	return graph.AgentResult{}, nil
}
func (startCloseRunner) Start(context.Context) error { return nil }
func (startCloseRunner) Close() error                { return nil }

func TestLifecycleIsOptionalSoAnAdapterWithNothingToStartStaysUnchanged(t *testing.T) {
	var stub graph.AgentRunner = &Stub{}
	if _, ok := stub.(Lifecycle); ok {
		t.Fatal("Stub satisfies Lifecycle: the interface has leaked into the port every graph depends on")
	}
	var withLifecycle graph.AgentRunner = startCloseRunner{}
	if _, ok := withLifecycle.(Lifecycle); !ok {
		t.Fatal("an adapter declaring Start/Close does not satisfy Lifecycle")
	}
}
