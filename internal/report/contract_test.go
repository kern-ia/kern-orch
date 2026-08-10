package report

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/yoann/kern-orch/internal/graph"
)

// contractFixture is the canonical kern.step-event/v1 payload. The identical file lives in
// Kern-UI/contracts/, where a mirror test asserts that ingestion accepts exactly this. The
// two bricks share no code by design, so this fixture is the whole agreement between them:
// if either side drifts, one of the two tests goes red.
const contractFixture = "../../contracts/kern.step-event.v1.json"

// The assertion runs against what the real Hook actually puts on the wire, not against a
// hand-built struct — a struct would only test itself.
func TestReporterEmitsTheContractFixture(t *testing.T) {
	want := map[string]any{}
	raw, err := os.ReadFile(filepath.FromSlash(contractFixture))
	if err != nil {
		t.Fatalf("read the contract fixture: %v", err)
	}
	if err := json.Unmarshal(raw, &want); err != nil {
		t.Fatalf("the fixture is not valid JSON: %v", err)
	}

	s := newSink(t, http.StatusAccepted)
	r := NewHTTP(s.server.URL)
	r.now = func() time.Time { return time.Date(2026, 7, 26, 12, 0, 2, 0, time.UTC) }

	// Reproduce exactly the run the fixture describes.
	state := graph.NewState()
	state.Set("echo", "...")

	hook := r.Hook("a23ead5373d9b746", "hello", nil)
	err = hook(context.Background(), graph.StepInfo{
		Step:     2,
		Frontier: []string{"synthese", "critique"},
	}, state)
	if err != nil {
		t.Fatalf("hook: %v", err)
	}

	r.Flush()

	got := s.last()
	if got == nil {
		t.Fatal("the reporter posted nothing")
	}

	if !reflect.DeepEqual(got, want) {
		t.Errorf("the emitted payload has drifted from the published contract.\n got: %#v\nwant: %#v", got, want)
	}
}

// Field names are the contract. Renaming one in the struct would keep DeepEqual honest,
// but this spells out which keys a sink is entitled to rely on.
func TestContractFieldNames(t *testing.T) {
	s := newSink(t, http.StatusAccepted)
	r := NewHTTP(s.server.URL)
	r.now = fixedNow

	state := graph.NewState()
	state.Set("echo", "...")
	_ = r.Hook("run", "g", nil)(context.Background(),
		graph.StepInfo{Step: 1, Frontier: []string{"a"}}, state)

	r.Flush()

	for _, key := range []string{"run_id", "graph", "step", "frontier", "state", "at"} {
		if _, ok := s.last()[key]; !ok {
			t.Errorf("field %q is missing from the payload", key)
		}
	}
}

const (
	contractV2        = "../../contracts/kern.step-event.v2.json"
	contractV2Failure = "../../contracts/kern.step-event.v2.failure.json"
)

func fixture(t *testing.T, path string) map[string]any {
	t.Helper()
	raw, err := os.ReadFile(filepath.FromSlash(path))
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	var want map[string]any
	if err := json.Unmarshal(raw, &want); err != nil {
		t.Fatalf("%s is not valid JSON: %v", path, err)
	}
	return want
}

// Mirror of the kern-ui test: what the real Hook puts on the wire must equal the published
// v2 fixture, byte for byte once parsed.
func TestReporterEmitsTheV2Fixture(t *testing.T) {
	want := fixture(t, contractV2)

	s := newSink(t, http.StatusAccepted)
	r := NewHTTP(s.server.URL)
	r.now = func() time.Time { return time.Date(2026, 7, 26, 12, 0, 2, 0, time.UTC) }
	r.Requester = "yoann"
	r.Dossier = "AF-2288"

	state := graph.NewState()
	state.Set("echo", "...")

	topo := &Topology{
		Entry: "greet",
		Nodes: []TopologyNode{
			{ID: "greet", Kind: "agent", Skill: "planner"},
			{ID: "synthese", Kind: "agent", Skill: "redacteur"},
			{ID: "critique", Kind: "tool"},
		},
		Edges: []TopologyEdge{
			{From: "greet", To: []string{"synthese", "critique"}},
			{From: "critique", Dynamic: true},
		},
	}

	err := r.Hook("a23ead5373d9b746", "hello", topo)(context.Background(),
		graph.StepInfo{Step: 2, Frontier: []string{"synthese", "critique"}}, state)
	if err != nil {
		t.Fatalf("hook: %v", err)
	}

	r.Flush()

	if got := s.last(); !reflect.DeepEqual(got, want) {
		t.Errorf("the emitted payload has drifted from the published contract.\n got: %#v\nwant: %#v", got, want)
	}
}

func TestReporterEmitsTheV2FailureFixture(t *testing.T) {
	want := fixture(t, contractV2Failure)

	s := newSink(t, http.StatusAccepted)
	r := NewHTTP(s.server.URL)
	r.now = func() time.Time { return time.Date(2026, 7, 26, 12, 0, 9, 0, time.UTC) }

	r.ReportFailure(context.Background(), "a23ead5373d9b746", "hello", 4,
		[]string{"synthese"}, []string{"synthese"}, "agent synthese: exit status 1")

	r.Flush()

	if got := s.last(); !reflect.DeepEqual(got, want) {
		t.Errorf("the emitted failure has drifted from the published contract.\n got: %#v\nwant: %#v", got, want)
	}
}

// A failure that names no node is still a valid failure: the field is omitted rather than
// sent empty, so a sink can tell "we do not know which" from "none".
func TestAFailureWithoutNamedNodesOmitsTheField(t *testing.T) {
	s := newSink(t, http.StatusAccepted)
	r := NewHTTP(s.server.URL)
	r.now = fixedNow

	r.ReportFailure(context.Background(), "r1", "g", 1, []string{"a"}, nil, "something broke")

	r.Flush()

	failure, ok := s.last()["error"].(map[string]any)
	if !ok {
		t.Fatalf("error = %#v, want an object", s.last()["error"])
	}
	if _, present := failure["nodes"]; present {
		t.Errorf("nodes = %v, want the field absent when nothing is known", failure["nodes"])
	}
}
