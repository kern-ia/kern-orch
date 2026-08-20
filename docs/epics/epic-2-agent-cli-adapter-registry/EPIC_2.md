---
type: Epic
title: "Agent CLI adapter registry: Claude Code and OpenCode"
description: "Replace the provisional, never-reconciled agentrunner protocol with a registry of adapters that speak each real CLI's actual protocol, so kern-orch can spawn and pilot Claude Code or OpenCode as graph nodes."
tags: [epic, change]
timestamp: 2026-08-20T10:00:00Z
epic: 2
slug: agent-cli-adapter-registry
status: open
gh_issue: 33
milestone: 2
resource: https://github.com/kern-ia/kern-orch/issues/33
source: ../../planning/changes/change-2-agent-cli-adapter-registry/index.md
---

# Epic 2: Agent CLI adapter registry: Claude Code and OpenCode

## Goal

`internal/agentrunner`'s `Subprocess` implementation speaks a JSON-lines protocol its own
package comment marks as a placeholder: *"PROVISIONAL CONTRACT (spec §6.4) — to be reconciled
with the real CLI once accessible."* It was never reconciled. Verified against the two most
recognizable agent CLIs on the market:

- **Claude Code** speaks stdio: `-p --input-format=stream-json --output-format=stream-json`
  streams typed JSON messages both ways over a pipe — not the single-request,
  token/result/error shape `agentrunner` invented.
- **OpenCode** speaks HTTP: `opencode serve` starts a long-lived server exposing an
  OpenAPI 3.1 surface (`POST /session/:id/message`); there is no stdin/stdout pipe to speak
  a protocol over at all.

Neither is a variant of the placeholder. This epic replaces it with a registry of adapters —
one Go type per target CLI — behind the unchanged `graph.AgentRunner` port, so a graph node
can be backed by a real, widely-used agent CLI without the graph engine knowing which one, and
without the wrapped tool's source code changing at all: composition stays exactly how
`kern-exec`'s existing wrapper scripts already compose `kern-exec` with an arbitrary binary.

This is market-facing, not an internal refactor for its own sake: whoever already runs Claude
Code or OpenCode should be able to point `kern-orch` at it and have it work, with one
configuration surface that fails loud rather than silently misrouting when misconfigured.

## Scope

**The adapter registry.** `internal/agentrunner` gains one adapter type per target CLI —
`ClaudeCode` and `OpenCode` initially — each satisfying `graph.AgentRunner` unchanged
(`Run(ctx, AgentRequest) (AgentResult, error)`). `graph`, `internal/checkpoint`,
`internal/journal` and every other package stay untouched; this is exactly the seam
`AgentRunner` was built to absorb.

**Adapter selection.** A new `KERN_AGENT_KIND` environment variable (`claude-code` |
`opencode`) selects the adapter; `KERN_AGENT_CLI` keeps its existing meaning as that adapter's
binary path. `config.FromEnv()` fails loud at load — before any run starts — when
`KERN_AGENT_CLI` is set but `KERN_AGENT_KIND` is absent or unrecognized. Neither set keeps
today's behaviour: fall back to `Stub` unchanged.

**Explicit lifecycle beyond `Run()`.** Adapters gain `Start(ctx) error` / `Close() error`,
called once by the run-setup path around the run's lifetime — not per node. Claude Code's
adapter's `Start`/`Close` are no-ops, matching its one-shot-per-call nature; OpenCode's `Start`
spawns `opencode serve` and waits for readiness, `Close` shuts it down. `Close` runs on every
exit path, including a `stop` request.

**Session-scoped concurrency for OpenCode.** `graph.Engine`'s level-synchronous fan-out calls
`AgentRunner.Run` from several goroutines at once against the same adapter instance. The
OpenCode adapter opens one session per call (`POST /session` scoped to that call's `NodeID`)
rather than serializing behind a lock — a fan-out of agent nodes stays genuinely parallel.

**Private wire translation, no forced shared type.** Each adapter decodes its CLI's real
message shapes internally and exposes only `graph.AgentResult` plus writes to the existing
`TokenSink`/`OnActivity` contracts, both already CLI-agnostic. Claude Code's `stream-json`
message envelope and OpenCode's `{ info: Message, parts: Part[] }` response are structurally
unrelated; no shared intermediate `Event` type is imposed across them.

**Documentation, filed outside this epic.** `kern-exec`'s `wrap-agent-cli.sh` and
`wrap-agent-cli-through-firewall.sh` examples need updating to show both adapters wired
through `kern-exec`'s confinement — the whole point of the composition model is that neither
example needs a code change, only the `KERN_AGENT_CLI`/`KERN_AGENT_KIND` pair and the wrapped
binary. That work lands in a different repository, so it is tracked as
[kern-exec#1](https://github.com/kern-ia/kern-exec/issues/1) rather than as a sub-issue of
this epic — `create-issues`/`implement-epic`'s GitHub wiring assumes one repo per epic.

## Out of scope

- A documented external plugin SDK for a third party to write their own adapter outside this
  repo. A plugin *system* is a later, separate change once two real adapters exist to
  generalize from — designing it now would be from imagination, not from the two concretely
  different shapes (stdio vs HTTP) this epic's audit actually found.
- Any Claude Code or OpenCode flag beyond what each adapter's translation needs to produce a
  working round trip — model selection, permission modes, MCP config are real and useful, not
  required to prove the registry works.
- OpenCode's async completion path (`POST /session/:id/prompt_async`). Unaudited: how a caller
  observes completion (polling, SSE, something else) is not known, and nothing in
  `AgentRunner.Run()`'s blocking contract needs it.
- Per-node adapter selection within one graph. Today's architecture builds exactly one
  `AgentRunner` per run and hands the same instance to every `AgentNode`
  (`builtinRegistry(runner, cfg)`); mixing Claude Code and OpenCode nodes in one graph needs
  the YAML schema and the runner construction to become per-node, which is real, separable
  work — a second epic once this registry is proven with one adapter per run.
- Detecting the adapter kind automatically by probing the binary. Rejected during scoping:
  guessing wrong silently routes every request through the wrong protocol, which is worse than
  one required, well-documented variable that fails loud when missing.

## Acceptance criteria

- [ ] The registry selects the correct adapter type for each `KERN_AGENT_KIND` value; an
      invalid or missing `KERN_AGENT_KIND` with `KERN_AGENT_CLI` set fails loud at
      `config.FromEnv()`, before any run starts.
- [ ] `Stub` and the existing behaviour when neither variable is set are unaffected.
- [ ] `Start`/`Close` are each called exactly once per run; `Close` runs on every exit path,
      including a `stop` request mid-run.
- [ ] Two concurrent fan-out agent nodes calling the OpenCode adapter open two sessions and run
      concurrently — never serialized behind a lock.
- [ ] Each adapter's wire-translation logic is unit tested against a recorded real message (a
      captured Claude Code `stream-json` transcript; a captured OpenCode `/message` response
      body), not a hand-invented fixture.
- [ ] A real-binary test set exists for both adapters (a genuine round trip through
      `claude -p --output-format=stream-json` and through `opencode serve`), self-skipping when
      the binary is absent — the same convention this repo's own `test:e2e` already uses for
      `DEEPSEEK_API_KEY`.
- [ ] `go build ./...`, `go vet ./...` and `go test ./...` are green. (`kern-exec`'s
      wrapper-script update is tracked separately at
      [kern-exec#1](https://github.com/kern-ia/kern-exec/issues/1), not a criterion of this
      epic.)
- [ ] An OKF fiche exists under `docs/index/` for each merged feature branch of this epic.

## Dependencies

None. Independent of Epic 1 (event journal) — this epic touches `internal/agentrunner` and
`internal/graph/node.go`'s `AgentRunner` consumer only; Epic 1 never touched
`internal/agentrunner` (verified: no commits in that epic modified it).

## Context

- [Technical specs](../../planning/SPECS.md) — the `AgentRunner` port, `graph.Engine`'s
  level-synchronous fan-out, the one-runner-per-run construction in `internal/cmd/runtime.go`,
  and the journal/projection mechanism this epic does not touch.
- [Conventions](../../planning/CONVENTIONS.md) — error handling, the fail-loud rule, the
  `Env*` configuration pattern, the OKF fiche requirement.
- [Change ledger](../../planning/changes/change-2-agent-cli-adapter-registry/index.md) — the
  eight decisions behind every line above, each grounded in the real CLI protocols audited.
- `docs/analyse-prime-agent.md` and `docs/analyse-deepseek-harness.md` (repository
  `SERENIS_PROJET/docs/`) — the source analyses that led to auditing `internal/agentrunner` in
  the first place; neither harness's own subprocess/plugin protocol was reused here, both were
  found to not map onto a headless Go graph engine.

## Notes

**The real risk is under-verification of the wire shapes, not the registry design.** The
registry itself (one Go type per adapter behind an unchanged interface) is low-risk — it is the
same seam `Stub` already proves works. What is genuinely unverified going in: the exact
`stream-json` message envelope Claude Code emits (confirmed the flags exist via `claude --help`;
the precise message schema was not captured and tested against), and OpenCode's synchronous
response shape was read from one documentation page fetch, not from a running server. Issue
sizing for `create-issues` should budget real time for capturing actual transcripts from both
CLIs before writing the translation logic against them — a translation written against assumed
shapes is the same mistake this epic exists to fix in the current placeholder.

**`kern-exec`'s wrapper-script pattern composes without any `kern-orch` code change**, and
that property must survive this epic: `KERN_AGENT_CLI` pointed at a wrapper script that in turn
invokes `kern-exec run ... -- <real CLI>` already works today for confinement; this epic only
adds `KERN_AGENT_KIND` beside it, naming which protocol the wrapped binary speaks.

**OpenCode's server lifecycle interacts with `kern-exec`'s network confinement in a way not yet
audited**: `kern-exec run --allow-connect <addr>` grants a specific destination from inside the
sandboxed network namespace — but the OpenCode *server* process would need to be the one confined
by `kern-exec` (if `kern-orch` spawns it directly) or reached as an already-running, separately
managed process outside kern-exec's sandbox (if `kern-orch` connects to a pre-started server).
Decision 02 assumed `kern-orch`'s own adapter spawns and owns the server process; whether that
spawn itself should run under `kern-exec` confinement, and if so how `--allow-connect` composes
with a server the same command starts, was not audited here and should be resolved early in
implementation, not discovered late.
