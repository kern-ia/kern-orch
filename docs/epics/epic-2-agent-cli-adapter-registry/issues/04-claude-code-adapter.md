---
type: Issue
title: "Implement the Claude Code adapter"
description: "A stdio adapter speaking Claude Code's real -p --input-format=stream-json --output-format=stream-json protocol, translating its messages into AgentResult and the existing TokenSink/OnActivity contracts."
tags: [epic-2]
timestamp: 2026-08-20T15:00:00Z
epic: 2
issue: 04
slug: claude-code-adapter
size: M
status: in-progress
gh_issue: 38
resource: https://github.com/kern-ia/kern-orch/issues/38
depends_on: [1, 2, 3]
---

# Implement the Claude Code adapter

## Summary

The first of the two real adapters this epic ships. Replaces the placeholder
`agentrunner.Subprocess`'s invented protocol with Claude Code's actual, verified CLI
interface: `claude -p --input-format=stream-json --output-format=stream-json` streams typed
JSON messages both ways over stdio -- not the single-request, token/result/error shape the
current code speaks.

Per decision 05 of the change ledger: this adapter owns its full translation privately. No
shared intermediate `Event` type is imposed between this and the OpenCode adapter (issue 05) --
they translate independently into the same narrow, already-CLI-agnostic surface:
`graph.AgentResult`, the `TokenSink io.Writer`, and the `OnActivity` two-state bracket.

## Scope

- `internal/agentrunner`: a new `ClaudeCode` type implementing `graph.AgentRunner` (and the
  no-op `Lifecycle` pair from issue 03, for symmetry -- its `Start`/`Close` do nothing, since
  each call is already a fresh, self-contained process).
- Spawn `claude -p --input-format=stream-json --output-format=stream-json` per call (this part
  matches the existing `Subprocess.Run()` spawn-per-call shape -- reuse what still applies:
  `exec.CommandContext`, stdin/stdout pipes, `Stderr`/`Env` passthrough).
- Write the request as Claude Code's real `stream-json` input shape (not the old
  `Request{NodeID, Prompt, State}` object) -- derive the exact input message shape from the
  fixture captured in issue 01 (`testdata/claude-code-stream.jsonl`) plus `claude --help`'s
  documented flags; if the input side needs its own separate capture beyond issue 01's output
  capture, say so explicitly in the PR rather than guessing the input shape from the output
  fixture.
- Parse the streamed output: extract incremental text into `TokenSink` as it arrives, and the
  final result's content into `graph.AgentResult.Output` -- decide and document explicitly
  which SDK message fields become which state keys (mirroring `displayMessage`'s existing
  `state["display:<nodeID>"]` convention where it applies).
- `OnActivity` fires exactly as `Subprocess`'s does today: `true` at spawn, `false` (with the
  node's own display message if it set one) when the process ends, on every exit path.

## Out of scope

- OpenCode -- issue 05.
- Any Claude Code flag beyond what a working round trip needs (model selection, permission
  mode, MCP config, `--agents`, etc.) -- Epic 2's Out of scope.
- Removing or keeping the old `Subprocess`/placeholder protocol -- decide explicitly in this PR
  whether it is deleted (nothing in the audit found a real external consumer of it) or kept
  behind its own `KERN_AGENT_KIND` value for backward compatibility, and state which, since the
  epic's ledger did not settle this directly.

## Acceptance criteria

- [ ] Unit test decodes `testdata/claude-code-stream.jsonl` (issue 01's real fixture) and
      asserts the exact `graph.AgentResult.Output` and `TokenSink` content produced -- not a
      hand-invented fixture.
- [ ] A real round trip (`KERN_AGENT_KIND=claude-code`, a real `claude` binary and API
      credential present) produces a non-empty `AgentResult`, **self-skipping** when the
      binary or credential is absent -- same convention as this repo's `DEEPSEEK_API_KEY`-gated
      e2e tests.
- [ ] `OnActivity` fires `true` then `false` exactly once per call, on both the success and
      error paths.
- [ ] A non-zero exit with no usable result is reported as an error naming the adapter.
- [ ] `go build ./...`, `go vet ./...` and `go test ./...` are green.
- [ ] An OKF fiche is added under `docs/index/` for this branch, per CONVENTIONS.md.

## Relevant files / areas

- `internal/agentrunner/subprocess.go`, `protocol.go` -- what to model the spawn/pipe mechanics
  on, and what NOT to repeat (the invented protocol).
- `internal/agentrunner/testdata/claude-code-stream.jsonl` (issue 01).
- `internal/cmd/runtime.go` -- `displayMessage`'s existing convention.

## Dependencies

Blocked by issues 01, 02, 03. Blocks issue 07.

## PR size note

Target ~500 changed lines; if this grows past ~1000, split it before opening the PR.
