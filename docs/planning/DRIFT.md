---
type: Drift
title: "kern-orch — Drift"
description: "Standards this codebase has drifted from, and why"
tags: [planning, drift]
timestamp: 2026-08-19T21:15:00Z
---

# Drift

## Epic 1: Event journal as the source of truth

### 01 — Nudge events recorded in issue 07 rather than issue 06

- **Decided**: issue 07's `## Out of scope` and the epic's ordering put nudge and freeze
  events in issue 06, which runs after 07
  ([issue 06](../epics/epic-1-event-journal-source-of-truth/issues/06-non-node-mutations-as-events.md)).
- **Actual**: `internal/cmd/journal_recorder.go` recorded a `journal.NudgeApplied` event
  from issue 07 onward, because making the `checkpoints` row a projection of the journal
  meant a mutation the journal doesn't carry is a mutation the row loses — and a nudge is
  applied by `Engine.OnBeforeLevel`, through no node.
- **Because**: two pre-existing tests proved the loss rather than argued it —
  `TestDaemonRunnerNudgeAppliesToTheNextLevel` and
  `TestDaemonRunnerDispatchWithAGraphLoadsTheFileAndNudgesTheMessage` both failed with the
  nudged value missing from state. Shipping issue 07 as scoped would have been a live
  data-loss regression on the daemon's steer endpoint.
- **Disposition**: resolved (2026-08-19) — issue 06 (PR #26) adopted the recording exactly
  as issue 07 left it, without adding a second nudge event (which would have double-applied
  the write on replay).
- **Revisit when**: not applicable — closed.
- **Evidence**: [drift record](../epics/epic-1-event-journal-source-of-truth/drift/07-nudge-event-recorded-early.md), PR #22 (issue 07), PR #26 (issue 06).

### 02 — A freeze inside a fan-out level is now a live refusal, not a silent bug

- **Decided**: nothing in the change ledger or in issue 06's own scope specifies validation
  for a fan-out containing a freeze — the ledger's decision 06 only commits to giving nudge,
  freeze and the combination rule their own event types, a recording decision. SPECS.md
  documented the pre-epic behaviour as fact: a fan-out uses additive `Merge`, with no
  carve-out for a branch that froze.
- **Actual**: `internal/graph/engine.go`'s `runLevel` fails the level live when a node's
  `Execute` calls `State.Freeze` inside a frontier of more than one node. A graph that
  exercised this combination before this epic ran to completion; the same graph now aborts
  the level with an explicit error naming the node and the frontier size.
- **Because**: `Merge` never folded a freezing branch's `Frozen` counter or its key
  removals — another branch in the same fan-out may depend on a key the freezing branch
  dropped. The state a fan-out freeze produced before this epic was already silently wrong;
  the gap became visible only once `journal/projection.Project` (issue 04) had to decide how
  to replay a `FreezeApplied` event and refused the case outright, having no single branch to
  attribute it to. The live guard closes the gap between "the engine runs it" and "the
  journal can replay it."
- **Disposition**: accepted (2026-08-19) — the prior behaviour was silently incorrect state,
  not a supported capability; refusing loud is strictly safer, consistent with
  CONVENTIONS.md's "misconfiguration fails loud" read broadly. SPECS.md's Architecture
  section should gain a line documenting this refusal alongside the two combination rules.
- **Revisit when**: a real graph is found that needs to freeze inside a fan-out for a
  legitimate reason — the fix then is a combination rule for the case, or a
  graph-validation-time refusal instead of a run-time one, not reverting the guard.
- **Evidence**: [drift record](../epics/epic-1-event-journal-source-of-truth/drift/06-freeze-inside-fanout-now-refused.md), `internal/graph/engine.go` (`runLevel`), PR #26 (issue 06), PR #20 (issue 04, the earlier half of the same finding on the replay side).
