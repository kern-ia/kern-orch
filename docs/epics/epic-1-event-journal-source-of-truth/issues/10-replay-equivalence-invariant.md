---
type: Issue
title: "Enforce replay equivalence as an executed invariant"
description: "Assert over real engine runs that replaying a journal yields the run's final state, plus an opt-in runtime check."
tags: [epic-1]
timestamp: 2026-08-19T10:20:00Z
epic: 1
issue: 10
slug: replay-equivalence-invariant
size: M
status: open
gh_issue: 14
resource: https://github.com/kern-ia/kern-orch/issues/14
depends_on: [6, 8, 9]
---

# Enforce replay equivalence as an executed invariant

## Summary

The second half of this epic. An invariant nobody executes is a comment, and the failure it guards
against is silent: a state key that appears with no event explaining it does not crash anything, it
just makes every later replay quietly wrong.

Decision 08 puts the check over **real engine runs** rather than as a unit test of the journal
package — that is what gives it teeth, because it exercises the emission sites rather than
hand-built event slices.

## Scope

- A test helper that runs a graph end to end and asserts `Project(journal) == final live state`,
  applied across the cases that matter: a single-node frontier, a fan-out, a freeze (default and
  non-default carry-over), a nudge, an approval, and a nested subgraph.
- An opt-in runtime check comparing projection against replay at level boundaries, **off by
  default**, exposed as a validated `Config` field per CONVENTIONS.md's rule that
  deployment-varying choices are configuration rather than constants and that a `DEFAULT_*`
  constant or test hook is not configurability.
- The runtime check fails loud, naming the divergent keys.

## Out of scope

- Making the runtime check the default. It doubles the state work on every level of every run to
  defend against a class of bug introduced in code, not in data.
- Coverage percentage targets. Decision 10 rejected a coverage-shaped acceptance set: it says
  nothing about whether the journal is complete, and coverage is not measured in this repo at all.

## Acceptance criteria

- [ ] Replay equivalence is asserted over real engine runs for: single-node frontier, fan-out,
      freeze with `DefaultCarryOver`, freeze with a non-default carry-over, nudge, approval, nested
      subgraph.
- [ ] The runtime check is a validated `Config` field, defaults to off, and its env var is
      documented alongside the others in `internal/config`.
- [ ] With the check enabled, a deliberately corrupted projection is detected and the error names
      the divergent keys.
- [ ] With the check disabled, no additional projection work happens per level.
- [ ] `go build ./...`, `go vet ./...` and `go test ./...` are green.
- [ ] An OKF fiche is added under `docs/index/` for this branch, per CONVENTIONS.md.

## Relevant files / areas

- `internal/config/config.go` — the `Config` struct and `FromEnv`, plus the `Env*` constant block
  where the new variable is documented.
- `internal/graph/engine_test.go`, `subgraph_test.go`, `zones_test.go` — the existing real-run
  coverage this extends.
- `internal/cmd/runtime.go` — where the check is wired into the hook chain.

## Dependencies

Blocked by 06, 08 and 09.


## PR size note

Target ~500 changed lines; if this grows past ~1000, split it before opening the PR.
