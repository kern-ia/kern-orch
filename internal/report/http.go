// Package report pushes run transitions to an external HTTP sink.
//
// It is observability, never a dependency of the run: the hook it produces reports errors
// to stderr and always returns nil, so a sink that is slow, broken or absent can never
// abort a graph — nor, since the queue below, even slow it down. The direction of
// dependency matches the rest of the infra: report depends on graph, never the reverse.
package report

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/yoann/kern-orch/internal/graph"
)

// DefaultTimeout caps how long a single report may take. It no longer holds up a run: see
// stepQueueSize.
const DefaultTimeout = 2 * time.Second

// stepQueueSize is how many levels may be waiting for delivery before the reporter starts
// dropping them.
//
// Dropping is the right failure here, and blocking is not. Every event carries the full
// merged state, so a sink that misses one level is corrected by the next; a graph that
// waits on a broken sink is a graph running slower because someone is watching it. This is
// the same trade the interface already makes for browsers that fall behind.
const stepQueueSize = 64

// DefaultFlushTimeout caps how long Flush waits for the queue to drain.
//
// Moving delivery off the engine's thread would only relocate the problem if the command
// then hung on exit instead: a sink that never answers must cost the run nothing, including
// nothing at the end of it. What is still queued when this expires is announced and dropped.
const DefaultFlushTimeout = 3 * time.Second

// Topology is the shape of the graph, sent once at the start of a run.
//
// It is declared data, read from the YAML, not read back from the running graph: the
// engine's edges are closures, so a conditional route cannot be enumerated. Such an edge
// arrives with no targets and Dynamic set, which tells a consumer its picture is
// incomplete rather than letting it read the node as terminal.
type Topology struct {
	Entry string         `json:"entry"`
	Nodes []TopologyNode `json:"nodes"`
	Edges []TopologyEdge `json:"edges,omitempty"`
}

// TopologyNode is one unit of work: its id, what kind of work it is, and — for an agent —
// the skill backing it. Skill is absent on tool nodes, which name a Go function instead.
type TopologyNode struct {
	ID    string `json:"id"`
	Kind  string `json:"kind"` // tool | agent | subgraph
	Skill string `json:"skill,omitempty"`
}

// TopologyEdge leaves a node towards its declared targets, or is flagged Dynamic when a
// router picks them at run time.
type TopologyEdge struct {
	From    string   `json:"from"`
	To      []string `json:"to,omitempty"`
	Dynamic bool     `json:"dynamic,omitempty"`
}

// Parent points a nested run at the node it belongs to.
//
// A nested run reports as a run of its own rather than folding into its parent's stream:
// the parent's level counter is a sequence a sink rejects out-of-order writes against, and
// two graphs advancing at once would corrupt it. Pointing back also composes at any depth,
// where embedding a child's topology inside its parent's would need a recursive schema.
type Parent struct {
	RunID  string `json:"run_id"`
	NodeID string `json:"node_id"`
}

// Failure ends a run that did not complete, naming what went wrong and where.
//
// Nodes holds every node of the level that failed, sorted. A node in the reported frontier
// that is absent from it **completed**: the engine waits for the whole level before it
// gives up, so this is knowledge and not a guess. Without it a consumer could only mark the
// entire frontier and hope.
type Failure struct {
	Message string   `json:"message"`
	Nodes   []string `json:"nodes,omitempty"`
}

// StepEvent is the payload accepted by the sink: one completed graph level.
//
// State is the flat business data, not graph.State's wire form. Marshalling the State
// itself would ship kern-orch's envelope (zones, frozen counter, internal step) across the
// contract, which is nobody else's business.
type StepEvent struct {
	RunID    string         `json:"run_id"`
	Graph    string         `json:"graph"`
	Step     int            `json:"step"`
	Frontier []string       `json:"frontier"`
	State    map[string]any `json:"state,omitempty"`
	At       time.Time      `json:"at"`

	// Topology rides on the first event of a run only: it never changes, and repeating it
	// on every level would be waste.
	Topology *Topology `json:"topology,omitempty"`

	// Error is set on the terminal event of a run that failed. Without it a failed run is
	// indistinguishable from a finished one.
	Error *Failure `json:"error,omitempty"`

	// Parent is set on a nested run and absent on a top-level one.
	Parent *Parent `json:"parent,omitempty"`

	// Requester names who asked for this run (C6); empty means open. Rides on the first
	// event only, like Topology.
	Requester string `json:"requester,omitempty"`

	// Dossier is a caller-supplied business label (e.g. a client case) grouping several
	// runs together for a consumer like kern-ui's dossiers list; empty means none. Rides
	// on the first event only, like Requester and Topology — it never changes over a
	// run's life.
	Dossier string `json:"dossier,omitempty"`
}

// flatten extracts the business data of a state, leaving its internals behind.
func flatten(s *graph.State) map[string]any {
	if s == nil {
		return nil
	}
	keys := s.Keys()
	if len(keys) == 0 {
		return nil
	}

	data := make(map[string]any, len(keys))
	for _, k := range keys {
		if v, ok := s.Get(k); ok {
			data[k] = v
		}
	}
	return data
}

// HTTPReporter posts step events to a single configured URL. The URL is the whole contract:
// the reporter knows nothing of the sink's route shape.
type HTTPReporter struct {
	URL     string
	Token   string // presented as a bearer credential; empty sends no header
	Timeout time.Duration
	Client  *http.Client

	// Requester names who asked for this run (C6); empty means open, no different from a
	// run with no requester at all. Rides on the first event only, the same way Topology
	// does, and for the same reason: it never changes over a run's life.
	Requester string

	// Dossier is a caller-supplied business label; empty means none. Same "first event
	// only" treatment as Requester, for the same reason.
	Dossier string

	// FlushTimeout caps how long Flush waits. Defaults to DefaultFlushTimeout.
	FlushTimeout time.Duration

	// Errf receives delivery failures. Defaults to stderr.
	Errf func(format string, args ...any)

	now func() time.Time

	// One queue and one worker, started on first use. A goroutine per event would be
	// simpler and wrong: a sink folds levels in sequence and rejects one older than the
	// level it already holds, so two reports racing would silently lose a frontier.
	start sync.Once
	stop  sync.Once
	queue chan StepEvent
	done  chan struct{}
}

// runReporter carries the per-run state the hook needs: the topology still to be announced,
// and the parent a nested run belongs to.
type runReporter struct {
	pending *Topology
	parent  *Parent
	sent    bool
}

// NewHTTP returns a reporter posting to url. An empty url yields a disabled reporter whose
// Hook is nil.
func NewHTTP(url string) *HTTPReporter {
	return &HTTPReporter{
		URL:     url,
		Timeout: DefaultTimeout,
		now:     func() time.Time { return time.Now().UTC() },
	}
}

// Enabled reports whether a sink is configured.
func (r *HTTPReporter) Enabled() bool { return r.URL != "" }

// Hook returns the StepFunc to register on the engine, or nil when no sink is configured
// so the caller can leave it out of the chain entirely.
func (r *HTTPReporter) Hook(runID, graphName string, topo *Topology) graph.StepFunc {
	return r.NestedHook(runID, graphName, topo, nil)
}

// NestedHook is Hook for a run that belongs to a subgraph node of another run. A nil parent
// makes it identical to Hook.
func (r *HTTPReporter) NestedHook(runID, graphName string, topo *Topology, parent *Parent) graph.StepFunc {
	if !r.Enabled() {
		return nil
	}
	run := &runReporter{pending: topo, parent: parent}

	return func(ctx context.Context, info graph.StepInfo, s *graph.State) error {
		if err := ctx.Err(); err != nil {
			// The run is already going down; do not add a doomed request to the noise.
			return nil
		}

		ev := StepEvent{
			RunID:    runID,
			Graph:    graphName,
			Step:     info.Step,
			Frontier: nonNil(info.Frontier),
			State:    flatten(s),
			At:       r.now(),
			Parent:   run.parent,
		}
		if !run.sent {
			ev.Topology = run.pending
			ev.Requester = r.Requester
			ev.Dossier = r.Dossier
			run.sent = true
		}

		r.enqueue(ev)
		// Always nil, and now also immediate: see the package comment.
		return nil
	}
}

// ReportFailure announces a run that ended badly.
//
// The step hook cannot do this: it only ever sees levels that succeeded, and the engine
// reports the failure by returning from Run. Without this call a failed run would look
// exactly like a finished one to the sink.
// frontier is the level that was running when things broke, and failed names which of those
// nodes actually broke. The frontier alone would leave a sink able to say only "somewhere in
// here".
func (r *HTTPReporter) ReportFailure(ctx context.Context, runID, graphName string, step int, frontier, failed []string, message string) {
	if !r.Enabled() {
		return
	}

	// The run context is usually already cancelled by the time we get here, so the report
	// gets a context of its own — otherwise no failure would ever be reported.
	// Through the same queue as the levels, so it cannot overtake them: a sink folding a
	// failure before the steps that led to it would show a failed run coming back to life.
	r.enqueue(StepEvent{
		RunID:    runID,
		Graph:    graphName,
		Step:     step,
		Frontier: nonNil(frontier),
		At:       r.now(),
		Error:    &Failure{Message: message, Nodes: failed},
	})
}

// enqueue hands an event to the worker, starting it on first use. A full queue drops rather
// than waits — see stepQueueSize.
func (r *HTTPReporter) enqueue(ev StepEvent) {
	if !r.Enabled() {
		return
	}
	r.start.Do(r.startWorker)

	select {
	case r.queue <- ev:
	default:
		r.errf("kern-orch: report queue full, dropping step %d of run %s\n", ev.Step, ev.RunID)
	}
}

func (r *HTTPReporter) startWorker() {
	r.queue = make(chan StepEvent, stepQueueSize)
	r.done = make(chan struct{})

	go func() {
		defer close(r.done)
		for ev := range r.queue {
			// A detached context: the run is usually already finishing, and delivery must
			// not be cancelled along with it.
			if err := r.send(context.WithoutCancel(context.Background()), ev); err != nil {
				r.errf("kern-orch: report step %d of run %s: %v\n", ev.Step, ev.RunID, err)
			}
		}
	}()
}

// Flush delivers everything still queued and stops the worker. A command must call it
// before exiting, or the last level of a run dies with the process.
//
// It is safe to call on a reporter that was never used, and safe to call twice.
func (r *HTTPReporter) Flush() {
	if r.queue == nil {
		return
	}
	r.stop.Do(func() {
		close(r.queue)
		select {
		case <-r.done:
		case <-time.After(r.flushTimeout()):
			// The worker is left to finish or not; this is a command on its way out, and
			// an undelivered level is worth a line on stderr, never a hang.
			r.errf("kern-orch: gave up waiting for the report sink after %s\n", r.flushTimeout())
		}
	})
}

func (r *HTTPReporter) flushTimeout() time.Duration {
	if r.FlushTimeout > 0 {
		return r.FlushTimeout
	}
	return DefaultFlushTimeout
}

func nonNil(frontier []string) []string {
	if frontier == nil {
		// An absent frontier and an empty one mean the same thing to the sink — the run is
		// over — but only a list survives the round trip unambiguously.
		return []string{}
	}
	return frontier
}

func (r *HTTPReporter) send(ctx context.Context, ev StepEvent) error {
	return postJSONWith(ctx, r.client(), r.URL, r.Token, r.timeout(), ev)
}

// postJSON marshals payload and POSTs it, using the default client. Shared by the step
// reporter and the registry publisher: one way to talk to a sink, so a timeout or an error
// message never depends on which contract is travelling.
func postJSON(ctx context.Context, url, token string, timeout time.Duration, payload any) error {
	return postJSONWith(ctx, http.DefaultClient, url, token, timeout, payload)
}

// token is presented as a bearer credential when set. An empty one sends **no header** at
// all rather than an empty bearer: a credential that says "I tried" is worse than none, and
// a sink should see an anonymous caller rather than a malformed one.
func postJSONWith(ctx context.Context, client *http.Client, url, token string, timeout time.Duration, payload any) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("encode: %w", err)
	}

	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("post: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= http.StatusBadRequest {
		return fmt.Errorf("sink answered %s", resp.Status)
	}
	return nil
}

func (r *HTTPReporter) client() *http.Client {
	if r.Client != nil {
		return r.Client
	}
	return http.DefaultClient
}

func (r *HTTPReporter) timeout() time.Duration {
	if r.Timeout > 0 {
		return r.Timeout
	}
	return DefaultTimeout
}

func (r *HTTPReporter) errf(format string, args ...any) {
	if r.Errf != nil {
		r.Errf(format, args...)
		return
	}
	stderrf(format, args...)
}

// stderrf is where a delivery failure goes when no Errf is set. Shared, so a broken sink
// reads the same whichever contract was travelling.
func stderrf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, format, args...)
}
