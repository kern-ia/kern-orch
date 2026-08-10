package tools

import (
	"testing"

	"github.com/yoann/kern-orch/internal/skills"
)

func TestValidateRejectsMissingRequiredParam(t *testing.T) {
	sk := skills.Skill{Params: []skills.Param{{Name: "name", Type: "string", Required: true}}}
	if err := Validate(sk, map[string]any{}); err == nil {
		t.Fatal("Validate accepted a missing required param")
	}
}

func TestValidateAcceptsMissingOptionalParam(t *testing.T) {
	sk := skills.Skill{Params: []skills.Param{{Name: "loud", Type: "bool", Required: false}}}
	if err := Validate(sk, map[string]any{}); err != nil {
		t.Fatalf("Validate: %v", err)
	}
}

func TestValidateRejectsWrongType(t *testing.T) {
	sk := skills.Skill{Params: []skills.Param{{Name: "name", Type: "string", Required: true}}}
	if err := Validate(sk, map[string]any{"name": 42.0}); err == nil {
		t.Fatal("Validate accepted a number where a string was declared")
	}
}

func TestValidateAcceptsCorrectTypes(t *testing.T) {
	sk := skills.Skill{Params: []skills.Param{
		{Name: "name", Type: "string", Required: true},
		{Name: "count", Type: "number"},
		{Name: "loud", Type: "bool"},
	}}
	err := Validate(sk, map[string]any{"name": "yoann", "count": 3.0, "loud": true})
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}
}
