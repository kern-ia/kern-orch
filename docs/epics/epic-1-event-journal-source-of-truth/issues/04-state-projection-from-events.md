---
type: Issue
title: "Project a run state by replaying its journal"
description: "A pure replay function that rebuilds graph.State from an ordered event slice under the recorded combination rule."
tags: [epic-1]
timestamp: 2026-08-19T10:20:00Z
epic: 1
issue: 04
slug: state-projection-from-events
size: M
status: open
gh_issue: 8
resource: https://github.com/kern-ia/kern-orch/issues/8
depends_on: [2]
---

# Project a run state by replaying its journal

## Summary

The half of the change that makes the state a projection rather than a record. Pure function over
an ordered event slice, no I/O — which is what makes it exhaustively testable before anything else
depends on it.

The combination rule is why this cannot be a naive "apply every produced key" fold.
`internal/graph/engine.go`'s `runLevel` **replaces** the shared state with the branch when the
frontier holds one node, and **merges additively** when it holds several — deliberately, so that
`Freeze` and key deletions propagate. Replay cannot re-derive which happened from the keys alone,
so it reads the rule off the level-closed event.

## Scope

- `Project(events []journal.Event) (*graph.State, error)` in the journal package or a sibling.
- Applies node-produced keys in frontier order within a level, then the level's recorded
  combination rule.
- Applies freeze events as a **replacement** — the state after a freeze holds exactly what the
  event recorded as carried over, and nothing else — and increments `Frozen`.
- Applies nudge events as key sets.
- Restores zone labels and the `Step` counter.
- Fails loud on an inconsistent sequence (a level closed that was never opened, a node produced
  that never started).

## Out of scope

- Reading events from the store — that is the caller's job (issues 07, 08).
- Closing an interrupted tail — issue 08.
- The equivalence check against a live run — issue 10.

## Acceptance criteria

- [ ] Projecting a single-node level yields the branch state, including keys the node deleted.
- [ ] Projecting a fan-out level yields the additive merge of every branch, in frontier order.
- [ ] Projecting a run containing a freeze yields exactly the carried-over keys, all in the
      persistent zone, with `Frozen` incremented — asserted with **a non-default carry-over**, not
      only `DefaultCarryOver`. The epic's Notes flag this precise gap: no current test exercises a
      non-default carry-over, and it is the case where a delta-shaped model silently breaks.
- [ ] Projecting a run containing a nudge yields the nudged value.
- [ ] Zones and `Step` survive projection.
- [ ] An out-of-order or incoherent event sequence returns an error naming what was inconsistent.
- [ ] `go build ./...`, `go vet ./...` and `go test ./...` are green.
- [ ] An OKF fiche is added under `docs/index/` for this branch, per CONVENTIONS.md.

## Relevant files / areas

- `internal/graph/state.go` — `State`, `Set`, `SetZoned`, `Merge`, `replaceWith`, `stateWire`.
- `internal/graph/zones.go` — `Freeze`, `CarryOver`, `DefaultCarryOver`, `ZonePersistent`,
  `ZoneEphemeral`.
- `internal/graph/engine.go` — `runLevel`, the `single := len(results) == 1` branch that decides
  replace versus merge.
- `internal/journal/` — the types from issue 02.

## Dependencies

Blocked by 02. Blocks 07, 08, 10.


## PR size note

Target ~500 changed lines; if this grows past ~1000, split it before opening the PR.
