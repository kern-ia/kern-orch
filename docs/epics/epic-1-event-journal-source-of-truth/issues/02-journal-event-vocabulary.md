---
type: Issue
title: "Define the internal journal event vocabulary"
description: "A new internal event type set, distinct from the kern.step-event wire contract, rich enough for exact replay."
tags: [epic-1]
timestamp: 2026-08-19T11:00:00Z
epic: 1
issue: 02
slug: journal-event-vocabulary
size: S
status: done
gh_issue: 6
gh_pr: 18
resource: https://github.com/kern-ia/kern-orch/issues/6
depends_on: [1]
---

# Define the internal journal event vocabulary

## Summary

Decision 02 of the change ledger is the load-bearing one: the journal gets its **own** internal
vocabulary, and `kern.step-event/v1` and `/v2` stay unchanged as a projection of it. The wire
contract cannot become the durable record, because every field replay needs — zones, the frozen
counter, per-node attribution — is a field its own comments say it deliberately refuses to carry
("marshalling the State itself would ship kern-orch's envelope across the contract, which is
nobody else's business").

This issue is types and encoding only: no store, no engine wiring.

## Scope

- A new package (suggested: `internal/journal`) declaring the event set:
  - run lifecycle: run started, run finished, run failed, run interrupted
  - level boundaries: level opened (frontier), level closed (**carrying the combination rule that
    was applied** — replace on a single-node frontier, additive merge on a fan-out)
  - node facts: node started, node produced keys, node failed
  - out-of-band mutations: nudge applied (with origin), freeze applied (with carry-over kept and
    dropped) — the payload shapes only; the emission is issues 05 and 06
- A monotonic per-run sequence number on every event, and the run id it belongs to.
- JSON encoding and decoding, round-trip tested.
- A closed discriminated union over the event type with an exhaustive switch, so adding an event
  type without handling it is a compile-time or test-time failure rather than a silent gap.

## Out of scope

- Persisting anything — issue 03.
- Projecting state from events — issue 04.
- Emitting events from the engine — issue 05.
- Any change to `internal/report`'s types or the `contracts/*.json` fixtures.

## Acceptance criteria

- [ ] Every event type round-trips through JSON encode/decode with no field loss, asserted per type.
- [ ] A level-closed event records which combination rule applied, and the two rules are
      distinguishable after a round trip.
- [ ] A freeze event records both what was carried over and what was dropped, as separate fields —
      not a delta (see the epic's Notes: freeze is a replacement, so key removals alone would
      reconstruct a state the run never had).
- [ ] A nudge event records its origin.
- [ ] Decoding an unknown event type fails loud rather than being skipped.
- [ ] The package imports nothing from `internal/report`, and `internal/report` imports nothing
      from it — asserted by a test or by the absence of the import, whichever is clearer.
- [ ] `go build ./...`, `go vet ./...` and `go test ./...` are green.
- [ ] An OKF fiche is added under `docs/index/` for this branch, per CONVENTIONS.md.

## Relevant files / areas

- New package, suggested `internal/journal/`.
- Read for reference, do not modify: `internal/report/http.go` (`StepEvent`, `Topology`, `Failure`,
  `Parent`) and `contracts/kern.step-event.v2.json`.
- `internal/graph/state.go` — `stateWire` shows the existing JSON conventions to match.

## Dependencies

Blocked by issue 01 (schema version exists before anything writes new records). Blocks 03, 04, 05.


## PR size note

Target ~500 changed lines; if this grows past ~1000, split it before opening the PR.
