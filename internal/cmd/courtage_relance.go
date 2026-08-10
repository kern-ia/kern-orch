package cmd

import (
	"encoding/json"

	"github.com/yoann/kern-orch/internal/graph"
)

// onRelanceNeeded routes after extraction_validee in examples/courtage-extraction.yaml
// (besoin #3, specs.md: relances pièces manquantes). memo_prep is unconditional — the
// memo drafting continues regardless — while the relance branch is conditional on
// pieces_manquantes (state key "interpretation", the demasked JSON dossier from besoin
// #1). A single edge/router combines both because kern-orch allows only one route per
// node (internal/graph/engine.go, routes map[string]RouteFunc) — Static's own multi-target
// fan-out cannot also be conditional, so this router does both jobs.
func onRelanceNeeded(s *graph.State) []string {
	raw, _ := s.Get("interpretation")
	text, _ := raw.(string)

	var parsed struct {
		PiecesManquantes []string `json:"pieces_manquantes"`
	}
	// Unmarshal error or empty text both fall through to an empty PiecesManquantes —
	// the safe default (no relance) rather than crashing or guessing.
	_ = json.Unmarshal([]byte(text), &parsed)

	if len(parsed.PiecesManquantes) > 0 {
		return []string{"memo_prep", "relance_prep"}
	}
	return []string{"memo_prep", "relance_non_necessaire"}
}
