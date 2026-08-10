package cmd

import (
	"testing"

	"github.com/yoann/kern-orch/internal/graph"
)

func TestOnRelanceNeededAlwaysIncludesMemoPrep(t *testing.T) {
	s := graph.NewState()
	s.Set("interpretation", `{"pieces_manquantes": []}`)

	got := onRelanceNeeded(s)

	found := false
	for _, id := range got {
		if id == "memo_prep" {
			found = true
		}
	}
	if !found {
		t.Errorf("onRelanceNeeded(%v) missing memo_prep, always required", got)
	}
}

func TestOnRelanceNeededRoutesToRelancePrepWhenPiecesAreMissing(t *testing.T) {
	s := graph.NewState()
	s.Set("interpretation", `{"pieces_manquantes": ["avis d'imposition N-1"]}`)

	got := onRelanceNeeded(s)

	if len(got) != 2 || got[1] != "relance_prep" {
		t.Errorf("got %v, want [memo_prep relance_prep]", got)
	}
}

func TestOnRelanceNeededRoutesToRelanceNonNecessaireWhenNothingIsMissing(t *testing.T) {
	s := graph.NewState()
	s.Set("interpretation", `{"pieces_manquantes": []}`)

	got := onRelanceNeeded(s)

	if len(got) != 2 || got[1] != "relance_non_necessaire" {
		t.Errorf("got %v, want [memo_prep relance_non_necessaire]", got)
	}
}

func TestOnRelanceNeededDefaultsToNoRelanceOnUnparseableInterpretation(t *testing.T) {
	// Safe default: never send a relance built from data we couldn't actually read.
	s := graph.NewState()
	s.Set("interpretation", "pas du JSON")

	got := onRelanceNeeded(s)

	if len(got) != 2 || got[1] != "relance_non_necessaire" {
		t.Errorf("got %v, want [memo_prep relance_non_necessaire]", got)
	}
}

func TestOnRelanceNeededDefaultsToNoRelanceWhenInterpretationMissing(t *testing.T) {
	got := onRelanceNeeded(graph.NewState())

	if len(got) != 2 || got[1] != "relance_non_necessaire" {
		t.Errorf("got %v, want [memo_prep relance_non_necessaire]", got)
	}
}
