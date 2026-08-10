package steer

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/yoann/kern-orch/internal/graph"
)

func TestDrainNudgesAppliesQueuedValuesInOrder(t *testing.T) {
	m := NewMailbox(nil)
	m.Nudge("message", "bonjour")
	m.Nudge("priority", "haute")

	s := graph.NewState()
	m.DrainNudges(s)

	if v, _ := s.Get("message"); v != "bonjour" {
		t.Errorf("message = %v, want bonjour", v)
	}
	if v, _ := s.Get("priority"); v != "haute" {
		t.Errorf("priority = %v, want haute", v)
	}
}

// A nudge is consumed once: the same value must not reapply to a later level.
func TestDrainNudgesEmptiesTheQueue(t *testing.T) {
	m := NewMailbox(nil)
	m.Nudge("message", "bonjour")

	m.DrainNudges(graph.NewState())
	s2 := graph.NewState()
	m.DrainNudges(s2)

	if _, ok := s2.Get("message"); ok {
		t.Error("a nudge applied a second time after being drained once")
	}
}

func TestAwaitDecisionReturnsWhatDecideSent(t *testing.T) {
	m := NewMailbox(nil)
	done := make(chan graph.Decision, 1)
	go func() {
		d, err := m.AwaitDecision(context.Background(), "confirm")
		if err != nil {
			t.Errorf("AwaitDecision: %v", err)
		}
		done <- d
	}()

	// Give the goroutine time to register before deciding.
	time.Sleep(20 * time.Millisecond)
	if err := m.Decide("confirm", graph.Approved); err != nil {
		t.Fatalf("Decide: %v", err)
	}

	select {
	case d := <-done:
		if d != graph.Approved {
			t.Errorf("got %v, want Approved", d)
		}
	case <-time.After(time.Second):
		t.Fatal("AwaitDecision never returned")
	}
}

func TestAwaitDecisionRespectsContextCancellation(t *testing.T) {
	m := NewMailbox(nil)
	ctx, cancel := context.WithCancel(context.Background())
	errc := make(chan error, 1)
	go func() {
		_, err := m.AwaitDecision(ctx, "confirm")
		errc <- err
	}()

	time.Sleep(20 * time.Millisecond)
	cancel()

	select {
	case err := <-errc:
		if !errors.Is(err, context.Canceled) {
			t.Errorf("err = %v, want context.Canceled", err)
		}
	case <-time.After(time.Second):
		t.Fatal("AwaitDecision never returned after cancellation")
	}
}

func TestDecideOnANodeWithNoPendingWaitErrors(t *testing.T) {
	m := NewMailbox(nil)
	if err := m.Decide("nobody-waiting", graph.Approved); !errors.Is(err, ErrNoPendingDecision) {
		t.Errorf("err = %v, want ErrNoPendingDecision", err)
	}
}

func TestStopCallsTheCancelFunc(t *testing.T) {
	called := false
	m := NewMailbox(func() { called = true })
	m.Stop()
	if !called {
		t.Error("Stop did not call the cancel func")
	}
}

func TestStopWithNoCancelFuncIsSafe(t *testing.T) {
	m := NewMailbox(nil)
	m.Stop() // must not panic
}
