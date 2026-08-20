package cmd

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/yoann/kern-orch/internal/config"
)

// catalogueSink records the last catalogue posted to it.
type catalogueSink struct {
	server *httptest.Server

	mu     sync.Mutex
	body   map[string]any
	bodies []map[string]any
	hits   int
}

func newCatalogueSink(t *testing.T) *catalogueSink {
	t.Helper()
	s := &catalogueSink{}
	s.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		s.mu.Lock()
		defer s.mu.Unlock()
		s.hits++
		_ = json.Unmarshal(raw, &s.body)
		var one map[string]any
		if json.Unmarshal(raw, &one) == nil {
			s.bodies = append(s.bodies, one)
		}
		w.WriteHeader(http.StatusAccepted)
	}))
	t.Cleanup(s.server.Close)
	return s
}

func (s *catalogueSink) last() map[string]any {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.body
}

func (s *catalogueSink) count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.hits
}

// skillsDir writes a skills tree and returns its path.
func skillsDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	write := func(name, body string) {
		t.Helper()
		if err := os.MkdirAll(filepath.Join(dir, name), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, name, "SKILL.md"), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("analyse", "---\nname: Analyse\ntype: tool\ndescription: Décompose une demande.\n---\nbody\n")
	write("scribe", "---\nname: Scribe\ntype: agent\ndescription: Rédige.\n---\nbody\n")
	return dir
}

func TestPublishSkillsPostsTheCatalogue(t *testing.T) {
	sink := newCatalogueSink(t)
	t.Setenv(config.EnvRegistryReportURL, sink.server.URL)

	out, err := execute(t, "publish-skills", "--skills-dir", skillsDir(t))
	if err != nil {
		t.Fatalf("publish-skills: %v (%s)", err, out)
	}

	body := sink.last()
	if body["source"] != "kern-orch" {
		t.Errorf("source = %v, want kern-orch", body["source"])
	}
	list, ok := body["skills"].([]any)
	if !ok || len(list) != 2 {
		t.Fatalf("skills = %#v, want the two skills on disk", body["skills"])
	}
	first := list[0].(map[string]any)
	if first["name"] != "Analyse" || first["kind"] != "tool" {
		t.Errorf("skills[0] = %v, want the Analyse tool first (sorted by name)", first)
	}
}

// Without a sink configured the command must say so rather than silently succeed: an
// operator running it expects a catalogue to land somewhere, and a no-op that looks like a
// success is how you spend an afternoon debugging the wrong brick.
func TestPublishSkillsSaysWhenNoSinkIsConfigured(t *testing.T) {
	t.Setenv(config.EnvRegistryReportURL, "")

	out, err := execute(t, "publish-skills", "--skills-dir", skillsDir(t))
	if err != nil {
		t.Fatalf("publish-skills: %v", err)
	}

	if !strings.Contains(out, config.EnvRegistryReportURL) {
		t.Errorf("output = %q, want it to name %s", out, config.EnvRegistryReportURL)
	}
}

// A run publishes the catalogue too, so a sink is fed without a separate command.
func TestRunPublishesTheCatalogue(t *testing.T) {
	sink := newCatalogueSink(t)
	dir := t.TempDir()
	graphPath := filepath.Join(dir, "g.yaml")
	if err := os.WriteFile(graphPath, []byte(exampleGraph), 0o644); err != nil {
		t.Fatal(err)
	}

	t.Setenv(config.EnvRegistryReportURL, sink.server.URL)
	t.Setenv(config.EnvSkillsDir, skillsDir(t))
	t.Setenv(config.EnvCheckpointDB, filepath.Join(dir, "k.db"))

	if _, err := execute(t, "run", graphPath); err != nil {
		t.Fatalf("run: %v", err)
	}

	if sink.count() == 0 {
		t.Error("the run never published the catalogue")
	}
}

// Publishing is observability: a sink that is down must not stop a graph from running.
func TestRunSurvivesADeadRegistrySink(t *testing.T) {
	dir := t.TempDir()
	graphPath := filepath.Join(dir, "g.yaml")
	if err := os.WriteFile(graphPath, []byte(exampleGraph), 0o644); err != nil {
		t.Fatal(err)
	}

	// Port 1 refuses connections on every platform we build for.
	t.Setenv(config.EnvRegistryReportURL, "http://127.0.0.1:1/registry")
	t.Setenv(config.EnvSkillsDir, skillsDir(t))
	t.Setenv(config.EnvCheckpointDB, filepath.Join(dir, "k.db"))

	out, err := execute(t, "run", graphPath)
	if err != nil {
		t.Fatalf("a dead registry sink aborted the run: %v (%s)", err, out)
	}
	if !strings.Contains(out, "completed") {
		t.Errorf("output = %q, want the run to have completed anyway", out)
	}
}

// This test used to assert the activity chain (runner hook → relay → reporter → sink) end to
// end, driving it through a shell script that spoke agentrunner's own JSON-lines protocol.
// That protocol is a placeholder no real CLI ever spoke, and the adapter registry retires it:
// a bare KERN_AGENT_CLI no longer selects anything, so the script has nothing to be run by.
//
// The fixture is kept and pointed at what the same configuration does now — refuse the run at
// config load, before a single node executes — because that is the criterion that replaced
// it, and asserting it here (rather than only in config's own tests) proves the refusal
// actually reaches the operator through the CLI. The activity chain's end-to-end coverage
// comes back with the first real adapter (issues 04/05), which is the first thing that can
// stand in for the script.
func TestRunRefusesAnAgentCLIWithNoAgentKind(t *testing.T) {
	sink := newCatalogueSink(t)
	dir := t.TempDir()

	agent := filepath.Join(dir, "agent.sh")
	script := "#!/bin/sh\ncat > /dev/null\nprintf '{\"type\":\"result\",\"output\":{}}\\n'\n"
	if err := os.WriteFile(agent, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	graphPath := filepath.Join(dir, "g.yaml")
	if err := os.WriteFile(graphPath, []byte(exampleGraph), 0o644); err != nil {
		t.Fatal(err)
	}

	t.Setenv(config.EnvAgentCLI, agent)
	t.Setenv(config.EnvAgentKind, "")
	t.Setenv(config.EnvActivityReportURL, sink.server.URL)
	t.Setenv(config.EnvCheckpointDB, filepath.Join(dir, "k.db"))

	_, err := execute(t, "run", graphPath)
	if err == nil {
		t.Fatalf("run: got nil error, want the run refused for a missing %s", config.EnvAgentKind)
	}
	if !strings.Contains(err.Error(), config.EnvAgentKind) {
		t.Fatalf("run error = %q, want it to name %s", err.Error(), config.EnvAgentKind)
	}

	// Refused at load means nothing ran: no node ever started, so no activity was reported.
	if got := sink.count(); got != 0 {
		t.Fatalf("the sink received %d signals, want none from a run that never started", got)
	}
}

// Delivery moved off the engine's thread, so the guarantee that matters is no longer
// "it was posted" but "the command waited for it before exiting".
func TestRunDeliversEveryLevelDespiteAsyncReporting(t *testing.T) {
	sink := newCatalogueSink(t)
	dir := t.TempDir()
	graphPath := filepath.Join(dir, "g.yaml")
	if err := os.WriteFile(graphPath, []byte(exampleGraph), 0o644); err != nil {
		t.Fatal(err)
	}

	t.Setenv(config.EnvStepReportURL, sink.server.URL)
	t.Setenv(config.EnvCheckpointDB, filepath.Join(dir, "k.db"))

	if _, err := execute(t, "run", graphPath); err != nil {
		t.Fatalf("run: %v", err)
	}

	// greet then finish, then the empty frontier that closes the run.
	if got := sink.count(); got < 2 {
		t.Fatalf("the sink received %d levels, want the whole run — the flush came too early", got)
	}
	if last := sink.last(); last["frontier"] == nil {
		t.Errorf("the last event carries no frontier: %v", last)
	} else if fr, _ := last["frontier"].([]any); len(fr) != 0 {
		t.Errorf("the last frontier is %v, want the empty one that closes the run", fr)
	}
}

// A sink nobody can reach must cost the run nothing at all, now that nothing waits on it.
func TestRunIsNotSlowedByAnUnreachableSink(t *testing.T) {
	dir := t.TempDir()
	graphPath := filepath.Join(dir, "g.yaml")
	if err := os.WriteFile(graphPath, []byte(exampleGraph), 0o644); err != nil {
		t.Fatal(err)
	}

	t.Setenv(config.EnvStepReportURL, "http://127.0.0.1:1/steps")
	t.Setenv(config.EnvCheckpointDB, filepath.Join(dir, "k.db"))

	out, err := execute(t, "run", graphPath)
	if err != nil {
		t.Fatalf("an unreachable sink aborted the run: %v (%s)", err, out)
	}
	if !strings.Contains(out, "completed") {
		t.Errorf("output = %q, want the run to have completed", out)
	}
}

// A subgraph must report as a run of its own, so the interface can draw what happens inside
// a sub-agent instead of a single dot.
func TestRunReportsANestedRunForASubgraph(t *testing.T) {
	sink := newCatalogueSink(t)
	dir := t.TempDir()

	child := filepath.Join(dir, "child.yaml")
	if err := os.WriteFile(child, []byte("entry: c1\nnodes:\n  - {id: c1, type: tool, func: seed}\n  - {id: c2, type: tool, func: double}\nedges:\n  - {from: c1, to: [c2]}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	parent := filepath.Join(dir, "parent.yaml")
	if err := os.WriteFile(parent, []byte("entry: nested\nnodes:\n  - {id: nested, type: subgraph, graph: child.yaml}\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	t.Setenv(config.EnvStepReportURL, sink.server.URL)
	t.Setenv(config.EnvCheckpointDB, filepath.Join(dir, "k.db"))

	if _, err := execute(t, "run", parent); err != nil {
		t.Fatalf("run: %v", err)
	}

	var nested []map[string]any
	for _, body := range sink.all() {
		if p, ok := body["parent"].(map[string]any); ok {
			if p["node_id"] != "nested" {
				t.Errorf("parent.node_id = %v, want the subgraph node", p["node_id"])
			}
			nested = append(nested, body)
		}
	}
	if len(nested) == 0 {
		t.Fatal("the nested graph reported nothing; a sub-agent is still a single dot")
	}

	// Its own run id, its own level sequence — never folded into the parent's.
	first := nested[0]
	if first["run_id"] == "" || first["run_id"] == nil {
		t.Error("the nested run has no id of its own")
	}
	if _, ok := first["topology"].(map[string]any); !ok {
		t.Error("the nested run never declared its shape")
	}
	if first["graph"] != "child" {
		t.Errorf("graph = %v, want the child's own name", first["graph"])
	}
}

// all returns every body the sink received, in arrival order.
func (s *catalogueSink) all() []map[string]any {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]map[string]any(nil), s.bodies...)
}
