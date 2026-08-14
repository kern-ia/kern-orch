package skills

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// Step is one agent instruction in a created sub-agent's chain (C11).
type Step struct {
	Name         string
	Instructions string
}

// validName matches the kebab-case convention every shipped skill directory already
// follows (courtage-extraction, community-management-agency, ...) — reused here rather
// than inventing a second naming rule, and safe as a directory name on every OS this
// runs on.
var validName = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)

// Create writes a new agent skill into customDir (C11) — one node per step, chained by
// unconditional edges in the order given. A single step needs no graph file at all: it is
// the ad-hoc one-node run C6 already builds for any agent skill with no `graph:` set.
//
// reg is the registry the caller already has loaded (shipped ∪ custom) — the collision
// check reads it rather than re-scanning disk, since the caller (the daemon handler) has
// one anyway to answer the request either way.
func Create(customDir string, reg *Registry, name, description, createdBy string, steps []Step) (Skill, error) {
	if !validName.MatchString(name) {
		return Skill{}, fmt.Errorf("skills: invalid name %q (want lowercase-kebab-case)", name)
	}
	if _, taken := reg.Get(name); taken {
		return Skill{}, fmt.Errorf("skills: %q already exists", name)
	}
	if len(steps) == 0 {
		return Skill{}, fmt.Errorf("skills: at least one step is required")
	}

	dir := filepath.Join(customDir, name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return Skill{}, fmt.Errorf("skills: create %q: %w", dir, err)
	}

	sk := Skill{
		Name:        name,
		Type:        TypeAgent,
		Description: description,
		CreatedBy:   createdBy,
		Dir:         dir,
		Custom:      true,
	}

	if len(steps) > 1 {
		graphPath := filepath.Join(dir, "graph.yaml")
		if err := os.WriteFile(graphPath, []byte(chainedGraphYAML(steps)), 0o644); err != nil {
			return Skill{}, fmt.Errorf("skills: write graph: %w", err)
		}
		// A shipped skill's `graph:` field is a path resolved against the process's
		// working directory (examples/prospection.yaml, not skills/prospection/…) —
		// topology.LoadFile has no notion of "relative to the skill's own directory". A
		// created skill's graph must be stored the same way, or dispatching it would
		// look for the file in the wrong place.
		sk.Graph = graphPath
	}

	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(skillMD(sk, steps)), 0o644); err != nil {
		return Skill{}, fmt.Errorf("skills: write SKILL.md: %w", err)
	}
	return sk, nil
}

// Delete removes a created skill's directory, refusing anything the requester did not
// create — checked here, not left to a caller's UI hiding a button.
func Delete(sk Skill, requestedBy string) error {
	if sk.CreatedBy != requestedBy {
		return fmt.Errorf("skills: %q was not created by %q", sk.Name, requestedBy)
	}
	return os.RemoveAll(sk.Dir)
}

func skillMD(sk Skill, steps []Step) string {
	var b strings.Builder
	b.WriteString("---\n")
	fmt.Fprintf(&b, "name: %s\n", sk.Name)
	b.WriteString("type: agent\n")
	fmt.Fprintf(&b, "description: %s\n", yamlString(sk.Description))
	if sk.Graph != "" {
		fmt.Fprintf(&b, "graph: %s\n", sk.Graph)
	}
	fmt.Fprintf(&b, "created_by: %s\n", sk.CreatedBy)
	b.WriteString("---\n\n")
	fmt.Fprintf(&b, "# %s\n\n", sk.Name)
	b.WriteString("Créé depuis l'éditeur no-code du Grimoire.\n\n")
	for i, s := range steps {
		fmt.Fprintf(&b, "%d. **%s** — %s\n", i+1, s.Name, s.Instructions)
	}
	return b.String()
}

// yamlString quotes a value that might otherwise break a single-line YAML scalar (a
// colon, a leading/trailing space) — descriptions are free text a person just typed.
func yamlString(s string) string {
	if s == "" {
		return `""`
	}
	return fmt.Sprintf("%q", s)
}

func chainedGraphYAML(steps []Step) string {
	ids := make([]string, len(steps))
	seen := make(map[string]int)
	for i, s := range steps {
		base := slugify(s.Name)
		if base == "" {
			base = fmt.Sprintf("etape-%d", i+1)
		}
		id := base
		if n := seen[base]; n > 0 {
			id = fmt.Sprintf("%s-%d", base, n+1)
		}
		seen[base]++
		ids[i] = id
	}

	var b strings.Builder
	fmt.Fprintf(&b, "entry: %s\n", ids[0])
	b.WriteString("nodes:\n")
	for i, s := range steps {
		fmt.Fprintf(&b, "  - id: %s\n", ids[i])
		b.WriteString("    type: agent\n")
		fmt.Fprintf(&b, "    prompt: %s\n", yamlString(s.Instructions))
	}
	b.WriteString("edges:\n")
	for i := 0; i < len(ids)-1; i++ {
		fmt.Fprintf(&b, "  - from: %s\n    to: [%s]\n", ids[i], ids[i+1])
	}
	return b.String()
}

var nonSlug = regexp.MustCompile(`[^a-z0-9]+`)

func slugify(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	s = nonSlug.ReplaceAllString(s, "-")
	return strings.Trim(s, "-")
}
