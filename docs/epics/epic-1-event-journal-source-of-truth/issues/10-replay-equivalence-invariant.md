---
type: Issue
title: "Assert replay equivalence over real engine runs"
description: "Assert over real engine runs that replaying a journal yields the run's final state, plus an opt-in runtime check."
tags: [epic-1]
timestamp: 2026-08-19T22:10:00Z
epic: 1
issue: 10
slug: replay-equivalence-invariant
size: M
status: pr-open
gh_issue: 14
gh_pr: 30
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

## Out of scope

- The opt-in runtime check itself. Split out to issue 13: it is a configuration surface with its own
  validation, its own env var and its own off-by-default behaviour, and it is useless until the
  test-side equivalence here proves the comparison is right in the first place.
- Coverage percentage targets. Decision 10 rejected a coverage-shaped acceptance set: it says
  nothing about whether the journal is complete, and coverage is not measured in this repo at all.

## Acceptance criteria

- [ ] Replay equivalence is asserted over real engine runs for: single-node frontier, fan-out,
      freeze with `DefaultCarryOver`, freeze with a non-default carry-over, nudge, approval, nested
      subgraph.
- [ ] Each equivalence assertion is shown to be non-vacuous: a deliberately wrong projection makes it
      fail, and the report says which mutation was used.
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
