package report

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/yoann/kern-orch/internal/graph"
)

func stepState(t *testing.T) *graph.State {
	t.Helper()
	s := graph.NewState()
	s.Set("echo", "...")
	return s
}

// The point of the queue. A graph must not run slower because something is watching it,
// and before this the engine waited on an HTTP round trip between every level.
func TestTheHookDoesNotWaitOnASlowSink(t *testing.T) {
	blocked := make(chan struct{})
	srv := newSlowSink(t, blocked)
	r := NewHTTP(srv.URL)
	r.now = fixedNow
	defer func() { close(blocked); r.Flush() }()

	hook := r.Hook("r1", "g", nil)

	done := make(chan struct{})
	go func() {
		_ = hook(context.Background(), graph.StepInfo{Step: 1, Frontier: []string{"a"}}, stepState(t))
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("the step hook blocked the engine on the sink")
	}
}

// Order is load-bearing here, unlike for activity: a sink folds steps in sequence and
// rejects a level older than the one it holds. Reporting off-thread must therefore keep a
// single queue, never a goroutine per event.
func TestStepsArriveInOrder(t *testing.T) {
	s := newSink(t, http.StatusAccepted)
	r := NewHTTP(s.server.URL)
	r.now = fixedNow

	hook := r.Hook("r1", "g", nil)
	for step := 1; step <= 20; step++ {
		_ = hook(context.Background(), graph.StepInfo{Step: step, Frontier: []string{"a"}}, stepState(t))
	}
	r.Flush()

	bodies := s.all()
	if len(bodies) != 20 {
		t.Fatalf("the sink received %d steps, want 20", len(bodies))
	}
	for i, body := range bodies {
		if got, want := body["step"], float64(i+1); got != want {
			t.Fatalf("step %d arrived in position %d — the queue reordered the run", int(got.(float64)), i+1)
		}
	}
}

// A failure is the last word about a run, and it must not overtake the levels that led to
// it: a sink folding them the other way round would show a failed run coming back to life.
func TestAFailureArrivesAfterTheStepsBeforeIt(t *testing.T) {
	s := newSink(t, http.StatusAccepted)
	r := NewHTTP(s.server.URL)
	r.now = fixedNow

	hook := r.Hook("r1", "g", nil)
	_ = hook(context.Background(), graph.StepInfo{Step: 1, Frontier: []string{"a"}}, stepState(t))
	_ = hook(context.Background(), graph.StepInfo{Step: 2, Frontier: []string{"b"}}, stepState(t))
	r.ReportFailure(context.Background(), "r1", "g", 2, []string{"b"}, []string{"b"}, "boom")
	r.Flush()

	bodies := s.all()
	if len(bodies) != 3 {
		t.Fatalf("the sink received %d events, want 3", len(bodies))
	}
	if _, isFailure := bodies[2]["error"]; !isFailure {
		t.Errorf("the last event is %v, want the failure", bodies[2])
	}
}

// Fire-and-forget must not become fire-and-lose: the command flushes before it exits, or
// the last level of a run dies with the process.
func TestFlushWaitsForEveryStep(t *testing.T) {
	s := newSink(t, http.StatusAccepted)
	r := NewHTTP(s.server.URL)
	r.now = fixedNow

	hook := r.Hook("r1", "g", nil)
	for step := 1; step <= 10; step++ {
		_ = hook(context.Background(), graph.StepInfo{Step: step, Frontier: []string{"a"}}, stepState(t))
	}
	r.Flush()

	if got := s.count(); got != 10 {
		t.Errorf("the sink received %d steps, want 10 — Flush returned too early", got)
	}
}

// A sink so slow that the queue fills must cost the run nothing. Dropping a level is safe
// where blocking is not: every event carries the full state, so the next one supersedes
// what was lost. This mirrors what the interface already does for slow browsers.
func TestAFullQueueDropsRatherThanBlocks(t *testing.T) {
	blocked := make(chan struct{})
	srv := newSlowSink(t, blocked)
	r := NewHTTP(srv.URL)
	r.now = fixedNow
	defer func() { close(blocked); r.Flush() }()

	hook := r.Hook("r1", "g", nil)

	done := make(chan struct{})
	go func() {
		for step := 1; step <= stepQueueSize*3; step++ {
			_ = hook(context.Background(), graph.StepInfo{Step: step, Frontier: []string{"a"}}, stepState(t))
		}
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("the engine was blocked by a queue nobody was draining")
	}
}

// Flush on a reporter that was never used, or has no sink, must be a harmless no-op rather
// than a wait on a worker that does not exist.
func TestFlushIsSafeWithoutASink(t *testing.T) {
	NewHTTP("").Flush()
	NewHTTP("http://sink.invalid/steps").Flush()
}

// Reporting must not hold up the exit either. Moving delivery off the engine's thread only
// relocates the wait if the command then blocks for ever on a sink that never answers.
func TestFlushGivesUpOnASinkThatNeverAnswers(t *testing.T) {
	blocked := make(chan struct{})
	defer close(blocked)
	srv := newSlowSink(t, blocked)

	r := NewHTTP(srv.URL)
	r.now = fixedNow
	r.FlushTimeout = 150 * time.Millisecond

	hook := r.Hook("r1", "g", nil)
	for step := 1; step <= 5; step++ {
		_ = hook(context.Background(), graph.StepInfo{Step: step, Frontier: []string{"a"}}, stepState(t))
	}

	start := time.Now()
	r.Flush()

	if waited := time.Since(start); waited > time.Second {
		t.Errorf("Flush waited %s on a dead sink; the command would hang on exit", waited)
	}
}

// The same for activity, which flushes on the same exit path.
func TestActivityFlushGivesUpOnASinkThatNeverAnswers(t *testing.T) {
	blocked := make(chan struct{})
	defer close(blocked)
	srv := newSlowSink(t, blocked)

	r := NewActivityReporter(srv.URL)
	r.now = fixedNow
	r.FlushTimeout = 150 * time.Millisecond

	r.Report(context.Background(), "r1", "g", "n", true)

	start := time.Now()
	r.Flush()

	if waited := time.Since(start); waited > time.Second {
		t.Errorf("Flush waited %s on a dead sink", waited)
	}
}

// A protected sink refuses an anonymous producer. The token is configured once and travels
// on every contract this brick emits — steps, catalogue and activity alike.
func TestEveryReportCarriesTheConfiguredToken(t *testing.T) {
	var seen []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = append(seen, r.Header.Get("Authorization"))
		w.WriteHeader(http.StatusAccepted)
	}))
	t.Cleanup(srv.Close)

	steps := NewHTTP(srv.URL)
	steps.Token = "un-secret"
	steps.now = fixedNow
	_ = steps.Hook("r1", "g", nil)(context.Background(),
		graph.StepInfo{Step: 1, Frontier: []string{"a"}}, stepState(t))
	steps.Flush()

	activity := NewActivityReporter(srv.URL)
	activity.Token = "un-secret"
	activity.now = fixedNow
	activity.Report(context.Background(), "r1", "g", "n", true)
	activity.Flush()

	registry := NewRegistryPublisher(srv.URL)
	registry.Token = "un-secret"
	registry.now = fixedNow
	_ = registry.Publish(context.Background(), nil)

	if len(seen) != 3 {
		t.Fatalf("the sink received %d requests, want one per contract", len(seen))
	}
	for i, header := range seen {
		if header != "Bearer un-secret" {
			t.Errorf("request %d sent %q, want the bearer token", i+1, header)
		}
	}
}

// No token configured means no header, not an empty one: an empty bearer is a credential
// that says "I tried", and a sink should see nothing rather than something malformed.
func TestNoTokenMeansNoHeader(t *testing.T) {
	var header string
	var seen bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		header, seen = r.Header.Get("Authorization"), true
		w.WriteHeader(http.StatusAccepted)
	}))
	t.Cleanup(srv.Close)

	r := NewHTTP(srv.URL)
	r.now = fixedNow
	_ = r.Hook("r1", "g", nil)(context.Background(),
		graph.StepInfo{Step: 1, Frontier: []string{"a"}}, stepState(t))
	r.Flush()

	if !seen {
		t.Fatal("nothing reached the sink")
	}
	if header != "" {
		t.Errorf("Authorization = %q, want the header absent", header)
	}
}
