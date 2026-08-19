---
type: Issue
title: "Close an interrupted run's tail with synthetic events"
description: "A run killed mid-level leaves a level opened and never closed; turn that hole into explicit, identifiable facts."
tags: [epic-1]
timestamp: 2026-08-19T21:30:00Z
epic: 1
issue: 12
slug: interrupted-tail-closing
size: M
status: done
gh_issue: 23
gh_pr: 28
resource: https://github.com/kern-ia/kern-orch/issues/23
depends_on: [8]
---

# Close an interrupted run's tail with synthetic events

## Summary

Split out of issue 08, which grew past a reviewable PR. Issue 08 makes `resume` rebuild state by
replaying a **coherent** journal. This issue handles the incoherent one.

A run killed mid-level is invisible in the old design — no checkpoint was written, so resume simply
restarted the whole level. With per-node events the hole is visible: a level opened and never
closed, nodes started and never resolved. Decision 05 of the change ledger turns that into a
recorded fact rather than an ambiguity, on the same reasoning applied one level down: the run's last
moments are worth recording, not silently restarting over.

## Scope

- Detect an unclosed tail when reading a run's journal: a `LevelOpened` with no matching
  `LevelClosed`, and nodes started but never resolved.
- Append explicit synthetic events closing it and marking the run interrupted. They are ordinary
  journal records, appended through the existing sequence contract, and identifiable as synthetic.
- `resume` derives its frontier from the closed journal.
- A run whose journal is already coherent is untouched — no synthetic events, no rewrite.

## Out of scope

- Retrying or partially re-executing the nodes that had completed in the interrupted level. Resume
  keeps restarting the level; this issue only makes the interruption visible and the state exact.
- The pre-journal case flagged by issue 07: a run checkpointed before the journal existed has no
  events at all, and would project to an empty state. That is an absent journal, not an interrupted
  one — a different problem, and issue 07's follow-up notes it as unresolved.

## Acceptance criteria

- [ ] A run killed mid-level, then resumed, has synthetic tail-closing events appended, is marked
      interrupted in its journal, and resumes from the correct frontier.
- [ ] The synthetic events are identifiable as synthetic when reading the journal afterwards — a
      test asserts on that marking, not merely on their presence.
- [ ] Replaying the closed journal yields a state a real run could have held: no node counted as
      producing keys it never produced.
- [ ] A coherent journal gains no synthetic events and its byte content is unchanged.
- [ ] The synthetic append respects `Append`'s sequence contract — starting at the stored next-seq
      and contiguous — rather than writing around it.
- [ ] `go build ./...`, `go vet ./...` and `go test ./...` are green.
- [ ] An OKF fiche is added under `docs/index/` for this branch, per CONVENTIONS.md.

## Relevant files / areas

- `internal/checkpoint/journal.go` — `Append`, `Read`, `ReadFrom`, and the sequence contract.
- `internal/journal/projection/` — `Project`, and its existing tolerance of an unclosed tail (it
  combines nothing rather than erroring; issue 04 left this deliberately for here).
- `internal/cmd/commands.go`, `internal/cmd/serve.go` — the two resume entry points.

## Dependencies

Blocked by issue 08.

## PR size note

Target ~500 changed lines; if this grows past ~1000, split it before opening the PR.
