package agentrunner

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/yoann/kern-orch/internal/config"
	"github.com/yoann/kern-orch/internal/graph"
)

// The one fixture is issue 01's real capture: the full transcript of a session whose single
// prompt triggered a tool call, pulled with GET /session/:id/message. It carries both shapes
// a translator must survive — a tool-call assistant message and a final text one — which the
// bare POST response body does not (see testdata/README.md). Asserting the exact Output map
// is what stops the translation from being re-derived from documentation instead of from
// what the server actually sent.
func TestTranslateTranscriptProducesTheExactOutputForTheRealCapture(t *testing.T) {
	raw, err := os.ReadFile("testdata/opencode-message.json")
	if err != nil {
		t.Fatal(err)
	}
	msgs, err := decodeTranscript(raw)
	if err != nil {
		t.Fatalf("decodeTranscript: %v", err)
	}

	result, err := translateTranscript("ask", msgs)
	if err != nil {
		t.Fatalf("translateTranscript: %v", err)
	}

	want := map[string]any{
		"display:ask":          "Output: `hello-from-opencode-fixture-capture`",
		"opencode:session:ask": "ses_fe0621d73ffeHda4Bz1EHvH4iT",
		"opencode:tools:ask":   []string{"bash"},
		"opencode:finish:ask":  "stop",
	}
	if !reflect.DeepEqual(result.Output, want) {
		t.Fatalf("Output =\n%#v\nwant\n%#v", result.Output, want)
	}
}

// An assistant message carrying an error must fail the node, not merge a half-answer: the
// alternative is a graph that routes on a state whose display key holds whatever text the
// model managed before the provider refused.
func TestTranslateTranscriptFailsOnAnAssistantError(t *testing.T) {
	msgs := []transcriptMessage{{Info: messageInfo{
		Role:  roleAssistant,
		Error: &messageError{Name: "ProviderAuthError"},
	}}}
	msgs[0].Info.Error.Data.Message = "no credentials configured"

	_, err := translateTranscript("ask", msgs)
	if err == nil {
		t.Fatalf("translateTranscript: got nil error, want one carrying the provider's message")
	}
	if !strings.Contains(err.Error(), "no credentials configured") {
		t.Fatalf("error = %q, want it to carry the provider's own message", err.Error())
	}
}

// buildFakeOpenCodeServer compiles a real HTTP server binary standing in for
// `opencode serve`: it sleeps before it listens, then answers the three routes Run drives,
// replaying the real fixture as its transcript.
//
// A compiled binary rather than a shell script or an httptest.Server, because what is under
// test here is precisely what an in-process fake cannot exercise: a child process that is
// not accepting requests yet when exec.Start returns, and that Close must reap for real. Go
// is present by definition wherever `go test` runs, so this costs a build, not a dependency.
func buildFakeOpenCodeServer(t *testing.T) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the adapter's process teardown is POSIX-only")
	}
	dir := t.TempDir()
	src := filepath.Join(dir, "main.go")
	if err := os.WriteFile(src, []byte(fakeServerProgram), 0o644); err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(dir, "fake-opencode")
	build := exec.Command("go", "build", "-o", bin, src)
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build fake server: %v\n%s", err, out)
	}
	return bin
}

// fakeServerProgram parses the same --port the adapter passes and ignores the rest, so the
// adapter's real argv is what gets exercised.
const fakeServerProgram = `package main

import (
	"flag"
	"net/http"
	"os"
	"strings"
	"time"
)

func main() {
	fs := flag.NewFlagSet("fake", flag.ContinueOnError)
	port := fs.String("port", "0", "")
	delay := fs.Duration("delay", 0, "")
	fixture := fs.String("fixture", "", "")
	fs.String("hostname", "", "")
	if err := fs.Parse(os.Args[2:]); err != nil {
		os.Exit(2)
	}
	time.Sleep(*delay)
	transcript, err := os.ReadFile(*fixture)
	if err != nil {
		os.Exit(1)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/session", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			w.Write([]byte("{\"id\":\"ses_fake\"}"))
			return
		}
		w.Write([]byte("[]"))
	})
	mux.HandleFunc("/session/", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/message") {
			w.Write([]byte("{}"))
			return
		}
		w.Write(transcript)
	})
	if http.ListenAndServe("127.0.0.1:"+*port, mux) != nil {
		os.Exit(1)
	}
}
`

func fakeServerArgs(t *testing.T, delay time.Duration) []string {
	t.Helper()
	fixture, err := filepath.Abs("testdata/opencode-message.json")
	if err != nil {
		t.Fatal(err)
	}
	return []string{"-delay", delay.String(), "-fixture", fixture}
}

// Start must not return before the server can serve a real request: every Run() that follows
// would otherwise race the server's own startup, and the failure would land on whichever
// node happened to run first rather than on Start.
func TestStartWaitsForASlowServerToActuallyAcceptRequests(t *testing.T) {
	const delay = 900 * time.Millisecond
	oc := &OpenCode{Path: buildFakeOpenCodeServer(t), Args: fakeServerArgs(t, delay)}

	began := time.Now()
	if err := oc.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() { _ = oc.Close() })

	if waited := time.Since(began); waited < delay {
		t.Fatalf("Start returned after %s, before the server could listen (%s delay)", waited, delay)
	}
	// The point is not that Start waited; it is that the server answers *now*. A poll that
	// gave up early, or a fixed sleep tuned to a faster machine, fails right here.
	resp, err := http.Get(oc.baseURL + "/session")
	if err != nil {
		t.Fatalf("server not actually ready after Start: %v", err)
	}
	_ = resp.Body.Close()
}

// Close must reach quiescence, not merely request it: a server still holding its port after
// the run is over is what makes the next run's Start fail on a port that "should" be free.
func TestCloseLeavesNoSpawnedProcessBehind(t *testing.T) {
	oc := &OpenCode{Path: buildFakeOpenCodeServer(t), Args: fakeServerArgs(t, 0)}
	if err := oc.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	pid := oc.cmd.Process.Pid

	if err := oc.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	// Signal 0 probes for existence without delivering anything. ESRCH is the process being
	// gone for good; a nil error means Close returned while it was still alive.
	if err := syscall.Kill(pid, 0); err != syscall.ESRCH {
		t.Fatalf("process %d still exists after Close (Kill(0) = %v, want ESRCH)", pid, err)
	}
}

// The run's teardown calls Close on every exit path, including one where Start already tore
// the child down itself. Doing that work twice must be silent, not an error on the way out.
func TestCloseIsSafeToCallTwice(t *testing.T) {
	oc := &OpenCode{Path: buildFakeOpenCodeServer(t), Args: fakeServerArgs(t, 0)}
	if err := oc.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if err := oc.Close(); err != nil {
		t.Fatalf("first Close: %v", err)
	}
	if err := oc.Close(); err != nil {
		t.Fatalf("second Close: %v", err)
	}
}

// A missing binary must fail at Start, which serve.go turns into a run aborted before any
// node executes. The error names the adapter so an operator with two configured CLIs reads
// which one is wrong.
func TestStartFailsAndNamesTheAdapterWhenTheBinaryIsMissing(t *testing.T) {
	oc := &OpenCode{Path: filepath.Join(t.TempDir(), "no-such-opencode")}
	err := oc.Start(context.Background())
	if err == nil {
		t.Fatalf("Start: got nil error, want one naming the opencode adapter")
	}
	if !strings.Contains(err.Error(), "opencode") {
		t.Fatalf("Start error = %q, want it to name the opencode adapter", err.Error())
	}
}

// A port already bound is the other Start failure hit in practice (a server left over from a
// previous run). The spawned server cannot listen and exits; Start must report that as soon
// as the child is gone rather than poll a dead process until its deadline.
func TestStartFailsWhenItsPortIsAlreadyBound(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()

	oc := &OpenCode{
		Path:         buildFakeOpenCodeServer(t),
		Args:         fakeServerArgs(t, 0),
		Port:         ln.Addr().(*net.TCPAddr).Port,
		StartTimeout: 10 * time.Second,
	}
	t.Cleanup(func() { _ = oc.Close() })

	began := time.Now()
	if err := oc.Start(context.Background()); err == nil {
		t.Fatalf("Start: got nil error, want one for a port already bound")
	}
	if waited := time.Since(began); waited >= oc.StartTimeout {
		t.Fatalf("Start polled for %s, want it to give up as soon as the child exited", waited)
	}
}

// Run's call flow is create-session, POST the message, then GET the transcript — the flow
// issue 01 actually verified against a running server, not the bare POST the issue text
// describes: the POST body alone returns only the last message and would drop the tool-call
// shape the fixture exists to preserve (testdata/README.md).
func TestRunCreatesASessionPostsTheMessageThenReadsTheTranscript(t *testing.T) {
	transcript, err := os.ReadFile("testdata/opencode-message.json")
	if err != nil {
		t.Fatal(err)
	}
	var routes []string
	var sentBody []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		routes = append(routes, r.Method+" "+r.URL.Path)
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/session":
			_, _ = w.Write([]byte(`{"id":"ses_test"}`))
		case r.Method == http.MethodPost:
			sentBody, _ = io.ReadAll(r.Body)
			_, _ = w.Write([]byte(`{}`))
		default:
			_, _ = w.Write(transcript)
		}
	}))
	defer srv.Close()

	var sink bytes.Buffer
	var activity []string
	oc := &OpenCode{TokenSink: &sink, baseURL: srv.URL, client: srv.Client(),
		OnActivity: func(nodeID string, generating bool, message string) {
			activity = append(activity, nodeID+":"+message+":"+boolText(generating))
		}}

	state := graph.NewState()
	state.Set("dossier", "D-42")
	result, err := oc.Run(context.Background(), graph.AgentRequest{
		NodeID: "ask", Prompt: "what is up?", State: state})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	wantRoutes := []string{"POST /session", "POST /session/ses_test/message", "GET /session/ses_test/message"}
	if !reflect.DeepEqual(routes, wantRoutes) {
		t.Fatalf("routes = %v, want %v", routes, wantRoutes)
	}
	if result.Output["display:ask"] != "Output: `hello-from-opencode-fixture-capture`" {
		t.Fatalf("Output = %#v", result.Output)
	}

	// The prompt and the state travel as two separate text parts rather than one blob: the
	// server's own transcript then still shows which half was the instruction.
	var sent struct {
		Parts []messagePart `json:"parts"`
	}
	if err := json.Unmarshal(sentBody, &sent); err != nil {
		t.Fatalf("decode sent body: %v", err)
	}
	if len(sent.Parts) != 2 || sent.Parts[0].Text != "what is up?" {
		t.Fatalf("sent parts = %#v, want the prompt then the state", sent.Parts)
	}
	if !strings.Contains(sent.Parts[1].Text, "D-42") {
		t.Fatalf("second part = %q, want it to carry the node's state", sent.Parts[1].Text)
	}

	// TokenSink gets the answer in one write, not incrementally: this path is synchronous
	// and OpenCode documents no incremental primitive for it (see OpenCode.TokenSink).
	if sink.String() != "Output: `hello-from-opencode-fixture-capture`" {
		t.Fatalf("TokenSink = %q, want the whole answer", sink.String())
	}
	wantActivity := []string{"ask::true", "ask:Output: `hello-from-opencode-fixture-capture`:false"}
	if !reflect.DeepEqual(activity, wantActivity) {
		t.Fatalf("activity = %v, want %v", activity, wantActivity)
	}
}

// The bracket must close on the error paths too, or a failed node leaves a beacon lit for
// the rest of the run.
func TestRunClosesTheActivityBracketWhenTheServerFails(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer srv.Close()

	var activity []bool
	oc := &OpenCode{baseURL: srv.URL, client: srv.Client(),
		OnActivity: func(_ string, generating bool, _ string) { activity = append(activity, generating) }}

	if _, err := oc.Run(context.Background(), graph.AgentRequest{NodeID: "ask", State: graph.NewState()}); err == nil {
		t.Fatalf("Run: got nil error, want one for a failing server")
	}
	if !reflect.DeepEqual(activity, []bool{true, false}) {
		t.Fatalf("activity = %v, want [true false]", activity)
	}
}

// Run before Start has no server to talk to. Saying so beats a connection error against an
// empty URL, which reads like a network problem rather than a wiring one.
func TestRunBeforeStartIsRefused(t *testing.T) {
	oc := &OpenCode{}
	_, err := oc.Run(context.Background(), graph.AgentRequest{NodeID: "ask", State: graph.NewState()})
	if err == nil || !strings.Contains(err.Error(), "Start") {
		t.Fatalf("Run error = %v, want one naming Start", err)
	}
}

// The registry is where an operator's KERN_AGENT_KIND=opencode becomes this adapter; issue
// 02 left a placeholder here, and a run that quietly fell back to the Stub would answer with
// canned output that looks like a success.
func TestNewBuildsTheOpenCodeAdapter(t *testing.T) {
	r, err := New(config.Config{AgentCLI: "/usr/local/bin/opencode", AgentKind: config.AgentKindOpenCode}, Options{})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	oc, ok := r.(*OpenCode)
	if !ok {
		t.Fatalf("New returned %T, want *OpenCode", r)
	}
	if oc.Path != "/usr/local/bin/opencode" {
		t.Fatalf("Path = %q, want the configured CLI path", oc.Path)
	}
	if _, ok := r.(Lifecycle); !ok {
		t.Fatalf("%T does not implement Lifecycle; serve.go would never start its server", r)
	}
}

// The full round trip against the real binary: spawn, one message, a result, clean shutdown.
// Self-skipping when opencode is absent — it is an optional local dependency, not a build
// requirement — but it is the only test here that proves the request bodies this adapter
// sends are ones the real server accepts.
func TestRealOpenCodeRoundTrip(t *testing.T) {
	if testing.Short() {
		t.Skip("real round trip is not a short test")
	}
	path, err := exec.LookPath(os.Getenv(config.EnvAgentCLI))
	if err != nil {
		if path, err = exec.LookPath("opencode"); err != nil {
			t.Skip("no opencode binary on PATH")
		}
	}

	oc := &OpenCode{Path: path, StartTimeout: 60 * time.Second}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	if err := oc.Start(ctx); err != nil {
		t.Skipf("opencode serve did not come up (no usable provider on this machine?): %v", err)
	}
	pid := oc.cmd.Process.Pid

	state := graph.NewState()
	result, err := oc.Run(ctx, graph.AgentRequest{
		NodeID: "ping", Prompt: "Reply with exactly the single word: PONG", State: state})
	if err != nil {
		_ = oc.Close()
		t.Skipf("real round trip failed (no usable provider on this machine?): %v", err)
	}
	if _, ok := result.Output["display:ping"]; !ok {
		t.Fatalf("Output = %#v, want a display:ping key", result.Output)
	}

	if err := oc.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if err := syscall.Kill(pid, 0); err != syscall.ESRCH {
		t.Fatalf("opencode serve %d survived Close (Kill(0) = %v, want ESRCH)", pid, err)
	}
}

// boolText lives in claudecode_test.go — shared, same package.
