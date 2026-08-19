---
type: Issue
title: "Record nudge, freeze and the combination rule as events"
description: "The three state mutations that no node performs become first-class events, which is what gives the invariant meaning."
tags: [epic-1]
timestamp: 2026-08-19T17:20:00Z
epic: 1
issue: 06
slug: non-node-mutations-as-events
size: M
status: pr-open
gh_pr: 26
gh_issue: 10
resource: https://github.com/kern-ia/kern-orch/issues/10
depends_on: [5]
---

# Record nudge, freeze and the combination rule as events

## Summary

Three state changes happen outside any node's execution and are invisible in the durable record
today. The invariant this epic adopts — anything a node can read must be reconstructable from the
journal — has no meaning until all three are recorded.

- **Nudge**: `steer.Mailbox.DrainNudges` applies queued key/value pairs through the engine's
  `NudgeFunc` before a level starts. A human changed the run and nothing records that they did.
- **Freeze**: `State.Freeze` replaces the state's contents with the carry-over, resets the zone map
  and increments `Frozen`.
- **The combination rule**: emitted with issue 05's level-closed event; this issue verifies it end
  to end rather than in isolation.

Decision 06 rejected recording only the resulting delta: the journal would then answer "what
changed" and never "who changed it", and human intervention is exactly what an audit trail exists
to show.

## Scope

- Emit a nudge event when `DrainNudges` applies pairs, carrying **the origin** — the run's
  `Requester` is already available on the checkpoint record and travels on the first step event.
- Emit a freeze event carrying what was carried over and what was dropped, from `State.Freeze`
  or from the `freeze` builtin tool that calls it — pick the site that sees both sets.
- A test proving a run that freezes with a **non-default carry-over** replays exactly. This is
  called out in the epic's Notes as the case a delta-shaped model gets wrong and that no current
  test covers.

## Out of scope

- Making `steer.Mailbox` durable. Explicitly out per decision 09: the journal makes it nearly free,
  which is exactly why it needs saying. It stays in-memory, as its package comment argues.
- Approval decisions, which an `ApprovalNode` already records as an ordinary state key and which
  therefore ride on issue 05's node-produced event.

## Acceptance criteria

- [ ] A run that receives a nudge produces a nudge event naming the key, the value and the origin.
- [ ] A run that freezes produces a freeze event listing both the carried-over keys and the dropped
      keys.
- [ ] Replaying a run that froze with `DefaultCarryOver` yields the same state as the live run.
- [ ] Replaying a run that froze with **a non-default carry-over** yields the same state as the
      live run.
- [ ] Replaying a run that was nudged yields the same state as the live run.
- [ ] `steer.Mailbox` still persists nothing, and its package comment still says why.
- [ ] `go build ./...`, `go vet ./...` and `go test ./...` are green.
- [ ] An OKF fiche is added under `docs/index/` for this branch, per CONVENTIONS.md.

## Relevant files / areas

- `internal/graph/zones.go` — `Freeze`, `CarryOver`, `DefaultCarryOver`.
- `internal/steer/mailbox.go` — `Nudge`, `DrainNudges`.
- `internal/cmd/runtime.go` — the `freeze` builtin tool registration in `builtinRegistry`, and
  where the mailbox is bound as `OnBeforeLevel`.
- `internal/checkpoint/store.go` — `Record.Requester`, the origin to carry.

## Dependencies

Blocked by 05. Blocks 10.


## PR size note

Target ~500 changed lines; if this grows past ~1000, split it before opening the PR.
