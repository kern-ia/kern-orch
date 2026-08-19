---
type: Decision
title: "Acceptance criteria"
description: "What does 'done' observably mean for this change?"
tags: [decision, change]
timestamp: 2026-08-19T09:16:15Z
phase: change
decision: 10
slug: acceptance-criteria
status: decided
verdict: "The behaviour-observable set, plus an OKF fiche per merged feature branch"
decided_via: triage
depends_on: ['scope-boundary', 'invariant-enforcement']
change: 1
change_slug: event-journal-source-of-truth
---

# Question
`create-issues` derives per-issue acceptance criteria from the epic's, so these have to be
observable rather than aspirational, and checkable with the tooling that actually exists here:
`go build ./...`, `go vet ./...`, `go test ./...`, and the fixture tests under `contracts/`.

# Options
- **Behaviour-observable set**: replay equivalence proven on real engine runs; the external
  contract fixtures unchanged and green; resume driven by replay including an interrupted tail; a
  refused unknown schema version naming the path; nudge, freeze and the combination rule each
  recoverable from the journal alone.
- **Coverage-shaped set** (a percentage target, a file-count target). Measurable, but it says
  nothing about whether the journal is actually complete — and coverage is not currently measured at
  all in this repo.

# Recommendation
The behaviour-observable set. Every item on it is a test that fails before the change and passes
after, and the fixture clause is what makes "kern-ui did not break" a checked fact rather than an
intention. Add one documentation criterion, since CONVENTIONS.md requires it: an OKF fiche under
`docs/index/` per merged feature branch.

# Verdict

The behaviour-observable set, plus an OKF fiche per merged feature branch.

The behaviour-observable set. Every item on it is a test that fails before the change and passes
after, and the fixture clause is what makes "kern-ui did not break" a checked fact rather than an
intention. Add one documentation criterion, since CONVENTIONS.md requires it: an OKF fiche under
`docs/index/` per merged feature branch.
