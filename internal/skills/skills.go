// Package skills is the registry of available skills. Each skill is a directory with a
// SKILL.md whose YAML frontmatter declares whether it is a tool (direct execution) or an
// agent (a graph node backed by the external LLM CLI) — the tool-vs-agent distinction
// from spec §6.5.
package skills

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"gopkg.in/yaml.v3"
)

// Type is the skill kind declared in frontmatter.
type Type string

const (
	TypeTool  Type = "tool"
	TypeAgent Type = "agent"
)

// Param declares one named input a tool skill's command accepts, sent under that name in
// the invocation's `input` object.
type Param struct {
	Name     string `yaml:"name"`
	Type     string `yaml:"type"` // string | number | bool; unrecognized types skip validation
	Required bool   `yaml:"required"`
}

// Skill is the parsed metadata of one SKILL.md.
type Skill struct {
	Name        string `yaml:"name"`
	Type        Type   `yaml:"type"`
	Description string `yaml:"description"`
	// Command is the subprocess a tool skill runs when invoked (EPIC-03): argv, first
	// element the executable. Empty means this skill is not invocable this way — most
	// skills, including every agent, leave it unset.
	Command []string `yaml:"command,omitempty"`
	// Params is the input the command accepts. Declared here, not guessed from a call,
	// so a caller can be told what a tool needs before ever invoking it.
	Params []Param `yaml:"params,omitempty"`
	// Graph is a topology YAML path for an agent skill dispatched from chat. Empty (most
	// agent skills, e.g. planner) means the one-node ad-hoc run C6 already builds; set,
	// Dispatch loads this file instead — a fixed multi-node pipeline (e.g. a human
	// approval gate between two agent steps) triggered the same way as a single agent.
	Graph string `yaml:"graph,omitempty"`
	Dir   string `yaml:"-"`

	// CreatedBy names the account that created this skill through C11's write path.
	// Empty for every skill shipped on disk — nobody "created" those, they came with the
	// product.
	CreatedBy string `yaml:"created_by,omitempty"`
	// Custom is true when this skill was loaded from the custom tier (LoadMerged's second
	// directory) rather than the shipped one — never itself written to a SKILL.md, derived
	// from which directory produced it, the same way Dir is.
	Custom bool `yaml:"-"`
}

// Registry holds the loaded skills keyed by name.
type Registry struct {
	byName map[string]Skill
}

// Get returns the skill and whether it exists.
func (r *Registry) Get(name string) (Skill, bool) {
	s, ok := r.byName[name]
	return s, ok
}

// Len returns the number of skills.
func (r *Registry) Len() int { return len(r.byName) }

// List returns skills sorted by name.
func (r *Registry) List() []Skill {
	out := make([]Skill, 0, len(r.byName))
	for _, s := range r.byName {
		out = append(out, s)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// Load scans dir for immediate subdirectories containing a SKILL.md and parses each.
// A missing dir yields an empty registry (skills are optional).
func Load(dir string) (*Registry, error) {
	reg := &Registry{byName: make(map[string]Skill)}
	if err := loadInto(reg, dir, false); err != nil {
		return nil, err
	}
	return reg, nil
}

// LoadMerged reads the shipped, read-only directory and the custom (user-created) one,
// and merges them into one registry (C11). A name present in both keeps the shipped
// version — a creation can never shadow a skill the product ships, the same guarantee
// C11's write path gives in the other direction (a shipped update can never overwrite a
// creation, since they live in separate directories to begin with).
func LoadMerged(shippedDir, customDir string) (*Registry, error) {
	reg := &Registry{byName: make(map[string]Skill)}
	if err := loadInto(reg, shippedDir, false); err != nil {
		return nil, err
	}
	custom := &Registry{byName: make(map[string]Skill)}
	if err := loadInto(custom, customDir, true); err != nil {
		return nil, err
	}
	for name, sk := range custom.byName {
		if _, taken := reg.byName[name]; taken {
			continue
		}
		reg.byName[name] = sk
	}
	return reg, nil
}

func loadInto(reg *Registry, dir string, custom bool) error {
	entries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("skills: read %q: %w", dir, err)
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		path := filepath.Join(dir, e.Name(), "SKILL.md")
		raw, err := os.ReadFile(path)
		if os.IsNotExist(err) {
			continue // a subdir without SKILL.md is not a skill
		}
		if err != nil {
			return fmt.Errorf("skills: read %q: %w", path, err)
		}
		sk, err := parse(raw, e.Name())
		if err != nil {
			return fmt.Errorf("skills: %q: %w", e.Name(), err)
		}
		sk.Dir = filepath.Join(dir, e.Name())
		sk.Custom = custom
		reg.byName[sk.Name] = sk
	}
	return nil
}

// parse extracts the YAML frontmatter (between the first two `---` lines) of a SKILL.md.
func parse(raw []byte, dirName string) (Skill, error) {
	fm, err := frontmatter(raw)
	if err != nil {
		return Skill{}, err
	}
	var sk Skill
	if err := yaml.Unmarshal(fm, &sk); err != nil {
		return Skill{}, fmt.Errorf("bad frontmatter: %w", err)
	}
	if sk.Name == "" {
		sk.Name = dirName
	}
	switch sk.Type {
	case TypeTool, TypeAgent:
	case "":
		return Skill{}, fmt.Errorf("missing `type` (tool|agent)")
	default:
		return Skill{}, fmt.Errorf("invalid type %q (want tool|agent)", sk.Type)
	}
	return sk, nil
}

var fence = []byte("---")

func frontmatter(raw []byte) ([]byte, error) {
	trimmed := bytes.TrimLeft(raw, " \t\r\n")
	if !bytes.HasPrefix(trimmed, fence) {
		return nil, fmt.Errorf("no frontmatter (expected leading ---)")
	}
	rest := trimmed[len(fence):]
	end := bytes.Index(rest, append([]byte("\n"), fence...))
	if end < 0 {
		return nil, fmt.Errorf("unterminated frontmatter (missing closing ---)")
	}
	return rest[:end], nil
}
