---
type: Decision
title: "Scope boundary: what this change explicitly does not do"
description: "Which adjacent items surfaced by the analysis stay out of this epic?"
tags: [decision, change]
timestamp: 2026-08-19T09:16:15Z
phase: change
decision: 09
slug: scope-boundary
status: decided
verdict: "Only the journal, projection, replay-based resume and the invariant; the wire format and fixtures do not move"
decided_via: triage
depends_on: ['persistence-approach']
change: 1
change_slug: event-journal-source-of-truth
---

# Question
The source analysis (`docs/analyse-deepseek-harness.md`) lists ten items, six of which depend on
this one. The audit also surfaced two tempting adjacent cleanups: `steer.Mailbox` is deliberately
non-durable, and a journal makes durability nearly free; and `internal/report`'s dropping queue
looks wrong next to a journal that may never drop.

Without an explicit boundary this change absorbs all of them and stops being one epic.

# Options
- **Out: everything except the journal, the projection, replay-based resume, and the invariant.**
  Specifically out — the tool-execution hook pipeline (item 3), the loop-breaker (4), tool-result
  spill (5), compaction triggers (7), the telemetry consumer (8), making the steer mailbox durable,
  extracting kern-memory, and any change to the `kern.step-event` wire format or its fixtures.
- **In: also rewire the reporter to consume the journal** rather than observe the engine in
  parallel, since decision 02 makes it a projection anyway.

# Recommendation
The first, with one qualification: decision 02 requires the reporter to become a projection of the
journal, so that rewiring is in scope — but its **wire output and fixtures must not change**, and
the dropping queue stays exactly as it is. A journal that never drops and a report that may drop are
not in conflict; they are the durable record and the best-effort view of it, which is the
distinction the whole change is built on. Everything else on that list waits.

# Verdict

Only the journal, projection, replay-based resume and the invariant; the wire format and fixtures do not move.

The first, with one qualification: decision 02 requires the reporter to become a projection of the
journal, so that rewiring is in scope — but its **wire output and fixtures must not change**, and
the dropping queue stays exactly as it is. A journal that never drops and a report that may drop are
not in conflict; they are the durable record and the best-effort view of it, which is the
distinction the whole change is built on. Everything else on that list waits.
