// Package tools invokes a skill's declared command as a subprocess and reads back a
// display value — the read side of an Espace widget: ask a tool for what it shows right
// now, get a label and a rendered string. kern-ui never formats the domain value itself;
// the tool renders its own string.
package tools

import "time"

// Request is the single object sent to the child on stdin.
type Request struct {
	Input map[string]any `json:"input"`
}

// Response is the single object the child answers on stdout.
type Response struct {
	Label string `json:"label,omitempty"`
	Value string `json:"value,omitempty"`
	Error string `json:"error,omitempty"`
}

// Result is what Invoke returns: the child's answer plus when it was asked, which is the
// whole of "how stale it may be" until a caching layer exists to say otherwise. JSON tags
// matter here: this crosses the wire to whatever reads C5, and every other kern-orch
// contract (kern.step-event, kern.registry, kern.activity) is snake_case.
type Result struct {
	Label string    `json:"label"`
	Value string    `json:"value"`
	AsOf  time.Time `json:"as_of"`
}

// Spec is what a caller sees before invoking: enough to build a call, not the result of
// making one.
type Spec struct {
	Name        string  `json:"name"`
	Description string  `json:"description,omitempty"`
	Params      []Param `json:"params,omitempty"`
}

// Param mirrors skills.Param; a separate type so this package's public surface does not
// leak skills' own type identity into every caller that only wants a tool's shape.
type Param struct {
	Name     string `json:"name"`
	Type     string `json:"type"`
	Required bool   `json:"required"`
}
