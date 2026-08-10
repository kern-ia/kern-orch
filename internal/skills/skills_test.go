package skills

import (
	"os"
	"path/filepath"
	"testing"
)

func writeSkill(t *testing.T, dir, name, body string) {
	t.Helper()
	sd := filepath.Join(dir, name)
	if err := os.MkdirAll(sd, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sd, "SKILL.md"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestLoadParsesFrontmatterTypeAndDescription(t *testing.T) {
	dir := t.TempDir()
	writeSkill(t, dir, "planner", "---\nname: planner\ntype: agent\ndescription: plans work\n---\n# Planner\nbody\n")
	writeSkill(t, dir, "sum", "---\nname: sum\ntype: tool\ndescription: adds numbers\n---\nbody\n")

	reg, err := Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if reg.Len() != 2 {
		t.Fatalf("len = %d; want 2", reg.Len())
	}
	planner, ok := reg.Get("planner")
	if !ok || planner.Type != TypeAgent || planner.Description != "plans work" {
		t.Fatalf("planner = %+v ok=%v", planner, ok)
	}
	sum, _ := reg.Get("sum")
	if sum.Type != TypeTool {
		t.Fatalf("sum type = %q; want tool", sum.Type)
	}
}

func TestLoadDefaultsNameFromDirAndRejectsBadType(t *testing.T) {
	dir := t.TempDir()
	// missing name -> defaults to directory name; bad type -> error
	writeSkill(t, dir, "weird", "---\ntype: gadget\n---\n")
	if _, err := Load(dir); err == nil {
		t.Fatal("expected error for invalid type")
	}
}

func TestLoadMissingTypeErrors(t *testing.T) {
	dir := t.TempDir()
	writeSkill(t, dir, "notype", "---\nname: notype\ndescription: x\n---\n")
	if _, err := Load(dir); err == nil {
		t.Fatal("expected error for missing type")
	}
}

func TestLoadEmptyDirIsEmptyRegistry(t *testing.T) {
	reg, err := Load(t.TempDir())
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if reg.Len() != 0 {
		t.Fatalf("len = %d; want 0", reg.Len())
	}
}

// A tool skill invoked by an outside caller (EPIC-03) needs a subprocess command and the
// input it accepts declared alongside it — without this the catalogue names a tool but
// nothing knows how to run it or what to send.
func TestLoadParsesCommandAndParams(t *testing.T) {
	dir := t.TempDir()
	writeSkill(t, dir, "greeting", `---
name: greeting
type: tool
description: greets someone
command: ["python3", "tool.py"]
params:
  - name: name
    type: string
    required: true
  - name: loud
    type: bool
---
`)
	reg, err := Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	sk, ok := reg.Get("greeting")
	if !ok {
		t.Fatal("greeting not loaded")
	}
	if len(sk.Command) != 2 || sk.Command[0] != "python3" || sk.Command[1] != "tool.py" {
		t.Fatalf("Command = %v", sk.Command)
	}
	if len(sk.Params) != 2 {
		t.Fatalf("Params = %+v; want 2", sk.Params)
	}
	if sk.Params[0].Name != "name" || sk.Params[0].Type != "string" || !sk.Params[0].Required {
		t.Errorf("Params[0] = %+v", sk.Params[0])
	}
	if sk.Params[1].Name != "loud" || sk.Params[1].Required {
		t.Errorf("Params[1] = %+v, want optional", sk.Params[1])
	}
}

// A skill with no declared command is simply not invocable this way — an agent-type skill,
// or a tool backed only by a topology.Registry func — and that must not be an error: most
// skills today have neither field.
func TestLoadSkillWithNoCommandIsFine(t *testing.T) {
	dir := t.TempDir()
	writeSkill(t, dir, "sum", "---\nname: sum\ntype: tool\n---\n")
	reg, err := Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	sk, _ := reg.Get("sum")
	if len(sk.Command) != 0 {
		t.Errorf("Command = %v, want none", sk.Command)
	}
}

func TestListIsSortedByName(t *testing.T) {
	dir := t.TempDir()
	writeSkill(t, dir, "zeta", "---\nname: zeta\ntype: tool\n---\n")
	writeSkill(t, dir, "alpha", "---\nname: alpha\ntype: agent\n---\n")
	reg, _ := Load(dir)
	list := reg.List()
	if len(list) != 2 || list[0].Name != "alpha" || list[1].Name != "zeta" {
		t.Fatalf("List not sorted: %+v", list)
	}
}
