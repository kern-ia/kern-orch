# Kern-Orch

**Kern-Orch is a tool that runs AI agents as a team, following a plan you draw as a diagram.**

Instead of calling one AI model and hoping for the best, Kern-Orch lets you describe a
step-by-step workflow — a "graph" — where each step is either:

- a small piece of code that does one deterministic thing (no AI involved), or
- an AI agent that gets a task and produces an answer.

Kern-Orch then executes that workflow for you: it runs the steps in the right order, passes
information from one step to the next, and — if something crashes halfway through — lets you
pick up exactly where it left off instead of starting over.

## Why this exists

AI agents are unreliable on their own: they can get stuck, go off-script, or lose track of
what they were doing on a long task. Kern-Orch's job is to keep them on rails:

- **You control the plan, not the AI.** The order of steps and the rules for what happens
  next ("if the agent says X, go here; otherwise go there") are written in plain configuration
  files, not left to the AI to decide.
- **The AI never runs "loose."** Kern-Orch never talks to an AI model directly inside its own
  code. It hands off each AI step to a separate, external program built specifically for that
  (a "provider CLI"), waits for the result, and continues the plan. This keeps AI calls
  contained and swappable.
- **Nothing is lost on failure.** Every step is saved to a local database as it happens. If
  the process is interrupted, you can resume the exact same run later.
- **Long tasks don't spiral.** An agent's working memory can be reset on purpose ("freeze")
  partway through a run, so it doesn't get bloated with old context and drift off task.

## The building blocks

| Concept | In plain words |
|---|---|
| **Graph** | The overall plan: a set of steps and the connections between them. |
| **Node** | One step in the plan. It's either a **tool** (plain code, no AI) or an **agent** (a task handed to an AI model). |
| **State** | The shared notebook that gets passed from step to step, so each one can read what happened before and add its own results. |
| **Edge** | The arrow between two steps — it can be fixed ("always go here next") or conditional ("go here only if the state says so"). |
| **Sub-graph** | A whole mini-plan nested inside a single step, useful for a self-contained sub-task. |
| **Checkpoint** | A saved snapshot of progress, written after each step, so a run can be resumed after a crash. |
| **Freeze** | Wiping an agent's short-term working memory on purpose, while keeping the important results, to stop long runs from becoming a mess. |

## How a run flows

```mermaid
flowchart LR
    A["1 . You write a plan\n(a YAML file describing steps)"] --> B["2 . Kern-Orch loads it\nand builds the graph"]
    B --> C["3 . Engine runs each step\nin order"]
    C --> D{"What kind of step?"}
    D -->|"Tool step"| E["Plain Go code runs\n(no AI, instant, free)"]
    D -->|"Agent step"| F["External AI CLI is launched\nas a separate process"]
    E --> G["Result is saved\nto the shared state"]
    F --> G
    G --> H["Progress is checkpointed\nto a local database"]
    H --> I{"More steps left?"}
    I -->|"Yes"| C
    I -->|"No"| J["Run finished"]
    H -.->|"Crash / interruption"| K["Resume later\nfrom the last checkpoint"]
```

The key idea: **the harness decides what happens next, the AI only does the thinking it's
asked to do for one step at a time.**

## Getting started

```bash
go build ./...
go run . run examples/hello.yaml
```

This runs a minimal example graph with no setup required — if no external AI CLI is
configured, Kern-Orch uses a built-in stand-in so you can see the whole flow working end to
end. To use a real AI provider for the agent steps, set the `KERN_AGENT_CLI` environment
variable to point at that provider's command-line tool.

Other useful commands:

```bash
go run . status              # see the state of past runs
go run . resume <run-id>      # continue a run after an interruption
go run . list-skills          # see what tools/agents are registered
```

## Connection contracts

Every `kern-*` brick publishes what it accepts and states what it needs. Nothing else is
part of the contract: internal packages, the checkpoint schema and the SQLite file may
change without notice. **kern-orch depends on no other brick** — it exposes and consumes
contracts, never internals.

### Consumed — provider CLI (`kern-link`)

kern-orch never calls an LLM itself. Each `agent` node spawns the binary named by
`KERN_AGENT_CLI` and speaks JSON-lines over its standard streams. Unset the variable and a
deterministic stub takes over, so the harness runs with nothing configured.

> **Status: provisional.** This protocol is specified in `internal/agentrunner/protocol.go`
> and awaits reconciliation with the real CLI. Treat it as a draft, not a stable contract.

kern-orch writes exactly one request object to the child's stdin, then closes it:

```json
{"node_id":"think","prompt":"Do some work","state":{"…":"…"}}
```

The child streams events back on stdout, one JSON object per line:

```json
{"type":"token","text":"…"}      // incremental output, forwarded to the token sink
{"type":"result","output":{…}}   // final; output is merged into the shared state
{"type":"error","message":"…"}   // aborts the run
```

The last `result` wins. A non-zero exit with no result is an error.

### Emitted — step transitions

When `KERN_STEP_REPORT_URL` is set, kern-orch POSTs one event per completed graph level to
that URL. Unset, it emits nothing and behaves exactly as before.

The URL is the whole contract: kern-orch knows nothing of the sink's route shape, and the
sink needs no knowledge of kern-orch beyond the schema below. Today's consumer is
[`kern-ui`](../Kern-UI/README.md).

#### `StepEvent` — contract `kern.step-event/v2`

<!-- CANONICAL BLOCK — mirrored verbatim in Kern-UI/README.md and Kern-Orch/README.md.
     Drift is caught by tests, not by discipline: the same payloads live in
     contracts/kern.step-event.v2*.json in both repos, and each side asserts against them on
     every CI run — kern-orch that its reporter emits exactly this, kern-ui that its
     ingestion accepts exactly this. Change the contract and both suites go red. -->

```json
{
  "run_id": "a23ead5373d9b746",
  "graph": "hello",
  "step": 2,
  "frontier": ["synthese", "critique"],
  "state": { "echo": "..." },
  "at": "2026-07-26T12:00:02Z",
  "requester": "yoann",
  "dossier": "AF-2288",
  "topology": {
    "entry": "greet",
    "nodes": [{ "id": "greet", "kind": "agent", "skill": "planner" }],
    "edges": [{ "from": "greet", "to": ["synthese"] }]
  }
}
```

| Field | Type | Required | Meaning |
|---|---|---|---|
| `run_id` | string | yes | Identifies the run. |
| `graph` | string | yes | Human label for the run, shown by the consumer. |
| `step` | int >= 0 | yes | Level counter, increases within a run. |
| `frontier` | string[] | yes | The nodes to execute **next**. An empty list means the run is over. |
| `state` | object | no | Flat business data. Never a producer's internal envelope. |
| `at` | RFC 3339 | yes | When the level completed. |
| `requester` | string | no | Who asked for this run (C6). Empty means open — steerable by anyone. Sent **once**, on the run's first event, like `topology`. |
| `dossier` | string | no | A caller-supplied business label (e.g. a client case) grouping several runs together for a consumer like kern-ui's dossiers list. Distinct from `requester` — an identity used for a steering-permission check, not a grouping key. Empty means the run belongs to no dossier. Sent **once**, on the run's first event. |
| `topology` | object | no | The graph's shape. Sent **once**, on the run's first event. |
| `topology.entry` | string | yes | Entry node id. Never appears in a frontier — it ran first. |
| `topology.nodes[]` | object | yes | `id` and `kind` (`tool` / `agent` / `subgraph`), plus `skill` on an agent node. |
| `topology.nodes[].skill` | string | no | The catalogue entry backing the node, as declared by `skill:` in the YAML. **Not the id** — a node `greet` may run the skill `planner`, so matching the two by name would be a guess. Absent on tool nodes, which name a Go function. |
| `topology.edges[]` | object | no | `from`, `to[]`, or `dynamic: true` when a router picks the targets at run time. |
| `error` | object | no | Set on the terminal event of a run that failed; `message` is required. |
| `parent` | object | no | Set on a **nested run** — the graph a subgraph node ran. `run_id` is the parent run, `node_id` the node it belongs to. Absent on a top-level run. |
| `error.nodes[]` | string[] | no | The nodes of `frontier` that actually broke. A node in `frontier` and **absent here completed** — the producer waits for the whole level before giving up. Omitted when the producer cannot say, and a consumer then falls back to marking the whole frontier. |

kern-orch fills `graph` with the topology file name minus its extension, and `state`
through `State.Keys()`/`Get()` — the wire form of `graph.State` (zones, freeze counter)
deliberately stays inside kern-orch.

**What a sink must accept**

- **Replays.** The same step may arrive twice; a sink must be idempotent.
- **`frontier: []`** as the end-of-run marker, not a missing field.

**What kern-orch guarantees**

- **Reporting never fails a run, nor slows it.** Whatever the sink answers — 500, timeout,
  unreachable host, malformed URL — the graph keeps going at full speed and the error goes
  to stderr. Levels are queued and delivered by a single worker off the engine's thread.
- **One POST per level, in order.** Order is a guarantee: a sink folds levels in sequence
  and rejects one older than the level it holds, so delivery uses one queue rather than a
  goroutine per event. A failure goes through the same queue and can never overtake the
  levels that led to it.
- **A sink too slow to keep up loses levels rather than blocking the run.** The queue holds
  64; past that, events are dropped and announced on stderr. Every event carries the full
  merged state, so the next one supersedes what was lost.
- **Exiting is bounded too.** The command waits up to 3 s for the queue to drain, then gives
  up loudly. Moving delivery off the engine's thread would only relocate the wait if the
  process then hung on exit.
- Granularity is the level, not the node: `Engine.OnStep` fires after a whole frontier
  completes.

### Emitted — agent activity

When `KERN_ACTIVITY_REPORT_URL` is set, kern-orch reports each time an agent node starts and
stops working. Unset, it reports nothing.

Unlike the step reporter this one posts **off the run's thread**. A step is reported between
levels, where a pause costs little; activity is reported at the exact moment an agent is
about to start, and making it wait on an HTTP round trip would let observability slow down
the thing it observes. The command flushes before exiting, because the signal that says an
agent *stopped* is the last one a run emits and precisely the one a process exiting would
drop.

#### `ActivityEvent` — contract `kern.activity/v1`

<!-- CANONICAL BLOCK — mirrored verbatim in Kern-UI/README.md and Kern-Orch/README.md.
     The same payload lives in contracts/kern.activity.v1.json in both repos, asserted from
     both sides on every CI run. -->

```json
{
  "run_id": "a23ead5373d9b746",
  "graph": "hello",
  "node_id": "greet",
  "generating": true,
  "at": "2026-07-26T12:00:01Z"
}
```

| Field | Type | Required | Meaning |
|---|---|---|---|
| `run_id` | string | yes | Identifies the run. |
| `graph` | string | yes | Human label. Required because this event routinely opens a run. |
| `node_id` | string | yes | The node whose model started or stopped. |
| `generating` | bool | yes | `true` when the model began working, `false` when it finished. |
| `at` | RFC 3339 | yes | When the transition happened. |

**What kern-orch guarantees**

- **The bracket always closes.** The stop is deferred, so it fires on every path out of an
  agent node — including the failing ones — and it is reported on a detached context, since
  a run is usually already cancelled by the time its last agent stops.
- **The bracket opens at spawn, not at the first token.** A provider that answers in one
  piece streams no token at all, and waiting for one would report such a node as never having
  worked. What travels is the coarse fact: the model is working.
- **Only agent nodes report.** A tool node runs Go code; no model is involved.
- **Signals may arrive out of order**, being sent off-thread. Each carries `at` so a sink can
  keep only the freshest word about a node.

### Emitted — skills catalogue

When `KERN_REGISTRY_REPORT_URL` is set, kern-orch POSTs its whole skills registry to that
URL: once at the start of every `run`, and on demand via `kern-orch publish-skills`. Unset,
it publishes nothing.

It is a second variable rather than a route derived from `KERN_STEP_REPORT_URL`, for the
reason stated above: the URL is the whole contract, so kern-orch must not invent a sibling
path on a host it knows nothing about.

#### `Catalogue` — contract `kern.registry/v1`

<!-- CANONICAL BLOCK — mirrored verbatim in Kern-UI/README.md and Kern-Orch/README.md.
     Drift is caught by tests, not by discipline: the same payload lives in
     contracts/kern.registry.v1.json in both repos, and each side asserts against it on
     every CI run — kern-orch that its publisher emits exactly this, kern-ui that its
     ingestion accepts exactly this. Change the contract and both suites go red. -->

```json
{
  "source": "kern-orch",
  "at": "2026-07-27T12:00:00Z",
  "skills": [
    { "name": "Analyse", "kind": "tool", "description": "Décompose une demande." },
    { "name": "Scribe", "kind": "agent", "description": "Rédige et reformule." }
  ]
}
```

| Field | Type | Required | Meaning |
|---|---|---|---|
| `source` | string | yes | Which brick published this catalogue. |
| `at` | RFC 3339 | yes | When it was read. |
| `skills[]` | array | yes | Every skill the producer holds. May be empty. |
| `skills[].name` | string | yes | The key. kern-orch already indexes its registry by name, so no second identifier was invented for the wire. |
| `skills[].kind` | string | yes | `tool` or `agent` — the `type:` of the SKILL.md frontmatter. |
| `skills[].description` | string | no | The frontmatter `description`, one line. |

**What kern-orch guarantees**

- **The catalogue is whole.** Each publication replaces the previous one; a skill removed
  from disk disappears downstream on the next publication.
- **Sorted by name**, so a sink never has to sort.
- **Publishing never fails a run**, exactly like step reporting: a broken sink costs a line
  on stderr and nothing else.

**What deliberately does not travel.** A skill's directory — a filesystem path is an
internal, not a contract. Its SKILL.md body. Any "wired" flag — a loaded skill is by
definition available, so the field would read true on every row.

### Asked for, not yet emitted

Topology, failure and the skills registry have all shipped. One thing is still asked for:

| What | Where it would live | Why kern-ui needs it |
|---|---|---|
| Tool invocation and readback | `internal/tools` | A consumer can list the wired tools but cannot ask any of them for a display value |

Stated in full, with the other bricks' contracts, in
[`../Kern-UI/docs/expected-contracts.md`](../Kern-UI/docs/expected-contracts.md).

### Serving — `kern-orch serve`

kern-orch can run as a long-lived service instead of a one-shot command: `kern-orch serve`
accepts runs over HTTP and executes them in the background, so a run no longer needs a
process kept alive for its whole duration. This is the seam EPIC-03 (tool exposition) needs
— a process that is actually there to answer — and it is what makes a centralised instance
possible at all.

| Method | Path | Purpose |
|---|---|---|
| `GET` | `/healthz` | Liveness — open, no credential. |
| `POST` | `/api/v1/runs` | `{"graph":"<path>"}` → **202** `{"run_id":...}`. Fails **synchronously** (400) on a bad path or a graph that will not load — a caller learns immediately rather than polling for a run that will never appear. |
| `GET` | `/api/v1/runs` | Every run known to the checkpoint store. |
| `GET` | `/api/v1/runs/{id}` | One run, including one just accepted: a `queued` checkpoint is written before the response returns, so a status query can never race the acceptance and find nothing. |
| `POST` | `/api/v1/runs/{id}/resume` | Continues a stopped run in the background. `404` on an unknown id; a no-op on one already complete. |

**Authentication.** `KERN_ORCH_TOKEN` is a bearer credential every endpoint but `/healthz`
requires. Unset, the daemon is open — the local-development case — and **the process refuses
to start on a public address without it**, the same rule kern-ui enforces on its own API for
the same reason: a warning scrolls past, a process that will not start does not.

**What this is not.** Runs report to kern-ui exactly as `run`/`resume` already do — nothing
changes there. It does not yet expose tools for invocation or readback (C5); that is
EPIC-03's remaining work, now with somewhere to live.

### Not yet defined

`kern-pilot` (steering), `kern-obs` (observability), `kern-policy`, `kern-guard`,
`kern-memory` — see [`docs/ROADMAP.md`](docs/ROADMAP.md). None of them has a contract yet;
kern-orch exposes no endpoint for them today.

## Project layout

- `internal/graph` — the core engine: steps, connections, shared state, execution order.
- `internal/agentrunner` — the bridge to the external AI command-line tool.
- `internal/checkpoint` — saves progress so runs can be resumed.
- `internal/skills` — the catalog of available tools and agents.
- `internal/topology` — turns a YAML plan file into a runnable graph.
- `internal/cmd` — the command-line interface (`run`, `resume`, `status`, `list-skills`).
- `examples/` — sample graphs you can run right away.
- `docs/` — architecture diagrams, roadmap, and glossary for a deeper dive.

## Learn more

- [`docs/ARCHITECTURE.md`](docs/ARCHITECTURE.md) — detailed diagrams of how the pieces fit
  together.
- [`docs/GLOSSAIRE.md`](docs/GLOSSAIRE.md) — full glossary of terms used across the project.
- [`docs/ROADMAP.md`](docs/ROADMAP.md) — what's built, what's planned.

## License

MIT — see [LICENSE](LICENSE).
