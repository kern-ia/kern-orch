---
type: Decision
title: "Wire translation: mapping each CLI's real message shapes onto AgentResult/token streaming"
description: "Claude Code's stream-json messages and OpenCode's REST response bodies both need to become graph.AgentResult and the existing token-stream/activity hooks."
tags: [decision, change]
timestamp: 2026-08-20T09:45:00Z
phase: change
decision: 05
slug: wire-translation
status: decided
verdict: "Each adapter owns its full wire translation privately into AgentResult/TokenSink/OnActivity; no shared intermediate Event type forced across adapters"
decided_via: triage
depends_on: ['approach']
change: 2
change_slug: agent-cli-adapter-registry
---

# Question
`graph.AgentResult{Output map[string]any}` and `Subprocess`'s `TokenSink io.Writer` (raw
incremental text) are the two things every adapter must produce. Claude Code's
`--output-format=stream-json` streams typed JSON messages (assistant content deltas, tool-call
events, a final result) — not raw token text; extracting a text stream means picking specific
message fields out of a richer envelope, and merging its final result into `AgentResult.Output`
means deciding which of Claude Code's own SDK message fields become graph state keys. OpenCode's
`POST /session/:id/message` returns `{ info: Message, parts: Part[] }` — a wholly different
shape, with no incremental streaming primitive documented for the synchronous endpoint (the
`prompt_async` variant returns `204` immediately and requires a separate mechanism, not yet
audited, to observe completion).

# Options
- **Each adapter owns its full translation privately** — decodes its CLI's real message shapes
  internally and exposes only `graph.AgentResult` plus writes to the shared `TokenSink`
  contract; no shared intermediate "Event" type is imposed across adapters, since Claude Code's
  and OpenCode's native shapes have no meaningful common structure beyond "text happened" and
  "it finished." `OnActivity`'s existing two-state (generating/done) bracket is reused as-is —
  it is already CLI-agnostic.
- **Design one canonical intermediate `Event` type (token/result/error, today's shape) and make
  every adapter translate into it**, keeping `consume()`'s scanning logic shared. Rejected as the
  primary shape: today's `Event{Type, Text, Output, Message}` was itself invented before ever
  being reconciled against a real CLI (the same gap this whole change exists to close) — forcing
  two structurally different protocols through it risks repeating exactly that mistake, just one
  layer down.

# Recommendation
The first. `graph.AgentResult` and the `TokenSink`/`OnActivity` contracts are already the
correct, minimal common surface — adding a second shared intermediate type between the wire and
that surface is exactly the kind of premature unification decision 01 already rejected once at
the transport level. Two adapters is not enough evidence to design a shared wire format; a third
adapter that shows real structural overlap is the trigger to revisit this, not a guess now.

# Verdict

Each adapter owns its full wire translation privately into AgentResult/TokenSink/OnActivity; no shared intermediate Event type forced across adapters.

The first. `graph.AgentResult` and the `TokenSink`/`OnActivity` contracts are already the
correct, minimal common surface — adding a second shared intermediate type between the wire and
that surface is exactly the kind of premature unification decision 01 already rejected once at
the transport level. Two adapters is not enough evidence to design a shared wire format; a third
adapter that shows real structural overlap is the trigger to revisit this, not a guess now.
