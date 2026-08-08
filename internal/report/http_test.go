package report

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/yoann/kern-orch/internal/graph"
)

func fixedNow() time.Time {
	return time.Date(2026, 7, 26, 12, 0, 0, 0, time.UTC)
}

// sink records what a reporter posted to it.
type sink struct {
	mu       sync.Mutex
	bodies   []map[string]any
	paths    []string
	status   int
	server   *httptest.Server
	requests int
}

func newSink(t *testing.T, status int) *sink {
	t.Helper()
	s := &sink{status: status}
	s.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)

		s.mu.Lock()
		s.bodies = append(s.bodies, body)
		s.paths = append(s.paths, r.URL.Path)
		s.requests++
		s.mu.Unlock()

		w.WriteHeader(s.status)
	}))
	t.Cleanup(s.server.Close)
	return s
}

func (s *sink) last() map[string]any {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.bodies) == 0 {
		return nil
	}
	return s.bodies[len(s.bodies)-1]
}

func (s *sink) count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.requests
}

func TestHookPostsTheStepEvent(t *testing.T) {
	s := newSink(t, http.StatusAccepted)
	r := NewHTTP(s.server.URL + "/api/v1/steps")
	r.now = fixedNow

	state := graph.NewState()
	state.Set("topic", "confinement")

	hook := r.Hook("run-42", "review", nil)
	if err := hook(context.Background(), graph.StepInfo{Step: 2, Frontier: []string{"a", "b"}}, state); err != nil {
		t.Fatalf("hook: %v", err)
	}

	r.Flush()

	body := s.last()
	if body == nil {
		t.Fatal("the sink received nothing")
	}
	if body["run_id"] != "run-42" {
		t.Errorf("run_id = %v, want run-42", body["run_id"])
	}
	if body["graph"] != "review" {
		t.Errorf("graph = %v, want review", body["graph"])
	}
	if body["step"] != float64(2) {
		t.Errorf("step = %v, want 2", body["step"])
	}
	if got, want := body["frontier"], []any{"a", "b"}; !equalAny(got, want) {
		t.Errorf("frontier = %v, want %v", got, want)
	}
	if body["at"] != "2026-07-26T12:00:00Z" {
		t.Errorf("at = %v, want the injected clock", body["at"])
	}
	if s.paths[0] != "/api/v1/steps" {
		t.Errorf("path = %q, want the configured URL untouched", s.paths[0])
	}
}

func TestHookSendsRequesterOnlyOnTheFirstEvent(t *testing.T) {
	s := newSink(t, http.StatusAccepted)
	r := NewHTTP(s.server.URL + "/api/v1/steps")
	r.now = fixedNow
	r.Requester = "yoann"

	hook := r.Hook("run-42", "review", nil)
	_ = hook(context.Background(), graph.StepInfo{Step: 1, Frontier: []string{"a"}}, graph.NewState())
	_ = hook(context.Background(), graph.StepInfo{Step: 2}, graph.NewState())
	r.Flush()

	s.mu.Lock()
	bodies := s.bodies
	s.mu.Unlock()
	if len(bodies) != 2 {
		t.Fatalf("got %d events, want 2", len(bodies))
	}
	if bodies[0]["requester"] != "yoann" {
		t.Errorf("requester on first event = %v, want yoann", bodies[0]["requester"])
	}
	if bodies[1]["requester"] != nil {
		t.Errorf("requester on second event = %v, want absent", bodies[1]["requester"])
	}
}

func TestHookSendsDossierOnlyOnTheFirstEvent(t *testing.T) {
	s := newSink(t, http.StatusAccepted)
	r := NewHTTP(s.server.URL + "/api/v1/steps")
	r.now = fixedNow
	r.Dossier = "AF-2288"

	hook := r.Hook("run-42", "review", nil)
	_ = hook(context.Background(), graph.StepInfo{Step: 1, Frontier: []string{"a"}}, graph.NewState())
	_ = hook(context.Background(), graph.StepInfo{Step: 2}, graph.NewState())
	r.Flush()

	s.mu.Lock()
	bodies := s.bodies
	s.mu.Unlock()
	if len(bodies) != 2 {
		t.Fatalf("got %d events, want 2", len(bodies))
	}
	if bodies[0]["dossier"] != "AF-2288" {
		t.Errorf("dossier on first event = %v, want AF-2288", bodies[0]["dossier"])
	}
	if bodies[1]["dossier"] != nil {
		t.Errorf("dossier on second event = %v, want absent", bodies[1]["dossier"])
	}
}

func TestHookReportsAnEmptyFrontierSoTheSinkCanCloseTheRun(t *testing.T) {
	s := newSink(t, http.StatusAccepted)
	r := NewHTTP(s.server.URL)
	r.now = fixedNow

	if err := r.Hook("run-42", "review", nil)(context.Background(), graph.StepInfo{Step: 3}, graph.NewState()); err != nil {
		t.Fatalf("hook: %v", err)
	}

	r.Flush()

	frontier, ok := s.last()["frontier"].([]any)
	if !ok || len(frontier) != 0 {
		t.Errorf("frontier = %v, want an empty list", s.last()["frontier"])
	}
}

func TestHookCarriesTheState(t *testing.T) {
	s := newSink(t, http.StatusAccepted)
	r := NewHTTP(s.server.URL)
	r.now = fixedNow

	state := graph.NewState()
	state.Set("topic", "confinement")

	_ = r.Hook("run-42", "review", nil)(context.Background(), graph.StepInfo{Step: 1, Frontier: []string{"a"}}, state)

	r.Flush()

	got, ok := s.last()["state"].(map[string]any)
	if !ok || got["topic"] != "confinement" {
		t.Errorf("state = %v, want the merged state", s.last()["state"])
	}
}

// A reporter is observability, never a dependency of the run: whatever the sink does, the
// graph must keep going.
func TestHookNeverFailsTheRun(t *testing.T) {
	cases := map[string]string{
		"sink returns 500":    "",
		"sink unreachable":    "http://127.0.0.1:1/steps",
		"url is nonsense":     "://not-a-url",
		"host does not exist": "http://sink.invalid/steps",
	}

	for name, url := range cases {
		t.Run(name, func(t *testing.T) {
			if url == "" {
				url = newSink(t, http.StatusInternalServerError).server.URL
			}
			r := NewHTTP(url)
			r.now = fixedNow
			r.Timeout = 200 * time.Millisecond

			err := r.Hook("run-42", "review", nil)(context.Background(), graph.StepInfo{Step: 1}, graph.NewState())
			if err != nil {
				t.Errorf("hook returned %v, want nil — a reporter must never abort a run", err)
			}
		})
	}
}

func TestHookIsNilWhenNoURLIsConfigured(t *testing.T) {
	if hook := NewHTTP("").Hook("run-42", "review", nil); hook != nil {
		t.Error("Hook() != nil for an unconfigured reporter, want nil so the caller can skip it")
	}
}

func TestHookStopsPostingOnceTheRunContextIsCancelled(t *testing.T) {
	s := newSink(t, http.StatusAccepted)
	r := NewHTTP(s.server.URL)
	r.now = fixedNow

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if err := r.Hook("run-42", "review", nil)(ctx, graph.StepInfo{Step: 1}, graph.NewState()); err != nil {
		t.Errorf("hook: %v, want nil", err)
	}
	r.Flush()

	if s.count() != 0 {
		t.Errorf("the sink received %d requests, want 0 on a cancelled context", s.count())
	}
}

func equalAny(got, want any) bool {
	g, ok := got.([]any)
	if !ok {
		return false
	}
	w := want.([]any)
	if len(g) != len(w) {
		return false
	}
	for i := range g {
		if g[i] != w[i] {
			return false
		}
	}
	return true
}

// newSlowSink holds every request open until release is closed.
func newSlowSink(t *testing.T, release <-chan struct{}) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		<-release
		w.WriteHeader(http.StatusAccepted)
	}))
	t.Cleanup(srv.Close)
	return srv
}

// all returns every body the sink received, in arrival order.
func (s *sink) all() []map[string]any {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]map[string]any(nil), s.bodies...)
}
