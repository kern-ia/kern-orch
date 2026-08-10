package config

import "testing"

func TestFromEnvReadsTelegramCredentials(t *testing.T) {
	t.Setenv(EnvTelegramBotToken, "un-jeton")
	t.Setenv(EnvTelegramChatID, "42")

	cfg := FromEnv()

	if cfg.TelegramBotToken != "un-jeton" {
		t.Errorf("TelegramBotToken = %q, want un-jeton", cfg.TelegramBotToken)
	}
	if cfg.TelegramChatID != "42" {
		t.Errorf("TelegramChatID = %q, want 42", cfg.TelegramChatID)
	}
}
