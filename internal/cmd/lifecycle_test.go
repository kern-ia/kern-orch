package cmd

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/yoann/kern-orch/internal/agentrunner"
	"github.com/yoann/kern-orch/internal/checkpoint"
	"github.com/yoann/kern-orch/internal/graph"
	"github.com/yoann/kern-orch/internal/report"
	"github.com/yoann/kern-orch/internal/steer"
)

// fakeLifecycleRunner is an adapter that implements both graph.AgentRunner and
// agentrunner.Lifecycle and records the order of every call it receives. It stands in for
// issues 04/05's real adapters, which do not exist yet: what has to be proven here is the
// wiring around a run, not any particular CLI's start-up.
type fakeLifecycleRunner struct {
	mu    sync.Mutex
	calls []string

	startErr error
	closeErr error

	// blockUntilCancel makes Run behave like a long node: it returns only when the run's
	// own context is cancelled. That is what lets the stop path be exercised while a node
	// is genuinely in flight rather than between two levels.
	blockUntilCancel bool
	running          chan struct{}
}

func (f *fakeLifecycleRunner) record(call string) {
	f.mu.Lock()
	f.calls = append(f.calls, call)
	f.mu.Unlock()
}

func (f *fakeLifecycleRunner) sequence() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.calls...)
}

func (f *fakeLifecycleRunner) Start(context.Context) error {
	f.record("start")
	return f.startErr
}

func (f *fakeLifecycleRunner) Close() error {
	f.record("close")
	return f.closeErr
}

func (f *fakeLifecycleRunner) Run(ctx context.Context, req graph.AgentRequest) (graph.AgentResult, error) {
	f.record("run:" + req.NodeID)
	if f.blockUntilCancel {
		if f.running != nil {
			close(f.running)
		}
		<-ctx.Done()
		return graph.AgentResult{}, ctx.Err()
	}
	return graph.AgentResult{Output: map[string]any{req.NodeID: "done"}}, nil
}

// agentGraph builds a linear graph of agent nodes backed by runner — the smallest shape
// that shows whether Start landed before the first node and Close after the last.
func agentGraph(t *testing.T, runner graph.AgentRunner, ids ...string) *graph.Graph {
	t.Helper()
	g := graph.NewGraph().SetEntry(ids[0])
	for i, id := range ids {
		g = g.AddNode(graph.NewAgentNode(id, "prompt", runner))
		if i > 0 {
			g = g.AddEdge(ids[i-1], graph.Static(ids[i]))
		}
	}
	if err := g.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}
	return g
}

// lifecyclePreparedRun assembles a preparedRun by hand, with no sink configured: the point
// under test is the run's own setup/teardown, and prepareRun's YAML loading would only add
// a file to the fixture without changing which lifecycle calls fire.
func lifecyclePreparedRun(runner graph.AgentRunner, g *graph.Graph, mailbox *steer.Mailbox) *preparedRun {
	return &preparedRun{
		graph: g, name: "lifecycle", runner: runner, mailbox: mailbox,
		reporter: report.NewHTTP(""), activity: &activityRelay{},
		activityReporter: report.NewActivityReporter(""),
	}
}

func assertSequence(t *testing.T, got, want []string) {
	t.Helper()
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("call sequence = %v, want %v", got, want)
	}
}

func TestTheAgentAdapterStartsOnceBeforeTheFirstNodeAndClosesOnceAfterTheRunEnds(t *testing.T) {
	fake := &fakeLifecycleRunner{}
	store := openDaemonStore(t, t.TempDir())
	prepared := lifecyclePreparedRun(fake, agentGraph(t, fake, "first", "second"), nil)

	if err := prepared.run(context.Background(), store, newRunID(), nil); err != nil {
		t.Fatalf("run: %v", err)
	}
	assertSequence(t, fake.sequence(), []string{"start", "run:first", "run:second", "close"})
}

func TestAFailingRunClosesTheAgentAdapterExactlyOnce(t *testing.T) {
	boom := errors.New("node exploded")
	fake := &fakeLifecycleRunner{}
	g := graph.NewGraph().SetEntry("agent").
		AddNode(graph.NewAgentNode("agent", "prompt", fake)).
		AddNode(graph.NewToolNode("boom", func(context.Context, *graph.State) error { return boom })).
		AddEdge("agent", graph.Static("boom"))
	if err := g.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}
	store := openDaemonStore(t, t.TempDir())
	prepared := lifecyclePreparedRun(fake, g, nil)

	if err := prepared.run(context.Background(), store, newRunID(), nil); err == nil {
		t.Fatal("run: want the node's failure, got nil")
	}
	assertSequence(t, fake.sequence(), []string{"start", "run:agent", "close"})
}

func TestAStoppedRunStillClosesTheAgentAdapterExactlyOnce(t *testing.T) {
	fake := &fakeLifecycleRunner{blockUntilCancel: true, running: make(chan struct{})}
	store := openDaemonStore(t, t.TempDir())

	// The exact mechanism StopRun uses: it looks the run's mailbox up and calls Stop,
	// which cancels the run's context. Going through the mailbox rather than the raw
	// cancel func is what makes this the stop path and not just a cancelled caller.
	runCtx, cancel := context.WithCancel(context.Background())
	defer cancel()
	mailbox := steer.NewMailbox(cancel)
	prepared := lifecyclePreparedRun(fake, agentGraph(t, fake, "hold"), mailbox)

	done := make(chan error, 1)
	go func() { done <- prepared.run(runCtx, store, newRunID(), nil) }()

	select {
	case <-fake.running:
	case <-time.After(2 * time.Second):
		t.Fatal("the agent node never started")
	}
	mailbox.Stop()

	select {
	case err := <-done:
		if err == nil {
			t.Fatal("run: want the stopped run's error, got nil")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("the stopped run never returned")
	}
	assertSequence(t, fake.sequence(), []string{"start", "run:hold", "close"})
}

func TestAFailingAdapterStartAbortsTheRunBeforeAnyNodeExecutesAndNamesTheAdapter(t *testing.T) {
	fake := &fakeLifecycleRunner{startErr: errors.New("no server")}
	store := openDaemonStore(t, t.TempDir())
	prepared := lifecyclePreparedRun(fake, agentGraph(t, fake, "never"), nil)

	err := prepared.run(context.Background(), store, newRunID(), nil)
	if err == nil {
		t.Fatal("run: want the start failure, got nil")
	}
	if !errors.Is(err, fake.startErr) {
		t.Fatalf("run: error %v does not wrap the adapter's own start error", err)
	}
	if !strings.Contains(err.Error(), "fakeLifecycleRunner") {
		t.Fatalf("run: error %q does not name the adapter that failed to start", err)
	}
	assertSequence(t, fake.sequence(), []string{"start"})
}

func TestAFailingAdapterCloseLeavesASuccessfulRunSuccessful(t *testing.T) {
	fake := &fakeLifecycleRunner{closeErr: errors.New("could not shut down")}
	store := openDaemonStore(t, t.TempDir())
	runID := newRunID()
	prepared := lifecyclePreparedRun(fake, agentGraph(t, fake, "only"), nil)

	if err := prepared.run(context.Background(), store, runID, nil); err != nil {
		t.Fatalf("run: a Close failure must not fail the run, got %v", err)
	}
	assertSequence(t, fake.sequence(), []string{"start", "run:only", "close"})

	rec, ok, err := store.Latest(context.Background(), runID)
	if err != nil {
		t.Fatalf("Latest: %v", err)
	}
	if !ok || rec.Status != checkpoint.StatusDone {
		t.Fatalf("checkpoint status = %q (found=%v), want %q", rec.Status, ok, checkpoint.StatusDone)
	}
}

func TestARunnerWithoutALifecycleRunsUntouched(t *testing.T) {
	stub := &agentrunner.Stub{Default: map[string]any{"ok": true}}
	store := openDaemonStore(t, t.TempDir())
	runID := newRunID()
	prepared := lifecyclePreparedRun(stub, agentGraph(t, stub, "plain"), nil)

	if err := prepared.run(context.Background(), store, runID, nil); err != nil {
		t.Fatalf("run: %v", err)
	}
	rec, ok, err := store.Latest(context.Background(), runID)
	if err != nil {
		t.Fatalf("Latest: %v", err)
	}
	if !ok || rec.Status != checkpoint.StatusDone {
		t.Fatalf("checkpoint status = %q (found=%v), want %q", rec.Status, ok, checkpoint.StatusDone)
	}
}
