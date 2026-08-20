---
type: Decision
title: "Scope boundary: what this change explicitly does not do"
description: "Two adapters, a registry, and a lifecycle are already substantial — what stays out?"
tags: [decision, change]
timestamp: 2026-08-20T09:45:00Z
phase: change
decision: 07
slug: scope-boundary
status: decided
verdict: "In scope: registry, lifecycle, session concurrency, KERN_AGENT_KIND, private wire translation, kern-exec wrapper docs. Out: plugin SDK, deep flag mapping, async OpenCode, per-node adapter mixing within one graph"
decided_via: triage
depends_on: ['subprocess-lifecycle', 'wire-translation']
change: 2
change_slug: agent-cli-adapter-registry
---

# Question
Without an explicit boundary this change risks absorbing a general third-party plugin SDK, a
full mapping of every Claude Code and OpenCode feature (MCP config, custom agents, permission
modes — dozens of flags surfaced by `claude --help` alone), and the async OpenCode path
(decision 06).

# Options
- **In scope**: the adapter registry (decision 01), the `Start`/`Close` lifecycle (02),
  session-scoped concurrency for OpenCode (03), `KERN_AGENT_KIND` selection (04), each adapter's
  private wire translation to `AgentResult`/`TokenSink`/`OnActivity` (05) for the synchronous
  path only (06), and updating the existing `wrap-agent-cli*.sh` examples in `kern-exec` to show
  both adapters wired through `kern-exec`'s confinement.
- **Out of scope**: a general SDK or documented interface for a third party to write their own
  adapter outside this repo (a plugin *system* is a later, separate change once two real
  adapters exist to generalize from); any Claude Code or OpenCode flag beyond what each adapter's
  translation needs (model selection, permission mode, MCP config are real and useful but not
  required to prove the registry works); the async OpenCode completion path (05); per-node
  adapter selection within one graph (today's one-runner-per-run model stays, per decision 01's
  audit finding) — a real, useful extension, but a second change once the registry exists and is
  proven with one adapter per run.

# Recommendation
Accept the split as stated. Two working adapters (Claude Code, OpenCode) sharing one registry
is itself the proof the abstraction is right; generalizing to a public plugin SDK before two
concrete cases exist would be designing the interface from imagination rather than from the two
real shapes this audit already found are genuinely different (stdio vs HTTP).

# Verdict

In scope: registry, lifecycle, session concurrency, KERN_AGENT_KIND, private wire translation, kern-exec wrapper docs. Out: plugin SDK, deep flag mapping, async OpenCode, per-node adapter mixing within one graph.

Accept the split as stated. Two working adapters (Claude Code, OpenCode) sharing one registry
is itself the proof the abstraction is right; generalizing to a public plugin SDK before two
concrete cases exist would be designing the interface from imagination rather than from the two
real shapes this audit already found are genuinely different (stdio vs HTTP).
