# Running the AIOS E2E test suite

The e2e tests under `test/e2e/` drive real Claude CLI and Codex CLI end-to-end
through AIOS. They are gated behind `AIOS_E2E=1` and the `e2e` build tag so
they do not run as part of `go test ./...`.

## Prerequisites

### Binaries

Three things must be on PATH or otherwise reachable:

1. `claude` — from the Claude CLI npm package
2. `codex` — from the Codex CLI npm package
3. `aios` — the AIOS binary (pointed to via `AIOS_BIN`; see below)

```bash
npm install -g @anthropic-ai/claude-code    # provides `claude`
npm install -g @openai/codex                # provides `codex`
go build -o bin/aios ./cmd/aios             # build AIOS
claude --version
codex --version
```

### Authentication

Each CLI owns its own auth. AIOS never reads or writes provider API keys —
auth is inherited via environment.

**Claude CLI** — interactive login (preferred) or environment:
```bash
# Interactive (recommended):
claude login
# OR environment variable:
export ANTHROPIC_API_KEY="sk-ant-..."
```

**Codex CLI** — interactive login or environment:
```bash
codex login           # interactive
# OR
export OPENAI_API_KEY="sk-..."
```

Validate auth works before running the suite:
```bash
claude -p "say hi" --output-format json
codex exec --json "say hi"
```

Claude should return a single JSON object; Codex returns NDJSON or a single
JSON object depending on the version. If either fails with a 401 or prompts for
re-auth, fix that first.

## Environment variables

| Variable | Purpose |
|---|---|
| `AIOS_E2E` | Set to `1` to opt into running e2e tests. Without it, the tests call `t.Skip`. |
| `AIOS_BIN` | Required. Absolute path to the built `aios` binary. Tests call `t.Fatal` if this is unset. |
| `ANTHROPIC_API_KEY` | Optional; used by Claude CLI if no login session is present. |
| `OPENAI_API_KEY` | Optional; used by Codex CLI if no login session is present. |

The AIOS test binary does NOT read any provider keys directly — it shells out to
the child CLIs, which pick up these env vars themselves.

## Running the suite

Build first, then export `AIOS_BIN`:

```bash
go build -o bin/aios ./cmd/aios
export AIOS_BIN=$(pwd)/bin/aios
```

The Makefile target (does not set `AIOS_BIN` or a timeout):

```bash
make e2e
```

Canonical command with recommended flags:

```bash
AIOS_E2E=1 AIOS_BIN=$(pwd)/bin/aios go test -tags=e2e ./test/e2e/... -v -timeout 30m
```

Run a single scenario:

```bash
AIOS_E2E=1 AIOS_BIN=$(pwd)/bin/aios go test -tags=e2e ./test/e2e/ -run TestE2E_Greenfield -v -timeout 10m
AIOS_E2E=1 AIOS_BIN=$(pwd)/bin/aios go test -tags=e2e ./test/e2e/ -run TestE2E_Bugfix    -v -timeout 10m
AIOS_E2E=1 AIOS_BIN=$(pwd)/bin/aios go test -tags=e2e ./test/e2e/ -run TestE2E_Refusal   -v -timeout 10m
```

Each test creates a temporary repo via `t.TempDir()`; no persistent state is
left behind.

## What each scenario covers

- **Greenfield** (`TestE2E_Greenfield`) — fresh repo, spec: "Build a CLI that
  reverses its argv, with unit tests". Succeeds when `aios run` exits zero and
  `aios/staging` has more than one commit.
- **Bugfix** (`TestE2E_Bugfix`) — repo seeded with a failing `TestAdd` in
  `add_test.go`, spec: "Make the failing test in add_test.go pass by fixing
  add.go. Do not change add_test.go." Succeeds when `go test ./...` passes on
  `aios/staging`.
- **Refusal** (`TestE2E_Refusal`) — impossible spec: "Implement SHA-256 in
  exactly 3 lines of Go with no external libs and all tests green". Succeeds
  when `aios run` returns a non-zero exit code, meaning AIOS reached `blocked`
  cleanly rather than looping.

## Troubleshooting

### `claude: command not found` or `codex: command not found`

The CLIs are not on PATH. Re-run `npm install -g ...` and check
`which claude` / `which codex`. On macOS with a Homebrew-managed Node, you may
need to add the npm global bin directory to PATH:

```bash
echo 'export PATH="$(npm prefix -g)/bin:$PATH"' >> ~/.zshrc && source ~/.zshrc
```

### `AIOS_BIN must point to built aios binary` (test fatal)

`AIOS_BIN` is unset or empty. Export it before running:

```bash
export AIOS_BIN=$(pwd)/bin/aios
```

### `claude exec: exit status 1 (stderr: not authenticated)` or similar

Auth expired. Re-run `claude login` or set `ANTHROPIC_API_KEY`. The AIOS engine
surfaces the CLI's own stderr verbatim when the binary returns non-zero, so the
root cause is almost always visible in the error string.

Same pattern for Codex: `codex exec: exit status 1 (stderr: ...)` — re-run
`codex login` or set `OPENAI_API_KEY`.

### `claude output parse: ...`

Claude CLI returned something other than the expected single JSON object — likely
a rate-limit error envelope or a changed output format. Re-run; if persistent,
upgrade: `npm install -g @anthropic-ai/claude-code@latest`.

### `codex output parse: ...` or `codex ndjson parse: ...`

`parseCodexOutput` auto-detects NDJSON vs single-object JSON. If this fires, the
Codex CLI emitted something neither form recognizes — likely a rate-limit
envelope or a newer CLI version with a changed event schema. Upgrade the CLI and
re-run; if it persists, file an issue with the raw output.

### Rate limit / throttling (429)

Both CLIs respect provider rate limits. If you see 429s mid-run, re-run the
single failing scenario after a short wait rather than the full suite.

### Test times out

The recommended timeout is 30m for the full suite, 10m per scenario. If a
scenario routinely times out, check `cfg.Budget.MaxRoundsPerTask` and
`cfg.Budget.MaxTokensPerTask` in `.aios/config.toml` — overly generous budgets
let the engines iterate longer than necessary.

### Flaky convergence

Real LLM calls are nondeterministic. A scenario that passes 4 out of 5 runs is
normal. The suite is not designed to be CI-blocking; the nightly workflow
reports results but does not gate merges.

## Running under CI

The nightly workflow at `.github/workflows/e2e.yml` runs on a schedule
(05:00 UTC) and on `workflow_dispatch`. It:

- Builds `bin/aios` from source
- Installs both CLIs via npm
- Sets `AIOS_BIN=$(pwd)/bin/aios` and `AIOS_E2E=1`
- Expects `ANTHROPIC_API_KEY` and `OPENAI_API_KEY` as repository secrets

It does not run on forks (`github.repository_owner == 'Solaxis'` guard).
