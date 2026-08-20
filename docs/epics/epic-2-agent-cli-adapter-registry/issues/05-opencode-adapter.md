---
type: Issue
title: "Implement the OpenCode adapter: spawn, session, and wire translation"
description: "An HTTP-backed adapter that spawns and owns an opencode serve process via the Start/Close lifecycle, and translates its real POST /session/:id/message response into AgentResult."
tags: [epic-2]
timestamp: 2026-08-20T14:00:00Z
epic: 2
issue: 05
slug: opencode-adapter
size: M
status: in-progress
gh_issue: 39
resource: https://github.com/kern-ia/kern-orch/issues/39
depends_on: [1, 2, 3]
---

# Implement the OpenCode adapter: spawn, session, and wire translation

## Summary

The second of the two real adapters, and the one that actually needs the `Lifecycle`
extension from issue 03: OpenCode has no stdin/stdout protocol at all. `opencode serve`
starts a long-lived HTTP server exposing an OpenAPI 3.1 surface; the useful unit of work is a
`POST /session/:id/message` call against an already-running server, not a process spawned and
torn down per node.

Per decision 05: this adapter's translation is private and independent of the Claude Code
adapter's (issue 04) -- `{ info: Message, parts: Part[] }` has no structural overlap with
Claude Code's `stream-json` envelope, and none is forced.

## Scope

- `internal/agentrunner`: a new `OpenCode` type implementing `graph.AgentRunner` and the
  `Lifecycle` interface from issue 03.
- `Start(ctx)`: spawn `opencode serve` (path from `Config`/`KERN_AGENT_CLI`, per issue 02's
  registry), then poll or otherwise wait for the server to be ready to accept requests before
  returning -- do not return successfully from `Start` before the server can serve a real
  request, since every subsequent `Run()` call would otherwise race the server's own startup.
- `Close()`: shut the server down (signal, then wait, matching this repo's existing "dispose
  reaches quiescence, not just requests it" posture from `docs/defensive-patterns.md` if that
  file's guidance is still current for this repo -- verify against `CONVENTIONS.md` first).
- `Run(ctx, req)`: issue `POST /session/:id/message` (creating the session first if needed) --
  translate `req.Prompt`/`req.State` into the real request body OpenCode's API expects, and its
  `{ info: Message, parts: Part[] }` response into `graph.AgentResult.Output`, derived from
  the fixture captured in issue 01 (`testdata/opencode-message.json`).
- `TokenSink`/`OnActivity`: the synchronous `/message` endpoint has no documented incremental
  streaming primitive (per the epic's audit) -- `OnActivity` still brackets `true`/`false`
  around the call; `TokenSink` may receive the whole response at once rather than incrementally
  if no streaming path exists. State this explicitly in the PR rather than silently leaving
  `TokenSink` unused.

## Out of scope

- Session-scoped concurrency for a fan-out of agent nodes -- issue 06. This issue may open one
  session per `Run()` call as the natural API shape, but proving concurrent calls stay
  concurrent (not serialized) is issue 06's acceptance criterion, not this one's.
- `POST /session/:id/prompt_async` and any async completion mechanism -- Epic 2's Out of scope
  (decision 06), unaudited.
- Any OpenCode flag or capability beyond a working synchronous round trip.

## Acceptance criteria

- [ ] `Start(ctx)` returns only once the spawned server demonstrably accepts a request (not on
      a fixed sleep) -- a test forces a slow-starting fake server and asserts `Start` still
      waits correctly.
- [ ] `Close()` terminates the spawned server process; a test asserts the process is gone
      after `Close` returns, not merely signalled.
- [ ] Unit test decodes `testdata/opencode-message.json` (issue 01's real fixture) and asserts
      the exact `graph.AgentResult.Output` produced.
- [ ] A real round trip (`KERN_AGENT_KIND=opencode`, a real `opencode` binary present) starts
      the server, sends one message, gets a result, and shuts down cleanly --
      **self-skipping** when the binary is absent.
- [ ] `Start` failing (e.g. the binary is missing, or the port is already bound) aborts the run
      before any node executes, with an error naming the adapter.
- [ ] `go build ./...`, `go vet ./...` and `go test ./...` are green.
- [ ] An OKF fiche is added under `docs/index/` for this branch, per CONVENTIONS.md.

## Relevant files / areas

- `internal/agentrunner/` -- new file(s) for the `OpenCode` type.
- `internal/agentrunner/testdata/opencode-message.json` (issue 01).
- `internal/cmd/serve.go` -- where issue 03 wired `Start`/`Close`; this issue is that wiring's
  first real exercise.
- `docs/defensive-patterns.md` if present in this repo -- the "dispose reaches quiescence"
  pattern for a spawned child process, already established for the agent CLI subprocess itself
  in `internal/agentrunner/subprocess.go`'s existing `cmd.Wait()` handling.

## Dependencies

Blocked by issues 01, 02, 03. Blocks issues 06 and 07.

## PR size note

Target ~500 changed lines; if this grows past ~1000, split it before opening the PR.
