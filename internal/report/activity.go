package report

import (
	"context"
	"sync"
	"time"
)

// ActivitySignal reports one node starting or stopping generation: `kern.activity/v1`.
//
// It is a sibling of StepEvent, not a field of it. A step describes a level that has
// *completed*; generation happens inside a level, so anything carried on a step event
// would arrive long after the fact it describes stopped being true.
type ActivitySignal struct {
	RunID      string    `json:"run_id"`
	Graph      string    `json:"graph"`
	NodeID     string    `json:"node_id"`
	Generating bool      `json:"generating"`
	At         time.Time `json:"at"`
}

// ActivityReporter posts activity signals to a single configured URL.
//
// Unlike the step reporter it posts **off the caller's thread**. A step is reported between
// levels, where a pause costs little; activity is reported at the exact moment an agent is
// about to start working, and making an agent wait on an HTTP round trip before it may
// think would let observability slow down the thing it observes.
//
// Fire-and-forget must not become fire-and-lose, so Flush waits for what is in flight. The
// signal that turns a beacon back off is the last one a run emits, and it is precisely the
// one a process exiting would drop.
type ActivityReporter struct {
	URL     string
	Token   string // presented as a bearer credential; empty sends no header
	Timeout time.Duration

	// FlushTimeout caps how long Flush waits. Defaults to DefaultFlushTimeout.
	FlushTimeout time.Duration

	// Errf receives delivery failures. Defaults to stderr.
	Errf func(format string, args ...any)

	wg  sync.WaitGroup
	now func() time.Time
}

// NewActivityReporter returns a reporter posting to url. An empty url yields a disabled
// reporter whose Report is a no-op.
func NewActivityReporter(url string) *ActivityReporter {
	return &ActivityReporter{
		URL:     url,
		Timeout: DefaultTimeout,
		now:     func() time.Time { return time.Now().UTC() },
	}
}

// Enabled reports whether a sink is configured.
func (r *ActivityReporter) Enabled() bool { return r.URL != "" }

// Report announces that nodeID started or stopped generating. It returns immediately.
//
// The context is detached from the caller's: a run is usually already cancelled by the time
// its last agent stops, and reporting on the run's context would mean never reporting the
// stop at all — leaving a beacon lit over a run that ended.
func (r *ActivityReporter) Report(ctx context.Context, runID, graphName, nodeID string, generating bool) {
	if !r.Enabled() {
		return
	}

	signal := ActivitySignal{
		RunID:      runID,
		Graph:      graphName,
		NodeID:     nodeID,
		Generating: generating,
		At:         r.now(),
	}

	r.wg.Add(1)
	go func() {
		defer r.wg.Done()
		if err := postJSON(context.WithoutCancel(ctx), r.URL, r.Token, r.timeout(), signal); err != nil {
			r.errf("kern-orch: report activity of node %s in run %s: %v\n", nodeID, runID, err)
		}
	}()
}

// Flush waits for every signal already handed to Report to finish its round trip. A
// command must call it before exiting.
//
// Bounded, for the same reason the queue exists: a sink that never answers must not hold up
// the exit any more than it holds up the run.
func (r *ActivityReporter) Flush() {
	done := make(chan struct{})
	go func() {
		r.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(r.flushTimeout()):
		r.errf("kern-orch: gave up waiting for the activity sink after %s\n", r.flushTimeout())
	}
}

func (r *ActivityReporter) flushTimeout() time.Duration {
	if r.FlushTimeout > 0 {
		return r.FlushTimeout
	}
	return DefaultFlushTimeout
}

func (r *ActivityReporter) timeout() time.Duration {
	if r.Timeout > 0 {
		return r.Timeout
	}
	return DefaultTimeout
}

func (r *ActivityReporter) errf(format string, args ...any) {
	if r.Errf != nil {
		r.Errf(format, args...)
		return
	}
	stderrf(format, args...)
}
