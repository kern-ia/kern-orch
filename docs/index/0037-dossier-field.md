# 0037 — `dossier` field on Dispatch, mirroring `requester`

## What

`POST /api/v1/dispatch` accepts a new optional `dossier` field, threaded through the same
path as `requester` end to end: `daemon.Runner.Dispatch` → `prepareRun`/`prepareAdhocRun`
→ the `HTTPReporter`/`StepEvent` wire payload (`kern.step-event/v2`) → the checkpoint
store (new `dossier` column, mirroring `requester`).

`StartRun`/`resume`/the bare CLI `run` command do not take a dossier — only `Dispatch`
does. `resume` (both the CLI command and the daemon's `ResumeRun`) carries the original
run's `Dossier` forward from its checkpoint, the same way it already does for `Requester`.

## Why

Built for `Kern-UI`'s Avel Finances advisor console: a "Dossiers" list groups several
runs under one client case. The project owner explicitly chose a real backend field over
reusing `requester` (an identity used for a steering-permission check, not a business
grouping key) — see `Kern-UI/docs/index/theming-convention.md` and
`Kern-UI/docs/index/dossier-field.md` for the full cross-repo decision.

## Found along the way

`requester` itself was never documented in either README's canonical `StepEvent` table
(`kern.step-event/v2`) despite being a real, tested, wire field since C6. Fixed alongside
`dossier` rather than left as a second undocumented field — both READMEs (this repo's and
`Kern-UI`'s) now list both.

No migration mechanism exists yet for the checkpoint SQLite schema (`CREATE TABLE IF NOT
EXISTS` only) — added one `ALTER TABLE ... ADD COLUMN` guard (errors ignored: the only
failure mode against this fixed schema is "column already exists"). Any existing local
dev checkpoint DB from before this change will pick up the column automatically on next
open; no manual deletion needed, unlike some earlier dev-only schema changes this
project has made.

## Verified

`go test ./...` green across `internal/checkpoint`, `internal/cmd`, `internal/daemon`,
`internal/report` — including new round-trip tests (`dossier_test.go` in `checkpoint`,
two new cases in `dispatch_test.go`, a new "first event only" case in
`report/http_test.go`) and the existing contract fixture test
(`TestReporterEmitsTheV2Fixture`) updated to assert the new fields rather than left
silently passing on an unchanged fixture.
