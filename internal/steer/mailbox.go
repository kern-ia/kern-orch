// Package steer is the in-memory control mailbox behind C6's write path: one per running
// run, holding pending nudges and pending approval decisions. It is deliberately not
// durable — a nudge or a decision in flight is lost if kern-orch restarts, the same
// crash story the engine's own approval pause already accepts. Persisting either would
// mean building a second checkpoint mechanism for state the run's own checkpoint doesn't
// need to know about between requests.
package steer

import (
	"context"
	"errors"
	"sync"

	"github.com/yoann/kern-orch/internal/graph"
)

// ErrNoPendingDecision is returned by Decide when no node is currently waiting under that
// id — either it never paused there, or it already got its answer.
var ErrNoPendingDecision = errors.New("steer: no pending decision for this node")

type nudge struct {
	key   string
	value any
}

// Mailbox is the seam between a run's HTTP-facing steer endpoints and its engine: nudges
// queue here until the engine's NudgeFunc drains them, and an ApprovalFunc blocks on a
// channel here until Decide is called for its node.
type Mailbox struct {
	mu      sync.Mutex
	nudges  []nudge
	waiting map[string]chan graph.Decision
	cancel  context.CancelFunc
}

// NewMailbox returns an empty mailbox. cancel may be nil for a mailbox that only needs to
// carry nudges/decisions in a test — Stop is then a no-op.
func NewMailbox(cancel context.CancelFunc) *Mailbox {
	return &Mailbox{waiting: make(map[string]chan graph.Decision), cancel: cancel}
}

// Nudge enqueues a state key/value pair for the next level to pick up.
func (m *Mailbox) Nudge(key string, value any) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.nudges = append(m.nudges, nudge{key: key, value: value})
}

// DrainNudges applies every queued nudge to s and empties the queue — a nudge applies
// exactly once, to the next level that starts after it arrived. It returns what it applied,
// nil when nothing was queued.
//
// The return value exists because a nudge is the one write to the shared state that passes
// through no node: nothing else in the run observes it, so a caller that has to record what
// happened to the state has no other way to learn of it. A map rather than the queue's own
// order because that is what the state ends up holding — a key nudged twice keeps the last
// value, exactly as the loop below leaves it.
func (m *Mailbox) DrainNudges(s *graph.State) map[string]any {
	m.mu.Lock()
	pending := m.nudges
	m.nudges = nil
	m.mu.Unlock()

	if len(pending) == 0 {
		return nil
	}
	applied := make(map[string]any, len(pending))
	for _, n := range pending {
		s.Set(n.key, n.value)
		applied[n.key] = n.value
	}
	return applied
}

// AwaitDecision blocks until Decide is called for nodeID, or ctx is cancelled — a stop
// request propagating as the same context cancellation that already kills an in-flight
// subprocess node.
func (m *Mailbox) AwaitDecision(ctx context.Context, nodeID string) (graph.Decision, error) {
	ch := make(chan graph.Decision, 1)
	m.mu.Lock()
	m.waiting[nodeID] = ch
	m.mu.Unlock()
	defer func() {
		m.mu.Lock()
		delete(m.waiting, nodeID)
		m.mu.Unlock()
	}()

	select {
	case d := <-ch:
		return d, nil
	case <-ctx.Done():
		return "", ctx.Err()
	}
}

// Decide answers a pending AwaitDecision for nodeID.
func (m *Mailbox) Decide(nodeID string, d graph.Decision) error {
	m.mu.Lock()
	ch, ok := m.waiting[nodeID]
	m.mu.Unlock()
	if !ok {
		return ErrNoPendingDecision
	}
	ch <- d
	return nil
}

// Stop cancels the run's context. A nil cancel func (no run actually bound) is a no-op.
func (m *Mailbox) Stop() {
	if m.cancel != nil {
		m.cancel()
	}
}
