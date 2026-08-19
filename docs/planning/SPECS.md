---
type: Technical Specification
title: "kern-orch — Technical Specs"
description: "A pure-Go agentic harness that executes an explicit graph of tool, agent, subgraph and approval nodes, checkpointing each level to SQLite and never calling an LLM itself."
tags: [planning, specs]
timestamp: 2026-08-19T09:16:15Z
status: final
mapped_commit: 85d3f0af6227f855a76b887cfb05d44f9dd72056
mapped_at: 2026-08-19T09:16:15Z
---

# kern-orch — Technical Specs

`kern-orch` is the orchestration brick of the `kern-*` family. It executes a declared graph
of nodes over a shared mutable state, persists progress so an interrupted run resumes, and
delegates every LLM call to an external provider CLI invoked as a subprocess. The harness
governs; the model executes one node at a time.

## Stack

| Concern | Choice |
|---|---|
| Language | Go 1.26.5 |
| Module path | `github.com/yoann/kern-orch` |
| CLI framework | `github.com/spf13/cobra` v1.10.2 |
| Database | `modernc.org/sqlite` v1.54.0 — pure Go, no cgo |
| Configuration format | `gopkg.in/yaml.v3` |
| HTTP | standard library `net/http` and `ServeMux` (Go 1.22 method-and-pattern routing) |
| PII masking | `github.com/kern-ia/kern-anon`, resolved through a local `replace` directive to `../Kern-Anon`; pulls `github.com/yalue/onnxruntime_go` for its ONNX NER engine |

There is no web framework, no ORM, no dependency-injection container and no code generator.
Outside Cobra, SQLite and YAML, the dependency tree is the standard library.

The `replace github.com/kern-ia/kern-anon => ../Kern-Anon` directive means the module does
not build from a clean checkout alone: a sibling `Kern-Anon` working copy must exist beside
it.

## Architecture

A single Go module, ~11 600 lines across 12 `internal/` packages and one `main.go`.

```
main.go              Cobra entrypoint
internal/graph       execution engine — State, Node, Graph, Engine, routing, zones
internal/topology    YAML → Graph loader, Registry of Go tool/router funcs
internal/agentrunner AgentRunner implementations: Stub and Subprocess
internal/checkpoint  Store interface + SQLite implementation
internal/steer       in-memory control mailbox (nudges, approval decisions)
internal/cmd         Cobra commands, runtime wiring, serve
internal/daemon      HTTP router, skill upload/creation endpoints
internal/report      HTTP sinks: step events, skills registry, agent activity
internal/skills      SKILL.md registry and write path
internal/tools       subprocess invocation of a tool skill, returning a display value
internal/notify      Telegram builtin tool
internal/config      environment-variable configuration
```

**Dependency direction is one-way and enforced by convention.** `graph` declares the ports
and depends on nothing internal; `agentrunner`, `checkpoint`, `report` and `steer` depend on
`graph`, never the reverse. The ports are:

- `AgentRunner.Run(ctx, AgentRequest) (AgentResult, error)` — how an agent node reaches the
  external LLM CLI.
- `StepFunc(ctx, StepInfo, *State) error` — called after every completed level; the seam the
  checkpoint store and the reporting sinks hook into.
- `NudgeFunc(ctx, *State) error` — called before every level, the injection point for live
  steering. Safe by construction rather than by locking: the previous level's goroutines have
  all returned and the next level's have not started.
- `ApprovalFunc(ctx, nodeID) (Decision, error)` — how an approval node blocks on a human.

### Execution model

The engine is **level-synchronous**. Each iteration executes the current frontier of nodes,
one goroutine per node, each on a `Clone()` of the shared state, then combines the branches
in frontier order and computes the next frontier from each node's route function.

Two combination rules, and the distinction is load-bearing:

- A **single-node frontier replaces** the shared state with its branch, so context-replacing
  operations — `Freeze`, key deletion — and the `Frozen` counter propagate.
- A **fan-out (more than one node) merges additively**, since each branch only contributes
  its own keys.

A cycle guard caps a run at 10 000 levels. A level that fails returns a `LevelError` naming
every node that failed, sorted; a node present in the frontier and absent from that list
**completed**, which is what lets a consumer colour the rest of the frontier rather than
blame all of it.

### Node kinds

`KindTool` (Go function, no LLM), `KindAgent` (delegates to the `AgentRunner`),
`KindSubgraph` (a nested graph with its own state, seeded from the parent via an input
function and merged back via an output function — one atomic step from the parent's
checkpoint view), and `KindApproval` (blocks the level until a human decides, recording the
answer under `decision:<nodeID>` so an ordinary conditional router can branch on it).

A node never chooses its successor. Routing belongs to the engine.

### State

`graph.State` is a `map[string]any` plus three pieces of metadata: a per-key **zone** label,
a `Step` counter, and a `Frozen` counter. A key is in `ZonePersistent` by default;
`SetZoned` places it in `ZoneEphemeral`. `Freeze` respawns a fresh context — it keeps the
carry-over (persistent-zone keys by default), drops the rest, and bumps `Frozen`. It is
exposed to YAML graphs as a builtin `freeze` tool.

State is not safe for concurrent mutation; isolation comes from the engine cloning per
branch and merging on a single goroutine.

## Data model & storage

One SQLite database, one table.

```sql
CREATE TABLE IF NOT EXISTS checkpoints (
    run_id     TEXT    NOT NULL,
    step       INTEGER NOT NULL,
    frontier   TEXT    NOT NULL,   -- JSON array of node ids still to execute
    state      TEXT    NOT NULL,   -- JSON of graph.State (step, frozen, data, zones)
    status     TEXT    NOT NULL,   -- queued | running | done | failed
    created_at TEXT    NOT NULL,   -- RFC3339Nano
    graph_path TEXT    NOT NULL DEFAULT '',
    requester  TEXT    NOT NULL DEFAULT '',
    dossier    TEXT    NOT NULL DEFAULT '',
    PRIMARY KEY (run_id, step)
);
```

**The durable truth of a run is a state snapshot per level, not a stream of facts.** `Save`
upserts on `(run_id, step)`; `Latest` reads the highest step for a run; `List` rolls up one
summary per run from each run's highest step. A `queued` marker is written at the reserved
step `-1` — negative so that `MAX(step)` picks the real checkpoint the instant one lands.

Operational details that constrain any change here:

- The connection pool is pinned to a single connection (`SetMaxOpenConns(1)`) so that
  `PRAGMA busy_timeout=5000` actually applies to every access rather than to whichever
  connection came first. Concurrent access is real: the steering endpoints read the latest
  checkpoint for a requester check while the run's own goroutine may be writing one.
- **There is no migration mechanism.** A single ad-hoc `ALTER TABLE ... ADD COLUMN dossier`
  runs on every open with its error deliberately ignored, since the only failure mode against
  this fixed schema is "column already exists". Any schema change beyond an additive nullable
  column has nothing to build on today.

Two other stores are directories, not databases: skills under `KERN_SKILLS_DIR` (default
`skills`) and created skills under `KERN_SKILLS_CUSTOM_DIR` (default `skills-custom`),
deliberately separate so a product update can overwrite the former wholesale without touching
a user creation. Uploaded documents land in `KERN_ORCH_UPLOAD_DIR` (default `./data/uploads`).

## Auth

Two independent credentials, both bearer-style, both from the environment:

- **Inbound** — `KERN_ORCH_TOKEN` guards every `/api/v1/*` route on the daemon through an
  `auth` wrapper reading `Authorization: Bearer …`. An empty token leaves the daemon open,
  which `serve` accepts on a loopback address and **refuses on a public one**. The same rule
  exists in kern-ui and is re-derived here rather than shared, because the two bricks depend
  on nothing of each other's.
- **Outbound** — `KERN_SINK_TOKEN` is presented to all three reporting sinks. One secret for
  three URLs, on the grounds that they are three contracts to the same consumer.

Beyond the token there is no user model. `Requester` is a caller-supplied string carried on a
run and checked by the write path (stop / nudge / decide) before acting; empty means open,
which is what every CLI-started run is. `Dossier` looks similar but is a grouping label with
no bearing on permission.

## Interfaces & integrations

### CLI

`run`, `resume`, `status`, `list-skills`, `serve`, plus domain commands. `resume <run-id>`
needs no graph argument: the graph path is recorded in the checkpoint.

### HTTP daemon (`serve`, default `127.0.0.1:7070`)

```
GET    /healthz
POST   /api/v1/runs                              start a run
GET    /api/v1/runs                              list runs
GET    /api/v1/runs/{id}                         run detail
POST   /api/v1/runs/{id}/resume
POST   /api/v1/runs/{id}/stop
POST   /api/v1/runs/{id}/nudge
POST   /api/v1/runs/{id}/nodes/{node}/decide
GET    /api/v1/tools                             tool skill specs
POST   /api/v1/tools/{name}/invoke
POST   /api/v1/dispatch                          run a skill by name
POST   /api/v1/uploads
POST   /api/v1/skills                            create a skill
DELETE /api/v1/skills/{name}
```

### Outbound contracts

Three fire-and-forget HTTP sinks, each with its own URL variable — a sibling route is never
invented on a host the harness knows nothing about. Wire contracts are versioned and pinned
as JSON fixtures under `contracts/`:

| Contract | Carries |
|---|---|
| `kern.step-event/v1`, `/v2` | one completed graph level: run id, graph, step, frontier, flattened state, timestamp; plus topology, requester and dossier on the first event only, `error` on a terminal failure, and `parent` on a nested run |
| `kern.registry/v1` | the whole skills catalogue |
| `kern.activity/v1` | one signal each time an agent node starts and stops working |

**Reporting is observability, never a dependency of the run.** The hook reports its own
errors to stderr and always returns `nil`, so a sink that is slow, broken or absent can never
abort a graph. Delivery runs off the engine's thread behind a 64-slot queue that **drops**
rather than blocks — every event carries the full merged state, so a missed level is
corrected by the next one. `Flush` waits at most 3 seconds on exit and announces what it
drops.

A nested run reports as a run of its own, pointing back at its parent node, rather than
folding into the parent's stream: the parent's level counter is a sequence the sink rejects
out-of-order writes against.

### Agent runner protocol

A **provisional** JSON-lines contract, marked as a placeholder to be reconciled with the real
multi-provider CLI. The harness writes one `Request` object (`node_id`, `prompt`, `state`) to
the child's stdin and closes it; the child streams one JSON object per line on stdout —
`{"type":"token"}` incremental output, `{"type":"result","output":{…}}` final (merged into the
state, last one wins), `{"type":"error"}` aborting the run. A non-zero exit with no result is
an error. With `KERN_AGENT_CLI` unset, a deterministic `Stub` runs instead, so the harness
works end to end with no LLM configured.

### Tool skill protocol

A separate one-shot subprocess contract: one `{"input":{…}}` object in, one
`{"label","value","error"}` object out. The tool renders its own display string; no consumer
formats the domain value itself.

### Steering

`internal/steer.Mailbox`, one per running run, holds pending nudges and pending approval
decisions in memory. **Deliberately not durable** — a nudge or decision in flight is lost on
restart, the same crash story the approval pause already accepts. Persisting it would mean a
second checkpoint mechanism for state the run's own checkpoint does not need.

## Deployment & operations

Unknown — no Dockerfile, no Kubernetes or IaC manifests, no Procfile, no systemd unit and no
platform configuration exist in the repository. Operation today is `go build` plus a
backgrounded process per brick, as documented in the ecosystem-level README one directory up.

**No CI exists**: there is no `.github/` directory and no workflow of any kind. Nothing checks
build, vet, test or lint at merge time.

Configuration is environment-only, resolved in `internal/config` with defaults in code and no
config file: `KERN_SKILLS_DIR`, `KERN_SKILLS_CUSTOM_DIR`, `KERN_CHECKPOINT_DB`,
`KERN_AGENT_CLI`, `KERN_STEP_REPORT_URL`, `KERN_REGISTRY_REPORT_URL`,
`KERN_ACTIVITY_REPORT_URL`, `KERN_SINK_TOKEN`, `KERN_ORCH_ADDR`, `KERN_ORCH_TOKEN`,
`KERN_TELEGRAM_BOT_TOKEN`, `KERN_TELEGRAM_CHAT_ID`, `KERN_ORCH_UPLOAD_DIR`. Precedence is
CLI flags over environment over defaults.

Observability beyond the three HTTP sinks and stderr logging: none. No metrics, no tracing, no
structured logging, no error tracker.

## Testing infrastructure

Standard-library `testing` only — no testify, no mocking framework, no golden-file harness
beyond the JSON fixtures in `contracts/`. Test files are co-located with the code they cover
(`internal/graph/state_test.go` beside `state.go`), 44 of them against 47 source files.

Commands: `go build ./...`, `go vet ./...`, `go test ./...`, and
`go test ./internal/<pkg>/ -run <TestName> -v` for a single case.

Coverage is not measured and `-race` is not part of any recorded routine, despite the engine
being concurrent by construction.

## Cross-cutting concerns

**The harness never calls an LLM.** Every model call leaves the process as a subprocess
invocation of an external provider CLI. This is the load-bearing seam of the whole design and
the reason the engine stays deterministic and testable without a key.

**Declared topology versus observed graph.** Edges in a running graph are Go closures, so a
conditional route cannot be enumerated from the engine. The topology shipped to consumers is
therefore read from the YAML, and a conditional edge travels with no targets and a `dynamic`
flag — telling a consumer its picture is incomplete rather than letting it read the node as
terminal.

**Feature index.** 45 OKF fiches under `docs/index/`, one per merged feature, each with
frontmatter (`id`, `feature`, `branch`, `status`, `files`, `tests`, dated `decisions`) and a
what / why / verified-for-real body. They are written in French and are the intended entry
point before re-reading the code.

**Module path.** `github.com/yoann/kern-orch` does not match the org repository
`github.com/kern-ia/Kern-Orch`. `CONVENTIONS.md` records this as an open decision to be taken
at org level rather than repo by repo; it is unresolved as of this mapping.

**An event vocabulary already exists, on the reporting side only.** `kern.step-event/v2`
describes a completed level as a fact on the wire, with topology, failure attribution and
parent linkage. It is one-way, lossy by design (the 64-slot dropping queue), and carries no
authority: the durable record remains the per-level state snapshot in `checkpoints`. Any work
on an append-only journal starts from a vocabulary that is already half-designed and living on
the wrong side of the boundary.
