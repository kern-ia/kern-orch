package cmd

import "github.com/yoann/kern-orch/internal/graph"

// decisionRouter generalizes onConfirmDecision (internal/cmd/runtime.go), which is
// hardcoded to a single approval node named "confirm" and two fixed target ids
// "approved"/"refused". A graph with more than one approval gate needs each gate to
// read its own decision and route to its own targets — node ids must stay unique within
// one graph, so "approved"/"refused" cannot be reused verbatim by a second gate.
func decisionRouter(nodeID string, approved, refused []string) graph.RouteFunc {
	return func(s *graph.State) []string {
		if v, _ := s.Get(graph.DecisionKey(nodeID)); v == string(graph.Approved) {
			return approved
		}
		return refused
	}
}

// onStrategyMode routes after the community-management-agency graph's strategiste node.
// The Python adapter (skills/community-management-agency/agent_cli.py) records the mode
// it ran under state key "mode": "avis" when the user supplied their own strategy (the
// strategist only reviewed it — nothing new to validate, so the graph goes straight to
// drafting), anything else ("proposition", or missing) means the strategist invented the
// strategy itself and a human must approve it before any drafting starts.
func onStrategyMode(s *graph.State) []string {
	if v, _ := s.Get("mode"); v == "avis" {
		return []string{"redacteur"}
	}
	return []string{"confirm_strategie"}
}
