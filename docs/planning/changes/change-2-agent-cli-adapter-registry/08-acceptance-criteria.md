---
type: Decision
title: "Acceptance criteria"
description: "What does 'done' observably mean for this change?"
tags: [decision, change]
timestamp: 2026-08-20T09:45:00Z
phase: change
decision: 08
slug: acceptance-criteria
status: decided
verdict: "Offline-provable test set plus a self-skipping real-binary set, matching the e2e self-skip convention already in this repo"
decided_via: triage
depends_on: ['scope-boundary']
change: 2
change_slug: agent-cli-adapter-registry
---

# Question
Same tooling constraint as epic 1: `go build ./...`, `go vet ./...`, `go test ./...`, no CI.
Real-CLI verification needs the actual binaries present, which local `go test` cannot assume —
acceptance splits between what unit/integration tests can prove offline and what needs a real
binary on the machine running the check.

# Options
- **Offline-provable set**: the registry selects the right adapter type for each
  `KERN_AGENT_KIND` value and fails loud on an invalid or missing one when `KERN_AGENT_CLI` is
  set; `Stub` and the existing provisional-protocol path (kept or explicitly removed, per
  decision 07's scope) are unaffected; `Start`/`Close` are called exactly once per run and
  `Close` runs on every exit path including `stop`; two concurrent fan-out nodes against a fake
  HTTP adapter open two sessions, never serialize; each adapter's wire-translation logic is unit
  tested against a canned real message (a recorded Claude Code `stream-json` transcript, a
  recorded OpenCode `/message` response body) rather than a hand-invented fixture.
- **Real-binary set, run only where the binary is present, explicitly allowed to skip
  otherwise** (the same self-skip convention `kern-orch`'s own `test:e2e` uses for
  `DEEPSEEK_API_KEY`): a real `claude -p --output-format=stream-json` round trip through the
  adapter produces a non-empty `AgentResult`; a real `opencode serve` round trip does the same.
- **Documentation**: `kern-exec`'s `wrap-agent-cli*.sh` examples updated for both adapters; an
  OKF fiche per merged feature branch, per `CONVENTIONS.md`.

# Recommendation
Both sets, with the real-binary set explicitly self-skipping rather than blocking a PR when
the binary is absent — matching the precedent already set for provider-key-gated e2e tests
elsewhere in this repo's own testing policy.

# Verdict

Offline-provable test set plus a self-skipping real-binary set, matching the e2e self-skip convention already in this repo.

Both sets, with the real-binary set explicitly self-skipping rather than blocking a PR when
the binary is absent — matching the precedent already set for provider-key-gated e2e tests
elsewhere in this repo's own testing policy.
