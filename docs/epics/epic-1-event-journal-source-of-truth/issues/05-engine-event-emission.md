---
type: Issue
title: "Emit per-node and level-boundary events from the engine"
description: "The engine reports node starts, outcomes and level boundaries through a port, at the grain decision 03 chose."
tags: [epic-1]
timestamp: 2026-08-19T14:35:00Z
epic: 1
issue: 05
slug: engine-event-emission
size: M
status: in-progress
gh_issue: 9
resource: https://github.com/kern-ia/kern-orch/issues/9
depends_on: [2]
---

# Emit per-node and level-boundary events from the engine

## Summary

Decision 03 chose per-node facts plus explicit level boundaries, because the facts of a run happen
per node and the current design computes that knowledge and then discards it. `graph.LevelError`
already documents the distinction that gets thrown away today: "a node in the level that is absent
from it **completed**".

This issue makes the engine emit those facts. It follows the existing dependency direction —
`graph` declares the port, and the infrastructure implements it, never the reverse. That direction
is a CONVENTIONS.md rule: any PR reversing it must justify it explicitly.

## Scope

- A port in `internal/graph` for emitting journal events, in the same style as the existing
  `StepFunc` / `NudgeFunc` / `AgentRunner` ports — an interface or function type declared by
  `graph`, with no import of the journal package if that would invert the direction (pass the
  payload as `graph`-owned types, or declare the port over an interface the journal satisfies;
  decide and record the choice in the package comment).
- `runLevel` emits: level opened with its frontier, node started per node, node produced with the
  keys it set, node failed with its error, level closed with the frontier and **the combination
  rule applied**.
- Run-level events: run started, run finished, run failed.
- A nil port is a no-op, matching how `onStep` and `beforeLevel` already behave.

## Out of scope

- Nudge and freeze events — issue 06.
- Writing any of it to a store — issue 07.
- Subgraph runs — issue 09.

## Acceptance criteria

- [ ] A single-node level emits level-opened, node-started, node-produced, level-closed in that
      order, with the level-closed event naming the replace rule.
- [ ] A fan-out level emits one node-started and one node-produced per node and a level-closed
      naming the merge rule.
- [ ] A level where one node fails and another completes emits node-failed for the first and
      node-produced for the second — the knowledge `LevelError` currently carries only as far as
      the caller.
- [ ] Node-produced carries the keys that node actually set on its branch, not the whole state.
- [ ] A nil emitter changes no behaviour and allocates nothing per level.
- [ ] `go test -race ./internal/graph/...` passes: emission happens from the per-node goroutines,
      so this is the second place `-race` becomes load-bearing.
- [ ] `go build ./...`, `go vet ./...` and `go test ./...` are green.
- [ ] An OKF fiche is added under `docs/index/` for this branch, per CONVENTIONS.md.

## Relevant files / areas

- `internal/graph/engine.go` — `Engine`, `runLevel`, `RunFrom`, `StepFunc`, `NudgeFunc`,
  `LevelError`, the `outcome` struct.
- `internal/graph/node.go` — the existing port declarations to match in style.
- `internal/cmd/runtime.go` — `multiStep`, where the new hook gets wired alongside `checkpointHook`.

## Dependencies

Blocked by 02. Blocks 06, 07, 09.


## PR size note

Target ~500 changed lines; if this grows past ~1000, split it before opening the PR.
