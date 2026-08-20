---
type: Decision
title: "OpenCode's async completion path is unaudited"
description: "docs research found POST /session/:id/prompt_async (202/204, no wait) alongside the synchronous /message endpoint, but not how a caller observes async completion."
tags: [decision, change]
timestamp: 2026-08-20T09:45:00Z
phase: change
decision: 06
slug: opencode-async-completion
status: decided
verdict: "OpenCode's async completion path (prompt_async) is explicitly out of scope, unaudited, not required by Run()'s blocking contract"
decided_via: triage
depends_on: ['wire-translation']
change: 2
change_slug: agent-cli-adapter-registry
---

# Question
The audit for this change fetched OpenCode's server docs page and confirmed the synchronous
`POST /session/:id/message` shape (`{ info: Message, parts: Part[] }`) and the existence of
`POST /session/:id/prompt_async` (fire-and-forget, `204`), but did not find or read how a caller
observes an async prompt's completion — polling, SSE, webhook, or something else. Building the
OpenCode adapter against the synchronous endpoint alone is safe and sufficient for `AgentRunner`'s
blocking `Run()` contract; the async endpoint is not required by anything in this change.

# Options
- **Scope the OpenCode adapter to the synchronous `/message` endpoint only; treat the async
  path as explicitly out of scope for this change** (decision 07), to be audited only if a later
  need for non-blocking agent calls arises — `AgentRunner.Run()` is blocking by contract today
  and nothing here proposes changing that.
- **Audit the async completion mechanism now**, in case OpenCode's synchronous endpoint has
  timeout or streaming limitations the audit has not yet surfaced. Costs a research cycle this
  change does not need to spend, against a contract (`Run()` blocks until done) that the
  synchronous endpoint already satisfies on its face.

# Recommendation
The first. Nothing in `graph.AgentRunner`'s current contract needs non-blocking calls, and
speculatively auditing a path this change will not use is scope creep the size guard exists to
catch. Recorded as a named gap rather than silently ignored, so a future change knows exactly
what was not checked and why.

# Verdict

OpenCode's async completion path (prompt_async) is explicitly out of scope, unaudited, not required by Run()'s blocking contract.

The first. Nothing in `graph.AgentRunner`'s current contract needs non-blocking calls, and
speculatively auditing a path this change will not use is scope creep the size guard exists to
catch. Recorded as a named gap rather than silently ignored, so a future change knows exactly
what was not checked and why.
