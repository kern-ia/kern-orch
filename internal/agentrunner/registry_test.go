package agentrunner

import (
	"strings"
	"testing"

	"github.com/yoann/kern-orch/internal/config"
)

func TestNewReturnsTheStubWhenNoAgentCLIIsConfigured(t *testing.T) {
	r, err := New(config.Config{}, Options{})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if _, ok := r.(*Stub); !ok {
		t.Fatalf("New returned %T, want *Stub when no agent CLI is configured", r)
	}
}

// The kind is only meaningful alongside a CLI path; config.FromEnv already refuses a kind
// without one, but the registry is reachable from a Config built in Go (tests, embedders) so
// it re-derives the stub decision from the CLI path alone rather than trusting the kind.
func TestNewReturnsTheStubWhenOnlyTheAgentKindIsConfigured(t *testing.T) {
	r, err := New(config.Config{AgentKind: config.AgentKindClaudeCode}, Options{})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if _, ok := r.(*Stub); !ok {
		t.Fatalf("New returned %T, want *Stub when no agent CLI is configured", r)
	}
}

// Issues 04 and 05 replace these two branches with real adapters. Until then the registry
// must fail loud and name the adapter that is missing — the one behaviour that proves the
// dispatch is wired at all, and the one that must never silently degrade to the stub: a run
// that quietly answers with canned stub output looks like it worked.
func TestNewReportsTheClaudeCodeAdapterAsNotYetImplemented(t *testing.T) {
	_, err := New(config.Config{AgentCLI: "/usr/local/bin/claude", AgentKind: config.AgentKindClaudeCode}, Options{})
	if err == nil {
		t.Fatalf("New: got nil error, want one naming the %s adapter", config.AgentKindClaudeCode)
	}
	if !strings.Contains(err.Error(), config.AgentKindClaudeCode) {
		t.Fatalf("New error = %q, want it to name %s", err.Error(), config.AgentKindClaudeCode)
	}
}

func TestNewReportsTheOpenCodeAdapterAsNotYetImplemented(t *testing.T) {
	_, err := New(config.Config{AgentCLI: "/usr/local/bin/opencode", AgentKind: config.AgentKindOpenCode}, Options{})
	if err == nil {
		t.Fatalf("New: got nil error, want one naming the %s adapter", config.AgentKindOpenCode)
	}
	if !strings.Contains(err.Error(), config.AgentKindOpenCode) {
		t.Fatalf("New error = %q, want it to name %s", err.Error(), config.AgentKindOpenCode)
	}
}

// A Config assembled in Go bypasses FromEnv's validation entirely, so the registry states the
// same rule at its own boundary rather than assuming every caller came through the env.
func TestNewRejectsAnUnknownAgentKind(t *testing.T) {
	_, err := New(config.Config{AgentCLI: "/usr/local/bin/claude", AgentKind: "bogus"}, Options{})
	if err == nil {
		t.Fatalf("New: got nil error, want one quoting the unknown kind")
	}
	if !strings.Contains(err.Error(), "bogus") {
		t.Fatalf("New error = %q, want it to quote the offending value", err.Error())
	}
}
