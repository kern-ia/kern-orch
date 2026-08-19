---
type: Decision
title: "Approach: how the journal replaces the snapshot"
description: "Add a journal beside the checkpoints table, demote the snapshot to a derived cache, or rewrite internal/checkpoint outright?"
tags: [decision, change]
timestamp: 2026-08-19T09:16:15Z
phase: change
decision: 01
slug: persistence-approach
status: decided
verdict: "Journal is authoritative; the snapshot becomes a derived cache written in the same transaction"
decided_via: triage
depends_on: []
change: 1
change_slug: event-journal-source-of-truth
---

# Question
`internal/checkpoint` today owns one table. `Save` upserts a full `graph.State` JSON blob keyed
`(run_id, step)`; `Latest` reads the highest step; `List` rolls up one summary per run. Eight call
sites depend on it: `internal/cmd/runtime.go` (`checkpointHook`), `internal/cmd/commands.go`
(`status`, `resume`), `internal/cmd/serve.go` (five `Latest` calls plus the `queued` marker at the
reserved step `-1`), and `internal/daemon/router.go`, whose two exported function types are typed
on `checkpoint.Record` and `checkpoint.Summary`.

The goal is that the journal becomes the source of truth and the state becomes its projection. How
that lands decides whether the eight call sites change once or twice.

# Options
- **Journal is authoritative, snapshot becomes a derived cache in the same transaction.** One new
  events table; the existing `checkpoints` row is kept but redefined as a materialized projection
  written atomically with the events of that level. `Latest` keeps working, so the five `serve.go`
  read sites and the daemon function types are untouched by this change; only the write path and
  the resume path move.
- **Dual-write with two independent truths.** Journal and snapshot both written, neither derived
  from the other. Cheapest diff, but it institutionalizes the exact ambiguity the change exists to
  remove: when they disagree, nothing says which is right.
- **Rewrite `internal/checkpoint` as a journal package and drop the snapshot entirely.** Purest
  result, but every read site must now replay to answer "what is the state of this run", including
  `GET /api/v1/runs` which today answers from one indexed row per run.

# Recommendation
The first. The snapshot is a genuinely useful read model — `status` and the five daemon read
paths want the current state, not a replay — and the failure mode of a cache is recoverable while
the failure mode of two truths is not. Writing both in one transaction is what makes the snapshot
a projection rather than a second record: it cannot drift, because it is never written
independently. It also keeps this change's blast radius on the write and resume paths instead of
spreading it across every consumer at once.

# Verdict

Journal is authoritative; the snapshot becomes a derived cache written in the same transaction.

The first. The snapshot is a genuinely useful read model — `status` and the five daemon read
paths want the current state, not a replay — and the failure mode of a cache is recoverable while
the failure mode of two truths is not. Writing both in one transaction is what makes the snapshot
a projection rather than a second record: it cannot drift, because it is never written
independently. It also keeps this change's blast radius on the write and resume paths instead of
spreading it across every consumer at once.
