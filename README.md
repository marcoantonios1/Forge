# Forge

An autonomous software engineering agent for the terminal. You describe a task in plain English; Forge classifies it, reads your repository, writes a patch (or runs build/test commands), and asks for confirmation before touching anything risky.

```
> add input validation to the login handler
compiled: feature/module [supervised]
  ⚙  Starting task  feature/module  [a3f2c1b0]
  →  git_status  (repo)
  ✓  git_status  on branch main, clean
  →  read_file  internal/auth/handler.go
  ✓  read_file  read 84 lines
  →  search_code  validateInput
  ✓  search_code  found 0 matches
┌─ Patch preview ────────────────────────────────┐
│  1 file(s) will be modified                    │
└────────────────────────────────────────────────┘
Apply this patch? [y]es / [n]o / [a]ll session  y
  ✔  Applied  internal/auth/handler.go  (1 hunks)
╔══════════════════════════════════════════╗
║  Task complete                           ║
╚══════════════════════════════════════════╝
   Added input validation to the login handler.
```

## How it works

```
Your input
    ↓
Task Compiler      — classifies intent, scope, and execution policy
    ↓
Clarification      — (supervised only) asks one question before starting, if ambiguous
    ↓
Agent Loop         — reads the repo and runs commands via tool calls; stuck detector
                      and a 100-iteration backstop guard against runaway loops
    ↓
Permission Gate    — prompts before git writes / run_command (unless pre-approved)
    ↓
Patch System       — generates a unified diff (including new files), validates it,
                      and applies atomically; undo is always available
    ↓
Confirmer          — shows a diff preview and prompts before any file is written
    ↓
Git Workflow       — branches off main if needed, commits, and pushes the result
```

All model calls are routed through [Costguard](https://github.com/marcoantonios1/costguard), an OpenAI-compatible proxy that handles routing, budget enforcement, and cost tracking. No provider is contacted directly.

## Requirements

- Go 1.26+
- A running Costguard instance
- A model served via Costguard (Ollama, Anthropic, OpenAI, etc.)

## Installation

See the setup guide for your OS:

- [macOS](docs/setup-mac.md)
- [Linux](docs/setup-linux.md)
- [Windows](docs/setup-windows.md)

---

## Configuration

| Variable | Default | Description |
|---|---|---|
| `COSTGUARD_URL` | `http://localhost:8080` | Costguard proxy URL |
| `COSTGUARD_MODE` | `balanced` | Routing mode: `cheap`, `balanced`, `best`, `private` |
| `COSTGUARD_AGENT` | `forge` | Agent identity header sent to Costguard |
| `COSTGUARD_PROVIDER` | _(unset)_ | Backend provider: `local_ollama`, `anthropic`, `openai`, … |
| `COSTGUARD_TEAM` | _(unset)_ | Team label for cost attribution |
| `COSTGUARD_PROJECT` | _(unset)_ | Project label for cost attribution |
| `COSTGUARD_TIMEOUT` | `60s` | Per-request timeout |
| `COSTGUARD_MAX_RETRIES` | `3` | Retries on 429 / 502 / 503 |
| `FORGE_COMPILER_MODEL` | `claude-sonnet-4-6` | Model used for task classification (`COMPILER_MODEL` also accepted, for backwards compatibility) |
| `FORGE_AGENT_MODEL` | `claude-sonnet-4-6` | Model used for the agent loop |
| `FORGE_DEBUG` | `false` | Verbose logging |

Environment variables take precedence over `.env`.

## Usage

### Interactive REPL

```bash
./bin/forge [--debug] [--yes] [--allowed-tools=<categories>] [--allow-main-commit]
```

| Flag | Description |
|---|---|
| `--debug` | Emit structured JSON events instead of formatted output, plus per-iteration agent logging |
| `--yes` | Approve all patches and tool permissions without prompting (forces autonomous behaviour) |
| `--allowed-tools` | Comma-separated tool categories to pre-approve for the session: `read`, `git_read`, `git_write`, `run`, `patch`, or `all` |
| `--allow-main-commit` | Allow committing directly to `main`/`master` instead of branching off (unsafe) |

REPL-only commands:

| Command | Description |
|---|---|
| `undo` | Reverts the most recently applied patch set (deletes files it created, restores files it edited) |

Press **Ctrl+C** to cancel the task currently running and return to the prompt; press it again with no task running (or twice within one second) to exit. **Ctrl+D** exits cleanly at any time.

### Headless / scripted mode

```bash
./bin/forge --print [--output=text|json] [--debug] "<task description>"
```

Runs one task non-interactively (always autonomous — no prompts), prints a result summary to stdout, and exits with a status code: `0` success, `1` failure/rejected, `130` if cancelled (Ctrl+C). `--output=json` emits a structured `{status, summary, files, iterations}` object; human-readable events still go to stderr so stdout stays clean for piping.

### Project bootstrap

```bash
./bin/forge init
```

Detects your build/test commands and languages via filesystem heuristics (no LLM call) and writes a starter `forge.md` to the repo root. Prompts before overwriting an existing one.

### forge.md

Drop a `forge.md` in your project root to give Forge persistent project context — build commands, style rules, frozen modules, banned patterns. It is read at startup and prepended to every agent system prompt. `forge init` generates a starter one for you.

```markdown
# forge.md
build: make build
test: make test
style: always use tabs, never spaces
banned: fmt.Println in production code
notes: internal/auth is frozen — do not modify
```

## Execution policies

The task compiler assigns one of three policies based on the task description:

| Policy | Behaviour |
|---|---|
| `autonomous` | Agent runs and applies patches / runs commands without confirmation |
| `supervised` | Agent may ask one clarifying question before starting; you confirm each patch and gated tool call |
| `safe` | Same as supervised, but used for higher-risk tasks (deletions, secrets, infra changes) |

`--yes` overrides all policies and bypasses confirmation.

## Tool permissions

Beyond patch confirmation, individual tool calls are gated by category. In `supervised`/`safe` mode, the first call to a gated category prompts:

```
⚡ Tool: run_command  Category: run
Allow? [y]es / [n]o / [a]ll session for run
```

`y` approves once, `a` approves the category for the rest of the session, `n` denies it. Categories: `read` (read_file/list_files/search_code), `git_read` (status/diff/log), `git_write`, and `run` (run_command). Pre-approve any subset up front with `--allowed-tools=read,git_read` or `--allowed-tools=all`. `autonomous` mode and `--yes` skip all prompts. Forge's own pre-task git context gathering (status/diff/log/pull at task start) and the post-task git workflow always bypass the gate — only tool calls the agent itself initiates are gated.

## Tools available to the agent

The agent can call:

- `read_file`, `list_files`, `search_code` — repo exploration
- `git_status`, `git_diff`, `git_log` — read-only repo state
- `run_command` — runs a build/test/lint command in the repo root, streaming output live; rejects destructive prefixes (`rm`, `dd`, `chmod -R 777 /`, etc.) before execution
- Patch blocks (`FORGE_PATCH_BEGIN`/`END`) — unified diffs, including brand-new files (`--- /dev/null`)

`git_branch`, `git_checkout`, `git_stash`, `git_pull`, `git_commit`, and `git_push` are registered for Forge's own internal use (pre-task repo prep and the post-task git workflow) — they're not in the agent's system prompt, so the agent itself never calls them directly.

## Git workflow

After a task applies at least one patch, Forge proposes a branch (off `main`/`master`, unless `--allow-main-commit`), a commit message, and a push to `origin`. In interactive mode you're shown the proposal and can accept, edit, or skip; in autonomous/headless mode it happens automatically.

## Safety backstops

- **Stuck detector** — fails fast with `agent stuck in loop: <reason>` if the agent repeats the same tool call 3x, gives the same response twice in a row, or a tool returns the same result 3 times in a row.
- **Iteration backstop** — hard cap of 100 iterations as a last resort (`task exceeded maximum iterations (100)`); the stuck detector fires first in practice.
- **Destructive command guard** — `run_command` refuses known-destructive prefixes regardless of what the model attempts.

## Architecture

```
cmd/forge/
    main.go            entry point, REPL, flag parsing, init/headless subcommands,
                        Ctrl+C handling, git workflow proposal

internal/
    config/            env + .env loader
    session/           crypto/rand session IDs
    projectconfig/     forge.md loader
    forgeinit/         `forge init` filesystem heuristics (build/test/language detection)
    compiler/          natural language → typed Task struct
    costguard/         OpenAI-compatible HTTP client with retry/backoff
    agent/             control loop, tool registry, system prompt, clarification,
                       stuck detector
    tools/             read_file, list_files, search_code, git_status/diff/log,
                       run_command (agent-callable); git_branch/checkout/stash/pull/
                       commit/push (Forge-internal repo prep and git workflow)
    patch/             unified diff parser (incl. new files), validator, atomic
                       applier, undo history
    confirm/           AutoConfirmer, NullConfirmer, SafeConfirmer, PermissionGate
    events/            event types, Emitter interface, Multi fan-out
    ui/                terminal renderer, diff coloriser, TTY detection
```

## Development

```bash
make build    # compile to ./bin/forge
make run      # build and run
make clean    # remove ./bin
go test ./... # run unit tests
```

## License

Apache 2.0
