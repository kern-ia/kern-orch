# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

> Conventions de développement (branches, commits, PR, lint, CI) : voir [`CONVENTIONS.md`](CONVENTIONS.md).

## Status

Functional end-to-end. The harness is a **Go 1.26** module (`github.com/yoann/kern-orch`) using **Cobra** (CLI), **`modernc.org/sqlite`** (pure-Go, no cgo), and **`gopkg.in/yaml.v3`**. All six planned features are built and merged to `dev`: graph engine (`internal/graph`), agent runner (`internal/agentrunner`), checkpoint store + resume (`internal/checkpoint`), skills registry (`internal/skills`), YAML topology loader (`internal/topology`), config (`internal/config`), and the wired CLI (`internal/cmd`). `run`/`resume`/`status`/`list-skills` all work; see `examples/hello.yaml`. The design spec `harnais-agentique-CDC-v2.md` is in French; keep design discussion consistent with it.

### How the pieces fit
`cmd` reads `config` (env) → builds an `AgentRunner` (`agentrunner.Subprocess` if `KERN_AGENT_CLI` is set, else `agentrunner.Stub`) → `topology.Load` turns a YAML graph into a `graph.Graph`, resolving `tool`/`router` names against a `topology.Registry` of Go funcs and backing `agent` nodes with the runner → `graph.Engine` runs it level-synchronously, calling an `OnStep` hook that persists each level to the `checkpoint` store. `resume` reloads the latest checkpoint and calls `Engine.RunFrom` (the graph path is recorded in the checkpoint, so `resume <run-id>` needs no graph argument). Dependency direction: `graph` defines the `AgentRunner` port and the `StepFunc` hook; `agentrunner` and `checkpoint` depend on `graph`, never the reverse.

**Context zones & freeze**: `State` keys carry a zone (`ZonePersistent` default, `ZoneEphemeral` for scratch via `SetZoned`). `State.Freeze(carry)` respawns a fresh context — keeps the carry-over (default: persistent-zone keys), drops the rest, bumps `Frozen`. Exposed as the builtin `freeze` tool for YAML graphs. Engine detail: a **single-node frontier replaces** the state with its branch (so `Freeze`/deletions propagate), while a **fan-out (>1) uses additive `Merge`**.

### Subgraphs (sub-agents, spec §3)
A node can run a nested graph: `graph.SubgraphNode` runs a child `Graph` with its own state (seeded from the parent via `WithInput`, default Clone; result merged back via `WithOutput`, default Merge). From the parent's checkpoint view the whole sub-run is one atomic step (spec §6.3 = checkpoint at sub-graph boundaries). In YAML: `type: subgraph` with a `graph: <file>` reference — loaded by `topology.LoadFile` (recursion-guarded). `Load([]byte)` rejects subgraph nodes, so `run`/`resume` use `LoadFile`. See `examples/parent.yaml` → `examples/child.yaml`.

### Provisional / to reconcile
The `agentrunner` JSON-lines protocol (`internal/agentrunner/protocol.go`) is a **placeholder** for spec §6.4 — reconcile with the real multi-provider CLI once accessible. The `topology.Registry` ships only a `noop` builtin tool; real projects register their own tool/router funcs in Go.

## Commands

- Build: `go build ./...`
- Vet: `go vet ./...`
- Test all: `go test ./...`
- Single package: `go test ./internal/cmd/`
- Single test: `go test ./internal/cmd/ -run TestRootHasExpectedSubcommands -v`
- Run CLI: `go run . <command>` (e.g. `go run . --help`, `go run . list-skills`)

## Workflow (greenfield-tdd-okf skill)

Git-flow: `main` (stable jalons) ← `dev` ← one branch per feature. **Merge `--no-ff` to `dev` only when tests are green + build passes + E2E done. Never commit directly to `main`/`dev`.** Each feature: write tests first on pure logic in the engine/services, then wire thin orchestration; write an OKF fiche in `docs/index/<n>-<feature>.md` (template in `.claude/skills/greenfield-tdd-okf/references/okf-fiche-template.md`, body ≤15 lines, dated decisions); log pitfalls in `retro.md` when they bite. Generic pitfalls also go back into the skill's `references/pieges.md`.

## What is being built

An **agentic harness** in Go: an orchestrator that runs an explicit **graph of agents and sub-agents** (LangGraph-style: shared mutable state, nodes, edges, conditional routing). It is not a sequential script runner — it owns agent/sub-agent lifecycle, routing, and state.

Critical architectural boundary: **the harness never calls LLMs directly.** LLM execution lives in a *separate, existing* multi-provider streaming CLI (Pi-inspired). The harness invokes that CLI **as a subprocess** for every "agent" node, streaming its stdout/stderr. This subprocess contract is the load-bearing seam of the whole system.

### Core model (LangGraph ported to Go)

- **State** — a shared Go struct/map mutated at each node and threaded along the graph.
- **Node** — either a **skill-tool** (direct execution, no LLM) or a **skill-agent** (spawns the external CLI subprocess with state as context).
- **Edge** — static or **conditional** (a routing function evaluated on prior node state/output).
- **Sub-graph / sub-agent** — a node may trigger a nested graph (child run with its own state, result bubbles back to the parent state).
- **Checkpoint** — state persisted to SQLite at each step for resume-after-failure, debug, and audit.

The same skills library is used two ways: as **tools** (functions invoked by an agent) and as **agents** (a skill that is itself a graph node). Tool-vs-agent is intended to be marked in the skill's `SKILL.md` frontmatter (`type: tool | agent`).

### Planned layout (`harness/`)

`internal/graph/` (engine: `state.go`, `node.go`, `edge.go`, `engine.go`) · `internal/skills/` (registry: `manager.go`, `loader.go`) · `internal/agentrunner/` (subprocess wrapper to the external CLI: spawn, stream parsing, timeout, retry) · `internal/checkpoint/` (SQLite) · `internal/cmd/` (Cobra: `run <graph>`, `resume <run-id>`, `status`, `list-skills`) · `internal/config/`.

## Working guidance from the spec

- **Do not start implementation before the subprocess contract (spec §6.4) is decided.** How state is serialized into the external CLI and how its streamed output is parsed back is the blocking dependency for `internal/agentrunner/` — everything else can proceed, this cannot.
- Start from `internal/graph/state.go` and `internal/graph/node.go` (the engine core). **Keep node execution fully isolated from the routing engine.**
- Several design choices are deliberately still open (spec §6) and should not be silently assumed: graph declaration format (static YAML/JSON vs. programmatic Go construction), whether conditional routing is decided in Go or delegated to an LLM/skill, checkpoint granularity (per-node vs. sub-graph boundaries), and how tool-vs-agent is determined. Surface these rather than picking one unprompted.

## Engineering constraints

- **SOLID.** Respect the five principles throughout — especially single responsibility (keep node execution, routing, skill registry, subprocess handling, and persistence in separate units) and dependency inversion (depend on interfaces, e.g. the `agentrunner` subprocess boundary and the `checkpoint` store, not concrete types). This mirrors the spec's requirement to isolate node execution from the routing engine.
- **DRY.** Do not duplicate logic. Factor shared behaviour (state serialization, stream parsing, error/retry handling) into reusable functions rather than copy-pasting across nodes, skills, or commands.
- **No dependency on a consumer, ever.** kern-orch emits contracts; it does not know who
  reads them. Concretely: no consumer module in `go.mod`, no shared package, no knowledge of
  a sink's route shape (one configured URL per contract, never a sibling path derived from
  another), and **no consumer vocabulary in the code** — a comment naming another brick's
  screen is how a brick quietly starts assuming a single consumer. Say "a sink", "a
  consumer", "the catalogue". Emitting is always best-effort: a broken sink costs a line on
  stderr and never a run.
- **Never delete without consent.** Do not remove files, functions, code, or data without explicit user approval. If something looks obsolete, propose the deletion and wait for confirmation before acting.

## Reference points (for design, not to fork)

- **LangGraph (Python)** — conceptual reference for the state/node/edge model.
- **charmbracelet/crush (Go)** — inspiration for `internal/skills/manager.go` (registry pattern) and its event bus (run observability); *not* its agent model (Crush has a simple coordinator/session, no graph).
- **Protocol-Lattice/go-agent** — Go multi-agent framework with graph-aware memory; closer to the graph/multi-agent need than Crush.
