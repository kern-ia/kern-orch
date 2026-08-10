package cmd

import (
	"testing"

	"github.com/yoann/kern-orch/internal/graph"
)

func TestOnStrategyModeRoutesAvisStraightToRedacteur(t *testing.T) {
	s := graph.NewState()
	s.Set("mode", "avis")

	got := onStrategyMode(s)

	if len(got) != 1 || got[0] != "redacteur" {
		t.Errorf("onStrategyMode(avis) = %v, want [redacteur]", got)
	}
}

func TestOnStrategyModeRoutesPropositionToApproval(t *testing.T) {
	s := graph.NewState()
	s.Set("mode", "proposition")

	got := onStrategyMode(s)

	if len(got) != 1 || got[0] != "confirm_strategie" {
		t.Errorf("onStrategyMode(proposition) = %v, want [confirm_strategie]", got)
	}
}

func TestOnStrategyModeDefaultsToPropositionWhenModeMissing(t *testing.T) {
	// Safe default: an unrecognized or absent mode must never skip the human gate.
	got := onStrategyMode(graph.NewState())

	if len(got) != 1 || got[0] != "confirm_strategie" {
		t.Errorf("onStrategyMode(missing) = %v, want [confirm_strategie]", got)
	}
}

func TestDecisionRouterApproved(t *testing.T) {
	s := graph.NewState()
	s.Set(graph.DecisionKey("confirm_strategie"), string(graph.Approved))

	route := decisionRouter("confirm_strategie", []string{"redacteur"}, []string{"strategie_refusee"})

	if got := route(s); len(got) != 1 || got[0] != "redacteur" {
		t.Errorf("decisionRouter approved = %v, want [redacteur]", got)
	}
}

func TestDecisionRouterRefused(t *testing.T) {
	s := graph.NewState()
	s.Set(graph.DecisionKey("confirm_strategie"), string(graph.Refused))

	route := decisionRouter("confirm_strategie", []string{"redacteur"}, []string{"strategie_refusee"})

	if got := route(s); len(got) != 1 || got[0] != "strategie_refusee" {
		t.Errorf("decisionRouter refused = %v, want [strategie_refusee]", got)
	}
}

func TestDecisionRouterDefaultsToRefusedWhenNoDecisionRecorded(t *testing.T) {
	route := decisionRouter("confirm_publication", []string{"publieur"}, []string{"refus_publication"})

	if got := route(graph.NewState()); len(got) != 1 || got[0] != "refus_publication" {
		t.Errorf("decisionRouter missing = %v, want [refus_publication]", got)
	}
}

func TestDecisionRouterReadsItsOwnNodeIDNotAnothersGate(t *testing.T) {
	// Two approval gates in the same graph must stay independent: a decision recorded
	// under "confirm_strategie" must never leak into a router built for "confirm_publication".
	s := graph.NewState()
	s.Set(graph.DecisionKey("confirm_strategie"), string(graph.Approved))

	route := decisionRouter("confirm_publication", []string{"publieur"}, []string{"refus_publication"})

	if got := route(s); len(got) != 1 || got[0] != "refus_publication" {
		t.Errorf("decisionRouter cross-gate leak: got %v, want [refus_publication]", got)
	}
}
