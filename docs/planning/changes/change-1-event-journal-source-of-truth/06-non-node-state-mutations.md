---
type: Decision
title: "Non-node state mutations: nudge, freeze, and the combination rule"
description: "Which mutations that no node performed must become events for replay to be exact?"
tags: [decision, change]
timestamp: 2026-08-19T09:16:15Z
phase: change
decision: 06
slug: non-node-state-mutations
status: decided
verdict: "Nudge, freeze and the combination rule each get their own event type"
decided_via: triage
depends_on: ['journal-grain']
change: 1
change_slug: event-journal-source-of-truth
---

# Question
Three state mutations happen outside any node's execution, and all three are currently invisible
in the durable record:

- **Nudge.** `steer.Mailbox.DrainNudges` applies queued key/value pairs to the shared state through
  the engine's `NudgeFunc`, before a level starts. A human changed the run's state and nothing
  records that they did.
- **Freeze.** `State.Freeze` in `internal/graph/zones.go` replaces the state's contents wholesale
  with the carry-over, resets the zone map and increments `Frozen`. Replaying "keys that were set"
  would faithfully reconstruct everything Freeze deliberately dropped.
- **The combination rule.** `runLevel` replaces the shared state with the branch when the frontier
  has one node, and merges additively when it has several — precisely so Freeze and key deletions
  propagate. Whether a level replaced or merged is a fact replay needs and cannot re-derive from the
  keys alone.

The invariant this change adopts — anything that reaches a node must be reconstructable from the
journal — has no meaning unless these three are recorded.

# Options
- **Each gets its own event type**: a nudge event naming the origin and the pairs applied, a
  freeze event recording what was carried over and what was dropped, and the combination rule
  carried on the level-boundary event.
- **Record only the resulting state delta**, without saying what caused it. Fewer event types, but
  the journal then answers "what changed" and never "who changed it" — and human intervention is
  exactly the thing an audit trail exists to show.

# Recommendation
Each gets its own event type. Freeze in particular is not a delta: it is a replacement, and a
journal that models it as key removals would reconstruct a state the run never had after any
carry-over rule other than the default. Naming the nudge's origin also costs almost nothing here —
the requester is already carried on the run — and it is the difference between a log and an audit
trail.

# Verdict

Nudge, freeze and the combination rule each get their own event type.

Each gets its own event type. Freeze in particular is not a delta: it is a replacement, and a
journal that models it as key removals would reconstruct a state the run never had after any
carry-over rule other than the default. Naming the nudge's origin also costs almost nothing here —
the requester is already carried on the run — and it is the difference between a log and an audit
trail.
