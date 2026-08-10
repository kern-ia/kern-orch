package cmd

import (
	"context"
	"testing"

	"github.com/yoann/kern-orch/internal/graph"
)

// The auto-publish graph (community-management-agency-auto) only skips the human
// approval gate for channels this project has a REAL connector for (Telegram, X) —
// everywhere else, run_publieur's own G2 guard (Python side) means nothing would happen
// anyway, so routing a draft-only channel to confirm_publication instead of straight to
// publieur costs nothing and keeps the safe default.

func TestOnAutoPublishRouteSendsTelegramStraightToPublieur(t *testing.T) {
	s := graph.NewState()
	s.Set("brief_editorial", "**Plateforme(s)** : Telegram")

	got := onAutoPublishRoute(s)

	if len(got) != 1 || got[0] != "auto_approve" {
		t.Errorf("onAutoPublishRoute(telegram) = %v, want [auto_approve]", got)
	}
}

func TestOnAutoPublishRouteSendsXStraightToPublieur(t *testing.T) {
	s := graph.NewState()
	s.Set("brief_editorial", "Plateforme(s) : X")

	got := onAutoPublishRoute(s)

	if len(got) != 1 || got[0] != "auto_approve" {
		t.Errorf("onAutoPublishRoute(X) = %v, want [auto_approve]", got)
	}
}

func TestOnAutoPublishRouteRequiresApprovalForDraftOnlyChannels(t *testing.T) {
	s := graph.NewState()
	s.Set("brief_editorial", "Plateforme(s) : LinkedIn")

	got := onAutoPublishRoute(s)

	if len(got) != 1 || got[0] != "confirm_publication" {
		t.Errorf("onAutoPublishRoute(linkedin) = %v, want [confirm_publication]", got)
	}
}

func TestOnAutoPublishRouteDefaultsToApprovalWhenBriefMissing(t *testing.T) {
	got := onAutoPublishRoute(graph.NewState())

	if len(got) != 1 || got[0] != "confirm_publication" {
		t.Errorf("onAutoPublishRoute(missing) = %v, want [confirm_publication]", got)
	}
}

// Real bug, found live: strategiste sometimes writes "Plateforme :" (singular), not
// always "Plateforme(s) :" — the original regex required the literal "(s)", so a real
// Telegram-bound run fell through to confirm_publication instead of auto-publishing.
func TestOnAutoPublishRouteDetectsTelegramWithoutThePluralS(t *testing.T) {
	s := graph.NewState()
	s.Set("brief_editorial", "Plateforme : Telegram (seule pertinente ici).")

	got := onAutoPublishRoute(s)

	if len(got) != 1 || got[0] != "auto_approve" {
		t.Errorf("onAutoPublishRoute(no plural s) = %v, want [auto_approve]", got)
	}
}

// The letter X alone must not fire on ordinary text — same requirement as the Python
// X_PLATFORM_RE this mirrors (skills/community-management-agency/agent_cli.py).
func TestOnAutoPublishRouteDoesNotMatchXInsideAWord(t *testing.T) {
	s := graph.NewState()
	s.Set("brief_editorial", "Plateforme(s) : LinkedIn (choix maximal pour ce public)")

	got := onAutoPublishRoute(s)

	if len(got) != 1 || got[0] != "confirm_publication" {
		t.Errorf("onAutoPublishRoute(no false positive) = %v, want [confirm_publication]", got)
	}
}

func TestAutoApproveToolRecordsAnApprovedDecisionForConfirmPublication(t *testing.T) {
	s := graph.NewState()

	if err := autoApproveTool(context.Background(), s); err != nil {
		t.Fatalf("autoApproveTool: %v", err)
	}

	v, ok := s.Get(graph.DecisionKey("confirm_publication"))
	if !ok || v != string(graph.Approved) {
		t.Errorf("decision:confirm_publication = %v, %v — want %q, true", v, ok, graph.Approved)
	}
}
