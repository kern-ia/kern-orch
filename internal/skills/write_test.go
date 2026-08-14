package skills

import (
	"os"
	"strings"
	"testing"
)

func TestCreateWritesAOneStepSkillWithNoGraphFile(t *testing.T) {
	dir := t.TempDir()
	reg := &Registry{byName: make(map[string]Skill)}

	sk, err := Create(dir, reg, "salutation", "dit bonjour", "elise", []Step{
		{Name: "accueil", Instructions: "Salue chaleureusement la personne."},
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if sk.Type != TypeAgent || sk.CreatedBy != "elise" || !sk.Custom {
		t.Fatalf("sk = %+v", sk)
	}
	if sk.Graph != "" {
		t.Errorf("Graph = %q, want empty — a single step is the ad-hoc one-node run", sk.Graph)
	}

	loaded, err := Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	got, ok := loaded.Get("salutation")
	if !ok {
		t.Fatal("the written skill does not load back")
	}
	if got.Description != "dit bonjour" || got.CreatedBy != "elise" {
		t.Errorf("loaded = %+v", got)
	}
}

func TestCreateWritesAChainedGraphForMultipleSteps(t *testing.T) {
	dir := t.TempDir()
	reg := &Registry{byName: make(map[string]Skill)}

	sk, err := Create(dir, reg, "accueil-client", "accueil en deux temps", "elise", []Step{
		{Name: "premier contact", Instructions: "Présente-toi et pose une question ouverte."},
		{Name: "synthese", Instructions: "Résume ce que la personne a dit."},
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if sk.Graph == "" {
		t.Fatal("Graph is empty, want a path — two steps need a chained graph")
	}
	// Resolved against the process's working directory, exactly like a shipped skill's
	// `graph: examples/…yaml` — never relative to sk.Dir, which topology.LoadFile has no
	// notion of.
	raw, err := os.ReadFile(sk.Graph)
	if err != nil {
		t.Fatalf("the declared graph file does not exist: %v", err)
	}
	text := string(raw)
	for _, want := range []string{"entry:", "type: agent", "to:"} {
		if !strings.Contains(text, want) {
			t.Errorf("graph file missing %q:\n%s", want, text)
		}
	}
}

func TestCreateRejectsANameAlreadyTaken(t *testing.T) {
	dir := t.TempDir()
	reg := &Registry{byName: map[string]Skill{"existant": {Name: "existant", Type: TypeAgent}}}

	_, err := Create(dir, reg, "existant", "x", "elise", []Step{{Name: "a", Instructions: "b"}})
	if err == nil {
		t.Fatal("expected an error for a name already taken")
	}
}

func TestCreateRejectsAnUnsafeName(t *testing.T) {
	dir := t.TempDir()
	reg := &Registry{byName: make(map[string]Skill)}

	cases := []string{"", "Has Spaces", "UPPER", "a/../b", "-leading-hyphen"}
	for _, name := range cases {
		if _, err := Create(dir, reg, name, "x", "elise", []Step{{Name: "a", Instructions: "b"}}); err == nil {
			t.Errorf("name %q was accepted, want rejected", name)
		}
	}
}

func TestCreateRejectsNoSteps(t *testing.T) {
	dir := t.TempDir()
	reg := &Registry{byName: make(map[string]Skill)}

	if _, err := Create(dir, reg, "vide", "x", "elise", nil); err == nil {
		t.Fatal("expected an error for zero steps")
	}
}

func TestDeleteRemovesAnOwnedSkill(t *testing.T) {
	dir := t.TempDir()
	reg := &Registry{byName: make(map[string]Skill)}
	sk, err := Create(dir, reg, "jetable", "x", "elise", []Step{{Name: "a", Instructions: "b"}})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	if err := Delete(sk, "elise"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := os.Stat(sk.Dir); !os.IsNotExist(err) {
		t.Error("the skill directory still exists after Delete")
	}
}

func TestDeleteRefusesAnotherAccountsCreation(t *testing.T) {
	dir := t.TempDir()
	reg := &Registry{byName: make(map[string]Skill)}
	sk, err := Create(dir, reg, "prive", "x", "elise", []Step{{Name: "a", Instructions: "b"}})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	err = Delete(sk, "quelquun-dautre")
	if err == nil {
		t.Fatal("expected an ownership error")
	}
	if _, statErr := os.Stat(sk.Dir); statErr != nil {
		t.Error("the skill directory was removed despite the ownership mismatch")
	}
}
