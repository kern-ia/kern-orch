---
type: Decision
title: "Resume: replay the journal or trust the snapshot"
description: "Does resume rebuild state by replaying events, or keep loading the cached snapshot?"
tags: [decision, change]
timestamp: 2026-08-19T09:16:15Z
phase: change
decision: 05
slug: resume-replay-vs-snapshot
status: decided
verdict: "Replay is authoritative, the cache is a fast path; an interrupted tail is closed with explicit synthetic events"
decided_via: triage
depends_on: ['journal-grain', 'schema-versioning-and-existing-data']
change: 1
change_slug: event-journal-source-of-truth
---

# Question
`resume` today calls `store.Latest`, takes the snapshot and the frontier, and calls
`Engine.RunFrom(ctx, rec.State, rec.Frontier)` (`internal/cmd/serve.go:110`,
`internal/cmd/commands.go:74`). The graph path travels in the checkpoint so `resume <run-id>` needs
no graph argument.

Under decision 01 the snapshot becomes a derived cache. A cache is allowed to be stale or absent;
resume is the one path where being wrong is unrecoverable, because it silently continues a run from
a state that never existed.

A second problem the current design does not have a name for: a run killed mid-level leaves the
last level half-executed. Today that is invisible — no checkpoint was written, so resume restarts
the whole level. With per-node events, the tail is visibly incomplete.

# Options
- **Replay is authoritative; the cache is a fast path validated by tests, and an interrupted tail
  is closed with explicit synthetic events** marking the run interrupted before replay proceeds. The
  incomplete tail becomes a recorded fact rather than an ambiguity.
- **Keep loading the snapshot, with replay available only as a debugging tool.** Smallest change to
  the resume path, but the journal is then not actually the source of truth — it is a log beside the
  truth, which is where the design already is.
- **Replay every time and drop the cache on the resume path**, verifying nothing. Simple and
  correct, but it gives up the ability to detect that cache and journal disagreed.

# Recommendation
The first. If resume does not replay, nothing in the system ever proves the journal is complete,
and an incomplete journal that is never exercised is worse than no journal — it looks authoritative
and is not. Closing an interrupted tail explicitly is the same reasoning applied one level down: the
run's last moments are a fact worth recording, not a hole to be silently restarted over.

# Verdict

Replay is authoritative, the cache is a fast path; an interrupted tail is closed with explicit synthetic events.

The first. If resume does not replay, nothing in the system ever proves the journal is complete,
and an incomplete journal that is never exercised is worse than no journal — it looks authoritative
and is not. Closing an interrupted tail explicitly is the same reasoning applied one level down: the
run's last moments are a fact worth recording, not a hole to be silently restarted over.
