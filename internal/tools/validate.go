package tools

import (
	"fmt"

	"github.com/yoann/kern-orch/internal/skills"
)

// Validate checks input against sk's declared params before anything is spawned: a caller
// missing a required value, or sending the wrong shape, learns synchronously rather than
// from a child process that never gets to say why it failed.
func Validate(sk skills.Skill, input map[string]any) error {
	for _, p := range sk.Params {
		v, ok := input[p.Name]
		if !ok {
			if p.Required {
				return fmt.Errorf("tools: missing required param %q", p.Name)
			}
			continue
		}
		if !matchesType(v, p.Type) {
			return fmt.Errorf("tools: param %q: want %s", p.Name, p.Type)
		}
	}
	return nil
}

// matchesType checks a decoded JSON value against a declared param type. An unrecognized
// type name is not validated — it is the tool author's business, not this package's.
func matchesType(v any, want string) bool {
	switch want {
	case "string":
		_, ok := v.(string)
		return ok
	case "number":
		_, ok := v.(float64)
		return ok
	case "bool":
		_, ok := v.(bool)
		return ok
	default:
		return true
	}
}
