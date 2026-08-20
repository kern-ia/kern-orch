# Real CLI transcript fixtures

These fixtures are **real, verbatim captures** from running CLI subprocesses — not
hand-written or reconstructed from documentation. Every acceptance criterion in
issue 01 of epic 2 exists specifically to stop `internal/agentrunner`'s placeholder
protocol (`protocol.go`) from being reinvented once more against an assumed shape.

Redaction touched only: the capturing machine's real absolute home-directory paths
(`/Users/yoann/...` → `/REDACTED/...`) and a temp socket path. No message content,
field name, or JSON structure was altered, reordered, or pretty-printed beyond what
each CLI already emitted on its own stdout/response body.

## `claude-code-stream.jsonl`

Captured 2026-08-20 with `claude --version` → `2.1.237 (Claude Code)`.

Two separate invocations of

```
claude -p --input-format=stream-json --output-format=stream-json --verbose [--allowedTools "Bash(echo:*)"]
```

were run back to back and their raw JSON-lines stdout concatenated verbatim (lines
1-4: first capture, lines 5-10: second capture). No `ANTHROPIC_API_KEY` is set in
this environment; the subprocess transparently inherited the parent Claude Code
session's own OAuth/subscription credentials (`"apiKeySource":"none"` in the
captured `system init` event confirms no API key was used) — this was not assumed in
advance, it was the first thing verified by piping a minimal prompt in and checking
the process actually completed rather than erroring.

- **Capture 1** (lines 1-4): a plain text turn. Input piped on stdin:
  `{"type":"user","message":{"role":"user","content":[{"type":"text","text":"Reply with exactly the single word: PONG"}]}}`.
  Emits `rate_limit_event` → `system`/`init` → `assistant` (plain `text` content
  block) → `result`.
- **Capture 2** (lines 5-10): a tool-call round trip, the second shape a translator
  must distinguish. Input piped on stdin:
  `{"type":"user","message":{"role":"user","content":[{"type":"text","text":"Run the shell command: echo hello-from-fixture-capture"}]}}`,
  run with `--allowedTools "Bash(echo:*)"` so the single, narrowly-scoped `echo`
  command could execute non-interactively without a permission prompt. Emits
  `rate_limit_event` → `system`/`init` → `assistant` (`tool_use` content block,
  `name":"Bash"`) → `user` (`tool_result` content block carrying real stdout) →
  `assistant` (final `text` content block) → `result`.

Reproduce with:

```
echo '{"type":"user","message":{"role":"user","content":[{"type":"text","text":"<prompt>"}]}}' \
  | claude -p --input-format=stream-json --output-format=stream-json --verbose
```

## `opencode-message.json`

Captured 2026-08-20 with `opencode --version` → `1.18.19`.

`opencode` was not installed on this machine. It was installed cleanly and
reversibly via `npm install -g opencode-ai@latest` — a user-local install (the
node toolchain here is `mise`-managed, so `npm config get prefix` resolves inside
`~/.local/share/mise/...`, never a system path; no `sudo` was used).

The server was started with `opencode serve --port 4890 --hostname 127.0.0.1
--print-logs`. No provider credentials are configured on this machine
(`opencode auth list` reported 0 credentials, and no `*_API_KEY` env var is set),
but `opencode` ships a real, network-backed, zero-cost default provider —
`"providerID":"opencode"` / `"OpenCode Zen"` — reachable with no login (its
`options.apiKey` is `"public"`), which the server picked as its default
`modelID":"big-pickle"`. This is a genuine hosted completion over the network, not
a local mock: the response fields (`cost`, `tokens`, per-part timestamps) come back
exactly as the real server produced them.

A session was created with `POST /session`, then a message that asks for a trivial
shell command was sent with `POST /session/:id/message`
(`{"parts":[{"type":"text","text":"Run the shell command: echo hello-from-opencode-fixture-capture"}]}`).
The server log confirms a real tool call ran:
`evaluated permission=bash pattern="echo hello-from-opencode-fixture-capture" action.permission=* action.action=allow action.pattern=*`.

The direct `POST .../message` response body only contains the session's *last*
message (the final text reply), so it alone would miss the tool-call shape. To
capture both shapes a translator needs to distinguish — a plain text turn and a
tool-call round trip — in the one fixture file, the full session transcript was
retrieved with a follow-up `GET /session/:id/message` and that response body (an
array of the user message, the tool-call assistant message, and the final text
assistant message) is what is committed here, verbatim except for the one redacted
path. This is a documented departure from the issue's literal wording ("capture the
raw JSON response body verbatim" of the `POST` call) made to satisfy the issue's
actual purpose (both message shapes present for issues 04/05) without inventing
anything: every byte in the file came back from a real HTTP response of a real
running server.

Reproduce with:

```
npm install -g opencode-ai@latest
opencode serve --port 4890 --hostname 127.0.0.1 --print-logs &
SID=$(curl -s -X POST http://127.0.0.1:4890/session -d '{}' | jq -r .id)
curl -s -X POST http://127.0.0.1:4890/session/$SID/message \
  -H "Content-Type: application/json" \
  -d '{"parts":[{"type":"text","text":"<prompt that triggers a tool call>"}]}'
curl -s http://127.0.0.1:4890/session/$SID/message   # full transcript, both shapes
```

The server was shut down cleanly after capture (`pkill -f "opencode serve"`).

## What is NOT captured

- OpenCode's async completion path (`prompt_async`) — out of scope for the whole
  epic (Epic 2, decision 06).
- Any provider other than each CLI's own default (Claude Code's inherited
  subscription session; OpenCode's public zero-cost `opencode/big-pickle`).
- Streaming/partial-token deltas — both captures show only the complete,
  non-streaming JSON objects each CLI/server returned to a blocking client; no
  `--output-format=stream-json` incremental delta framing beyond full message
  objects was exercised, since only complete objects were pulled from stdout/HTTP.
