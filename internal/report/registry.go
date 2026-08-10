package report

import (
	"context"
	"sort"
	"time"

	"github.com/yoann/kern-orch/internal/skills"
)

// Catalogue is the whole skills registry at one instant: `kern.registry/v1`.
//
// It is published whole, never patched. A skill removed upstream must disappear
// downstream, and an incremental protocol would have to carry deletions — a second thing
// to get wrong, for a payload that fits in a packet.
type Catalogue struct {
	Source string           `json:"source"`
	At     time.Time        `json:"at"`
	Skills []CatalogueEntry `json:"skills"`
}

// CatalogueEntry is one skill as it crosses the contract.
//
// Deliberately narrower than skills.Skill: the directory does not travel, because a
// filesystem path is an internal rather than a contract. Nor does any "wired" flag — a
// loaded skill is by definition available here, so the field would read true on every row
// and tell a consumer nothing it did not already know.
type CatalogueEntry struct {
	Name        string `json:"name"`
	Kind        string `json:"kind"` // tool | agent
	Description string `json:"description,omitempty"`
}

// RegistryPublisher posts the skills catalogue to a single configured URL.
//
// It is a separate type from HTTPReporter on purpose. The step reporter's comment holds
// here too — the URL is the whole contract, and the publisher knows nothing of the sink's
// route shape — so a sibling route cannot be guessed from the step URL. Two endpoints,
// two configured URLs.
type RegistryPublisher struct {
	URL     string
	Token   string // presented as a bearer credential; empty sends no header
	Timeout time.Duration

	now func() time.Time
}

// NewRegistryPublisher returns a publisher posting to url. An empty url yields a disabled
// publisher whose Publish is a no-op.
func NewRegistryPublisher(url string) *RegistryPublisher {
	return &RegistryPublisher{
		URL:     url,
		Timeout: DefaultTimeout,
		now:     func() time.Time { return time.Now().UTC() },
	}
}

// Enabled reports whether a sink is configured.
func (p *RegistryPublisher) Enabled() bool { return p.URL != "" }

// Publish sends the catalogue, sorted by name so a consumer never has to sort.
//
// Unlike the step hook it returns its error: publishing happens once, outside the graph,
// where the caller can decide what a failure is worth. It must still never abort a run —
// that is the caller's contract to keep, and cmd honours it by warning and carrying on.
func (p *RegistryPublisher) Publish(ctx context.Context, list []skills.Skill) error {
	if !p.Enabled() {
		return nil
	}

	// An absent list and an empty one mean the same thing — no skills — but only a list
	// survives the round trip unambiguously, and the sink distinguishes "no skills" from
	// "no producer".
	entries := make([]CatalogueEntry, 0, len(list))
	for _, s := range list {
		entries = append(entries, CatalogueEntry{
			Name:        s.Name,
			Kind:        string(s.Type),
			Description: s.Description,
		})
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name < entries[j].Name })

	return postJSON(ctx, p.URL, p.Token, p.timeout(), Catalogue{
		Source: "kern-orch",
		At:     p.now(),
		Skills: entries,
	})
}

func (p *RegistryPublisher) timeout() time.Duration {
	if p.Timeout > 0 {
		return p.Timeout
	}
	return DefaultTimeout
}
