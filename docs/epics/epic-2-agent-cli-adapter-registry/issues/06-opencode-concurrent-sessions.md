---
type: Issue
title: "Prove OpenCode fan-out calls stay concurrent, one session per call"
description: "A fan-out of agent nodes calling the OpenCode adapter must open one session per call and run genuinely in parallel - never serialized behind a lock."
tags: [epic-2]
timestamp: 2026-08-20T17:30:00Z
epic: 2
issue: 06
slug: opencode-concurrent-sessions
size: S
status: done
gh_issue: 40
resource: https://github.com/kern-ia/kern-orch/issues/40
gh_pr: 47
depends_on: [5]
---

# Prove OpenCode fan-out calls stay concurrent, one session per call

## Summary

Decision 03 of the change ledger. `graph.Engine`'s level-synchronous fan-out calls
`AgentRunner.Run` from several goroutines at once, against the **same** adapter instance
(`internal/cmd/runtime.go`'s `newRunner` builds exactly one runner per run, shared by every
`AgentNode`). `Subprocess.Run()` today handles this safely by accident -- each call spawns its
own process, so there is no shared mutable state. The OpenCode adapter (issue 05) owns one
persistent server; without this issue's guarantee, a naive implementation could serialize
concurrent calls behind a lock, silently defeating the parallelism `graph.Engine`'s fan-out
promises everywhere else.

## Scope

- Verify (or, if issue 05's implementation needs it, adjust) that each `Run()` call opens its
  own `POST /session` scoped to that call's `NodeID`, rather than sharing or queuing behind one
  session.
- A concurrency test: two (or more) fan-out agent nodes, backed by the same `OpenCode` adapter
  instance, issuing `Run()` calls at the same time -- assert they open distinct sessions and
  that neither blocks on the other (a timing-based or explicit-ordering assertion, not a flaky
  sleep-based one).
- If OpenCode's server has any real per-instance concurrency limit undocumented until now,
  surface it explicitly in this issue's PR rather than silently working around it.

## Out of scope

- The adapter's core spawn/translation logic -- issue 05, a hard dependency.
- Any change to `graph.Engine`'s own fan-out/level-synchronous scheduling.

## Acceptance criteria

- [ ] A test with N concurrent `Run()` calls against one `OpenCode` adapter instance asserts N
      distinct sessions were opened.
- [ ] The same test asserts the calls did not serialize -- e.g. by proving call B's request
      reached the server before call A's response returned, using a fake/instrumented server
      rather than timing alone.
- [ ] A real multi-node fan-out graph run (`examples/`-style, or a new minimal fixture graph)
      against a real `opencode serve` produces correct, independent results per node.
- [ ] `go build ./...`, `go vet ./...` and `go test ./...` are green.
- [ ] An OKF fiche is added under `docs/index/` for this branch, per CONVENTIONS.md.

## Relevant files / areas

- `internal/agentrunner/` -- the `OpenCode` adapter from issue 05.
- `internal/graph/engine.go` -- `runLevel`'s fan-out, for reference on what guarantee this issue
  must not undermine.

## Dependencies

Blocked by issue 05. This is the last issue of epic 2 -- nothing in this epic depends on it
(the "Blocks issue 07" line carried over from issue 04's file was stray: epic 2 only has
issues 01-06).

## PR size note

Target ~500 changed lines; if this grows past ~1000, split it before opening the PR.
