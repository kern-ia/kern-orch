---
type: Decision
title: "Approach: registry of per-CLI adapters, port unchanged"
description: "How does support for multiple real agent CLIs land inside internal/agentrunner without touching graph.AgentRunner?"
tags: [decision, change]
timestamp: 2026-08-20T09:45:00Z
phase: change
decision: 01
slug: approach
status: decided
verdict: "A registry of adapters inside internal/agentrunner, one Go type per target CLI, all satisfying graph.AgentRunner unchanged"
decided_via: triage
depends_on: []
change: 2
change_slug: agent-cli-adapter-registry
---

# Question
`graph.AgentRunner` is a one-method interface (`Run(ctx, AgentRequest) (AgentResult, error)`),
already dependency-inverted: `graph` declares it, `agentrunner` implements it, and nothing in
`graph` names a concrete CLI. `internal/cmd/runtime.go`'s `newRunner` is the **only** call site
that builds one, and it builds exactly one runner per run, shared by every `AgentNode` in that
graph — there is no per-node selection today.

Today `agentrunner` has two implementations: `Stub` (deterministic, no CLI) and `Subprocess`
(one CLI, a provisional stdin/stdout JSON-lines protocol never reconciled against a real CLI —
its own package comment says so). Both real targets this change adds — Claude Code
(`-p --input-format=stream-json --output-format=stream-json`, stdio) and OpenCode (`opencode
serve`, HTTP/OpenAPI, `POST /session/:id/message`) — speak protocols `Subprocess` does not
speak and were never going to converge into one shape: one is a stream over a pipe, the other
is a spawned server reached over HTTP.

# Options
- **A registry of adapters inside `agentrunner`, one Go type per target CLI, all satisfying
  `graph.AgentRunner` unchanged.** `newRunner` picks the adapter by a new selector (decision 04)
  instead of a bare path. `graph` and every other package stay untouched — this is exactly the
  seam `AgentRunner` was built for.
- **One `Subprocess` type parameterized by a wire-format strategy** (a smaller interface for
  encode-request/decode-events, `Subprocess` stays the transport). Fails for OpenCode: the
  transport itself differs (spawn-once-serve-many over HTTP vs spawn-per-call over a pipe), not
  just the wire encoding — parameterizing only the encoding leaves the wrong transport underneath
  one of the two targets.
- **Fork `agentrunner` into one package per CLI.** Rejected: `Stub`, activity-hook semantics,
  and the `AgentRunner` contract itself would triplicate for no reason three call sites don't
  need duplicated.

# Recommendation
The first. `AgentRunner` already isolates `graph` from every implementation detail below it —
the registry is additive, and it is the only option where a third CLI later is one more adapter,
not a refactor. The wire-format-strategy option undersells how different OpenCode's transport
actually is; conflating "different bytes on the wire" with "different way of reaching the
process at all" is where that option breaks.

# Verdict

A registry of adapters inside internal/agentrunner, one Go type per target CLI, all satisfying graph.AgentRunner unchanged.

The first. `AgentRunner` already isolates `graph` from every implementation detail below it —
the registry is additive, and it is the only option where a third CLI later is one more adapter,
not a refactor. The wire-format-strategy option undersells how different OpenCode's transport
actually is; conflating "different bytes on the wire" with "different way of reaching the
process at all" is where that option breaks.
