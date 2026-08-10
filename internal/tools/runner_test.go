package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"testing"

	"github.com/yoann/kern-orch/internal/skills"
)

func helperTool(t *testing.T, mode string) skills.Skill {
	t.Helper()
	return skills.Skill{
		Name: "greeting",
		Type: skills.TypeTool,
		Command: []string{
			os.Args[0],
			"-test.run=TestHelperProcess",
		},
	}
}

func helperEnv(mode string) []string {
	return append(os.Environ(), "GO_WANT_HELPER_PROCESS=1", "HELPER_MODE="+mode)
}

func TestInvokeReturnsLabelAndValue(t *testing.T) {
	sk := helperTool(t, "ok")
	r := &Runner{Env: helperEnv("ok")}
	res, err := r.Invoke(context.Background(), sk, map[string]any{"name": "Yoann"})
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	if res.Label != "Salutation" || res.Value != "Bonjour, Yoann !" {
		t.Fatalf("got %+v", res)
	}
	if res.AsOf.IsZero() {
		t.Error("AsOf not set")
	}
}

func TestInvokeReturnsTheChildsError(t *testing.T) {
	sk := helperTool(t, "error")
	r := &Runner{Env: helperEnv("error")}
	if _, err := r.Invoke(context.Background(), sk, nil); err == nil {
		t.Fatal("Invoke succeeded against a child that reported an error")
	}
}

func TestInvokeRejectsANonToolSkill(t *testing.T) {
	sk := skills.Skill{Name: "planner", Type: skills.TypeAgent}
	r := &Runner{}
	if _, err := r.Invoke(context.Background(), sk, nil); err == nil {
		t.Fatal("Invoke ran an agent skill as a tool")
	}
}

func TestInvokeRejectsASkillWithNoCommand(t *testing.T) {
	sk := skills.Skill{Name: "sum", Type: skills.TypeTool}
	r := &Runner{}
	if _, err := r.Invoke(context.Background(), sk, nil); err == nil {
		t.Fatal("Invoke ran a skill with no command")
	}
}

func TestInvokeValidatesBeforeSpawning(t *testing.T) {
	sk := helperTool(t, "ok")
	sk.Params = []skills.Param{{Name: "name", Type: "string", Required: true}}
	r := &Runner{Env: helperEnv("ok")}
	if _, err := r.Invoke(context.Background(), sk, map[string]any{}); err == nil {
		t.Fatal("Invoke ran with a missing required param")
	}
}

// TestHelperProcess is not a real test: when GO_WANT_HELPER_PROCESS=1 it impersonates a
// tool's subprocess, reading the Request from stdin and answering per HELPER_MODE.
func TestHelperProcess(t *testing.T) {
	if os.Getenv("GO_WANT_HELPER_PROCESS") != "1" {
		return
	}
	var req Request
	_ = json.NewDecoder(os.Stdin).Decode(&req)
	enc := json.NewEncoder(os.Stdout)
	switch os.Getenv("HELPER_MODE") {
	case "ok":
		name, _ := req.Input["name"].(string)
		_ = enc.Encode(Response{Label: "Salutation", Value: fmt.Sprintf("Bonjour, %s !", name)})
	case "error":
		_ = enc.Encode(Response{Error: "tool broke"})
	}
	os.Exit(0)
}
