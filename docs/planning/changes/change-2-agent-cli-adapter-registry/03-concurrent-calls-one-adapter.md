---
type: Decision
title: "Concurrent calls into one adapter instance, inside a fan-out level"
description: "graph.Engine runs every node of a frontier in its own goroutine. Can two agent nodes safely call the same adapter instance at once?"
tags: [decision, change]
timestamp: 2026-08-20T09:45:00Z
phase: change
decision: 03
slug: concurrent-calls-one-adapter
status: decided
verdict: "The OpenCode adapter opens one session per call; concurrent fan-out calls become concurrent sessions, never serialized"
decided_via: triage
depends_on: ['subprocess-lifecycle']
change: 2
change_slug: agent-cli-adapter-registry
---

# Question
`Engine.runLevel` spawns one goroutine per node in the current frontier and lets them run
concurrently; a fan-out of two agent nodes calls `AgentRunner.Run` from two goroutines at the
same time, against the **same** runner instance (`newRunner` builds exactly one per run).
`Subprocess.Run()` already handles this safely today — each call spawns its own child process,
so there is no shared mutable state between concurrent calls. An adapter that owns one persistent
child (decision 02, OpenCode) changes that: multiple goroutines would issue HTTP requests against
the same running server concurrently.

# Options
- **The OpenCode adapter opens one session per call and treats concurrent calls as concurrent
  sessions against the one running server** (`POST /session` per call, scoped to that call's
  `NodeID`) — the server is built to serve one process, many sessions; this is squarely within
  its own design, not a workaround.
- **Serialize all calls into one adapter instance behind a mutex.** Safe by construction but
  turns a fan-out of agent nodes into a sequential queue for any node backed by that adapter,
  silently changing the performance characteristics `graph.Engine`'s level-synchronous model
  promises elsewhere.

# Recommendation
The first. It costs nothing extra to implement — OpenCode's API is already session-scoped —
and the second option would quietly defeat the one guarantee `Engine.runLevel`'s design exists to
give: that a fan-out actually runs in parallel.

# Verdict

The OpenCode adapter opens one session per call; concurrent fan-out calls become concurrent sessions, never serialized.

The first. It costs nothing extra to implement — OpenCode's API is already session-scoped —
and the second option would quietly defeat the one guarantee `Engine.runLevel`'s design exists to
give: that a fan-out actually runs in parallel.
