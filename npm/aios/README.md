# @mooncodemaster/aios

Native npm distribution for [AIOS](https://github.com/MoonCodeMaster/AIOS) — a
dual-AI project orchestrator that drives **Claude CLI** and **Codex CLI** as
a coder↔reviewer pair with per-task `git worktree` isolation and a full
audit trail on disk.

## Install

```bash
npm install -g @mooncodemaster/aios
```

Then run:

```bash
aios init
aios new "Add a /health endpoint with a unit test"
aios run
```

## How this package works

This is a thin launcher. The native `aios` binary is delivered via one of
five platform-specific sibling packages, selected automatically at install
time by npm's `os` and `cpu` fields:

- `@mooncodemaster/aios-darwin-arm64`
- `@mooncodemaster/aios-darwin-x64`
- `@mooncodemaster/aios-linux-arm64`
- `@mooncodemaster/aios-linux-x64`
- `@mooncodemaster/aios-win32-x64`

No postinstall scripts. No network download during install. The native
binary is present on disk the moment `npm install` completes.

## Prerequisites

AIOS orchestrates two other CLIs. You need both authenticated:

```bash
npm install -g @anthropic-ai/claude-code    # claude
npm install -g @openai/codex                # codex
```

## Documentation

See the main repository: <https://github.com/MoonCodeMaster/AIOS>.

## License

[MIT](./LICENSE).
