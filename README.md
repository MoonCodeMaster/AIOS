# AIOS

> Dual-AI project orchestrator. One AI writes. Another reviews. You keep your Friday back.

AIOS drives **Claude CLI** and **Codex CLI** as a coder↔reviewer pair over a spec-driven task queue.
Approved work lands on `aios/staging`. You merge to `main` when you're ready.

## Install (v0)

Prereqs: a working `git`, plus both CLI engines authenticated:

```bash
npm install -g @anthropic-ai/claude-code    # Claude CLI
npm install -g @openai/codex                # Codex CLI
```

Install AIOS:

```bash
brew install Solaxis/aios/aios            # (after first release)
# or
go install github.com/Solaxis/aios/cmd/aios@latest
```

## Quick start

```bash
cd your-repo
aios init                             # interactive bootstrap; writes .aios/config.toml
aios new "Build a CLI that reverses argv with unit tests"
# review the proposed spec + task list; confirm with `y`
aios run                              # AIOS iterates until aios/staging is green
git log aios/staging                  # audit the coder↔reviewer history
git merge aios/staging                # you're the last human in the loop
```

## Commands

| Command | Purpose |
|---|---|
| `aios init` | Bootstrap the repo; autodetect verify commands. |
| `aios new "<idea>"` | Brainstorm → spec → task decomposition. |
| `aios run` | Iterate pending tasks; coder↔reviewer loop; auto-merge to `aios/staging`. |
| `aios status` | Print current task list with status. |
| `aios resume <id>` | Unblock a blocked task with a note. |

### Parallel tasks (v0.1+)

By default, `aios run` uses 4 worker goroutines that pull from the dep DAG.
Override with `--max-parallel N` or `[parallel] max_parallel_tasks` in
`.aios/config.toml`. A run-level token cap (`--max-tokens-run`,
default 1,000,000) prevents runaway spend across all parallel tasks.

### MCP servers (v0.1+)

Configure MCP servers in `.aios/config.toml`:

````toml
[mcp.servers.github]
binary = "github-mcp-server"
args = ["stdio"]
env = { GITHUB_TOKEN = "${env:GITHUB_TOKEN}" }
allowed_tools = ["search_code", "get_pr"]
````

Tasks opt in per-task with `mcp_allow:` in their frontmatter. Default is
deny-all — a task with no `mcp_allow` cannot use any MCP server.

````yaml
---
id: 003-add-login
kind: feature
mcp_allow: [github]
acceptance:
  - works
---
````

Every MCP tool call is recorded in `.aios/runs/<run-id>/task-<id>/round-N/mcp-calls.json`.

See [`docs/e2e-setup.md`](docs/e2e-setup.md) for how to run the optional e2e suite against real Claude + Codex CLIs.

## License

MIT.
