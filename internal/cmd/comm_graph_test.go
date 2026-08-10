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

// The graph itself is the one piece no unit test on comm_routers.go exercises: that
// every node id a router can return actually exists, and that the two approval gates
// don't collide on node ids. topology.Load's own g.Validate() catches exactly this by
// probing every router with an empty state — a router returning a typo'd node id fails
// here, not at run time.
func TestCommunityManagementAgencyGraphLoadsAndValidates(t *testing.T) {
	reg := builtinRegistry(&agentrunner.Stub{}, config.Config{})
	reg.OnApproval(func(context.Context, string) (graph.Decision, error) {
		return graph.Refused, nil
	})

	data, err := os.ReadFile("../../examples/community-management-agency.yaml")
	if err != nil {
		t.Fatalf("read graph: %v", err)
	}

	if _, err := topology.Load(data, reg); err != nil {
		t.Fatalf("topology.Load: %v", err)
	}
}

func TestCommunityManagementAgencyAutoGraphLoadsAndValidates(t *testing.T) {
	reg := builtinRegistry(&agentrunner.Stub{}, config.Config{})
	reg.OnApproval(func(context.Context, string) (graph.Decision, error) {
		return graph.Refused, nil
	})

	data, err := os.ReadFile("../../examples/community-management-agency-auto.yaml")
	if err != nil {
		t.Fatalf("read graph: %v", err)
	}

	if _, err := topology.Load(data, reg); err != nil {
		t.Fatalf("topology.Load: %v", err)
	}
}
