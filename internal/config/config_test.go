package config

import (
	"strings"
	"testing"
)

func TestFromEnvReadsTelegramCredentials(t *testing.T) {
	t.Setenv(EnvTelegramBotToken, "un-jeton")
	t.Setenv(EnvTelegramChatID, "42")

	cfg, err := FromEnv()
	if err != nil {
		t.Fatalf("FromEnv: %v", err)
	}

	if cfg.TelegramBotToken != "un-jeton" {
		t.Errorf("TelegramBotToken = %q, want un-jeton", cfg.TelegramBotToken)
	}
	if cfg.TelegramChatID != "42" {
		t.Errorf("TelegramChatID = %q, want 42", cfg.TelegramChatID)
	}
}

// The runtime equivalence check must default to off: it is documented as an opt-in
// diagnostic that doubles a run's per-level state work, so an operator who never heard of it
// must never pay for it by accident.
func TestFromEnvDefaultsRuntimeEquivalenceCheckToOff(t *testing.T) {
	cfg, err := FromEnv()
	if err != nil {
		t.Fatalf("FromEnv: %v", err)
	}
	if cfg.RuntimeEquivalenceCheck {
		t.Fatalf("RuntimeEquivalenceCheck = true, want false when %s is unset", EnvRuntimeEquivalenceCheck)
	}
}

func TestFromEnvEnablesRuntimeEquivalenceCheckWhenSetTrue(t *testing.T) {
	t.Setenv(EnvRuntimeEquivalenceCheck, "true")

	cfg, err := FromEnv()
	if err != nil {
		t.Fatalf("FromEnv: %v", err)
	}
	if !cfg.RuntimeEquivalenceCheck {
		t.Fatalf("RuntimeEquivalenceCheck = false, want true when %s=true", EnvRuntimeEquivalenceCheck)
	}
}

// An invalid value must fail loud at load — never a silent fall-back to the off default,
// which would hide a typo as "check disabled" instead of surfacing it.
func TestFromEnvRejectsAnInvalidRuntimeEquivalenceCheckValue(t *testing.T) {
	t.Setenv(EnvRuntimeEquivalenceCheck, "sometimes")

	_, err := FromEnv()
	if err == nil {
		t.Fatalf("FromEnv: got nil error, want one naming %s", EnvRuntimeEquivalenceCheck)
	}
	if !strings.Contains(err.Error(), EnvRuntimeEquivalenceCheck) {
		t.Fatalf("FromEnv error = %q, want it to name %s", err.Error(), EnvRuntimeEquivalenceCheck)
	}
}

// A configured CLI with no kind is the misconfiguration this validation exists for: before
// KERN_AGENT_KIND existed, KERN_AGENT_CLI alone meant "the one JSON-lines protocol", so an
// operator upgrading from that world must be told which second variable is now required
// rather than silently getting a runner that speaks the wrong protocol to their binary.
func TestFromEnvRejectsAnAgentCLIWithNoAgentKind(t *testing.T) {
	t.Setenv(EnvAgentCLI, "/usr/local/bin/claude")
	t.Setenv(EnvAgentKind, "")

	_, err := FromEnv()
	if err == nil {
		t.Fatalf("FromEnv: got nil error, want one naming %s and %s", EnvAgentCLI, EnvAgentKind)
	}
	if !strings.Contains(err.Error(), EnvAgentCLI) || !strings.Contains(err.Error(), EnvAgentKind) {
		t.Fatalf("FromEnv error = %q, want it to name both %s and %s", err.Error(), EnvAgentCLI, EnvAgentKind)
	}
}

func TestFromEnvRejectsAnUnrecognizedAgentKind(t *testing.T) {
	t.Setenv(EnvAgentCLI, "/usr/local/bin/claude")
	t.Setenv(EnvAgentKind, "bogus")

	_, err := FromEnv()
	if err == nil {
		t.Fatalf("FromEnv: got nil error, want one quoting the invalid value")
	}
	if !strings.Contains(err.Error(), "bogus") {
		t.Fatalf("FromEnv error = %q, want it to quote the offending value", err.Error())
	}
}

func TestFromEnvAcceptsTheClaudeCodeAgentKind(t *testing.T) {
	t.Setenv(EnvAgentCLI, "/usr/local/bin/claude")
	t.Setenv(EnvAgentKind, AgentKindClaudeCode)

	cfg, err := FromEnv()
	if err != nil {
		t.Fatalf("FromEnv: %v", err)
	}
	if cfg.AgentKind != AgentKindClaudeCode {
		t.Fatalf("AgentKind = %q, want %q", cfg.AgentKind, AgentKindClaudeCode)
	}
}

func TestFromEnvAcceptsTheOpenCodeAgentKind(t *testing.T) {
	t.Setenv(EnvAgentCLI, "/usr/local/bin/opencode")
	t.Setenv(EnvAgentKind, AgentKindOpenCode)

	cfg, err := FromEnv()
	if err != nil {
		t.Fatalf("FromEnv: %v", err)
	}
	if cfg.AgentKind != AgentKindOpenCode {
		t.Fatalf("AgentKind = %q, want %q", cfg.AgentKind, AgentKindOpenCode)
	}
}

// No CLI configured is the stub path, and it predates this validation entirely: a kind is
// meaningless with no binary to speak to, so an unset KERN_AGENT_KIND must not be able to
// fail a run that never spawns anything.
func TestFromEnvIgnoresTheAgentKindWhenNoAgentCLIIsSet(t *testing.T) {
	t.Setenv(EnvAgentCLI, "")
	t.Setenv(EnvAgentKind, "")

	cfg, err := FromEnv()
	if err != nil {
		t.Fatalf("FromEnv: %v", err)
	}
	if cfg.AgentKind != "" {
		t.Fatalf("AgentKind = %q, want empty when %s is unset", cfg.AgentKind, EnvAgentCLI)
	}
}
