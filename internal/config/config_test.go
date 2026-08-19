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
