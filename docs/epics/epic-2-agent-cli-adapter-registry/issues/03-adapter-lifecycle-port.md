---
type: Issue
title: "Add the Start/Close adapter lifecycle, wired around a run"
description: "Extend the adapter contract with an explicit Start(ctx)/Close() lifecycle called once per run - not per node - with Close guaranteed on every exit path including stop."
tags: [epic-2]
timestamp: 2026-08-20T15:10:00Z
epic: 2
issue: 03
slug: adapter-lifecycle-port
size: S
status: in-progress
gh_issue: 37
resource: https://github.com/kern-ia/kern-orch/issues/37
depends_on: [2]
---

# Add the Start/Close adapter lifecycle, wired around a run

## Summary

Decision 02 of the change ledger, the load-bearing one: `Subprocess.Run()` today spawns a
fresh process **per call** -- correct for Claude Code's one-shot `-p` mode, wrong for OpenCode,
where the useful unit is one HTTP call against an *already-running* `opencode serve` process,
not a server restarted on every graph node. Adapters need a lifecycle beyond `Run()`.

This issue adds the interface extension and wires it into `internal/cmd/serve.go`'s run-setup
and run-teardown paths. It does not implement OpenCode's real `Start` (spawning the server) --
that is issue 05 -- but it must prove the wiring is correct with a fake adapter whose `Start`/
`Close` record that they were called, exactly once, on every exit path.

## Scope

- A new interface, e.g. `agentrunner.Lifecycle` with `Start(ctx context.Context) error` and
  `Close() error`, that an adapter may optionally implement (Go's usual optional-interface
  pattern -- a type assertion at the call site, not a requirement on `graph.AgentRunner`
  itself, since `Stub` and any adapter with nothing to start/stop should not be forced to
  implement no-op methods on the port everything already depends on).
- Wire the call sites: after the registry (issue 02) constructs a runner, if it satisfies
  `Lifecycle`, call `Start` before the run begins; call `Close` when the run ends -- success,
  failure, or a `stop` request (`internal/cmd/serve.go`'s existing stop path, which already
  cancels a run's context, is where `Close` must also fire -- trace how `stop` currently
  propagates before assuming a single defer suffices, since the run and the runner may not
  share exactly one goroutine's lifetime).
- `Start`/`Close` failures: a `Start` failure aborts the run before any node executes, with an
  error naming which adapter failed to start. A `Close` failure is logged, never escalated to
  fail an otherwise-successful run -- matches this repo's existing posture (`checkpoint.Close`
  and the report queue's own `Flush` are both best-effort on the way out).

## Out of scope

- OpenCode's real `Start` (spawning `opencode serve`, waiting for readiness) -- issue 05.
- Claude Code's adapter, whose `Start`/`Close` are no-ops (it does not need this issue's
  interface at all, strictly, but should implement it as a no-op pair for symmetry and to
  exercise the wiring with a second real type) -- issue 04.

## Acceptance criteria

- [ ] A fake adapter recording call order/count proves `Start` is called exactly once before
      the run's first node executes, and `Close` is called exactly once after the run ends.
- [ ] A run stopped mid-execution (the existing `stop` request path) still calls `Close`
      exactly once.
- [ ] A `Start` failure aborts the run before any node's `Execute` runs, with an error naming
      the adapter.
- [ ] A `Close` failure is logged and does not change a successful run's reported outcome.
- [ ] An adapter that does not implement `Lifecycle` (e.g. `Stub`, or any type without
      `Start`/`Close`) runs exactly as before this issue -- no behaviour change.
- [ ] `go build ./...`, `go vet ./...` and `go test ./...` are green.
- [ ] An OKF fiche is added under `docs/index/` for this branch, per CONVENTIONS.md.

## Relevant files / areas

- `internal/cmd/serve.go` -- the run-setup path (`prepareRun`/`prepareAdhocRun`, per the epic's
  audit) and the `stop` request path.
- `internal/agentrunner/` -- the new `Lifecycle` interface.

## Dependencies

Blocked by issue 02. Blocks issues 04 and 05.

## PR size note

Target ~500 changed lines; if this grows past ~1000, split it before opening the PR.
