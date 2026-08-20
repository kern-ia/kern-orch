package cmd

import (
	"strings"
	"testing"

	"github.com/yoann/kern-orch/internal/agentrunner"
	"github.com/yoann/kern-orch/internal/config"
)

func TestNewRunnerFallsBackToTheStubWithNoAgentCLI(t *testing.T) {
	r, err := newRunner(config.Config{}, &activityRelay{})
	if err != nil {
		t.Fatalf("newRunner: %v", err)
	}
	if _, ok := r.(*agentrunner.Stub); !ok {
		t.Fatalf("newRunner returned %T, want *agentrunner.Stub", r)
	}
}

// The registry's error has to reach the caller, not be swallowed into a stub: newRunner is
// the only place between config validation and a live run where a misconfigured adapter can
// still be caught before any node executes.
func TestNewRunnerPropagatesTheRegistryError(t *testing.T) {
	_, err := newRunner(config.Config{AgentCLI: "/usr/local/bin/claude", AgentKind: "bogus"}, &activityRelay{})
	if err == nil {
		t.Fatalf("newRunner: got nil error, want the registry's")
	}
	if !strings.Contains(err.Error(), "bogus") {
		t.Fatalf("newRunner error = %q, want it to quote the offending kind", err.Error())
	}
}
