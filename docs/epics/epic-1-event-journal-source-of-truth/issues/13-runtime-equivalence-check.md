---
type: Issue
title: "Opt-in runtime replay-equivalence check"
description: "Expose the projection-versus-replay comparison as a validated Config field, off by default."
tags: [epic-1]
timestamp: 2026-08-19T16:00:00Z
epic: 1
issue: 13
slug: runtime-equivalence-check
size: S
status: open
gh_issue: 24
resource: https://github.com/kern-ia/kern-orch/issues/24
depends_on: [10]
---

# Opt-in runtime replay-equivalence check

## Summary

Split out of issue 10, which grew past a reviewable PR. Issue 10 proves the equivalence holds in
tests over real engine runs. This issue exposes the same comparison at runtime so a suspect run can
be diagnosed without rebuilding.

It is deliberately second: the runtime check is useless until the test-side equivalence proves the
comparison itself is right.

## Scope

- A validated `Config` field controlling the check, **off by default**, with its env var documented
  in the `Env*` constant block in `internal/config` alongside the others.
- When enabled, the check compares the projection against a replay at level boundaries and fails
  loud, naming the divergent keys.
- When disabled, no additional projection work happens per level.

## Out of scope

- Making the check the default. It doubles the state work on every level of every run to defend
  against a class of bug introduced in code, not in data.
- Any change to the equivalence comparison itself — that is issue 10's, and this issue only wires it.

## Acceptance criteria

- [ ] The check is a validated `Config` field that defaults to off, and its env var is documented in
      `internal/config` in the same style as the existing ones, stating why it is its own variable.
- [ ] An invalid value for the field fails loud at load, per CONVENTIONS.md — never a silent
      fall-back to the default.
- [ ] With the check enabled, a deliberately corrupted projection is detected and the error names the
      divergent keys.
- [ ] With the check disabled, no additional projection work happens per level — asserted by
      instrumenting the projection call, not by timing.
- [ ] `go build ./...`, `go vet ./...` and `go test ./...` are green.
- [ ] An OKF fiche is added under `docs/index/` for this branch, per CONVENTIONS.md.

## Relevant files / areas

- `internal/config/config.go` — the `Env*` constant block and the `Config` struct.
- `internal/cmd/runtime.go` — where the check joins the hook chain, beside `multiStep`/`multiEvent`.
- Whatever issue 10 lands as the comparison helper.

## Dependencies

Blocked by issue 10.

## PR size note

Target ~500 changed lines; if this grows past ~1000, split it before opening the PR.
