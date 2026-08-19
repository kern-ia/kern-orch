---
type: Drift
title: "A freeze inside a fan-out level is now a live refusal, not a silent bug"
description: "Issue 06 made the engine reject a node that calls State.Freeze inside a multi-node level, a behaviour change no decision in the change ledger named."
tags: [epic-1, drift]
timestamp: 2026-08-19T21:15:00Z
epic: 1
issue: 6
gh_issue: 10
---

# A freeze inside a fan-out level is now a live refusal, not a silent bug

- **Decided**: nothing in `docs/planning/changes/change-1-event-journal-source-of-truth/`
  or in issue 06's own scope (`../issues/06-non-node-mutations-as-events.md`) says a fan-out
  containing a freeze should be rejected. The change ledger's decision 06 only commits to
  giving nudge, freeze and the combination rule "each their own event type" — a recording
  decision, not a validation one. SPECS.md documents the pre-epic behaviour as fact: a
  fan-out uses "additive `Merge`", with no carve-out for a branch that froze.
- **Actual**: `internal/graph/engine.go`'s `runLevel` now fails the level live when a node's
  `Execute` calls `State.Freeze` inside a frontier of more than one node
  (`branch.Frozen != s.Frozen && len(frontier) > 1`), with the error `node %q:
  graph.State.Freeze called inside a %d-node fan-out level, which has no single branch to
  attribute the freeze to`. A graph that exercised this combination before this epic ran to
  completion; the same graph now aborts the level.
- **Because**: `Merge` only ever folds a branch's own keys onto the shared state — it does
  not fold `Frozen`, and it cannot fold a freeze's key *removals*, since another branch in
  the same fan-out may still depend on a key the freezing branch dropped. The state a fan-out
  freeze would have produced before this epic was therefore already silently wrong: the
  freezing branch's replacement semantics were being discarded by `Merge` without any error,
  a fact that only became visible once `journal/projection.Project` (issue 04) had to decide
  how to replay a `FreezeApplied` event and refused the case outright — there is no single
  branch to attribute a fan-out freeze to, and no combination rule that reconstructs it. The
  live engine change closes the gap between "the engine runs it" and "the journal can replay
  it": a run this now rejects live must never leave behind a journal that only replay
  discovers is unreplayable.
- **Alternatives tried**: none — the guard was added inline while implementing issue 06's
  freeze-event recording, once the incompatibility with issue 04's projection became visible
  as a design question rather than a bug. No alternative was recorded in the issue's own PR
  beyond the reasoning above.
- **Disposition**: accepted. The prior behaviour was silently incorrect state, not a
  supported capability; refusing loud is strictly safer, and it matches CONVENTIONS.md's
  "misconfiguration fails loud" read broadly (a request the engine cannot correctly honor).
  SPECS.md's Architecture section should gain a line noting the refusal, since it currently
  describes only the two combination rules and not this exception.
- **Revisit when**: a real graph is found that relies on freezing inside a fan-out for a
  reason that turns out to be legitimate — at that point the fix is a combination rule for
  this case (e.g. requiring every branch in the fan-out to freeze together, or refusing at
  graph-validation time instead of at run time), not reverting the refusal.
- **Evidence**: `internal/graph/engine.go` (`runLevel`, the guard above), PR #26 (issue 06,
  #10); `internal/journal/projection` (issue 04, PR #20) is the earlier half of the same
  finding, on the replay side.
