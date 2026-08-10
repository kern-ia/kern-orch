package notify

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"testing"
)

func TestSendPostsTheChatAndTheText(t *testing.T) {
	var gotPath string
	var gotBody url.Values
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		_ = r.ParseForm()
		gotBody = r.Form
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
	}))
	defer srv.Close()

	c := &Client{BaseURL: srv.URL, Token: "un-jeton", ChatID: "42"}
	if err := c.Send(context.Background(), "bonjour"); err != nil {
		t.Fatalf("Send: %v", err)
	}

	if gotPath != "/botun-jeton/sendMessage" {
		t.Errorf("path = %q, want the bot's own sendMessage endpoint", gotPath)
	}
	if gotBody.Get("chat_id") != "42" {
		t.Errorf("chat_id = %q, want 42", gotBody.Get("chat_id"))
	}
	if gotBody.Get("text") != "bonjour" {
		t.Errorf("text = %q, want bonjour", gotBody.Get("text"))
	}
}

// Telegram answers 200 with {"ok": false, ...} on a rejected request — the HTTP status
// alone does not say whether the message went anywhere.
func TestSendFailsWhenTelegramRefusesTheRequest(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": false, "description": "chat not found"})
	}))
	defer srv.Close()

	c := &Client{BaseURL: srv.URL, Token: "x", ChatID: "0"}
	if err := c.Send(context.Background(), "bonjour"); err == nil {
		t.Fatal("Send succeeded against a refusal Telegram itself reported")
	}
}

func TestSendFailsOnAnHTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	c := &Client{BaseURL: srv.URL, Token: "x", ChatID: "0"}
	if err := c.Send(context.Background(), "bonjour"); err == nil {
		t.Fatal("Send succeeded against a 500")
	}
}

// The one test this package cannot fully trust without it: a real message reaching a
// real Telegram chat. Skipped unless the environment provides real credentials.
func TestSendReachesTheRealTelegramAPI(t *testing.T) {
	token := os.Getenv("TELEGRAM_BOT_TOKEN")
	chatID := os.Getenv("TELEGRAM_CHAT_ID")
	if token == "" || chatID == "" {
		t.Skip("set TELEGRAM_BOT_TOKEN and TELEGRAM_CHAT_ID to run this against the real API")
	}

	c := New(token, chatID)
	if err := c.Send(context.Background(), "kern-orch : test d'intégration réel (go test)."); err != nil {
		t.Fatalf("Send against the real Telegram API: %v", err)
	}
}
