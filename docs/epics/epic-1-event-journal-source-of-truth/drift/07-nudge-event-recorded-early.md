---
type: Drift
title: "Nudge events recorded in issue 07 rather than issue 06"
description: "Making the checkpoint row a projection of the journal forces every state mutation to emit an event, and the nudge did not."
tags: [epic-1, drift]
timestamp: 2026-08-19T16:45:00Z
epic: 1
issue: 07
gh_issue: 11
---

# Nudge events recorded in issue 07 rather than issue 06

- **Decided**: issue 07's `## Out of scope` and the epic's ordering put nudge and freeze
  events in issue 06 (`../issues/06-non-node-mutations-as-events.md`), which runs after 07.
- **Actual**: `internal/cmd/journal_recorder.go` records a `journal.NudgeApplied` event when
  the steer mailbox drains a nudge, and `internal/steer.Mailbox.DrainNudges` returns what it
  applied so the recorder can. Freeze is untouched and stays entirely issue 06's.
- **Because**: the two lines of issue 07 are incompatible as written. The moment the
  `checkpoints` row is derived from the journal (`## Scope`, first two bullets), a state
  mutation the journal does not carry is a mutation the row loses — and a nudge is exactly
  that: applied by `Engine.OnBeforeLevel` from the steer mailbox, through no node, so nothing
  else in the run observes it. Two pre-existing tests proved it rather than argued it:
  `TestDaemonRunnerNudgeAppliesToTheNextLevel` failed with
  `state[probe] = <nil>, want hello — the nudge never reached the run`, and
  `TestDaemonRunnerDispatchWithAGraphLoadsTheFileAndNudgesTheMessage` with
  `nudged message = <nil>`. That is a data-loss regression on the daemon's steer path, and it
  contradicts issue 07's own acceptance criterion that `status` and `GET /api/v1/runs/{id}`
  return what they returned before the change.
- **Alternatives tried**: (1) leave the nudge unrecorded and accept that the row loses it
  until issue 06 merges — rejected, it ships a silent regression on a live endpoint;
  (2) keep marshalling the live state into the row for keys the journal cannot explain —
  rejected, it is precisely the "row that looks derived and is not" the issue exists to
  remove; (3) reorder the epic so 06 lands first — not this agent's call, and 06 was
  deliberately scheduled after 07 because it also touches `internal/cmd/runtime.go`.
- **Disposition**: fix-now — the minimum slice of issue 06 that keeps 07's own acceptance
  criteria true. Freeze events, the combination rule's own treatment and any change to
  `internal/graph`'s event port were left alone.
- **Revisit when**: issue 06 (#10) is implemented. Its author inherits `recordNudge`,
  `nudgeOrigin` and `DrainNudges`'s return value, and should either adopt them or replace
  them with a graph-level emission — but must not add a *second* nudge event beside this one,
  which would double-apply the write on replay.
- **Evidence**: this record, PR for issue 07 (#11).
