package agentrunner

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/yoann/kern-orch/internal/config"
	"github.com/yoann/kern-orch/internal/graph"
)

// TestConcurrentRunCallsOpenDistinctSessionsWithoutSerializing is issue 06's core proof:
// graph.Engine's runLevel spawns one goroutine per frontier node and calls Run on the same
// *OpenCode instance from all of them at once (internal/cmd/runtime.go builds exactly one
// runner per run). Nothing in Run (see opencode.go) takes a lock or otherwise serializes
// callers, but that must be demonstrated, not asserted from reading the source.
//
// The proof is structural, not timed. The fake server deliberately blocks the *first*
// POST /session it receives until a *second*, distinct POST /session physically arrives.
// If Run queued concurrent callers behind any shared lock, the second call could never
// reach the server while the first sits inside that blocked handler -- the request simply
// would not exist yet -- and this test would time out and fail. There is no timing window
// to get lucky or unlucky on: either the second request reaches the server while the first
// is still in flight, or it does not.
func TestConcurrentRunCallsOpenDistinctSessionsWithoutSerializing(t *testing.T) {
	const n = 3
	var arrived int32
	secondArrived := make(chan struct{})
	var closeOnce sync.Once

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/session":
			count := atomic.AddInt32(&arrived, 1)
			if count == 1 {
				// The structural wait: this handler does not respond -- and so this call's
				// Run does not proceed -- until a second, independent session-creation
				// request has reached the server. A lock-serialized Run would never let
				// that second request happen, so this would time out instead of unblocking.
				select {
				case <-secondArrived:
				case <-time.After(5 * time.Second):
					t.Errorf("no second concurrent session-creation request arrived; Run appears serialized")
				}
			} else {
				closeOnce.Do(func() { close(secondArrived) })
			}
			fmt.Fprintf(w, `{"id":"ses-%d"}`, count)
		case r.Method == http.MethodPost:
			_, _ = w.Write([]byte(`{}`))
		default:
			// The transcript's own sessionID field is what translateTranscript records into
			// the state (opencode.go, opencode_wire.go) -- it must echo the session this
			// particular request's URL names, or every call's result would collapse onto one
			// fixed id and defeat the very distinctness this test checks.
			sid := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/session/"), "/message")
			fmt.Fprintf(w, `[{"info":{"id":"m1","sessionID":%q,"role":"assistant","finish":"stop"},`+
				`"parts":[{"type":"text","text":"pong"}]}]`, sid)
		}
	}))
	defer srv.Close()

	oc := &OpenCode{baseURL: srv.URL, client: srv.Client()}

	var wg sync.WaitGroup
	results := make([]graph.AgentResult, n)
	errs := make([]error, n)
	for i := range n {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			results[i], errs[i] = oc.Run(context.Background(), graph.AgentRequest{
				NodeID: fmt.Sprintf("node-%d", i), State: graph.NewState(),
			})
		}(i)
	}
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Fatalf("Run[%d]: %v", i, err)
		}
	}

	// Each call's own AgentResult must carry a distinct opencode:session:<nodeID> --
	// confirming the sessions really are per-call, not shared or reused across the fan-out.
	seen := make(map[string]bool, n)
	for i, res := range results {
		key := fmt.Sprintf("opencode:session:node-%d", i)
		id, _ := res.Output[key].(string)
		if id == "" {
			t.Fatalf("result[%d] carries no session id under %q: %#v", i, key, res.Output)
		}
		if seen[id] {
			t.Fatalf("session id %q reused across concurrent calls, want %d distinct sessions", id, n)
		}
		seen[id] = true
	}
	if len(seen) != n {
		t.Fatalf("got %d distinct sessions, want %d", len(seen), n)
	}
}

// TestRealOpenCodeFanOutGraphProducesIndependentResultsPerNode drives a real fan-out level
// (one entry ToolNode routing to several OpenCode agent nodes via graph.Static, exactly
// graph.Engine's runLevel shape) against a real `opencode serve`, the same optional local
// dependency TestRealOpenCodeRoundTrip uses. It is the acceptance criterion that a fake
// server cannot stand in for: proof that the real binary's session/message/transcript
// handling holds up when several sessions are open on it at once, not merely that this
// adapter's own HTTP calls are unlocked.
func TestRealOpenCodeFanOutGraphProducesIndependentResultsPerNode(t *testing.T) {
	if testing.Short() {
		t.Skip("real fan-out run is not a short test")
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
	defer func() { _ = oc.Close() }()

	words := []string{"ALPHA", "BRAVO", "CHARLIE"}
	g := graph.NewGraph()
	g.AddNode(graph.NewToolNode("fanout", func(context.Context, *graph.State) error { return nil }))
	g.SetEntry("fanout")
	nodeIDs := make([]string, len(words))
	for i, w := range words {
		id := "say-" + w
		nodeIDs[i] = id
		g.AddNode(graph.NewAgentNode(id, "Reply with exactly the single word: "+w, oc))
	}
	g.AddEdge("fanout", graph.Static(nodeIDs...))

	s := graph.NewState()
	if err := graph.NewEngine(g).Run(ctx, s); err != nil {
		t.Skipf("real fan-out run failed (no usable provider on this machine?): %v", err)
	}

	seen := make(map[string]bool, len(nodeIDs))
	for _, id := range nodeIDs {
		raw, ok := s.Get("opencode:session:" + id)
		sessionID, _ := raw.(string)
		if !ok || sessionID == "" {
			t.Fatalf("node %q: no session id recorded in the merged state", id)
		}
		if seen[sessionID] {
			t.Fatalf("session id %q reused across real fan-out nodes %v", sessionID, nodeIDs)
		}
		seen[sessionID] = true

		display, _ := s.Get("display:" + id)
		if text, _ := display.(string); text == "" {
			t.Fatalf("node %q produced no display output", id)
		}
	}
}
