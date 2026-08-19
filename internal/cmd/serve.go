package cmd

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/yoann/kern-orch/internal/checkpoint"
	"github.com/yoann/kern-orch/internal/config"
	"github.com/yoann/kern-orch/internal/daemon"
	"github.com/yoann/kern-orch/internal/graph"
	"github.com/yoann/kern-orch/internal/report"
	"github.com/yoann/kern-orch/internal/skills"
	"github.com/yoann/kern-orch/internal/steer"
	"github.com/yoann/kern-orch/internal/tools"
	"github.com/yoann/kern-orch/internal/topology"
)

// preparedRun is a graph built and its reporters wired, ready to execute. Separating this
// from execution is what lets a caller fail fast: loading a bad graph or an absent file
// surfaces synchronously, before anything runs in the background where nobody could see it.
type preparedRun struct {
	graph            *graph.Graph
	graphPath        string
	name             string
	requester        string
	dossier          string
	mailbox          *steer.Mailbox
	reporter         *report.HTTPReporter
	activity         *activityRelay
	activityReporter *report.ActivityReporter
}

// prepareRun builds the graph and wires every reporter run/resume/the daemon share. It
// touches no store and starts no engine — see preparedRun.
//
// mailbox is C6's steer surface for this run — nil for the bare CLI's `run`/`resume`,
// which has nothing live to answer an approval or a nudge through. A graph containing an
// approval node refuses to load in that case (see wireApproval), rather than hang forever.
func prepareRun(cfg config.Config, runID, graphPath, requester, dossier string, mailbox *steer.Mailbox) (*preparedRun, error) {
	activity := &activityRelay{}
	reporter := report.NewHTTP(cfg.StepReportURL)
	reporter.Token = cfg.SinkToken
	reporter.Requester = requester
	reporter.Dossier = dossier

	// Wired before the graph is built, not after: a subgraph node receives its hook at
	// construction time.
	reg := builtinRegistry(newRunner(cfg, activity), cfg)
	nestedRuns(reg, reporter, cfg, runID)
	wireApproval(reg, mailbox, activity)

	g, err := topology.LoadFile(graphPath, reg)
	if err != nil {
		return nil, err
	}

	name := graphName(graphPath)
	activityReporter := report.NewActivityReporter(cfg.ActivityReportURL)
	activityReporter.Token = cfg.SinkToken
	activity.fn = func(nodeID string, generating bool, message string) {
		activityReporter.Report(context.Background(), runID, name, nodeID, generating, message)
	}

	return &preparedRun{
		graph: g, graphPath: graphPath, name: name, requester: requester, dossier: dossier, mailbox: mailbox,
		reporter: reporter, activity: activity, activityReporter: activityReporter,
	}, nil
}

// run executes a prepared graph to completion — fresh if resume is nil, continued from a
// replayed resume point otherwise — and reports a failure if there is one. It is the single
// implementation `run`, `resume` and the daemon all call: none of the three may drift from
// how a report is flushed or a failure is announced.
//
// It takes a checkpoint.ResumePoint rather than a checkpoint.Record so that the state it
// continues from is the one replayed from the journal. A Record also carries the cached
// row's state, and this is the path where continuing from a state the run never had is
// unrecoverable — so the type that reaches here has no such field to reach for.
func (p *preparedRun) run(ctx context.Context, store *checkpoint.SQLiteStore, runID string, resume *checkpoint.ResumePoint) error {
	defer p.reporter.Flush()
	defer p.activityReporter.Flush()

	// Built before the engine, and a failure here stops the run before it starts: the
	// recorder reads where this run's journal left off, and a run that cannot know its own
	// next sequence number cannot be recorded at all. Starting anyway would produce a run
	// whose events are refused level after level.
	recorder, err := newJournalRecorder(ctx, store, runID, p.name)
	if err != nil {
		return err
	}

	steps := &stepCounter{}
	hook := multiStep(
		checkpointHook(recorder, p.graphPath, p.requester, p.dossier),
		steps.count,
		p.reporter.Hook(runID, p.name, describeTopology(p.graphPath)),
	)

	// OnEvent is what makes the journal exist at all: until it is registered the engine's
	// emit is the nil no-op. It is chained through multiEvent even with a single hook, so a
	// later observer (issue 11's reporter) is added without touching this line.
	engine := graph.NewEngine(p.graph).OnStep(hook).OnEvent(multiEvent(recorder.record))
	if p.mailbox != nil {
		engine.OnBeforeLevel(func(ctx context.Context, s *graph.State) error {
			// Recorded, not just applied: a nudge is the one write to the shared state that
			// passes through no node, so the journal would otherwise never hear of it — and
			// the row, now derived from the journal, would lose the very key the steer
			// endpoint was called to set.
			return recorder.recordNudge(ctx, p.mailbox.DrainNudges(s))
		})
	}

	if resume != nil {
		steps.last = resume.Step
		err = engine.RunFrom(ctx, resume.State, resume.Frontier)
	} else {
		err = engine.Run(ctx, graph.NewState())
	}
	if err != nil {
		p.reporter.ReportFailure(ctx, runID, p.name, steps.last, steps.frontier, failedNodes(err), err.Error())
	}
	return err
}

// daemonRunner implements daemon.Runner using exactly the engine wiring the CLI's `run` and
// `resume` commands use. It is the only place internal/daemon's abstract Runner meets a real
// graph — the daemon package itself knows nothing of graphs, checkpoints or reporters.
type daemonRunner struct {
	cfg   config.Config
	store *checkpoint.SQLiteStore

	mu        sync.Mutex
	mailboxes map[string]*steer.Mailbox
}

// registerMailbox creates and stores the mailbox a live run's steer endpoints reach it
// through, keyed by run id. Removed once the run's own goroutine finishes (see StartRun/
// ResumeRun) — a finished run has nothing left to steer.
func (d *daemonRunner) registerMailbox(runID string, cancel context.CancelFunc) *steer.Mailbox {
	m := steer.NewMailbox(cancel)
	d.mu.Lock()
	if d.mailboxes == nil {
		d.mailboxes = make(map[string]*steer.Mailbox)
	}
	d.mailboxes[runID] = m
	d.mu.Unlock()
	return m
}

func (d *daemonRunner) unregisterMailbox(runID string) {
	d.mu.Lock()
	delete(d.mailboxes, runID)
	d.mu.Unlock()
}

func (d *daemonRunner) mailboxFor(runID string) (*steer.Mailbox, bool) {
	d.mu.Lock()
	defer d.mu.Unlock()
	m, ok := d.mailboxes[runID]
	return m, ok
}

// StartRun prepares and validates the graph synchronously — a caller learns about a bad
// path or a bad graph immediately — then runs it in the background. A queued checkpoint is
// written before returning, so a status query racing the response never finds nothing.
// requester is recorded on every checkpoint; empty leaves the run open to any actor.
func (d *daemonRunner) StartRun(ctx context.Context, graphPath, requester string) (string, error) {
	abs, err := filepath.Abs(graphPath)
	if err != nil {
		return "", err
	}
	runID := newRunID()

	// The run's own context, not ctx: the HTTP request that started it returns long
	// before the run does. Its cancel func is C6's stop — the same shape agentrunner's
	// exec.CommandContext already uses to kill an in-flight subprocess node.
	runCtx, cancel := context.WithCancel(context.Background())
	mailbox := d.registerMailbox(runID, cancel)

	// StartRun has no dossier of its own — it is the CLI/status-only path (`run`,
	// `resume`), never called with one; Dispatch is where a caller supplies a dossier.
	prepared, err := prepareRun(d.cfg, runID, abs, requester, "", mailbox)
	if err != nil {
		cancel()
		d.unregisterMailbox(runID)
		return "", err
	}

	if err := d.store.Save(ctx, checkpoint.Record{
		RunID: runID, Step: checkpoint.QueuedStep, State: graph.NewState(),
		Status: checkpoint.StatusQueued, GraphPath: abs, Requester: requester,
	}); err != nil {
		cancel()
		d.unregisterMailbox(runID)
		return "", err
	}

	go func() {
		defer d.unregisterMailbox(runID)
		if err := prepared.run(runCtx, d.store, runID, nil); err != nil {
			slog.Error("kern-orch: run failed", "run_id", runID, "error", err)
			return
		}
		slog.Info("kern-orch: run completed", "run_id", runID)
	}()
	return runID, nil
}

// ResumeRun continues a run in the background, from the state its journal replays to. A run
// with an empty frontier is already complete — a no-op, not an error, matching what
// `kern-orch resume` tells a human.
//
// It goes through the same checkpoint.ResumePoint as the CLI's `resume`, which is what makes
// POST /api/v1/runs/{id}/resume and the command line one operation rather than two
// implementations that agree until one of them is changed.
func (d *daemonRunner) ResumeRun(ctx context.Context, runID string) error {
	resume, ok, err := d.store.ResumePoint(ctx, runID)
	if err != nil {
		return err
	}
	if !ok {
		return daemon.ErrUnknownRun
	}
	if len(resume.Frontier) == 0 {
		return nil
	}
	if resume.GraphPath == "" {
		return fmt.Errorf("run %q has no recorded graph path", runID)
	}

	runCtx, cancel := context.WithCancel(context.Background())
	mailbox := d.registerMailbox(runID, cancel)

	prepared, err := prepareRun(d.cfg, runID, resume.GraphPath, resume.Requester, resume.Dossier, mailbox)
	if err != nil {
		cancel()
		d.unregisterMailbox(runID)
		return err
	}

	go func() {
		defer d.unregisterMailbox(runID)
		if err := prepared.run(runCtx, d.store, runID, &resume); err != nil {
			slog.Error("kern-orch: resumed run failed", "run_id", runID, "error", err)
			return
		}
		slog.Info("kern-orch: resumed run completed", "run_id", runID)
	}()
	return nil
}

func (d *daemonRunner) ListRuns(ctx context.Context) ([]checkpoint.Summary, error) {
	return d.store.List(ctx)
}

func (d *daemonRunner) GetRun(ctx context.Context, runID string) (checkpoint.Record, bool, error) {
	return d.store.Latest(ctx, runID)
}

// authorized reports whether actor may steer a run whose checkpoint names requester —
// empty means open, the default every run has until a caller supplies one.
func authorized(requester, actor string) bool {
	return requester == "" || requester == actor
}

// notLive is what StopRun/Nudge/Decide return for a run that exists but has no mailbox —
// already finished, or never actually started live in this process (e.g. right after a
// restart, before anyone has resumed it). Steering nothing is not the same fact as
// steering an unknown run, but v1 does not yet give it its own HTTP status: it reads as
// 400, a request that cannot be satisfied right now.
func notLive(runID string) error {
	return fmt.Errorf("run %q is not currently live", runID)
}

// StopRun cancels a live run's context. The same mechanism already kills an in-flight
// subprocess node (agentrunner.Subprocess uses exec.CommandContext) — stopping a run is
// just that, triggered by a person instead of a timeout.
func (d *daemonRunner) StopRun(ctx context.Context, runID, actor string) error {
	rec, ok, err := d.store.Latest(ctx, runID)
	if err != nil {
		return err
	}
	if !ok {
		return daemon.ErrUnknownRun
	}
	if !authorized(rec.Requester, actor) {
		return daemon.ErrForbidden
	}
	m, ok := d.mailboxFor(runID)
	if !ok {
		return notLive(runID)
	}
	m.Stop()
	return nil
}

// Nudge queues a state key/value for the next level of a live run to pick up.
func (d *daemonRunner) Nudge(ctx context.Context, runID, actor, key string, value any) error {
	rec, ok, err := d.store.Latest(ctx, runID)
	if err != nil {
		return err
	}
	if !ok {
		return daemon.ErrUnknownRun
	}
	if !authorized(rec.Requester, actor) {
		return daemon.ErrForbidden
	}
	m, ok := d.mailboxFor(runID)
	if !ok {
		return notLive(runID)
	}
	m.Nudge(key, value)
	return nil
}

// Decide answers a pending approval node.
func (d *daemonRunner) Decide(ctx context.Context, runID, nodeID, actor, decision string) error {
	rec, ok, err := d.store.Latest(ctx, runID)
	if err != nil {
		return err
	}
	if !ok {
		return daemon.ErrUnknownRun
	}
	if !authorized(rec.Requester, actor) {
		return daemon.ErrForbidden
	}
	d2, err := parseDecision(decision)
	if err != nil {
		return err
	}
	m, ok := d.mailboxFor(runID)
	if !ok {
		return notLive(runID)
	}
	if err := m.Decide(nodeID, d2); err != nil {
		if errors.Is(err, steer.ErrNoPendingDecision) {
			return daemon.ErrUnknownNode
		}
		return err
	}
	return nil
}

// parseDecision validates the wire value against the two the engine understands — an
// invalid one is the caller's mistake, reported as such rather than silently defaulted.
func parseDecision(s string) (graph.Decision, error) {
	switch graph.Decision(s) {
	case graph.Approved, graph.Refused:
		return graph.Decision(s), nil
	default:
		return "", fmt.Errorf("invalid decision %q (want %q or %q)", s, graph.Approved, graph.Refused)
	}
}

// Dispatch resolves an explicit `/skill text…` chat command (C6). A tool skill delegates
// straight to the same invocation C5 already built; an agent skill launches a new
// one-node run whose whole prompt is text — no template, nothing to configure, matching
// what a skill actually carries today.
func (d *daemonRunner) Dispatch(ctx context.Context, skillName, text, requester, dossier string) (daemon.DispatchResult, error) {
	reg, err := skills.LoadMerged(d.cfg.SkillsDir, d.cfg.CustomSkillsDir)
	if err != nil {
		return daemon.DispatchResult{}, err
	}
	sk, ok := reg.Get(skillName)
	if !ok {
		return daemon.DispatchResult{}, &daemon.UnknownSkillError{Known: skillNames(reg)}
	}

	switch sk.Type {
	case skills.TypeTool:
		if len(sk.Command) == 0 {
			return daemon.DispatchResult{}, &daemon.UnknownSkillError{Known: skillNames(reg)}
		}
		input, err := dispatchInput(sk, text)
		if err != nil {
			return daemon.DispatchResult{}, err
		}
		runner := &tools.Runner{Stderr: os.Stderr}
		result, err := runner.Invoke(ctx, sk, input)
		if err != nil {
			return daemon.DispatchResult{}, err
		}
		return daemon.DispatchResult{Kind: "tool", Result: &result}, nil

	case skills.TypeAgent:
		runID := newRunID()
		runCtx, cancel := context.WithCancel(context.Background())
		mailbox := d.registerMailbox(runID, cancel)

		var prepared *preparedRun
		if sk.Graph != "" {
			// A fixed multi-node pipeline (e.g. an approval gate between two agent
			// steps) rather than the one-node ad-hoc run below — same file-loading
			// path `run`/`resume` already use. The chat's text becomes the entry
			// node's input via the existing nudge mechanism: OnBeforeLevel drains it
			// into state before the first level runs, so no new plumbing is needed
			// to get free text from the chat into a file-defined graph.
			prepared, err = prepareRun(d.cfg, runID, sk.Graph, requester, dossier, mailbox)
			if err == nil {
				mailbox.Nudge("message", text)
			}
		} else {
			prepared, err = prepareAdhocRun(d.cfg, runID, skillName, text, requester, dossier, mailbox)
		}
		if err != nil {
			cancel()
			d.unregisterMailbox(runID)
			return daemon.DispatchResult{}, err
		}
		if err := d.store.Save(ctx, checkpoint.Record{
			RunID: runID, Step: checkpoint.QueuedStep, State: graph.NewState(),
			Status: checkpoint.StatusQueued, Requester: requester, Dossier: dossier,
		}); err != nil {
			cancel()
			d.unregisterMailbox(runID)
			return daemon.DispatchResult{}, err
		}

		go func() {
			defer d.unregisterMailbox(runID)
			if err := prepared.run(runCtx, d.store, runID, nil); err != nil {
				slog.Error("kern-orch: dispatched run failed", "run_id", runID, "error", err)
				return
			}
			slog.Info("kern-orch: dispatched run completed", "run_id", runID)
		}()
		return daemon.DispatchResult{Kind: "run", RunID: runID}, nil

	default:
		return daemon.DispatchResult{}, fmt.Errorf("dispatch: skill %q has unsupported type %q", skillName, sk.Type)
	}
}

// dispatchInput maps free text onto a tool skill's declared params for chat dispatch: no
// required param means text is ignored, exactly one means text is its whole value, more
// than one has no way to split unambiguously — refused rather than guessed at.
func dispatchInput(sk skills.Skill, text string) (map[string]any, error) {
	var only skills.Param
	required := 0
	for _, p := range sk.Params {
		if p.Required {
			required++
			only = p
		}
	}
	switch required {
	case 0:
		return nil, nil
	case 1:
		return map[string]any{only.Name: text}, nil
	default:
		return nil, fmt.Errorf("dispatch: skill %q needs several values, not yet usable from the chat", sk.Name)
	}
}

// skillNames lists every loaded skill's name, sorted (List already sorts) — what an
// unknown-skill error shows so a mistyped command reveals what is real.
func skillNames(reg *skills.Registry) []string {
	list := reg.List()
	names := make([]string, len(list))
	for i, sk := range list {
		names[i] = sk.Name
	}
	return names
}

// prepareAdhocRun builds a one-node graph for a single agent skill dispatched from chat —
// no YAML file, no template: text is the whole prompt. It shares every other seam with a
// file-loaded run (same runner construction, same reporter/activity wiring); only how the
// graph itself is built differs.
func prepareAdhocRun(cfg config.Config, runID, skillName, prompt, requester, dossier string, mailbox *steer.Mailbox) (*preparedRun, error) {
	activity := &activityRelay{}
	reporter := report.NewHTTP(cfg.StepReportURL)
	reporter.Token = cfg.SinkToken
	reporter.Requester = requester
	reporter.Dossier = dossier
	runner := newRunner(cfg, activity)

	g := graph.NewGraph().SetEntry(skillName).AddNode(graph.NewAgentNode(skillName, prompt, runner))
	if err := g.Validate(); err != nil {
		return nil, err
	}

	activityReporter := report.NewActivityReporter(cfg.ActivityReportURL)
	activityReporter.Token = cfg.SinkToken
	activity.fn = func(nodeID string, generating bool, message string) {
		activityReporter.Report(context.Background(), runID, skillName, nodeID, generating, message)
	}

	return &preparedRun{
		graph: g, graphPath: "", name: skillName, requester: requester, dossier: dossier, mailbox: mailbox,
		reporter: reporter, activity: activity, activityReporter: activityReporter,
	}, nil
}

// ListTools returns every loaded skill invocable as a tool (type: tool, a declared
// command). Skills are re-read on every call, same as list-skills and the registry
// publisher — a directory, not a store with its own change notifications.
func (d *daemonRunner) ListTools(ctx context.Context) ([]tools.Spec, error) {
	reg, err := skills.LoadMerged(d.cfg.SkillsDir, d.cfg.CustomSkillsDir)
	if err != nil {
		return nil, err
	}
	var specs []tools.Spec
	for _, sk := range reg.List() {
		if sk.Type != skills.TypeTool || len(sk.Command) == 0 {
			continue
		}
		specs = append(specs, toolSpec(sk))
	}
	return specs, nil
}

// InvokeTool runs the named tool skill's command and returns its display value. A name
// that is not a loaded, command-backed tool skill is daemon.ErrUnknownTool — the router
// maps that to 404 rather than a caller having to parse an error string.
func (d *daemonRunner) InvokeTool(ctx context.Context, name string, input map[string]any) (tools.Result, error) {
	reg, err := skills.LoadMerged(d.cfg.SkillsDir, d.cfg.CustomSkillsDir)
	if err != nil {
		return tools.Result{}, err
	}
	sk, ok := reg.Get(name)
	if !ok || sk.Type != skills.TypeTool || len(sk.Command) == 0 {
		return tools.Result{}, daemon.ErrUnknownTool
	}
	runner := &tools.Runner{Stderr: os.Stderr}
	return runner.Invoke(ctx, sk, input)
}

// CreateSkill writes a new agent skill into the custom tier (C11). The freshly-merged
// registry the collision check reads is also what gets re-published afterwards — best
// effort, same as startup's own publish — so a signed-in account sees their creation in
// the Grimoire without waiting for the next run to carry the catalogue along.
func (d *daemonRunner) CreateSkill(ctx context.Context, name, description, createdBy string, steps []skills.Step) (skills.Skill, error) {
	reg, err := skills.LoadMerged(d.cfg.SkillsDir, d.cfg.CustomSkillsDir)
	if err != nil {
		return skills.Skill{}, err
	}
	sk, err := skills.Create(d.cfg.CustomSkillsDir, reg, name, description, createdBy, steps)
	if err != nil {
		return skills.Skill{}, err
	}
	d.republishSkills(ctx)
	return sk, nil
}

// DeleteSkill removes a created skill, refusing anyone but its own creator.
// daemon.ErrUnknownSkill covers both "no such skill" and "not a custom one" — a shipped
// skill was never a candidate for this path, so there is nothing more specific to say.
func (d *daemonRunner) DeleteSkill(ctx context.Context, name, requestedBy string) error {
	reg, err := skills.LoadMerged(d.cfg.SkillsDir, d.cfg.CustomSkillsDir)
	if err != nil {
		return err
	}
	sk, ok := reg.Get(name)
	if !ok || !sk.Custom {
		return daemon.ErrUnknownSkill
	}
	if err := skills.Delete(sk, requestedBy); err != nil {
		return daemon.ErrNotSkillOwner
	}
	d.republishSkills(ctx)
	return nil
}

// republishSkills pushes the freshly-changed catalogue, best-effort like every other
// publish in this file — a dead sink must never turn a real create/delete into a failure.
func (d *daemonRunner) republishSkills(ctx context.Context) {
	reg, err := skills.LoadMerged(d.cfg.SkillsDir, d.cfg.CustomSkillsDir)
	if err != nil {
		slog.Warn("kern-orch: reload skills catalogue after write", "error", err)
		return
	}
	if err := publishRegistry(ctx, d.cfg, reg); err != nil {
		slog.Warn("kern-orch: publish skills catalogue after write", "error", err)
	}
}

// Upload saves an uploaded document under cfg.UploadDir and returns its local path — the
// same "text IS the document path" convention a chat command or courtage-extraction's
// Telegram listener already dispatch with, just a third real way for that path to exist
// on disk. filepath.Base strips any directory component the client sent (a browser's
// File.name can carry one on some platforms) — never trust a client-supplied path as
// anything but a display name.
func (d *daemonRunner) Upload(_ context.Context, filename string, content io.Reader) (string, error) {
	if err := os.MkdirAll(d.cfg.UploadDir, 0o755); err != nil {
		return "", fmt.Errorf("upload: create %q: %w", d.cfg.UploadDir, err)
	}
	safeName := filepath.Base(filename)
	if safeName == "." || safeName == "/" || safeName == "" {
		safeName = "document"
	}
	dest := filepath.Join(d.cfg.UploadDir, fmt.Sprintf("%d_%s", time.Now().UnixNano(), safeName))

	f, err := os.Create(dest)
	if err != nil {
		return "", fmt.Errorf("upload: create %q: %w", dest, err)
	}
	defer f.Close()
	if _, err := io.Copy(f, content); err != nil {
		return "", fmt.Errorf("upload: write %q: %w", dest, err)
	}
	return dest, nil
}

// toolSpec translates a skills.Skill into the narrower shape a tool caller needs — the
// same edge-translation reasoning as kern-notify's kernui.Run: a caller asking what a tool
// needs should not also learn the skill catalogue's own internal shape.
func toolSpec(sk skills.Skill) tools.Spec {
	params := make([]tools.Param, len(sk.Params))
	for i, p := range sk.Params {
		params[i] = tools.Param{Name: p.Name, Type: p.Type, Required: p.Required}
	}
	return tools.Spec{Name: sk.Name, Description: sk.Description, Params: params}
}

const shutdownGrace = 10 * time.Second

func newServeCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "serve",
		Short: "Run kern-orch as a long-lived service, accepting runs over HTTP",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg := config.FromEnv()

			if err := checkServeExposure(cfg.ServeAddr, cfg.ServeToken); err != nil {
				return err
			}

			store, err := openStore(cfg)
			if err != nil {
				return err
			}
			defer store.Close()

			// Same reasoning as `run`'s own call: the catalogue rides along so kern-ui's
			// Grimoire has something to show without a human running `publish-skills` by
			// hand first. A daemon runs for hours or days, so this fires once at startup
			// rather than per-dispatch — best-effort, a dead sink must never stop the
			// server from listening.
			if reg, err := skills.LoadMerged(cfg.SkillsDir, cfg.CustomSkillsDir); err != nil {
				slog.Warn("kern-orch: load skills catalogue", "error", err)
			} else if err := publishRegistry(cmd.Context(), cfg, reg); err != nil {
				slog.Warn("kern-orch: publish skills catalogue", "error", err)
			}

			runner := &daemonRunner{cfg: cfg, store: store}
			srv := &http.Server{
				Addr:              cfg.ServeAddr,
				Handler:           daemon.NewRouter(runner, cfg.ServeToken),
				ReadHeaderTimeout: 5 * time.Second,
			}

			ctx, stop := signal.NotifyContext(cmd.Context(), syscall.SIGINT, syscall.SIGTERM)
			defer stop()

			errc := make(chan error, 1)
			go func() {
				slog.Info("kern-orch: serving", "addr", cfg.ServeAddr,
					"authenticated", cfg.ServeToken != "")
				if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
					errc <- err
					return
				}
				errc <- nil
			}()

			select {
			case err := <-errc:
				return err
			case <-ctx.Done():
				slog.Info("kern-orch: shutting down")
				shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownGrace)
				defer cancel()
				// Runs already in flight keep going in their own goroutines and checkpoint
				// as they always do; this only stops the server from taking new requests.
				return srv.Shutdown(shutdownCtx)
			}
		},
	}
}

// checkServeExposure refuses a public address with no credential. A warning would scroll
// past on the first busy day; a process that will not start does not. kern-ui enforces the
// same rule on its own API for the same reason — re-derived here, not shared: neither brick
// depends on the other's internals.
func checkServeExposure(addr, token string) error {
	if !isPublicAddr(addr) || token != "" {
		return nil
	}
	return fmt.Errorf(
		"refusing to listen on %s with no credential: set %s.\n"+
			"Anyone who can reach this address could start, resume or read every run.\n"+
			"To run locally instead, leave %s at 127.0.0.1:7070",
		addr, config.EnvServeToken, config.EnvServeAddr)
}

// isPublicAddr reports whether addr can be reached from another machine. An empty host is
// the trap: `:7070` reads as innocent and binds every interface.
func isPublicAddr(addr string) bool {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		host = addr
	}
	host = strings.Trim(host, "[]")

	switch host {
	case "":
		return true
	case "localhost":
		return false
	}
	if ip := net.ParseIP(host); ip != nil {
		return !ip.IsLoopback()
	}
	return true
}
