package daemon

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/yoann/kern-orch/internal/checkpoint"
	"github.com/yoann/kern-orch/internal/tools"
)

// fakeRunner is a Runner a test can script without touching the engine or a real database.
type fakeRunner struct {
	startID  string
	startErr error
	started  []string // graph paths passed to StartRun

	resumeErr     error
	resumed       []string // run ids passed to ResumeRun
	resumeUnknown bool

	runs    []checkpoint.Summary
	listErr error

	get    checkpoint.Record
	getOK  bool
	getErr error

	toolSpecs    []tools.Spec
	listToolsErr error

	invoked       []invocation // calls received by InvokeTool
	invokeUnknown bool
	invokeResult  tools.Result
	invokeErr     error

	startedRequesters []string // requester passed to each StartRun call

	stopped       []call
	stopUnknown   bool
	stopForbidden bool
	stopErr       error

	nudged         []nudgeCall
	nudgeUnknown   bool
	nudgeForbidden bool
	nudgeErr       error

	decided         []decideCall
	decideUnknown   bool
	decideForbidden bool
	decideErr       error

	dispatched           []dispatchCall
	dispatchUnknownSkill *UnknownSkillError
	dispatchResult       DispatchResult
	dispatchErr          error

	uploaded   []uploadCall
	uploadPath string
	uploadErr  error
}

type invocation struct {
	name  string
	input map[string]any
}

type call struct{ runID, actor string }

type nudgeCall struct {
	runID, actor, key string
	value             any
}

type decideCall struct{ runID, nodeID, actor, decision string }

type dispatchCall struct{ skill, text, requester, dossier string }

func (f *fakeRunner) StartRun(_ context.Context, graphPath, requester string) (string, error) {
	f.started = append(f.started, graphPath)
	f.startedRequesters = append(f.startedRequesters, requester)
	if f.startErr != nil {
		return "", f.startErr
	}
	return f.startID, nil
}

func (f *fakeRunner) StopRun(_ context.Context, runID, actor string) error {
	f.stopped = append(f.stopped, call{runID, actor})
	switch {
	case f.stopUnknown:
		return ErrUnknownRun
	case f.stopForbidden:
		return ErrForbidden
	default:
		return f.stopErr
	}
}

func (f *fakeRunner) Nudge(_ context.Context, runID, actor, key string, value any) error {
	f.nudged = append(f.nudged, nudgeCall{runID, actor, key, value})
	switch {
	case f.nudgeUnknown:
		return ErrUnknownRun
	case f.nudgeForbidden:
		return ErrForbidden
	default:
		return f.nudgeErr
	}
}

func (f *fakeRunner) Decide(_ context.Context, runID, nodeID, actor, decision string) error {
	f.decided = append(f.decided, decideCall{runID, nodeID, actor, decision})
	switch {
	case f.decideUnknown:
		return ErrUnknownNode
	case f.decideForbidden:
		return ErrForbidden
	default:
		return f.decideErr
	}
}

func (f *fakeRunner) Dispatch(_ context.Context, skill, text, requester, dossier string) (DispatchResult, error) {
	f.dispatched = append(f.dispatched, dispatchCall{skill, text, requester, dossier})
	if f.dispatchUnknownSkill != nil {
		return DispatchResult{}, f.dispatchUnknownSkill
	}
	if f.dispatchErr != nil {
		return DispatchResult{}, f.dispatchErr
	}
	return f.dispatchResult, nil
}

func (f *fakeRunner) ResumeRun(_ context.Context, runID string) error {
	f.resumed = append(f.resumed, runID)
	if f.resumeUnknown {
		return ErrUnknownRun
	}
	return f.resumeErr
}

func (f *fakeRunner) ListRuns(context.Context) ([]checkpoint.Summary, error) {
	return f.runs, f.listErr
}

func (f *fakeRunner) GetRun(context.Context, string) (checkpoint.Record, bool, error) {
	return f.get, f.getOK, f.getErr
}

func (f *fakeRunner) ListTools(context.Context) ([]tools.Spec, error) {
	return f.toolSpecs, f.listToolsErr
}

func (f *fakeRunner) InvokeTool(_ context.Context, name string, input map[string]any) (tools.Result, error) {
	f.invoked = append(f.invoked, invocation{name: name, input: input})
	if f.invokeUnknown {
		return tools.Result{}, ErrUnknownTool
	}
	return f.invokeResult, f.invokeErr
}

const token = "un-secret-de-daemon"

func router(t *testing.T, r Runner, tok string) http.Handler {
	t.Helper()
	return NewRouter(r, tok)
}

func authed(req *http.Request) *http.Request {
	req.Header.Set("Authorization", "Bearer "+token)
	return req
}

func TestHealthzStaysOpen(t *testing.T) {
	h := router(t, &fakeRunner{}, token)

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/healthz", nil))

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200 — a probe carries no credential", rec.Code)
	}
}

// The hole a daemon exposed if it did not: anyone reaching it could start or read any run.
func TestEveryOtherEndpointRefusesAnAnonymousCaller(t *testing.T) {
	h := router(t, &fakeRunner{}, token)

	cases := []struct{ method, path string }{
		{http.MethodPost, "/api/v1/runs"},
		{http.MethodGet, "/api/v1/runs"},
		{http.MethodGet, "/api/v1/runs/r1"},
		{http.MethodPost, "/api/v1/runs/r1/resume"},
		{http.MethodGet, "/api/v1/tools"},
		{http.MethodPost, "/api/v1/tools/greeting/invoke"},
		{http.MethodPost, "/api/v1/runs/r1/stop"},
		{http.MethodPost, "/api/v1/runs/r1/nudge"},
		{http.MethodPost, "/api/v1/runs/r1/nodes/confirm/decide"},
		{http.MethodPost, "/api/v1/dispatch"},
	}
	for _, c := range cases {
		t.Run(c.method+" "+c.path, func(t *testing.T) {
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, httptest.NewRequest(c.method, c.path, bytes.NewReader([]byte("{}"))))

			if rec.Code != http.StatusUnauthorized {
				t.Errorf("status = %d, want 401", rec.Code)
			}
		})
	}
}

func TestAWrongTokenIsRefused(t *testing.T) {
	h := router(t, &fakeRunner{}, token)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/runs", nil)
	req.Header.Set("Authorization", "Bearer pas-le-bon")
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", rec.Code)
	}
}

// An empty configured token leaves the router open — that is local development, and the
// binary's own startup check is what stops it from happening on a public address.
func TestAnUnconfiguredTokenLeavesTheRouterOpen(t *testing.T) {
	h := router(t, &fakeRunner{startID: "r1"}, "")

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/runs", nil))

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200 with no token configured", rec.Code)
	}
}

func TestStartingARunReturnsItsID(t *testing.T) {
	f := &fakeRunner{startID: "a1b2c3"}
	h := router(t, f, token)

	rec := httptest.NewRecorder()
	body := `{"graph":"examples/hello.yaml"}`
	h.ServeHTTP(rec, authed(httptest.NewRequest(http.MethodPost, "/api/v1/runs", bytes.NewReader([]byte(body)))))

	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want 202: %s", rec.Code, rec.Body)
	}
	var out struct {
		RunID string `json:"run_id"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if out.RunID != "a1b2c3" {
		t.Errorf("run_id = %q, want a1b2c3", out.RunID)
	}
	if len(f.started) != 1 || f.started[0] != "examples/hello.yaml" {
		t.Errorf("StartRun called with %v, want [examples/hello.yaml]", f.started)
	}
}

func TestStartingARunPassesTheRequesterThrough(t *testing.T) {
	f := &fakeRunner{startID: "a1b2c3"}
	h := router(t, f, token)

	rec := httptest.NewRecorder()
	body := `{"graph":"examples/hello.yaml","requester":"yoann"}`
	h.ServeHTTP(rec, authed(httptest.NewRequest(http.MethodPost, "/api/v1/runs", bytes.NewReader([]byte(body)))))

	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want 202: %s", rec.Code, rec.Body)
	}
	if len(f.startedRequesters) != 1 || f.startedRequesters[0] != "yoann" {
		t.Errorf("requester passed to StartRun = %v, want [yoann]", f.startedRequesters)
	}
}

// A caller who never mentions a requester gets an open run — the default that keeps every
// CLI-started run steerable by anyone, unchanged.
func TestStartingARunWithNoRequesterIsFine(t *testing.T) {
	f := &fakeRunner{startID: "a1b2c3"}
	h := router(t, f, token)

	rec := httptest.NewRecorder()
	body := `{"graph":"examples/hello.yaml"}`
	h.ServeHTTP(rec, authed(httptest.NewRequest(http.MethodPost, "/api/v1/runs", bytes.NewReader([]byte(body)))))

	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want 202: %s", rec.Code, rec.Body)
	}
	if len(f.startedRequesters) != 1 || f.startedRequesters[0] != "" {
		t.Errorf("requester = %v, want [\"\"]", f.startedRequesters)
	}
}

func TestStartingARunRejectsAMalformedBody(t *testing.T) {
	h := router(t, &fakeRunner{}, token)

	rec := httptest.NewRecorder()
	req := authed(httptest.NewRequest(http.MethodPost, "/api/v1/runs", bytes.NewReader([]byte("{"))))
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rec.Code)
	}
}

func TestStartingARunRejectsAnEmptyGraphPath(t *testing.T) {
	h := router(t, &fakeRunner{}, token)

	rec := httptest.NewRecorder()
	req := authed(httptest.NewRequest(http.MethodPost, "/api/v1/runs", bytes.NewReader([]byte(`{"graph":""}`))))
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rec.Code)
	}
}

// A path the loader will reject anyway — missing, malformed YAML — fails synchronously
// rather than inside a goroutine nobody can see: a caller deserves to know immediately
// rather than poll for a run that will never appear.
func TestStartingARunSurfacesAnImmediateFailure(t *testing.T) {
	h := router(t, &fakeRunner{startErr: errors.New("no such file")}, token)

	rec := httptest.NewRecorder()
	req := authed(httptest.NewRequest(http.MethodPost, "/api/v1/runs",
		bytes.NewReader([]byte(`{"graph":"absent.yaml"}`))))
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400: %s", rec.Code, rec.Body)
	}
}

func TestListingRuns(t *testing.T) {
	f := &fakeRunner{runs: []checkpoint.Summary{{RunID: "r1", Status: checkpoint.StatusRunning}}}
	h := router(t, f, token)

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, authed(httptest.NewRequest(http.MethodGet, "/api/v1/runs", nil)))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body)
	}
	var out []checkpoint.Summary
	_ = json.NewDecoder(rec.Body).Decode(&out)
	if len(out) != 1 || out[0].RunID != "r1" {
		t.Errorf("got %v, want the one run", out)
	}
}

func TestGettingAKnownRun(t *testing.T) {
	f := &fakeRunner{get: checkpoint.Record{RunID: "r1", Status: checkpoint.StatusRunning}, getOK: true}
	h := router(t, f, token)

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, authed(httptest.NewRequest(http.MethodGet, "/api/v1/runs/r1", nil)))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body)
	}
}

func TestGettingAnUnknownRunIs404(t *testing.T) {
	h := router(t, &fakeRunner{getOK: false}, token)

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, authed(httptest.NewRequest(http.MethodGet, "/api/v1/runs/jamais", nil)))

	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", rec.Code)
	}
}

func TestResumingAKnownRun(t *testing.T) {
	f := &fakeRunner{}
	h := router(t, f, token)

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, authed(httptest.NewRequest(http.MethodPost, "/api/v1/runs/r1/resume", nil)))

	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want 202: %s", rec.Code, rec.Body)
	}
	if len(f.resumed) != 1 || f.resumed[0] != "r1" {
		t.Errorf("ResumeRun called with %v, want [r1]", f.resumed)
	}
}

func TestResumingAnUnknownRunIs404(t *testing.T) {
	h := router(t, &fakeRunner{resumeUnknown: true}, token)

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, authed(httptest.NewRequest(http.MethodPost, "/api/v1/runs/jamais/resume", nil)))

	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", rec.Code)
	}
}

func TestListingTools(t *testing.T) {
	f := &fakeRunner{toolSpecs: []tools.Spec{{Name: "greeting", Description: "greets someone"}}}
	h := router(t, f, token)

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, authed(httptest.NewRequest(http.MethodGet, "/api/v1/tools", nil)))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body)
	}
	var out []tools.Spec
	_ = json.NewDecoder(rec.Body).Decode(&out)
	if len(out) != 1 || out[0].Name != "greeting" {
		t.Errorf("got %v, want the one tool", out)
	}
}

func TestInvokingAToolReturnsItsResult(t *testing.T) {
	f := &fakeRunner{invokeResult: tools.Result{Label: "Salutation", Value: "Bonjour, Yoann !"}}
	h := router(t, f, token)

	rec := httptest.NewRecorder()
	body := `{"input":{"name":"Yoann"}}`
	req := authed(httptest.NewRequest(http.MethodPost, "/api/v1/tools/greeting/invoke", bytes.NewReader([]byte(body))))
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body)
	}
	var out tools.Result
	_ = json.NewDecoder(rec.Body).Decode(&out)
	if out.Label != "Salutation" || out.Value != "Bonjour, Yoann !" {
		t.Errorf("got %+v", out)
	}
	if len(f.invoked) != 1 || f.invoked[0].name != "greeting" || f.invoked[0].input["name"] != "Yoann" {
		t.Errorf("InvokeTool called with %+v", f.invoked)
	}
}

func TestInvokingAnUnknownToolIs404(t *testing.T) {
	h := router(t, &fakeRunner{invokeUnknown: true}, token)

	rec := httptest.NewRecorder()
	req := authed(httptest.NewRequest(http.MethodPost, "/api/v1/tools/jamais/invoke", bytes.NewReader([]byte(`{}`))))
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", rec.Code)
	}
}

func TestInvokingAToolSurfacesAValidationFailure(t *testing.T) {
	h := router(t, &fakeRunner{invokeErr: errors.New("missing required param \"name\"")}, token)

	rec := httptest.NewRecorder()
	req := authed(httptest.NewRequest(http.MethodPost, "/api/v1/tools/greeting/invoke", bytes.NewReader([]byte(`{}`))))
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400: %s", rec.Code, rec.Body)
	}
}

func TestStoppingARun(t *testing.T) {
	f := &fakeRunner{}
	h := router(t, f, token)

	rec := httptest.NewRecorder()
	req := authed(httptest.NewRequest(http.MethodPost, "/api/v1/runs/r1/stop", bytes.NewReader([]byte(`{"actor":"yoann"}`))))
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want 202: %s", rec.Code, rec.Body)
	}
	if len(f.stopped) != 1 || f.stopped[0] != (call{"r1", "yoann"}) {
		t.Errorf("StopRun called with %+v, want [{r1 yoann}]", f.stopped)
	}
}

func TestStoppingAnUnknownRunIs404(t *testing.T) {
	h := router(t, &fakeRunner{stopUnknown: true}, token)

	rec := httptest.NewRecorder()
	req := authed(httptest.NewRequest(http.MethodPost, "/api/v1/runs/jamais/stop", bytes.NewReader([]byte(`{}`))))
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", rec.Code)
	}
}

func TestStoppingSomeoneElsesRunIs403(t *testing.T) {
	h := router(t, &fakeRunner{stopForbidden: true}, token)

	rec := httptest.NewRecorder()
	req := authed(httptest.NewRequest(http.MethodPost, "/api/v1/runs/r1/stop", bytes.NewReader([]byte(`{"actor":"pas-le-demandeur"}`))))
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Errorf("status = %d, want 403", rec.Code)
	}
}

func TestNudgingARun(t *testing.T) {
	f := &fakeRunner{}
	h := router(t, f, token)

	rec := httptest.NewRecorder()
	body := `{"actor":"yoann","key":"message","value":"bonjour"}`
	req := authed(httptest.NewRequest(http.MethodPost, "/api/v1/runs/r1/nudge", bytes.NewReader([]byte(body))))
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want 202: %s", rec.Code, rec.Body)
	}
	if len(f.nudged) != 1 || f.nudged[0].key != "message" || f.nudged[0].value != "bonjour" {
		t.Errorf("Nudge called with %+v", f.nudged)
	}
}

func TestNudgingRejectsAnEmptyKey(t *testing.T) {
	h := router(t, &fakeRunner{}, token)

	rec := httptest.NewRecorder()
	req := authed(httptest.NewRequest(http.MethodPost, "/api/v1/runs/r1/nudge", bytes.NewReader([]byte(`{"value":"x"}`))))
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rec.Code)
	}
}

func TestNudgingAnUnknownRunIs404(t *testing.T) {
	h := router(t, &fakeRunner{nudgeUnknown: true}, token)

	rec := httptest.NewRecorder()
	req := authed(httptest.NewRequest(http.MethodPost, "/api/v1/runs/jamais/nudge", bytes.NewReader([]byte(`{"key":"x","value":"y"}`))))
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", rec.Code)
	}
}

func TestNudgingSomeoneElsesRunIs403(t *testing.T) {
	h := router(t, &fakeRunner{nudgeForbidden: true}, token)

	rec := httptest.NewRecorder()
	req := authed(httptest.NewRequest(http.MethodPost, "/api/v1/runs/r1/nudge", bytes.NewReader([]byte(`{"key":"x","value":"y"}`))))
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Errorf("status = %d, want 403", rec.Code)
	}
}

func TestDecidingAnApprovalNode(t *testing.T) {
	f := &fakeRunner{}
	h := router(t, f, token)

	rec := httptest.NewRecorder()
	body := `{"actor":"yoann","decision":"approve"}`
	req := authed(httptest.NewRequest(http.MethodPost, "/api/v1/runs/r1/nodes/confirm/decide", bytes.NewReader([]byte(body))))
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body)
	}
	want := decideCall{"r1", "confirm", "yoann", "approve"}
	if len(f.decided) != 1 || f.decided[0] != want {
		t.Errorf("Decide called with %+v, want [%+v]", f.decided, want)
	}
}

func TestDecidingAnUnknownNodeIs404(t *testing.T) {
	h := router(t, &fakeRunner{decideUnknown: true}, token)

	rec := httptest.NewRecorder()
	body := `{"decision":"approve"}`
	req := authed(httptest.NewRequest(http.MethodPost, "/api/v1/runs/r1/nodes/jamais/decide", bytes.NewReader([]byte(body))))
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", rec.Code)
	}
}

func TestDecidingSomeoneElsesRunIs403(t *testing.T) {
	h := router(t, &fakeRunner{decideForbidden: true}, token)

	rec := httptest.NewRecorder()
	body := `{"decision":"approve"}`
	req := authed(httptest.NewRequest(http.MethodPost, "/api/v1/runs/r1/nodes/confirm/decide", bytes.NewReader([]byte(body))))
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Errorf("status = %d, want 403", rec.Code)
	}
}

func TestDecidingSurfacesAnInvalidDecisionValue(t *testing.T) {
	h := router(t, &fakeRunner{decideErr: errors.New(`invalid decision "maybe" (want approve|refuse)`)}, token)

	rec := httptest.NewRecorder()
	body := `{"decision":"maybe"}`
	req := authed(httptest.NewRequest(http.MethodPost, "/api/v1/runs/r1/nodes/confirm/decide", bytes.NewReader([]byte(body))))
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400: %s", rec.Code, rec.Body)
	}
}

func TestDispatchingATool(t *testing.T) {
	f := &fakeRunner{dispatchResult: DispatchResult{Kind: "tool", Result: &tools.Result{Label: "Battement", Value: "17:09:11"}}}
	h := router(t, f, token)

	rec := httptest.NewRecorder()
	body := `{"skill":"heartbeat","text":""}`
	req := authed(httptest.NewRequest(http.MethodPost, "/api/v1/dispatch", bytes.NewReader([]byte(body))))
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body)
	}
	var out DispatchResult
	_ = json.NewDecoder(rec.Body).Decode(&out)
	if out.Kind != "tool" || out.Result == nil || out.Result.Value != "17:09:11" {
		t.Errorf("got %+v", out)
	}
	if len(f.dispatched) != 1 || f.dispatched[0].skill != "heartbeat" {
		t.Errorf("Dispatch called with %+v", f.dispatched)
	}
}

func TestDispatchingAnAgentSkillReturnsARunID(t *testing.T) {
	f := &fakeRunner{dispatchResult: DispatchResult{Kind: "run", RunID: "abc123"}}
	h := router(t, f, token)

	rec := httptest.NewRecorder()
	body := `{"skill":"planner","text":"analyse ceci"}`
	req := authed(httptest.NewRequest(http.MethodPost, "/api/v1/dispatch", bytes.NewReader([]byte(body))))
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body)
	}
	var out DispatchResult
	_ = json.NewDecoder(rec.Body).Decode(&out)
	if out.Kind != "run" || out.RunID != "abc123" {
		t.Errorf("got %+v", out)
	}
}

func TestDispatchingAnUnknownSkillIs404WithTheKnownNames(t *testing.T) {
	f := &fakeRunner{dispatchUnknownSkill: &UnknownSkillError{Known: []string{"heartbeat", "planner"}}}
	h := router(t, f, token)

	rec := httptest.NewRecorder()
	body := `{"skill":"jamais","text":""}`
	req := authed(httptest.NewRequest(http.MethodPost, "/api/v1/dispatch", bytes.NewReader([]byte(body))))
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404: %s", rec.Code, rec.Body)
	}
	if !strings.Contains(rec.Body.String(), "heartbeat") || !strings.Contains(rec.Body.String(), "planner") {
		t.Errorf("body = %s, want the known skill names", rec.Body.String())
	}
}

func TestDispatchingRejectsAnEmptySkillName(t *testing.T) {
	h := router(t, &fakeRunner{}, token)

	rec := httptest.NewRecorder()
	req := authed(httptest.NewRequest(http.MethodPost, "/api/v1/dispatch", bytes.NewReader([]byte(`{"skill":""}`))))
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rec.Code)
	}
}

func TestDispatchingSurfacesAValidationFailure(t *testing.T) {
	h := router(t, &fakeRunner{dispatchErr: errors.New(`skill "greeting" needs several values, not yet usable from the chat`)}, token)

	rec := httptest.NewRecorder()
	body := `{"skill":"greeting","text":"x"}`
	req := authed(httptest.NewRequest(http.MethodPost, "/api/v1/dispatch", bytes.NewReader([]byte(body))))
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400: %s", rec.Code, rec.Body)
	}
}
