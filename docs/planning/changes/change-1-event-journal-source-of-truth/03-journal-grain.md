---
type: Decision
title: "Journal grain: per level or per node"
description: "Is the unit of the journal a completed frontier, or an individual node execution?"
tags: [decision, change]
timestamp: 2026-08-19T09:16:15Z
phase: change
decision: 03
slug: journal-grain
status: decided
verdict: "Per-node facts with explicit level-boundary events"
decided_via: triage
depends_on: ['event-vocabulary-vs-wire-contract']
change: 1
change_slug: event-journal-source-of-truth
---

# Question
The engine is level-synchronous: `runLevel` runs every node of a frontier in its own goroutine on
a `Clone()` of the state, then combines the branches in frontier order. The existing checkpoint and
the existing wire contract are both **level**-grained, because that is the granularity at which the
shared state is coherent.

But the facts of a run happen per node: node X started, node X produced these keys, node Y failed.
`graph.LevelError` already carries this distinction and documents why it matters — "a node in the
level that is absent from it **completed**" — and that knowledge is currently thrown away as soon
as the level fails.

# Options
- **Per-node facts, with explicit level-boundary events.** The journal records each node's start,
  outcome and produced keys, plus a level-open/level-close pair carrying the combination rule that
  was applied. Replay reconstructs the state by applying nodes in frontier order under the recorded
  rule. Richer, and it makes partial-level failure a first-class fact.
- **Per level, mirroring the current checkpoint.** Smallest change, and it matches both existing
  representations — but a failed level then still records nothing about which nodes had completed,
  and the audit trail stays exactly as coarse as it is today.

# Recommendation
Per-node with level boundaries. The whole point of the change is that the record explains what
happened rather than only where the run got to, and node-level attribution is precisely the
information the current design computes and then discards. The level events are what keep replay
deterministic: the combination rule is recorded, never re-derived.

# Verdict

Per-node facts with explicit level-boundary events.

Per-node with level boundaries. The whole point of the change is that the record explains what
happened rather than only where the run got to, and node-level attribution is precisely the
information the current design computes and then discards. The level events are what keep replay
deterministic: the combination rule is recorded, never re-derived.
