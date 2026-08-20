---
type: Decision
title: "Adapter selection: how a run picks its CLI, and what replaces KERN_AGENT_CLI"
description: "KERN_AGENT_CLI names one binary path with no arguments and no way to say which wire protocol that binary speaks. What replaces it?"
tags: [decision, change]
timestamp: 2026-08-20T09:45:00Z
phase: change
decision: 04
slug: adapter-selection
status: decided
verdict: "A new KERN_AGENT_KIND selects the adapter; KERN_AGENT_CLI keeps meaning that adapter's binary path; missing/invalid KIND fails loud at config load"
decided_via: triage
depends_on: ['approach']
change: 2
change_slug: agent-cli-adapter-registry
---

# Question
`config.EnvAgentCLI` (`KERN_AGENT_CLI`) is documented and used as a bare path:
`agentrunner.NewSubprocessFromEnv` reads it, and if unset falls back to `Stub`. It cannot express
"this is Claude Code" versus "this is OpenCode" — the two need different adapters entirely, not
just a different path. `internal/config`'s existing convention (`CONVENTIONS.md`: "deployment
varying choices are validated Config fields... a misconfiguration fails loud at load") is the
standard to extend, not bypass — `Config.RuntimeEquivalenceCheck` from epic 1 is the most recent
example of the pattern: an env var, documented inline for why it is its own variable, validated
at `FromEnv()` load time.

# Options
- **A new `KERN_AGENT_KIND` selecting the adapter (`claude-code` | `opencode`), plus
  `KERN_AGENT_CLI` reinterpreted as that adapter's binary path** (unchanged meaning for
  Claude Code — still a bare path to the `claude` binary; for OpenCode, the path to the
  `opencode` binary `serve` is run against). An unset `KERN_AGENT_KIND` with `KERN_AGENT_CLI`
  set fails loud rather than guessing which protocol the binary speaks — silently defaulting to
  the wrong adapter would make every request to the wrong protocol shape.
- **Auto-detect the adapter kind by probing the binary** (e.g. `<path> --version` output,
  or attempting the stdio handshake and falling back to HTTP on failure). Rejected: fragile
  against version-string changes in either tool, and it manufactures ambiguity `KERN_AGENT_KIND`
  removes for the cost of one more variable — CONVENTIONS.md's own rule against
  "misconfiguration failing silently" argues directly against inferring instead of asking.

# Recommendation
The first. It is the smallest change consistent with the existing `Env*` convention, it fails
loud on the exact ambiguity that would otherwise misroute every request, and each future adapter
adds one more accepted value to `KERN_AGENT_KIND` rather than a new detection heuristic.
OpenCode's adapter needs one more variable this decision does not fully resolve — its server
port/address — left to decision 06.

# Verdict

A new KERN_AGENT_KIND selects the adapter; KERN_AGENT_CLI keeps meaning that adapter's binary path; missing/invalid KIND fails loud at config load.

The first. It is the smallest change consistent with the existing `Env*` convention, it fails
loud on the exact ambiguity that would otherwise misroute every request, and each future adapter
adds one more accepted value to `KERN_AGENT_KIND` rather than a new detection heuristic.
OpenCode's adapter needs one more variable this decision does not fully resolve — its server
port/address — left to decision 06.
