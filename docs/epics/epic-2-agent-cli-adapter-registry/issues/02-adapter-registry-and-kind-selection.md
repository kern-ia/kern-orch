---
type: Issue
title: "Add the adapter registry and KERN_AGENT_KIND selection"
description: "A registry type in internal/agentrunner selecting between adapters by KERN_AGENT_KIND, with config.FromEnv() failing loud on a missing or invalid value."
tags: [epic-2]
timestamp: 2026-08-20T17:05:00Z
epic: 2
issue: 02
slug: adapter-registry-and-kind-selection
size: M
status: done
gh_issue: 36
gh_pr: 44
resource: https://github.com/kern-ia/kern-orch/issues/36
depends_on: []
---

# Add the adapter registry and KERN_AGENT_KIND selection

## Summary

The foundational piece every other issue in this epic builds on. Decision 01 of the change
ledger: a registry of adapters inside `internal/agentrunner`, one Go type per target CLI, all
satisfying `graph.AgentRunner` unchanged -- `graph` and every other package stay untouched.
Decision 04: a new `KERN_AGENT_KIND` env var selects the adapter; `KERN_AGENT_CLI` keeps its
existing meaning as that adapter's binary path; a missing or invalid `KERN_AGENT_KIND` with
`KERN_AGENT_CLI` set fails loud at config load, before any run starts -- never a silent
fallback.

This issue does NOT implement either adapter's real logic (issues 04, 05) -- it defines the
registry shape and the config validation, with placeholder/stub adapter types wired through it
so the registry itself is provably correct before real CLIs are involved.

## Scope

- `internal/config`: add `EnvAgentKind = "KERN_AGENT_KIND"` to the `Env*` block, documented in
  the same style as the existing entries (why it is its own variable -- see
  `EnvRuntimeEquivalenceCheck`'s comment for the pattern to match). Add `Config.AgentKind
  string`. `FromEnv()` validates: if `KERN_AGENT_CLI` is set and `KERN_AGENT_KIND` is empty or
  not one of the recognized values (`"claude-code"`, `"opencode"`), return an error naming the
  offending value -- mirror `boolEnvOr`'s error-returning shape, not a new pattern.
- `internal/agentrunner`: a small registry function, e.g. `New(cfg config.Config, activity
  *activityRelay) (graph.AgentRunner, error)` (or equivalent), replacing `newRunner`'s current
  `if/else` in `internal/cmd/runtime.go`. Neither `KERN_AGENT_CLI` nor `KERN_AGENT_KIND` set
  still returns `&Stub{}`, unchanged behaviour.
- Update `internal/cmd/runtime.go`'s `newRunner` to call the new registry function and
  propagate its error (today `newRunner` returns only a `graph.AgentRunner`, no error -- this
  issue's signature change is the first ripple; `serve.go`'s two call sites, lines ~66 and
  ~508, both need to handle the new error return).

## Out of scope

- The real Claude Code or OpenCode adapter implementations -- issues 04 and 05. This issue may
  register placeholder/not-yet-implemented adapter constructors that return a clear "not yet
  implemented" error for `claude-code`/`opencode`, to prove the registry dispatches correctly
  without depending on 04/05 landing first.
- The `Start`/`Close` lifecycle extension to the port -- issue 03.

## Acceptance criteria

- [ ] `KERN_AGENT_CLI` set, `KERN_AGENT_KIND` unset -> `config.FromEnv()` returns an error
      naming both variables.
- [ ] `KERN_AGENT_CLI` set, `KERN_AGENT_KIND` set to an unrecognized value (e.g. `"bogus"`) ->
      `config.FromEnv()` returns an error naming the invalid value.
- [ ] `KERN_AGENT_CLI` set, `KERN_AGENT_KIND` set to `"claude-code"` or `"opencode"` ->
      `config.FromEnv()` succeeds.
- [ ] Neither variable set -> `config.FromEnv()` succeeds, `Config.AgentKind` is empty, and the
      registry returns `&Stub{}` -- behaviourally identical to today.
- [ ] `internal/cmd`'s existing tests exercising `newRunner`/`config.FromEnv()` still pass,
      updated for the new error return where required.
- [ ] `go build ./...`, `go vet ./...` and `go test ./...` are green.
- [ ] An OKF fiche is added under `docs/index/` for this branch, per CONVENTIONS.md.

## Relevant files / areas

- `internal/config/config.go` -- `Env*` block, `Config` struct, `FromEnv()`, `boolEnvOr`.
- `internal/cmd/runtime.go` -- `newRunner`.
- `internal/cmd/serve.go` -- lines ~66 and ~508, the two `newRunner` call sites.
- `internal/agentrunner/stub.go` -- the no-variables-set fallback to preserve unchanged.

## Dependencies

None. Blocks issues 03, 04, 05.

## PR size note

Target ~500 changed lines; if this grows past ~1000, split it before opening the PR.
