package cmd

import (
	"context"
	"os"
	"testing"

	"github.com/yoann/kern-orch/internal/agentrunner"
	"github.com/yoann/kern-orch/internal/config"
	"github.com/yoann/kern-orch/internal/graph"
	"github.com/yoann/kern-orch/internal/topology"
)

// Same rationale as TestCommunityManagementAgencyGraphLoadsAndValidates: topology.Load's
// own g.Validate() probes onExtractionDecision with an empty state, catching a typo'd
// target node id (extraction_validee/extraction_a_corriger) here rather than at run time.
func TestCourtageExtractionGraphLoadsAndValidates(t *testing.T) {
	reg := builtinRegistry(&agentrunner.Stub{}, config.Config{})
	reg.OnApproval(func(context.Context, string) (graph.Decision, error) {
		return graph.Refused, nil
	})

	data, err := os.ReadFile("../../examples/courtage-extraction.yaml")
	if err != nil {
		t.Fatalf("read graph: %v", err)
	}

	if _, err := topology.Load(data, reg); err != nil {
		t.Fatalf("topology.Load: %v", err)
	}
}
