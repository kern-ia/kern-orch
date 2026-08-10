package cmd

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/yoann/kern-orch/internal/checkpoint"
	"github.com/yoann/kern-orch/internal/config"
	"github.com/yoann/kern-orch/internal/daemon"
	"github.com/yoann/kern-orch/internal/skills"
)

// writeDispatchSkills writes one tool skill ("echo", a real Python subprocess) and one
// agent skill ("planner", backed by the deterministic stub since KERN_AGENT_CLI is unset
// in tests) — enough to exercise both branches Dispatch can take.
func writeDispatchSkills(t *testing.T, dir string) {
	t.Helper()
	toolDir := filepath.Join(dir, "echo")
	if err := os.MkdirAll(toolDir, 0o755); err != nil {
		t.Fatal(err)
	}
	skillMD := "---\nname: echo\ntype: tool\ncommand: [\"python3\", \"" + filepath.Join(toolDir, "tool.py") + "\"]\n---\n"
	if err := os.WriteFile(filepath.Join(toolDir, "SKILL.md"), []byte(skillMD), 0o644); err != nil {
		t.Fatal(err)
	}
	toolPy := "#!/usr/bin/env python3\nimport json, sys\njson.load(sys.stdin)\nprint(json.dumps({\"label\": \"Echo\", \"value\": \"ok\"}))\n"
	if err := os.WriteFile(filepath.Join(toolDir, "tool.py"), []byte(toolPy), 0o755); err != nil {
		t.Fatal(err)
	}

	agentDir := filepath.Join(dir, "planner")
	if err := os.MkdirAll(agentDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(agentDir, "SKILL.md"), []byte("---\nname: planner\ntype: agent\n---\n"), 0o644); err != nil {
		t.Fatal(err)
	}
}

// writeGraphDispatchSkill writes a "pipeline" agent skill whose SKILL.md declares a
// `graph:` — two sequential agent nodes, no approval (that mechanism has its own tests;
// this one is only about Dispatch choosing the file-loading path and delivering the
// chat's text into state before the entry node runs).
func writeGraphDispatchSkill(t *testing.T, skillsDir, graphPath string) {
	t.Helper()
	graphDir := filepath.Join(skillsDir, "pipeline")
	if err := os.MkdirAll(graphDir, 0o755); err != nil {
		t.Fatal(err)
	}
	skillMD := "---\nname: pipeline\ntype: agent\ngraph: " + graphPath + "\n---\n"
	if err := os.WriteFile(filepath.Join(graphDir, "SKILL.md"), []byte(skillMD), 0o644); err != nil {
		t.Fatal(err)
	}

	// Distinct static prompts on purpose: the stub's default behaviour echoes whichever
	// node ran last into state["echo"] — "second-step" surviving proves the pipeline
	// advanced past the first node, not just that Dispatch ran *a* single node.
	yaml := "entry: first\nnodes:\n  - id: first\n    type: agent\n    prompt: first-step\n  - id: second\n" +
		"    type: agent\n    prompt: second-step\nedges:\n  - from: first\n    to: [second]\n"
	if err := os.WriteFile(graphPath, []byte(yaml), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestDaemonRunnerDispatchInvokesATool(t *testing.T) {
	dir := t.TempDir()
	writeDispatchSkills(t, dir)
	store := openDaemonStore(t, dir)
	d := &daemonRunner{cfg: config.Config{SkillsDir: dir}, store: store}

	result, err := d.Dispatch(context.Background(), "echo", "", "", "")
	if err != nil {
		t.Fatalf("Dispatch: %v", err)
	}
	if result.Kind != "tool" || result.Result == nil || result.Result.Value != "ok" {
		t.Errorf("got %+v", result)
	}
}

// The real proof dispatch's agent path works: a run genuinely launched, genuinely
// completed, carrying the requester, the dossier, and the chat text as its whole prompt.
func TestDaemonRunnerDispatchLaunchesAnAgentRun(t *testing.T) {
	dir := t.TempDir()
	writeDispatchSkills(t, dir)
	store := openDaemonStore(t, dir)
	d := &daemonRunner{cfg: config.Config{SkillsDir: dir}, store: store}

	result, err := d.Dispatch(context.Background(), "planner", "analyse ceci", "yoann", "AF-2288")
	if err != nil {
		t.Fatalf("Dispatch: %v", err)
	}
	if result.Kind != "run" || result.RunID == "" {
		t.Fatalf("got %+v", result)
	}
	waitForRun(t, store, result.RunID, checkpoint.StatusDone)

	rec, ok, err := d.GetRun(context.Background(), result.RunID)
	if err != nil || !ok {
		t.Fatalf("GetRun: ok=%v err=%v", ok, err)
	}
	if rec.Requester != "yoann" {
		t.Errorf("Requester = %q, want yoann", rec.Requester)
	}
	if rec.Dossier != "AF-2288" {
		t.Errorf("Dossier = %q, want AF-2288", rec.Dossier)
	}
	if v, _ := rec.State.Get("echo"); v != "analyse ceci" {
		t.Errorf("stub did not receive the dispatched text as its prompt: %v", v)
	}
}

// A dispatch with no dossier leaves the run's Dossier empty — no default is invented.
func TestDaemonRunnerDispatchWithNoDossierLeavesItEmpty(t *testing.T) {
	dir := t.TempDir()
	writeDispatchSkills(t, dir)
	store := openDaemonStore(t, dir)
	d := &daemonRunner{cfg: config.Config{SkillsDir: dir}, store: store}

	result, err := d.Dispatch(context.Background(), "planner", "analyse ceci", "yoann", "")
	if err != nil {
		t.Fatalf("Dispatch: %v", err)
	}
	waitForRun(t, store, result.RunID, checkpoint.StatusDone)

	rec, ok, err := d.GetRun(context.Background(), result.RunID)
	if err != nil || !ok {
		t.Fatalf("GetRun: ok=%v err=%v", ok, err)
	}
	if rec.Dossier != "" {
		t.Errorf("Dossier = %q, want empty", rec.Dossier)
	}
}

// The real proof for the demo's pipeline skill: a `graph:` in SKILL.md makes Dispatch
// load the FILE graph (both nodes run, not a single ad-hoc one), and the chat's text
// reaches the entry node via the existing nudge mechanism before it executes.
func TestDaemonRunnerDispatchWithAGraphLoadsTheFileAndNudgesTheMessage(t *testing.T) {
	dir := t.TempDir()
	writeDispatchSkills(t, dir)
	graphPath := filepath.Join(t.TempDir(), "pipeline.yaml")
	writeGraphDispatchSkill(t, dir, graphPath)
	store := openDaemonStore(t, dir)
	d := &daemonRunner{cfg: config.Config{SkillsDir: dir}, store: store}

	result, err := d.Dispatch(context.Background(), "pipeline", "bonjour le CRM", "yoann", "")
	if err != nil {
		t.Fatalf("Dispatch: %v", err)
	}
	if result.Kind != "run" || result.RunID == "" {
		t.Fatalf("got %+v", result)
	}
	waitForRun(t, store, result.RunID, checkpoint.StatusDone)

	rec, ok, err := d.GetRun(context.Background(), result.RunID)
	if err != nil || !ok {
		t.Fatalf("GetRun: ok=%v err=%v", ok, err)
	}
	if v, _ := rec.State.Get("message"); v != "bonjour le CRM" {
		t.Errorf("nudged message = %v, want the dispatched text delivered before the entry node ran", v)
	}
	// Both nodes ran, in order: the stub's default behaviour overwrites state["echo"]
	// with whichever node ran last — "second-step" surviving is proof the pipeline
	// advanced past the first node, not that a single ad-hoc node ran instead.
	if v, _ := rec.State.Get("echo"); v != "second-step" {
		t.Errorf("echo = %v, want \"second-step\" — proof the pipeline ran both nodes in order", v)
	}
}

func TestDaemonRunnerDispatchOnAnUnknownSkillListsKnownNames(t *testing.T) {
	dir := t.TempDir()
	writeDispatchSkills(t, dir)
	store := openDaemonStore(t, dir)
	d := &daemonRunner{cfg: config.Config{SkillsDir: dir}, store: store}

	_, err := d.Dispatch(context.Background(), "jamais", "", "", "")
	var unknown *daemon.UnknownSkillError
	if !errors.As(err, &unknown) {
		t.Fatalf("err = %v, want *daemon.UnknownSkillError", err)
	}
	if len(unknown.Known) != 2 {
		t.Errorf("Known = %v, want [echo planner]", unknown.Known)
	}
}

func TestDispatchInputWithNoRequiredParamIgnoresText(t *testing.T) {
	input, err := dispatchInput(skills.Skill{}, "hello")
	if err != nil || input != nil {
		t.Errorf("got %v, %v; want nil, nil", input, err)
	}
}

func TestDispatchInputWithOneRequiredParamUsesTheWholeText(t *testing.T) {
	sk := skills.Skill{Params: []skills.Param{{Name: "name", Required: true}}}
	input, err := dispatchInput(sk, "Yoann")
	if err != nil {
		t.Fatalf("dispatchInput: %v", err)
	}
	if input["name"] != "Yoann" {
		t.Errorf("input = %v, want name=Yoann", input)
	}
}

func TestDispatchInputWithSeveralRequiredParamsRefuses(t *testing.T) {
	sk := skills.Skill{
		Name: "multi",
		Params: []skills.Param{
			{Name: "a", Required: true},
			{Name: "b", Required: true},
		},
	}
	if _, err := dispatchInput(sk, "x"); err == nil {
		t.Error("dispatchInput accepted a skill needing two values from one chat message")
	}
}
