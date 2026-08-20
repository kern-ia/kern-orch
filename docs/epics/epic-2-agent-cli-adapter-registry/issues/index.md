# Issues — Epic 2: Agent CLI adapter registry: Claude Code and OpenCode

* [Capture real Claude Code and OpenCode transcripts as test fixtures](./01-capture-real-cli-transcripts.md) - S, done (#35, PR #42)
* [Add the adapter registry and KERN_AGENT_KIND selection](./02-adapter-registry-and-kind-selection.md) - M, done (#36, PR #44)
* [Add the Start/Close adapter lifecycle, wired around a run](./03-adapter-lifecycle-port.md) - S, done (#37, PR #43)
* [Implement the Claude Code adapter](./04-claude-code-adapter.md) - M, open (#38)
* [Implement the OpenCode adapter: spawn, session, and wire translation](./05-opencode-adapter.md) - M, in-progress (#39)
* [Prove OpenCode fan-out calls stay concurrent, one session per call](./06-opencode-concurrent-sessions.md) - S, open (#40)

The wrapper-script documentation follow-up lives outside this epic's implement-epic scope: [kern-exec#1](https://github.com/kern-ia/kern-exec/issues/1).
