package agentrunner

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os/exec"
	"strconv"
	"sync"
	"syscall"
	"time"

	"github.com/yoann/kern-orch/internal/graph"
)

// Defaults for the two waits this adapter owns. Both are generous rather than tight: they
// exist to turn a hang into a named error, not to police a slow machine. A cold `opencode
// serve` on a laptop that has to load its config and providers is routinely seconds.
const (
	defaultOpenCodeStartTimeout = 60 * time.Second
	openCodeReadyPollInterval   = 25 * time.Millisecond
	openCodeReadyProbeTimeout   = 1 * time.Second
	openCodeStopGrace           = 5 * time.Second
)

// OpenCode is the AgentRunner for the OpenCode CLI. Unlike Subprocess it speaks no
// stdin/stdout protocol at all: `opencode serve` is a long-lived HTTP server, and a node is
// one session's worth of HTTP calls against it.
//
// It therefore implements Lifecycle (issue 03) — and is the reason that interface exists.
// The server is started once per run and shut down once per run; restarting it per node
// would pay a multi-second startup for every node of the graph and throw away the process's
// warm state for nothing.
type OpenCode struct {
	Path string   // `opencode` binary path
	Args []string // extra args appended to `serve --hostname … --port …`
	Env  []string // child environment (nil => inherit parent)

	// Port pins the server's port. Zero reserves a free one at Start, which is the normal
	// case: the port is an implementation detail of one run, and pinning it by default would
	// make two concurrent runs on one machine collide over it.
	Port int

	// StartTimeout bounds the readiness wait; zero uses defaultOpenCodeStartTimeout.
	StartTimeout time.Duration

	Stderr io.Writer // the server's own logs; nil discards them

	// TokenSink receives the node's answer. This path is synchronous: OpenCode's
	// POST /session/:id/message returns when the turn is finished, and the epic's audit
	// found no documented incremental primitive for it (the event streams under /event are
	// out of this issue's scope). So the sink is written once, with the whole answer,
	// rather than incrementally — the contract "everything the model said reaches the sink"
	// holds; the contract "it arrives as it is produced" does not, and is not claimed.
	TokenSink io.Writer

	// OnActivity brackets the window during which the model is working on a node, exactly as
	// Subprocess.OnActivity documents it — including the stop call carrying the node's own
	// state["display:<nodeID>"] output. Here the bracket spans the whole session round trip,
	// which is the only granularity a synchronous call can honestly report.
	OnActivity func(nodeID string, generating bool, message string)

	baseURL string
	client  *http.Client
	cmd     *exec.Cmd

	// exited is closed once the child has been reaped. It is what lets both the readiness
	// poll and Close observe the process's real end rather than assume it.
	exited chan struct{}
	// stopOnce guards the teardown so Start's own cleanup and the run's Close can both run
	// it without racing or double-waiting.
	stopOnce sync.Once
}

// Start spawns `opencode serve` and returns only once that server answers a real request.
//
// The wait is a poll of an actual endpoint, never a sleep: the startup cost of `opencode
// serve` varies with the machine, the config, and the providers it loads, so any fixed
// duration is either a wasted second on a fast machine or a race on a slow one — and the
// race would surface as a failure on whichever node happened to run first, far from its
// cause.
func (r *OpenCode) Start(ctx context.Context) error {
	if r.cmd != nil {
		return fmt.Errorf("agentrunner: opencode: already started")
	}
	port := r.Port
	if port == 0 {
		free, err := freeTCPPort()
		if err != nil {
			return fmt.Errorf("agentrunner: opencode: reserve a port: %w", err)
		}
		port = free
	}
	r.baseURL = fmt.Sprintf("http://127.0.0.1:%d", port)
	if r.client == nil {
		r.client = &http.Client{}
	}

	args := append([]string{"serve", "--hostname", "127.0.0.1", "--port", strconv.Itoa(port)}, r.Args...)
	// Not exec.CommandContext: the server must outlive nothing but the run, and its teardown
	// is Close's job. Binding it to ctx would have a cancelled run kill the server from under
	// Close before Close could wait for it, which is the one thing that wait exists to do.
	cmd := exec.Command(r.Path, args...)
	cmd.Env = r.Env
	cmd.Stdout = r.Stderr
	cmd.Stderr = r.Stderr
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("agentrunner: opencode: start %q: %w", r.Path, err)
	}
	r.cmd = cmd
	r.exited = make(chan struct{})
	go func() {
		_ = cmd.Wait()
		close(r.exited)
	}()

	if err := r.waitReady(ctx); err != nil {
		// A server that never came up still left a process behind when it merely failed to
		// bind; tearing it down here means a failed Start owns no orphan.
		r.stopServer()
		return err
	}
	return nil
}

// waitReady polls until the server accepts a request, the child dies, or the wait runs out.
//
// The probe is GET /session — the same subsystem Run uses — rather than a dedicated health
// endpoint. A health route can answer while the session store is still coming up, and
// "healthy" that does not imply "can serve the one call this adapter makes" is exactly the
// false readiness this wait exists to rule out.
func (r *OpenCode) waitReady(ctx context.Context) error {
	timeout := r.StartTimeout
	if timeout == 0 {
		timeout = defaultOpenCodeStartTimeout
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	ticker := time.NewTicker(openCodeReadyPollInterval)
	defer ticker.Stop()
	for {
		// Checked before probing, not only after: a child that has already died cannot
		// answer, and probing it first would spend a full probe timeout finding that out.
		// The child is checked before probing as well as after: a child that has already
		// died cannot answer, and probing it first would spend a whole probe timeout finding
		// that out. Either way the report is immediate rather than at the deadline — a port
		// already bound is the common cause, and making an operator wait a minute for it
		// helps nobody.
		select {
		case <-r.exited:
			return r.exitedEarly()
		default:
		}
		if r.probeReady(ctx) {
			return nil
		}
		select {
		case <-r.exited:
			return r.exitedEarly()
		case <-ctx.Done():
			return fmt.Errorf("agentrunner: opencode: server at %s not ready: %w", r.baseURL, ctx.Err())
		case <-ticker.C:
		}
	}
}

// exitedEarly is the error for a server process that died before it ever served a request.
func (r *OpenCode) exitedEarly() error {
	return fmt.Errorf("agentrunner: opencode: %q exited before its server accepted a request "+
		"(is %s already bound?)", r.Path, r.baseURL)
}

// probeReady reports whether one request reached a server that answered it. Any HTTP status
// counts: the server responding at all is the fact being established, and a route that
// answers 404 is still a server that has finished starting.
func (r *OpenCode) probeReady(ctx context.Context) bool {
	ctx, cancel := context.WithTimeout(ctx, openCodeReadyProbeTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, r.baseURL+"/session", nil)
	if err != nil {
		return false
	}
	resp, err := r.client.Do(req)
	if err != nil {
		return false
	}
	_, _ = io.Copy(io.Discard, resp.Body)
	_ = resp.Body.Close()
	return true
}

// Close shuts the server down and returns only once its process is really gone.
//
// Signal, then wait, then kill if the wait ran out, then wait again — the same posture
// Subprocess already takes with cmd.Wait(). A Close that returned on the signal alone would
// leave the port held for an unbounded moment after the run reported itself finished, and
// the next run's Start would fail on a port that by every visible account should be free.
func (r *OpenCode) Close() error {
	r.stopServer()
	return nil
}

func (r *OpenCode) stopServer() {
	r.stopOnce.Do(func() {
		if r.cmd == nil || r.cmd.Process == nil {
			return
		}
		// An error here means the process is already gone, which is the outcome wanted; the
		// wait below settles it either way.
		_ = r.cmd.Process.Signal(syscall.SIGTERM)
		select {
		case <-r.exited:
			return
		case <-time.After(openCodeStopGrace):
		}
		_ = r.cmd.Process.Kill()
		<-r.exited
	})
}

// Run implements graph.AgentRunner: one session, one message, one transcript read.
//
// A session per call rather than one reused for the whole run. OpenCode sessions accumulate
// conversation history, so a shared one would feed every node the previous nodes' turns —
// the graph's own state is what carries context between nodes here, deliberately and
// explicitly, and a second implicit channel behind it would make a node's input depend on
// which siblings happened to run first. It is also the shape concurrent fan-out needs
// (issue 06).
//
// The call flow is create-session → POST the message → GET the transcript, which is what
// issue 01 verified against a running server. The POST's own response body carries only the
// session's last message, so it alone would drop the tool-call message entirely; the
// transcript read is how the tools a node ran survive into the state (testdata/README.md).
func (r *OpenCode) Run(ctx context.Context, req graph.AgentRequest) (graph.AgentResult, error) {
	if r.baseURL == "" {
		return graph.AgentResult{}, fmt.Errorf("agentrunner: opencode: Run before Start: no server to talk to")
	}

	// Deferred so the bracket closes on every path out of here, error ones included. result
	// is read by the closure at call time, so an error path narrates nothing rather than a
	// stale value — same reasoning as Subprocess.Run.
	var result graph.AgentResult
	r.activity(req.NodeID, true, "")
	defer func() { r.activity(req.NodeID, false, displayMessage(req.NodeID, result)) }()

	sessionID, err := r.createSession(ctx, req.NodeID)
	if err != nil {
		return graph.AgentResult{}, err
	}
	parts, err := promptParts(req)
	if err != nil {
		return graph.AgentResult{}, err
	}
	if _, err := r.postJSON(ctx, "/session/"+sessionID+"/message", map[string]any{"parts": parts}); err != nil {
		return graph.AgentResult{}, err
	}
	body, err := r.getJSON(ctx, "/session/"+sessionID+"/message")
	if err != nil {
		return graph.AgentResult{}, err
	}
	msgs, err := decodeTranscript(body)
	if err != nil {
		return graph.AgentResult{}, err
	}

	result, err = translateTranscript(req.NodeID, msgs)
	if err != nil {
		return graph.AgentResult{}, err
	}
	if r.TokenSink != nil {
		_, _ = io.WriteString(r.TokenSink, displayMessage(req.NodeID, result))
	}
	return result, nil
}

// promptParts turns the request into the text parts OpenCode accepts.
//
// Two parts, not one concatenated string: the server keeps them separate in its own
// transcript, so debugging a bad answer against the server's session shows which half was
// the instruction and which was the state it was given. A node whose state is empty sends
// one part, because an empty JSON object in the context window is noise.
func promptParts(req graph.AgentRequest) ([]map[string]string, error) {
	parts := []map[string]string{{"type": partTypeText, "text": req.Prompt}}
	if req.State == nil || len(req.State.Keys()) == 0 {
		return parts, nil
	}
	stateJSON, err := json.Marshal(req.State)
	if err != nil {
		return nil, fmt.Errorf("agentrunner: opencode: marshal state: %w", err)
	}
	parts = append(parts, map[string]string{
		"type": partTypeText,
		"text": "Current graph state (JSON):\n" + string(stateJSON),
	})
	return parts, nil
}

// createSession opens the session this call runs in. The node's ID becomes its title so a
// human reading `opencode` sessions afterwards can tell which graph node produced which.
func (r *OpenCode) createSession(ctx context.Context, nodeID string) (string, error) {
	body, err := r.postJSON(ctx, "/session", map[string]any{"title": "kern-orch " + nodeID})
	if err != nil {
		return "", err
	}
	var session struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(body, &session); err != nil {
		return "", fmt.Errorf("agentrunner: opencode: decode session: %w", err)
	}
	if session.ID == "" {
		return "", fmt.Errorf("agentrunner: opencode: created session carries no id")
	}
	return session.ID, nil
}

func (r *OpenCode) postJSON(ctx context.Context, path string, payload any) ([]byte, error) {
	encoded, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("agentrunner: opencode: marshal %s body: %w", path, err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, r.baseURL+path, bytes.NewReader(encoded))
	if err != nil {
		return nil, fmt.Errorf("agentrunner: opencode: build POST %s: %w", path, err)
	}
	req.Header.Set("Content-Type", "application/json")
	return r.do(req, path)
}

func (r *OpenCode) getJSON(ctx context.Context, path string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, r.baseURL+path, nil)
	if err != nil {
		return nil, fmt.Errorf("agentrunner: opencode: build GET %s: %w", path, err)
	}
	return r.do(req, path)
}

// do runs one request and returns its body, turning a non-2xx into an error carrying the
// server's own response — OpenCode explains a refused request in the body, and swallowing it
// would leave the operator with a bare status code.
func (r *OpenCode) do(req *http.Request, path string) ([]byte, error) {
	client := r.client
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("agentrunner: opencode: %s %s: %w", req.Method, path, err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("agentrunner: opencode: read %s %s: %w", req.Method, path, err)
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return nil, fmt.Errorf("agentrunner: opencode: %s %s: %s: %s",
			req.Method, path, resp.Status, bytes.TrimSpace(body))
	}
	return body, nil
}

func (r *OpenCode) activity(nodeID string, generating bool, message string) {
	if r.OnActivity != nil {
		r.OnActivity(nodeID, generating, message)
	}
}

// freeTCPPort reserves a port by binding it and releasing it immediately. The window between
// the release and the server's own bind is a real race, but a lost one is not silent: the
// server then exits without listening, and waitReady reports that as a Start failure rather
// than letting a run proceed against a server that is not there.
func freeTCPPort() (int, error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	port := ln.Addr().(*net.TCPAddr).Port
	return port, ln.Close()
}
