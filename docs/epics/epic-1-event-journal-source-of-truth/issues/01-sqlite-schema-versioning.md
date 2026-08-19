---
type: Issue
title: "Add a monotonic SQLite schema version and refuse unknown versions"
description: "Replace the ad-hoc ALTER TABLE with a real schema_version mechanism that fails loud on a database this build does not understand."
tags: [epic-1]
timestamp: 2026-08-19T12:45:00Z
epic: 1
issue: 01
slug: sqlite-schema-versioning
size: S
status: pr-open
gh_issue: 5
gh_pr: 17
resource: https://github.com/kern-ia/kern-orch/issues/5
depends_on: []
---

# Add a monotonic SQLite schema version and refuse unknown versions

## Summary

Every later issue in this epic adds or redefines tables, and there is nothing to build on:
`internal/checkpoint/sqlite.go` creates its table with `CREATE TABLE IF NOT EXISTS` and carries
one ad-hoc `ALTER TABLE checkpoints ADD COLUMN dossier TEXT NOT NULL DEFAULT ''` whose error is
deliberately ignored, with the comment "no migration mechanism exists yet in this project's dev
stage, so this one ALTER TABLE covers the gap."

This issue closes that gap first, so the rest of the epic does not invent a second ad-hoc
mechanism. Sequencing it before anything else is called out explicitly in the epic's Notes.

## Scope

- A `SCHEMA_VERSION` constant in `internal/checkpoint`, monotonic, starting at the version that
  describes today's table.
- Persist the version in the database (a one-row `schema_meta` table is enough; do not use
  `PRAGMA user_version` if a text-readable record is preferred — decide and document the choice
  in the package comment).
- `OpenSQLite` reads the stored version and, when it does not match what the build understands,
  **refuses with an error naming the database file path** rather than reinterpreting it. This is
  CONVENTIONS.md's fail-loud rule applied to the one place where silence corrupts data.
- A fresh database is stamped with the current version at creation.
- Remove the ad-hoc `ALTER TABLE` and its ignored error, now that a mechanism exists.

## Out of scope

- Any new table for journal events — that is issue 03.
- Backfilling or converting existing `checkpoints` rows. Decision 04 rules this out: an old
  snapshot has no per-node facts to recover, and a synthesized journal would be fiction presented
  as record.
- A generalized migration runner. One version stamp plus a refusal is the whole mechanism this
  epic needs.

## Acceptance criteria

- [ ] Opening a database stamped with an unknown version returns an error whose message contains
      the database file path; a test asserts on the path being present, not just on failure.
- [ ] Opening a fresh database stamps it with `SCHEMA_VERSION` and succeeds.
- [ ] Opening a database stamped with the current version succeeds and does not rewrite the stamp.
- [ ] The `ALTER TABLE ... ADD COLUMN dossier` statement and its ignored error are gone.
- [ ] Existing `internal/checkpoint` tests pass unchanged, except where they construct a database
      directly and now need the stamp.
- [ ] `go build ./...`, `go vet ./...` and `go test ./...` are green.
- [ ] An OKF fiche is added under `docs/index/` for this branch, per CONVENTIONS.md.

## Relevant files / areas

- `internal/checkpoint/sqlite.go` — `schema`, `OpenSQLite`, the ad-hoc `ALTER TABLE` at the end of
  `OpenSQLite`.
- `internal/checkpoint/store_test.go`, `resume_test.go`, `dossier_test.go`, `graphpath_test.go`,
  `requester_test.go` — every test that opens a store.

## Dependencies

None. Blocks every other issue in this epic.


## PR size note

Target ~500 changed lines; if this grows past ~1000, split it before opening the PR.
