# Change 2 — Agent CLI adapter registry (Claude Code, OpenCode)

* [Approach: registry of per-CLI adapters, port unchanged](01-approach.md) - decided
* [Subprocess lifecycle: spawn-per-call versus spawn-once-serve-many](02-subprocess-lifecycle.md) - decided
* [Concurrent calls into one adapter instance, inside a fan-out level](03-concurrent-calls-one-adapter.md) - decided
* [Adapter selection: how a run picks its CLI, and what replaces KERN_AGENT_CLI](04-adapter-selection.md) - decided
* [Wire translation: mapping each CLI's real message shapes onto AgentResult/token streaming](05-wire-translation.md) - decided
* [OpenCode's async completion path is unaudited](06-opencode-async-completion.md) - decided
* [Scope boundary: what this change explicitly does not do](07-scope-boundary.md) - decided
* [Acceptance criteria](08-acceptance-criteria.md) - decided
