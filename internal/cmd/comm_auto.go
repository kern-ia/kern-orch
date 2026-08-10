package cmd

import (
	"context"
	"regexp"

	"github.com/yoann/kern-orch/internal/graph"
)

// telegramPlatformRE and xPlatformRE mirror TELEGRAM_PLATFORM_RE / X_PLATFORM_RE in
// skills/community-management-agency/agent_cli.py — kept in sync by hand since Go and
// Python cannot share a regex. Both read the same "Plateforme(s) :" line the strategiste
// prompt produces; \bX\b stays case-sensitive on purpose so the bare letter never fires
// on ordinary French text (a global case-insensitive match would).
var (
	telegramPlatformRE = regexp.MustCompile(`(?i:plateforme(\(s\))?)[*_\s]*:[^\n]*(?i:telegram)`)
	xPlatformRE        = regexp.MustCompile(`(?i:plateforme(\(s\))?)[*_\s]*:[^\n]*\b(X|Twitter|twitter)\b`)
)

// onAutoPublishRoute is the entry-conditional edge of community-management-agency-auto,
// wired only in that graph's topology (examples/community-management-agency-auto.yaml) —
// the plain community-management-agency graph always routes redacteur -> confirm_publication
// and never uses this router. It skips the human approval gate ONLY for a channel this
// project has a real send connector for (Telegram, X, run_publieur in agent_cli.py) —
// anywhere else, publieur's own G2 guard means nothing would happen even if approved, so
// routing to confirm_publication instead costs nothing and keeps the safe default: a
// channel this router does not recognize always gets a human in the loop.
func onAutoPublishRoute(s *graph.State) []string {
	brief, _ := s.Get("brief_editorial")
	text, _ := brief.(string)

	if telegramPlatformRE.MatchString(text) || xPlatformRE.MatchString(text) {
		return []string{"auto_approve"}
	}
	return []string{"confirm_publication"}
}

// autoApproveTool records the same decision a human clicking "Valider" on
// confirm_publication would have — the graph's own approval bookkeeping, not a shortcut
// around it. publieur (agent_cli.py's run_publieur) reads this exact key regardless of
// which path set it, so no Python change was needed to support the auto route.
func autoApproveTool(_ context.Context, s *graph.State) error {
	s.Set(graph.DecisionKey("confirm_publication"), string(graph.Approved))
	return nil
}
