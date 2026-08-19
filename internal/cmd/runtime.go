package cmd

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/yoann/kern-orch/internal/agentrunner"
	"github.com/yoann/kern-orch/internal/checkpoint"
	"github.com/yoann/kern-orch/internal/config"
	"github.com/yoann/kern-orch/internal/graph"
	"github.com/yoann/kern-orch/internal/journal/projection"
	"github.com/yoann/kern-orch/internal/notify"
	"github.com/yoann/kern-orch/internal/report"
	"github.com/yoann/kern-orch/internal/skills"
	"github.com/yoann/kern-orch/internal/steer"
	"github.com/yoann/kern-orch/internal/topology"
)

// newRunner returns the real subprocess runner when KERN_AGENT_CLI is set, otherwise the
// deterministic stub so the harness runs with no LLM configured.
// activityRelay is the seam between the runner and the reporter. The runner is built before
// the run has an id, so the hook cannot be written at construction time; the relay is handed
// over empty and filled once the id exists. A nil target is a no-op, which is what an
// unconfigured sink amounts to.
type activityRelay struct {
	fn func(nodeID string, generating bool, message string)
}

func (a *activityRelay) call(nodeID string, generating bool, message string) {
	if a.fn != nil {
		a.fn(nodeID, generating, message)
	}
}

func newRunner(cfg config.Config, activity *activityRelay) graph.AgentRunner {
	if r, ok := agentrunner.NewSubprocessFromEnv(); ok {
		r.Stderr = os.Stderr
		r.TokenSink = os.Stderr
		if activity != nil {
			r.OnActivity = activity.call
		}
		return r
	}
	return &agentrunner.Stub{}
}

// builtinRegistry wires the built-in tool/router functions available to every graph.
// Projects extend this set in Go; the YAML topology references entries by name.
func builtinRegistry(runner graph.AgentRunner, cfg config.Config) *topology.Registry {
	reg := topology.NewRegistry(runner)
	reg.Tool("noop", func(context.Context, *graph.State) error { return nil })
	// double: demo tool for the subgraph example — multiplies state key "n" by 2.
	reg.Tool("double", func(_ context.Context, s *graph.State) error {
		if v, ok := s.Get("n"); ok {
			if iv, ok := v.(int); ok {
				s.Set("n", iv*2)
			}
		}
		return nil
	})
	// seed: demo tool that initializes state key "n" to 3.
	reg.Tool("seed", func(_ context.Context, s *graph.State) error {
		s.Set("n", 3)
		return nil
	})
	// freeze: respawns a fresh context — drops ephemeral-zone keys, keeps persistent
	// ones ("gel = respawn contexte frais"). Node type: tool, func: freeze.
	reg.Tool("freeze", func(_ context.Context, s *graph.State) error {
		s.Freeze(nil)
		return nil
	})
	// announce: demo tool for the notify example — sets state key "message" to a fixed
	// demo string, standing in for what an agent's own output would set.
	reg.Tool("announce", func(_ context.Context, s *graph.State) error {
		s.Set("message", "Test kern-orch : le nœud notify a bien envoyé ce message.")
		return nil
	})
	// notify: an agent's own outbound channel to a human — sends state key "message" to
	// Telegram. Unconfigured (no KERN_TELEGRAM_BOT_TOKEN/KERN_TELEGRAM_CHAT_ID) fails
	// the node rather than dropping the message silently.
	var notifyClient *notify.Client
	if cfg.TelegramBotToken != "" && cfg.TelegramChatID != "" {
		notifyClient = notify.New(cfg.TelegramBotToken, cfg.TelegramChatID)
	}
	reg.Tool("notify", notify.Tool(notifyClient))
	// wait: demo tool for C6 — blocks until the run's own context is cancelled, proving
	// stop actually interrupts a live node rather than just refusing new work.
	reg.Tool("wait", func(ctx context.Context, _ *graph.State) error {
		<-ctx.Done()
		return ctx.Err()
	})
	// pause: demo tool for C6 — sleeps briefly so a nudge sent while it runs has time to
	// land before the next level starts.
	reg.Tool("pause", func(ctx context.Context, _ *graph.State) error {
		select {
		case <-time.After(150 * time.Millisecond):
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	})
	// onConfirmDecision: demo router for the C6 approval example — reads the decision an
	// approval node named "confirm" recorded and picks the matching branch.
	reg.Router("onConfirmDecision", func(s *graph.State) []string {
		if v, _ := s.Get(graph.DecisionKey("confirm")); v == string(graph.Approved) {
			return []string{"approved"}
		}
		return []string{"refused"}
	})
	// community-management-agency routers (internal/cmd/comm_routers.go): onStrategyMode
	// after the strategiste node, and one decisionRouter per approval gate — this graph
	// has two, so it cannot reuse onConfirmDecision above (hardcoded to a single node
	// named "confirm").
	reg.Router("onStrategyMode", onStrategyMode)
	reg.Router("onStrategieDecision", decisionRouter("confirm_strategie",
		[]string{"redacteur"}, []string{"strategie_refusee"}))
	reg.Router("onPublicationDecision", decisionRouter("confirm_publication",
		[]string{"publieur"}, []string{"refus_publication"}))
	// community-management-agency-auto (internal/cmd/comm_auto.go) — a separate graph,
	// additive to the one above. See that file for the routing rule.
	reg.Router("onAutoPublishRoute", onAutoPublishRoute)
	reg.Tool("autoApprove", autoApproveTool)
	// courtage-extraction (internal/cmd/courtage_anon.go) — kern-anon wired in as a real
	// Go dependency (go.mod replace github.com/kern-ia/kern-anon => ../Kern-Anon). Masking
	// happens before any interpretive model sees the text; demasking after, via a token
	// map rather than Presidio's own position-based Deanonymize (see that file's doc).
	reg.Tool("anonymizePII", anonymizePII)
	reg.Tool("deanonymizePII", deanonymizePII)
	reg.Router("onExtractionDecision", decisionRouter("confirm_extraction",
		[]string{"extraction_validee"}, []string{"extraction_a_corriger"}))
	// courtage-extraction besoin #2 (mémorandum) — chained onto the same graph/state as
	// besoin #1 (user's choice: "un seul flux dossier -> mémo"), own masking pass so it
	// never clobbers besoin #1's own state keys. See internal/cmd/courtage_anon.go.
	reg.Tool("anonymizeMemoInput", anonymizeMemoInput)
	reg.Tool("deanonymizeMemoOutput", deanonymizeMemoOutput)
	reg.Router("onMemoDecision", decisionRouter("confirm_memo",
		[]string{"memo_valide"}, []string{"memo_a_corriger"}))
	// courtage-extraction besoin #3 (relances pièces manquantes, specs.md) — notifies the
	// internal team (existing "notify" tool above, same fixed Telegram destination), never
	// the client directly (no client->chat_id model exists, and Telegram cannot message a
	// user who hasn't messaged the bot first — see docs/index for the cadrage decision).
	reg.Router("onRelanceNeeded", onRelanceNeeded)
	return reg
}

// openStore opens the checkpoint store, creating the parent directory if needed.
func openStore(cfg config.Config) (*checkpoint.SQLiteStore, error) {
	if dir := filepath.Dir(cfg.CheckpointDB); dir != "" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, err
		}
	}
	return checkpoint.OpenSQLite(cfg.CheckpointDB)
}

// checkpointHook persists a level under the recorder's run: the level's journal events and
// the projection row derived from them, in one transaction (see journalRecorder.checkpoint).
// It records graphPath so `resume` can reload the graph without the caller re-supplying it,
// requester so C6's write path knows who may steer this run (empty means anyone may), and
// dossier so a consumer can group this run under a case (empty means none).
//
// It no longer takes the store or the run id: both are the recorder's, and the state it once
// marshalled is now derived from the events rather than supplied. That is the whole change —
// the row is a projection of the journal, not a second record kept beside it.
func checkpointHook(rec *journalRecorder, graphPath, requester, dossier string) graph.StepFunc {
	return rec.checkpoint(graphPath, requester, dossier)
}

// reportHook rewires a reporter's own step hook to flatten the journal's projected state
// instead of the engine's live one — this is issue 11's whole change: internal/report stops
// observing the run in parallel and becomes a reader of the record. The hook's own signature
// (StepEvent, flatten, the queue) is untouched; only what the caller hands it as state moves
// from the engine's *graph.State to Project(store.Read(runID)).
//
// It belongs after checkpointHook in the chain (multiStep already orders durability before
// observers), so the events it reads back have already been written by this same level.
//
// A journal that cannot be read or replayed here costs this level's report a line on stderr
// and nothing else — the same best-effort contract report.HTTPReporter's own package doc
// already promises for a broken sink, extended to a broken read of the record it now depends
// on. It must never be the reason a run fails.
func reportHook(store *checkpoint.SQLiteStore, runID string, hook graph.StepFunc) graph.StepFunc {
	if hook == nil {
		return nil
	}
	return func(ctx context.Context, info graph.StepInfo, _ *graph.State) error {
		events, err := store.Read(ctx, runID)
		if err != nil {
			fmt.Fprintf(os.Stderr, "kern-orch: report step %d of run %s: read journal: %v\n", info.Step, runID, err)
			return nil
		}
		state, err := projection.Project(events)
		if err != nil {
			fmt.Fprintf(os.Stderr, "kern-orch: report step %d of run %s: project journal: %v\n", info.Step, runID, err)
			return nil
		}
		return hook(ctx, info, state)
	}
}

// multiStep chains several step hooks into the single one Engine.OnStep accepts. Hooks run
// in the order given and the first error aborts the run, so durability comes first and
// best-effort observers last. Nil hooks are skipped, which lets a caller pass a disabled
// reporter without branching.
func multiStep(hooks ...graph.StepFunc) graph.StepFunc {
	return func(ctx context.Context, info graph.StepInfo, s *graph.State) error {
		for _, hook := range hooks {
			if hook == nil {
				continue
			}
			if err := hook(ctx, info, s); err != nil {
				return err
			}
		}
		return nil
	}
}

// multiEvent chains several event hooks into the single one Engine.OnEvent accepts — the
// same shape multiStep gives OnStep, and for the same reason: the engine offers one seam,
// and more than one thing will want to watch a run. Hooks run in the order given and the
// first error aborts the run, so the durable record comes first and best-effort observers
// last. Nil hooks are skipped, which lets a caller pass a disabled recorder without
// branching.
//
// A hook registered here is called from the per-node goroutines, not only from the run's
// own — graph.EventFunc says so — so every hook chained here must be safe for concurrent
// use. multiEvent adds no locking of its own: serializing here would hide the requirement
// from the hooks that actually hold state, and turn the level's parallelism into a queue.
func multiEvent(hooks ...graph.EventFunc) graph.EventFunc {
	return func(ctx context.Context, ev graph.Event) error {
		for _, hook := range hooks {
			if hook == nil {
				continue
			}
			if err := hook(ctx, ev); err != nil {
				return err
			}
		}
		return nil
	}
}

// graphName is the label the UI shows for a run: the topology file without its extension.
func graphName(graphPath string) string {
	base := filepath.Base(graphPath)
	return strings.TrimSuffix(base, filepath.Ext(base))
}

// describeTopology reads the graph's declared shape for the reporter. A failure here is
// never fatal: the run matters, drawing it does not.
func describeTopology(graphPath string) *report.Topology {
	d, err := topology.DescribeFile(graphPath)
	if err != nil {
		return nil
	}

	topo := &report.Topology{Entry: d.Entry}
	for _, n := range d.Nodes {
		topo.Nodes = append(topo.Nodes, report.TopologyNode{ID: n.ID, Kind: n.Kind, Skill: n.Skill})
	}
	for _, e := range d.Edges {
		topo.Edges = append(topo.Edges, report.TopologyEdge{From: e.From, To: e.To, Dynamic: e.Dynamic})
	}
	return topo
}

// newRunID returns a short random run identifier.
func newRunID() string {
	var b [8]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}

// stepCounter remembers where the run had got to, so a failure can be reported against it.
// The engine returns its error from Run, long after the last hook fired — by then the level
// and the frontier are gone unless something kept them.
type stepCounter struct {
	last     int
	frontier []string
}

func (c *stepCounter) count(_ context.Context, info graph.StepInfo, _ *graph.State) error {
	c.last = info.Step
	c.frontier = info.Frontier
	return nil
}

// publishRegistry pushes reg's catalogue to the configured sink. The caller loads reg
// itself — a plain skills.Load for the publish-skills CLI's explicit --skills-dir escape
// hatch, LoadMerged everywhere else (C11) so a created skill rides along too.
//
// Best-effort by design, exactly like the step reporter: publishing is observability, and
// a sink that is slow, broken or absent must never be able to stop a graph from running.
// The caller gets the error only so it can say something useful; it must not propagate it.
func publishRegistry(ctx context.Context, cfg config.Config, reg *skills.Registry) error {
	pub := report.NewRegistryPublisher(cfg.RegistryReportURL)
	pub.Token = cfg.SinkToken
	if !pub.Enabled() {
		return nil
	}
	return pub.Publish(ctx, reg.List())
}

// failedNodes pulls the nodes a level error names. An error of any other shape names none,
// and the caller then reports the frontier alone — which is what happened before the engine
// carried this.
func failedNodes(err error) []string {
	var lvl *graph.LevelError
	if errors.As(err, &lvl) {
		return lvl.Nodes
	}
	return nil
}

// nestedRuns wires every subgraph node so its nested graph reports and journals as a run of
// its own, pointing back at the node it belongs to.
//
// Each execution gets a fresh run id: the same node running twice — a retry, a loop — is two
// nested runs, not one run reported or journalled twice. Reporting and journalling share
// that id (graph.WithChildRun mints it once and hands both hooks the same value) rather than
// minting it twice, so a consumer correlating the two never has to guess which report goes
// with which journal. The shape reported is read from the file the loader resolved; a graph
// built in Go carries no file and travels without a topology, exactly as an undeclared
// parent would.
//
// Reporting stays gated on the reporter being configured, as it always was. Journalling is
// not: the journal is this codebase's source of truth (see the package doc on cmd), so a
// nested run gets one whenever cfg's checkpoint store can be opened at all, with no separate
// switch to leave off by accident.
func nestedRuns(reg *topology.Registry, reporter *report.HTTPReporter, cfg config.Config, parentRun string) {
	reg.OnChildRun(buildChildRunHooks(reporter, cfg, parentRun))
}

// buildChildRunHooks is nestedRuns' factory, split out so it can be exercised directly: the
// only way to reach it through reg.OnChildRun is by building and running a real graph, which
// would make every case here — the fresh id per call, journalling without a reporter, best-
// effort degradation when the store cannot open — an integration test when each is a fact
// about this one function.
func buildChildRunHooks(reporter *report.HTTPReporter, cfg config.Config, parentRun string) func(nodeID, graphRef string) *graph.ChildRunHooks {
	return func(nodeID, graphRef string) *graph.ChildRunHooks {
		runID := newRunID()
		name := graphName(graphRef)

		// One store connection per execution, closed when the child returns (see
		// graph.ChildRunHooks.Close) — not one cached for the parent run's whole life. A
		// long-lived daemon runs many parents over time, and a connection kept open past
		// the one nested run it served would leak one per subgraph node the daemon ever
		// sees. A store that cannot be opened, or a recorder that cannot read its next
		// sequence number, costs this one child its own journal — never the run itself:
		// the parent's own checkpoint already captures the whole sub-run as one atomic
		// step (see subgraph.go's SubgraphNode doc), so this is additive record-keeping,
		// best-effort like describeTopology below.
		//
		// Reporting now needs the store too, and not only journalling: reportHook projects
		// the state it flattens from this same store (issue 11), so a child whose store
		// cannot be opened has nothing to derive a step event from either. That is a change
		// from before this issue, where a live nested run could still report over HTTP with
		// no journal at all — see this branch's PR body for why that is the right trade.
		s, err := openStore(cfg)
		if err != nil {
			return nil
		}
		hooks := &graph.ChildRunHooks{Close: func() { _ = s.Close() }}

		rec, err := newJournalRecorder(context.Background(), s, runID, name)
		if err != nil {
			hooks.Close()
			return nil
		}
		hooks.Event = multiEvent(rec.record)

		if reporter.Enabled() {
			stepHook := reporter.NestedHook(runID, name, describeTopology(graphRef),
				&report.Parent{RunID: parentRun, NodeID: nodeID})
			// A nested run has no per-level checkpoint hook the way the top-level run does
			// (checkpointHook) — record only flushes its own terminal event (closesTheRun),
			// so without an explicit flush first, reportHook would read back an empty
			// journal for every level except after the run has already finished. See
			// journalRecorder.flush.
			hooks.Step = multiStep(nestedFlushHook(rec), reportHook(s, runID, stepHook))
		}

		return hooks
	}
}

// nestedFlushHook writes a nested run's buffered level events to the store with no
// projection row, so the reportHook chained right after it (see buildChildRunHooks) reads
// back a journal that already includes the level that just closed.
func nestedFlushHook(rec *journalRecorder) graph.StepFunc {
	return func(ctx context.Context, _ graph.StepInfo, _ *graph.State) error {
		return rec.flush(ctx)
	}
}

// wireApproval binds a run's mailbox as the decision source for every approval node in
// its graph. A nil mailbox — the bare CLI's `run`/`resume`, which has no live steer
// surface — leaves approval nodes unconfigured, and the loader refuses to build such a
// graph rather than let it hang forever with nothing to answer it.
//
// The wait is bracketed through the same activity relay an agent node already uses
// (report.ActivityReporter, C10): without it, a run parked on an approval node reports
// nothing at all until a level completes — which never happens until someone decides it —
// so kern-ui would have no way to show a human the very decision they are meant to make.
// A pending approval is exactly the kind of "something is happening" the beacon already
// knows how to say.
func wireApproval(reg *topology.Registry, mailbox *steer.Mailbox, activity *activityRelay) {
	if mailbox == nil {
		return
	}
	reg.OnApproval(func(ctx context.Context, nodeID string) (graph.Decision, error) {
		activity.call(nodeID, true, "")
		defer activity.call(nodeID, false, "")
		return mailbox.AwaitDecision(ctx, nodeID)
	})
}
