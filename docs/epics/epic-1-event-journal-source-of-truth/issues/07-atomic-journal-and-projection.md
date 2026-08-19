---
type: Issue
title: "Write the journal and the projection cache in one transaction"
description: "checkpointHook becomes an atomic journal append plus projection upsert, so the snapshot can never be written independently."
tags: [epic-1]
timestamp: 2026-08-19T10:20:00Z
epic: 1
issue: 07
slug: atomic-journal-and-projection
size: M
status: open
gh_issue: 11
resource: https://github.com/kern-ia/kern-orch/issues/11
depends_on: [3, 4, 5]
---

# Write the journal and the projection cache in one transaction

## Summary

This is where the snapshot stops being a second truth. Decision 01: the journal is authoritative
and the existing `checkpoints` row is redefined as a **materialized projection written atomically
with the events of its level**. It cannot drift, because it is never written independently.

The eight existing call sites keep reading that row unchanged, which is what keeps this epic's
blast radius on the write and resume paths instead of spreading it across every consumer at once.

## Scope

- Replace `checkpointHook`'s bare `store.Save` with a single transaction: append the level's
  events, then upsert the projection row for `(run_id, step)`.
- The projection row's `state` column is produced by issue 04's `Project`, not by marshalling the
  live `*graph.State` — otherwise it is still an independent write that merely looks derived.
- A failure in either half rolls back both. The run aborts, because durability comes first: the
  existing `multiStep` comment already establishes that ordering ("durability comes first and
  best-effort observers last").
- Keep the `queued` marker at the reserved step `-1` working: it is written before any event
  exists, and `Latest`/`List` both key on `MAX(step)`.

## Out of scope

- Changing how `status`, `serve`'s five `Latest` calls, or the daemon's function types read the
  projection. They keep working; that is the point.
- Resume — issue 08.
- The reporter — issue 11.

## Acceptance criteria

- [ ] After a level completes, the events and the projection row for that step exist together; no
      test can observe one without the other.
- [ ] Forcing the projection upsert to fail leaves no events for that level in the table, and the
      run aborts with an error.
- [ ] Forcing the event append to fail leaves the previous projection row intact and the run
      aborts.
- [ ] The projection row's state equals `Project(events up to that level)` — asserted directly, not
      by comparing two marshalled live states.
- [ ] `status` and `GET /api/v1/runs/{id}` return what they returned before this change, for a run
      that completed normally.
- [ ] The `queued` marker at step `-1` still makes a run visible immediately after acceptance.
- [ ] `go test -race ./...` passes.
- [ ] `go build ./...`, `go vet ./...` and `go test ./...` are green.
- [ ] An OKF fiche is added under `docs/index/` for this branch, per CONVENTIONS.md.

## Relevant files / areas

- `internal/cmd/runtime.go` — `checkpointHook`, `multiStep`, `openStore`.
- `internal/checkpoint/sqlite.go` — `Save`, and the new transaction spanning both writes.
- `internal/cmd/serve.go` — the `queued` marker write at `checkpoint.QueuedStep`, and the five
  `Latest` read sites that must stay working.
- `internal/daemon/router.go` — the two function types typed on `checkpoint.Record` and
  `checkpoint.Summary`.

## Dependencies

Blocked by 03, 04 and 05. Blocks 08, 09, 11.


## PR size note

Target ~500 changed lines; if this grows past ~1000, split it before opening the PR.
