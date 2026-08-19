---
type: Issue
title: "Persist and read the journal in SQLite"
description: "An append-only events table with ordered reads per run, written under the schema version from issue 01."
tags: [epic-1]
timestamp: 2026-08-19T11:45:00Z
epic: 1
issue: 03
slug: journal-store-append-read
size: M
status: pr-open
gh_issue: 7
gh_pr: 19
resource: https://github.com/kern-ia/kern-orch/issues/7
depends_on: [1, 2]
---

# Persist and read the journal in SQLite

## Summary

The durable half of the journal: an append-only table, ordered reads, and the transaction boundary
the projection cache will later join (issue 07).

Concurrency is real here and currently untested. `OpenSQLite` pins the pool to a single connection
(`SetMaxOpenConns(1)`) specifically so `PRAGMA busy_timeout=5000` applies to every access, because
the steering endpoints read the latest checkpoint while a live run's goroutine writes one. Adding a
second write path to the same database does not get to ignore that.

## Scope

- An `events` table keyed by `(run_id, seq)`, append-only, created under the `SCHEMA_VERSION`
  mechanism from issue 01 (bump the version).
- `Append(ctx, runID, events...)` — a batch write in one transaction; rejects a batch whose first
  `seq` is not the stored next-seq for that run, so a gap or a replay collision fails loud rather
  than corrupting the sequence.
- `Read(ctx, runID)` — all events for a run in `seq` order.
- `ReadFrom(ctx, runID, fromSeq)` — the suffix, for consumers that already applied a prefix.
- Rejection of a non-JSON-serializable payload naming the offending type, rather than storing a
  broken row.

## Out of scope

- Writing the projection cache in the same transaction — issue 07 does that, and it is where the
  atomicity requirement actually lands.
- Replaying events into a state — issue 04.
- Deleting or compacting events.

## Acceptance criteria

- [ ] `Append` then `Read` returns the exact events in the exact order, per run.
- [ ] Appending a batch whose first `seq` does not match the stored next-seq returns an error; the
      table is unchanged afterwards.
- [ ] `ReadFrom` with a `fromSeq` at or past the end returns an empty slice, not an error; a
      negative `fromSeq` returns an error.
- [ ] Two runs interleaving appends keep independent, monotonic sequences.
- [ ] `go test -race ./internal/checkpoint/...` passes with a test that appends from one goroutine
      while reading from another — the epic's Notes flag that `-race` is in no current routine, and
      this is the issue that changes it.
- [ ] The schema version was bumped and an older database is refused by the issue-01 mechanism.
- [ ] `go build ./...`, `go vet ./...` and `go test ./...` are green.
- [ ] An OKF fiche is added under `docs/index/` for this branch, per CONVENTIONS.md.

## Relevant files / areas

- `internal/checkpoint/sqlite.go` — `schema`, `OpenSQLite`, `busyTimeoutMS`, `SetMaxOpenConns(1)`.
- `internal/checkpoint/store.go` — the `Store` interface, if the journal joins it rather than
  living beside it.
- `internal/journal/` — the types from issue 02.

## Dependencies

Blocked by 01 and 02. Blocks 07.


## PR size note

Target ~500 changed lines; if this grows past ~1000, split it before opening the PR.
