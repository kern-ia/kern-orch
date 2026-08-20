---
type: Decision
title: "Subprocess lifecycle: spawn-per-call versus spawn-once-serve-many"
description: "Does an adapter own a persistent child process across a run, or does the current spawn-inside-Run() model extend to every adapter?"
tags: [decision, change]
timestamp: 2026-08-20T09:45:00Z
phase: change
decision: 02
slug: subprocess-lifecycle
status: decided
verdict: "Adapters gain an explicit Start(ctx)/Close() lifecycle, called once around the run, separate from per-node Run()"
decided_via: triage
depends_on: ['approach']
change: 2
change_slug: agent-cli-adapter-registry
---

# Question
`Subprocess.Run()` spawns a fresh `exec.CommandContext` **on every call** — natural for Claude
Code's `-p` mode, where each invocation is deliberately a one-shot process that exits when done.

OpenCode's model is the opposite: `opencode serve` starts a long-lived HTTP server; the useful
unit of work is one REST call (`POST /session/:id/message`) against an **already-running**
server, not a fresh process per node. Spawning and tearing down an OpenCode server once per
`AgentNode.Execute` — once per node in the graph, potentially many times per run — pays a full
server-startup cost on every node for no reason. A graph with an agent node inside a fan-out
(several nodes calling the OpenCode adapter concurrently, per `graph.Engine`'s level-synchronous
model) would also either serialize on one server's readiness or need several server instances,
neither of which the current `Run()`-scoped spawn expresses.

# Options
- **Adapters gain an explicit lifecycle beyond `Run()`: `Start(ctx) error` / `Close() error`,
  called once by `newRunner`'s caller** (`serve.go`'s run-setup path, which already constructs
  and tears down the runner once per run) around the run's lifetime, not per node. Claude Code's
  adapter's `Start`/`Close` are no-ops (it has no persistent process to own); OpenCode's `Start`
  spawns and waits for the server's readiness, `Close` shuts it down.
- **Leave `Run()` as the only lifecycle method; have the OpenCode adapter lazily start its server
  on first `Run()` call and never stop it until process exit.** Simpler signature, but leaks a
  server process past the run that started it, and gives no clean shutdown point for the same
  reason `internal/checkpoint`'s store needed an explicit `Close()` — an orphaned child process is
  the subprocess equivalent of an orphaned file handle.

# Recommendation
The first. An explicit `Start`/`Close` matches the pattern `checkpoint.SQLiteStore` and
`behaviorapi.IngestHandler` already use in this ecosystem for anything owning a resource beyond
one call, and it gives `serve.go` one place to guarantee cleanup on every exit path — including a
`stop` request, which already cancels a run's context today. Only OpenCode's adapter does
non-trivial work in either method for now; the interface cost is one no-op pair for Claude Code's
adapter, not asymmetric complexity.

# Verdict

Adapters gain an explicit Start(ctx)/Close() lifecycle, called once around the run, separate from per-node Run().

The first. An explicit `Start`/`Close` matches the pattern `checkpoint.SQLiteStore` and
`behaviorapi.IngestHandler` already use in this ecosystem for anything owning a resource beyond
one call, and it gives `serve.go` one place to guarantee cleanup on every exit path — including a
`stop` request, which already cancels a run's context today. Only OpenCode's adapter does
non-trivial work in either method for now; the interface cost is one no-op pair for Claude Code's
adapter, not asymmetric complexity.
