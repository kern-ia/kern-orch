---
type: Epic
title: "Event journal as the source of truth"
description: "Replace the per-level state snapshot with an append-only event journal from which the run state is projected, and enforce that anything a node can read is reconstructable from it."
tags: [epic, change]
timestamp: 2026-08-19T10:02:00Z
epic: 1
slug: event-journal-source-of-truth
status: open
gh_issue: 4
milestone: 1
resource: https://github.com/kern-ia/kern-orch/issues/4
source: ../../planning/changes/change-1-event-journal-source-of-truth/index.md
---

# Epic 1: Event journal as the source of truth

## Goal

`internal/checkpoint` currently records where a run got to — a full `graph.State` blob per
completed level, keyed `(run_id, step)`. It does not record what happened, so a finished run
cannot explain its own routing, a failed level loses the knowledge of which nodes completed,
and a human's intervention through a nudge is indistinguishable from a node's own output.

This epic makes an append-only event journal the durable record of a run, projects the state
from it, and demotes the existing snapshot to a cache written in the same transaction. It
then enforces the invariant that gives the journal its value: **anything a node can read must
be reconstructable from the journal alone.**

The externally visible behaviour of kern-orch does not change. `kern.step-event/v1` and `/v2`
keep their exact wire format and their pinned fixtures; the reporter becomes a projection of
the journal instead of a parallel observer of the engine.

## Scope

**The journal and its store.** A new internal event vocabulary, distinct from the
`kern.step-event` wire contract, rich enough for exact replay: per-node facts (started,
produced keys, failed) plus explicit level-boundary events carrying the combination rule that
was applied. A monotonic `schema_version` in the SQLite store, with a refusal that names the
database path when the build does not understand the version on disk. No backfill of existing
`checkpoints` rows.

**The projection.** `graph.State` becomes the result of applying the journal. The existing
`checkpoints` row is redefined as a materialized projection written atomically with the events
of its level, so it can never be written independently and therefore cannot drift. The eight
existing call sites keep reading it: `internal/cmd/commands.go` (`status`, `resume`),
`internal/cmd/serve.go` (five `Latest` calls plus the `queued` marker at step `-1`), and the
two `internal/daemon/router.go` function types typed on `checkpoint.Record` and
`checkpoint.Summary`.

**Non-node mutations become events.** Three state changes happen outside any node and are
invisible today. Each gets its own event type: a **nudge** (naming its origin and the pairs
applied), a **freeze** (recording what was carried over and what was dropped — it is a
replacement, not a delta, so key removals would reconstruct a state the run never had), and
the **combination rule** on the level-boundary event (`runLevel` replaces the shared state on
a single-node frontier and merges additively on a fan-out; replay cannot re-derive which
happened from the keys alone).

**Replay-based resume.** `resume` rebuilds state by replaying the journal rather than trusting
the cache, which is the only path where being wrong is unrecoverable. A run killed mid-level
leaves a visibly incomplete tail; that tail is closed with explicit synthetic events marking
the run interrupted, before replay proceeds.

**Nested runs.** A subgraph writes its own journal, linked by parent run id and node id —
the same shape `internal/report/http.go` already chose for nested reporting, and for the same
reason: one monotonic sequence per run, and composition at any depth without a recursive
schema.

**The invariant, executed.** A replay-equivalence check asserted over real engine runs, not
as a unit test of the journal package: replaying a run's journal yields exactly the state the
run ended with. The same comparison is exposed as an opt-in runtime check at level boundaries,
off by default, as a validated `Config` field.

**The reporter rewired.** `internal/report` consumes the journal instead of observing the
engine in parallel. Its 64-slot dropping queue stays exactly as it is: a journal that never
drops and a report that may drop are the durable record and the best-effort view of it.

## Out of scope

- Any change to the `kern.step-event` wire format, its version, or its pinned fixtures under
  `contracts/`. The fixtures staying green is an acceptance criterion, not a constraint to
  negotiate.
- The tool-execution hook pipeline and monotonic guards (`kern-policy` / `kern-guard`).
- The repeat-tool loop-breaker.
- Tool-result spill and any `kern-memory` extraction.
- Compaction triggers alongside the existing freeze.
- A telemetry consumer built on the journal (`kern-obs`).
- Making `steer.Mailbox` durable. The journal makes it nearly free, which is exactly why it
  needs saying: it stays in-memory, as its package comment already argues.
- Backfilling existing checkpoint databases into synthetic events.
- Resolving the `github.com/yoann/kern-orch` module path, which is an org-level decision.

## Acceptance criteria

- Replaying a completed run's journal yields exactly the state that run ended with, asserted
  over real engine runs covering a single-node frontier, a fan-out, a freeze, a nudge and a
  nested subgraph.
- The `contracts/kern.step-event.v1.json`, `v2.json`, `v2.failure.json` and `v2.nested.json`
  fixtures are unchanged, and the `internal/report` tests asserting against them pass.
- `resume` reconstructs its state by replaying the journal; a run interrupted mid-level
  resumes from a journal whose tail was explicitly closed, and the interruption is visible in
  the record.
- Opening a database whose `schema_version` the build does not understand fails loud with an
  error naming the file path, rather than reinterpreting it.
- A nudge, a freeze and the level combination rule are each recoverable from the journal
  alone, with the nudge naming its origin.
- A nested subgraph run has its own journal carrying its parent run id and node id.
- The opt-in runtime equivalence check is a validated `Config` field, off by default, and
  detects a deliberately corrupted projection when enabled.
- `go build ./...`, `go vet ./...` and `go test ./...` are green.
- An OKF fiche exists under `docs/index/` for each merged feature branch of this epic.

## Dependencies

None.

## Context

- [Technical specs](../../planning/SPECS.md) — the checkpoint schema, the level-synchronous
  engine, the three outbound contracts, and the absent migration mechanism.
- [Conventions](../../planning/CONVENTIONS.md) — error handling, the fail-loud rule, the
  no-consumer-dependency rule, and the OKF fiche requirement.
- [Change ledger](../../planning/changes/change-1-event-journal-source-of-truth/index.md) —
  the ten decisions behind every line above, each with the audit facts that produced it.
- `docs/analyse-deepseek-harness.md` (repository root `docs/`) — the source analysis this
  change is item 1 and 2 of.

## Notes

**The riskiest interaction is Freeze.** `State.Freeze` replaces the state's contents wholesale
with the carry-over and resets the zone map. A journal modelling it as key removals would
reconstruct correctly under `DefaultCarryOver` and incorrectly under any other carry-over
rule — a bug that only appears once someone uses the extension point, and that replay
equivalence will only catch if a freeze test exercises a non-default carry-over. It does not
today.

**The schema-version work is a prerequisite, not a subtask.** There is no migration mechanism
to build on: `sqlite.go` carries one ad-hoc `ALTER TABLE ... ADD COLUMN dossier` with its
error deliberately ignored. Sequencing it first is what keeps the rest of the epic from
inventing a second ad-hoc mechanism.

**Concurrency is real here and untested.** The store pins its pool to a single connection so
`PRAGMA busy_timeout=5000` applies, because the steering endpoints read the latest checkpoint
while a live run writes one. The engine runs a goroutine per frontier node. `-race` is not
currently part of any routine in this repo, and this epic is the wrong one to keep that true.

**This epic is large — expect 8 to 10 issues.** It stays a single epic because it is one
subsystem and one shippable outcome, held together by the scope boundary above. If
`create-issues` finds it cannot size the work under an L ceiling, the split to make is
journal-and-store first, projection-and-resume second.
