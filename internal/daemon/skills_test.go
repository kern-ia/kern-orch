package daemon

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/yoann/kern-orch/internal/skills"
)

type createSkillCall struct {
	name, description, createdBy string
	steps                        []skills.Step
}

type deleteSkillCall struct{ name, requestedBy string }

func (f *fakeRunner) CreateSkill(_ context.Context, name, description, createdBy string, steps []skills.Step) (skills.Skill, error) {
	f.createSkillCalls = append(f.createSkillCalls, createSkillCall{name, description, createdBy, steps})
	return f.createSkillResult, f.createSkillErr
}

func (f *fakeRunner) DeleteSkill(_ context.Context, name, requestedBy string) error {
	f.deleteSkillCalls = append(f.deleteSkillCalls, deleteSkillCall{name, requestedBy})
	return f.deleteSkillErr
}

func TestCreateSkillWritesTheStepsThrough(t *testing.T) {
	f := &fakeRunner{createSkillResult: skills.Skill{Name: "accueil", Type: skills.TypeAgent, Custom: true, CreatedBy: "elise"}}
	h := NewRouter(f, "")

	body, _ := json.Marshal(map[string]any{
		"name": "accueil", "description": "accueille un prospect", "actor": "elise",
		"steps": []map[string]string{
			{"name": "premier contact", "instructions": "Présente-toi."},
			{"name": "synthese", "instructions": "Résume l'échange."},
		},
	})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/skills", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201: %s", rec.Code, rec.Body)
	}
	if len(f.createSkillCalls) != 1 {
		t.Fatalf("CreateSkill called %d times, want 1", len(f.createSkillCalls))
	}
	call := f.createSkillCalls[0]
	if call.name != "accueil" || call.createdBy != "elise" || len(call.steps) != 2 {
		t.Errorf("call = %+v", call)
	}
	if call.steps[0].Name != "premier contact" || call.steps[1].Instructions != "Résume l'échange." {
		t.Errorf("steps = %+v", call.steps)
	}

	var got skills.Skill
	if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.Name != "accueil" {
		t.Errorf("response = %+v", got)
	}
}

func TestCreateSkillRejectsAnEmptyName(t *testing.T) {
	f := &fakeRunner{}
	h := NewRouter(f, "")

	body, _ := json.Marshal(map[string]any{"name": "", "actor": "elise", "steps": []map[string]string{{"name": "a", "instructions": "b"}}})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/skills", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
	if len(f.createSkillCalls) != 0 {
		t.Error("the runner was called despite the missing name")
	}
}

func TestCreateSkillSurfacesTheRunnersError(t *testing.T) {
	f := &fakeRunner{createSkillErr: errNamedTaken}
	h := NewRouter(f, "")

	body, _ := json.Marshal(map[string]any{"name": "existant", "actor": "elise", "steps": []map[string]string{{"name": "a", "instructions": "b"}}})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/skills", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400: %s", rec.Code, rec.Body)
	}
}

func TestDeleteSkillRemovesTheOwnersCreation(t *testing.T) {
	f := &fakeRunner{}
	h := NewRouter(f, "")

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/skills/accueil", bytes.NewReader(mustJSON(t, map[string]string{"actor": "elise"})))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body)
	}
	if len(f.deleteSkillCalls) != 1 || f.deleteSkillCalls[0] != (deleteSkillCall{"accueil", "elise"}) {
		t.Errorf("calls = %+v", f.deleteSkillCalls)
	}
}

func TestDeleteSkillMapsUnknownTo404(t *testing.T) {
	f := &fakeRunner{deleteSkillErr: ErrUnknownSkill}
	h := NewRouter(f, "")

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/skills/fantome", bytes.NewReader(mustJSON(t, map[string]string{"actor": "elise"})))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}

func TestDeleteSkillMapsWrongOwnerTo403(t *testing.T) {
	f := &fakeRunner{deleteSkillErr: ErrNotSkillOwner}
	h := NewRouter(f, "")

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/skills/accueil", bytes.NewReader(mustJSON(t, map[string]string{"actor": "quelquun-dautre"})))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", rec.Code)
	}
}

func mustJSON(t *testing.T, v any) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

var errNamedTaken = errors.New(`skills: "existant" already exists`)
