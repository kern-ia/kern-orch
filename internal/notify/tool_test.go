package notify

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/yoann/kern-orch/internal/graph"
)

func TestToolSendsTheMessageStateKey(t *testing.T) {
	var gotText string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		gotText = r.FormValue("text")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
	}))
	defer srv.Close()

	fn := Tool(&Client{BaseURL: srv.URL, Token: "x", ChatID: "0"})
	s := graph.NewState()
	s.Set("message", "mission bloquée")

	if err := fn(context.Background(), s); err != nil {
		t.Fatalf("Tool: %v", err)
	}
	if gotText != "mission bloquée" {
		t.Errorf("text sent = %q, want the message key's value", gotText)
	}
}

// A graph author who wires a notify node but forgets the credentials should see that
// immediately, not have every message silently vanish — the same "refuse, don't
// silently degrade" rule this ecosystem already applies to kern-exec's sandboxing.
func TestToolFailsWhenUnconfigured(t *testing.T) {
	fn := Tool(nil)
	s := graph.NewState()
	s.Set("message", "hello")

	if err := fn(context.Background(), s); err == nil {
		t.Fatal("Tool succeeded with no client configured")
	}
}

func TestToolFailsWhenNoMessageKey(t *testing.T) {
	fn := Tool(&Client{BaseURL: "http://unused", Token: "x", ChatID: "0"})
	s := graph.NewState()

	if err := fn(context.Background(), s); err == nil {
		t.Fatal("Tool succeeded with no message key in state")
	}
}

func TestToolFailsWhenMessageIsEmpty(t *testing.T) {
	fn := Tool(&Client{BaseURL: "http://unused", Token: "x", ChatID: "0"})
	s := graph.NewState()
	s.Set("message", "")

	if err := fn(context.Background(), s); err == nil {
		t.Fatal("Tool succeeded with an empty message")
	}
}
