package report

import (
	"context"
	"net/http"
	"reflect"
	"testing"
	"time"

	"github.com/yoann/kern-orch/internal/skills"
)

const contractRegistry = "../../contracts/kern.registry.v1.json"

// Mirror of the kern-ui test: what the real publisher puts on the wire must equal the
// published fixture. The two bricks share no code, so this file is the whole agreement.
func TestPublisherEmitsTheRegistryFixture(t *testing.T) {
	want := fixture(t, contractRegistry)

	s := newSink(t, http.StatusAccepted)
	p := NewRegistryPublisher(s.server.URL)
	p.now = func() time.Time { return time.Date(2026, 7, 27, 12, 0, 0, 0, time.UTC) }

	err := p.Publish(context.Background(), []skills.Skill{
		{Name: "Scribe", Type: skills.TypeAgent, Description: "Rédige et reformule à partir des notes du run."},
		{Name: "Analyse", Type: skills.TypeTool, Description: "Décompose une demande en éléments vérifiables."},
	})
	if err != nil {
		t.Fatalf("Publish: %v", err)
	}

	if got := s.last(); !reflect.DeepEqual(got, want) {
		t.Errorf("the emitted catalogue has drifted from the published contract.\n got: %#v\nwant: %#v", got, want)
	}
}

// Field names are the contract. This spells out which keys a sink may rely on.
func TestRegistryContractFieldNames(t *testing.T) {
	s := newSink(t, http.StatusAccepted)
	p := NewRegistryPublisher(s.server.URL)
	p.now = fixedNow

	_ = p.Publish(context.Background(), []skills.Skill{{Name: "A", Type: skills.TypeTool}})

	for _, key := range []string{"source", "at", "skills"} {
		if _, ok := s.last()[key]; !ok {
			t.Errorf("field %q is missing from the payload", key)
		}
	}
}

// A created skill carries who made it, so a consumer can decide whether to offer deletion.
func TestPublisherCarriesCustomAndCreatedBy(t *testing.T) {
	s := newSink(t, http.StatusAccepted)
	p := NewRegistryPublisher(s.server.URL)
	p.now = fixedNow

	_ = p.Publish(context.Background(), []skills.Skill{
		{Name: "accueil", Type: skills.TypeAgent, Custom: true, CreatedBy: "elise"},
	})

	entry := s.last()["skills"].([]any)[0].(map[string]any)
	if entry["custom"] != true {
		t.Errorf("custom = %v, want true", entry["custom"])
	}
	if entry["created_by"] != "elise" {
		t.Errorf("created_by = %v, want elise", entry["created_by"])
	}
}

// A shipped skill (Custom false, CreatedBy empty) must not carry either field across the
// wire — the fixture never has them, and this is what keeps that assertion meaningful.
func TestPublisherOmitsCustomFieldsForAShippedSkill(t *testing.T) {
	s := newSink(t, http.StatusAccepted)
	p := NewRegistryPublisher(s.server.URL)
	p.now = fixedNow

	_ = p.Publish(context.Background(), []skills.Skill{{Name: "planner", Type: skills.TypeAgent}})

	entry := s.last()["skills"].([]any)[0].(map[string]any)
	if _, ok := entry["custom"]; ok {
		t.Error("a shipped skill carries a `custom` field")
	}
	if _, ok := entry["created_by"]; ok {
		t.Error("a shipped skill carries a `created_by` field")
	}
}

// The directory a skill lives in is kern-orch's business: a filesystem path is an internal,
// not a contract, and a consumer must never be handed one.
func TestPublisherDoesNotLeakTheSkillDirectory(t *testing.T) {
	s := newSink(t, http.StatusAccepted)
	p := NewRegistryPublisher(s.server.URL)
	p.now = fixedNow

	_ = p.Publish(context.Background(), []skills.Skill{
		{Name: "A", Type: skills.TypeTool, Dir: "/home/someone/skills/a"},
	})

	entry := s.last()["skills"].([]any)[0].(map[string]any)
	for key, v := range entry {
		if s, ok := v.(string); ok && s == "/home/someone/skills/a" {
			t.Errorf("field %q carries the skill's directory across the contract", key)
		}
	}
	if _, ok := entry["dir"]; ok {
		t.Error("the payload has a `dir` field")
	}
}

// A producer with no skills must still be able to say so: an empty list is a statement, and
// it is what lets a sink tell an empty catalogue from one it never received.
func TestPublisherSendsAnEmptyCatalogueAsAList(t *testing.T) {
	s := newSink(t, http.StatusAccepted)
	p := NewRegistryPublisher(s.server.URL)
	p.now = fixedNow

	if err := p.Publish(context.Background(), nil); err != nil {
		t.Fatalf("Publish: %v", err)
	}

	got, ok := s.last()["skills"].([]any)
	if !ok {
		t.Fatalf("skills = %#v, want a list even when empty", s.last()["skills"])
	}
	if len(got) != 0 {
		t.Errorf("skills = %v, want none", got)
	}
}

// No URL configured means no publisher: the caller must be able to skip the work entirely
// rather than post into the void.
func TestPublisherIsDisabledWithoutAURL(t *testing.T) {
	p := NewRegistryPublisher("")

	if p.Enabled() {
		t.Error("a publisher with no URL reports itself enabled")
	}
	if err := p.Publish(context.Background(), []skills.Skill{{Name: "A", Type: skills.TypeTool}}); err != nil {
		t.Errorf("Publish on a disabled publisher = %v, want nil", err)
	}
}

// Publishing the catalogue is observability, exactly like the step reporter: a sink that is
// down must never be able to stop a run from starting.
func TestPublisherReportsSinkFailuresWithoutPanicking(t *testing.T) {
	s := newSink(t, http.StatusInternalServerError)
	p := NewRegistryPublisher(s.server.URL)
	p.now = fixedNow

	if err := p.Publish(context.Background(), []skills.Skill{{Name: "A", Type: skills.TypeTool}}); err == nil {
		t.Error("a refusing sink was reported as a success")
	}
}
