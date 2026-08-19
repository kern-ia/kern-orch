---
type: Issue
title: "Rewire the reporter to project the journal"
description: "internal/report becomes a projection of the journal instead of a parallel observer, with its wire format and fixtures untouched."
tags: [epic-1]
timestamp: 2026-08-19T10:20:00Z
epic: 1
issue: 11
slug: reporter-consumes-journal
size: M
status: open
gh_issue: 15
resource: https://github.com/kern-ia/kern-orch/issues/15
depends_on: [7]
---

# Rewire the reporter to project the journal

## Summary

Decision 02 makes `kern.step-event` a projection of the journal. This issue is that rewiring — and
its whole point is that **nothing changes on the wire**. The fixtures under `contracts/` staying
byte-identical and their tests staying green is the proof that kern-ui was never in this epic's
blast radius.

The 64-slot dropping queue stays exactly as it is. A journal that never drops and a report that may
drop are not in conflict: they are the durable record and the best-effort view of it, which is the
distinction this whole epic is built on.

## Scope

- `internal/report` derives its `StepEvent` from journal events rather than from the engine's
  `StepFunc` observing the live state.
- The flattening stays: `State` on the wire is the flat business data, never `graph.State`'s wire
  form with zones, the frozen counter and the internal step. That boundary is deliberate and
  documented in the type's own comment.
- Topology, requester and dossier keep riding on the first event of a run only.
- `Error` still set on the terminal event of a failed run, still naming every failed node.
- `Parent` still set on nested runs.

## Out of scope

- Any change to the `kern.step-event` version, field set, or JSON shape.
- Any change to the queue size, the drop behaviour, `DefaultTimeout` or `DefaultFlushTimeout`.
- Emitting anything new to consumers. CONVENTIONS.md's rule stands: kern-orch emits contracts and
  does not know who reads them.

## Acceptance criteria

- [ ] `contracts/kern.step-event.v1.json`, `v2.json`, `v2.failure.json` and `v2.nested.json` are
      unchanged — verified by `git diff` being empty for those paths.
- [ ] Every existing test in `internal/report` passes without modification, including the
      fixture-pinned ones.
- [ ] A run's emitted `StepEvent` sequence is identical before and after the rewiring, asserted by
      capturing the sink's received payloads.
- [ ] A broken or absent sink still costs a line on stderr and never a run.
- [ ] The queue still drops rather than blocks under a stalled sink.
- [ ] `go build ./...`, `go vet ./...` and `go test ./...` are green.
- [ ] An OKF fiche is added under `docs/index/` for this branch, per CONVENTIONS.md.

## Relevant files / areas

- `internal/report/http.go` — `StepEvent`, `flatten`, `stepQueueSize`, the hook constructor.
- `internal/report/contract_test.go`, `v2_test.go`, `http_test.go`, `queue_test.go`.
- `contracts/*.json`.
- `internal/cmd/runtime.go` — `multiStep`, `describeTopology`, `nestedRuns`.

## Dependencies

Blocked by 07.


## PR size note

Target ~500 changed lines; if this grows past ~1000, split it before opening the PR.
