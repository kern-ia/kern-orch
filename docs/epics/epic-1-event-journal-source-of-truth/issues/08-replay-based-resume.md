---
type: Issue
title: "Resume by replaying the journal, closing an interrupted tail"
description: "resume rebuilds state from events rather than trusting the cache, and an interrupted run's tail becomes an explicit fact."
tags: [epic-1]
timestamp: 2026-08-19T10:20:00Z
epic: 1
issue: 08
slug: replay-based-resume
size: M
status: open
gh_issue: 12
resource: https://github.com/kern-ia/kern-orch/issues/12
depends_on: [4, 7]
---

# Resume by replaying the journal, closing an interrupted tail

## Summary

Decision 05: if resume does not replay, nothing in the system ever proves the journal is complete —
and an incomplete journal that is never exercised is worse than no journal, because it looks
authoritative and is not.

`resume` today reads `store.Latest` and calls `Engine.RunFrom(ctx, rec.State, rec.Frontier)`. Under
decision 01 that row is a cache, and a cache is allowed to be stale or absent. Resume is the one
path where being wrong is unrecoverable, because it silently continues a run from a state that
never existed.

A run killed mid-level is invisible today — no checkpoint was written, so resume restarts the whole
level. With per-node events the tail is visibly incomplete, and that becomes a recorded fact rather
than an ambiguity.

## Scope

- `resume` reads the run's events and projects the state, rather than taking the cached row's state.
- Before replaying, detect an unclosed tail — a level opened and never closed, nodes started and
  never resolved — and **append explicit synthetic events** closing it and marking the run
  interrupted. The synthetic events are ordinary journal records, distinguishable as synthetic.
- The frontier resume restarts from is derived from the closed journal.
- The graph path keeps travelling in the projection row, so `resume <run-id>` still needs no graph
  argument.

## Out of scope

- Retrying or partially re-executing the nodes that had completed in the interrupted level. Resume
  keeps restarting the level; this issue only makes the interruption visible and the state exact.
- The equivalence check — issue 10.

## Acceptance criteria

- [ ] `resume` on a cleanly checkpointed run produces exactly the state the run had, derived by
      replay, and the run completes as before.
- [ ] A run killed mid-level, then resumed, has synthetic tail-closing events appended, is marked
      interrupted in its journal, and resumes from the correct frontier.
- [ ] The interruption is visible when reading the journal afterwards — a test asserts the
      synthetic events are present and identifiable as synthetic.
- [ ] Deliberately corrupting the cached projection row does **not** change what `resume`
      reconstructs.
- [ ] `resume <run-id>` still works with no graph argument.
- [ ] `POST /api/v1/runs/{id}/resume` behaves identically to the CLI path.
- [ ] `go build ./...`, `go vet ./...` and `go test ./...` are green.
- [ ] An OKF fiche is added under `docs/index/` for this branch, per CONVENTIONS.md.

## Relevant files / areas

- `internal/cmd/commands.go:74` — the `store.Latest` call behind the `resume` command.
- `internal/cmd/serve.go:110` — `engine.RunFrom(ctx, resume.State, resume.Frontier)`.
- `internal/checkpoint/resume_test.go` — the existing resume coverage.
- `internal/graph/engine.go` — `RunFrom`.

## Dependencies

Blocked by 04 and 07. Blocks 10.


## PR size note

Target ~500 changed lines; if this grows past ~1000, split it before opening the PR.
