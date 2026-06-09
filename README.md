# Forge

An autonomous software engineering agent for the terminal. You describe a task in plain English; Forge classifies it, reads your repository, writes a patch, and asks for confirmation before touching anything.

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
│  1 file(s) will be modified                      │
└──────────────────────────────────────────────────┘
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
Agent Loop         — reads the repo with read_file, list_files, search_code, git_*
    ↓
Patch System       — generates a unified diff, validates it, and applies atomically
    ↓
Confirmer          — shows a diff preview and prompts before any file is written
```

All model calls are routed through [Costguard](https://github.com/your-org/costguard), an OpenAI-compatible proxy that handles routing, budget enforcement, and cost tracking. No provider is contacted directly.

## Requirements

- Go 1.22+
- A running Costguard instance
- A model served via Costguard (Ollama, Anthropic, OpenAI, etc.)

## Installation

```bash
git clone https://github.com/marcoantonios1/Forge
cd Forge
make build
# binary is at ./bin/forge
```

## Configuration

Copy `.env.example` to `.env` and fill in your values:

```bash
cp .env.example .env
```

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
| `COMPILER_MODEL` | `claude-haiku-4-5-20251001` | Model used for task classification and agent loop |
| `FORGE_DEBUG` | `false` | Verbose logging |

Environment variables take precedence over `.env`.

## Usage

```bash
./bin/forge [--debug] [--yes]
```

| Flag | Description |
|---|---|
| `--debug` | Emit structured JSON events instead of formatted output |
| `--yes` | Approve all patches without prompting (autonomous mode) |

### forge.md

Drop a `forge.md` in your project root to give Forge persistent project context — build commands, style rules, frozen modules, banned patterns. It is read at startup and prepended to every agent system prompt.

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
| `autonomous` | Agent runs and applies patches without confirmation |
| `supervised` | Agent runs; you confirm each patch |
| `safe` | Agent runs; confirmation required; used for deletions, secrets, infra changes |

`--yes` overrides all policies and bypasses confirmation.

## Architecture

```
cmd/forge/
    main.go               entry point, REPL, flag parsing

internal/
    config/               env + .env loader
    session/              crypto/rand session IDs
    projectconfig/        forge.md loader
    compiler/             natural language → typed Task struct
    costguard/            OpenAI-compatible HTTP client with retry/backoff
    agent/                control loop, tool registry, system prompt
    tools/                read_file, list_files, search_code, git_status, git_diff, git_log
    patch/                unified diff parser, validator, atomic applier, undo history
    confirm/              AutoConfirmer, NullConfirmer, SafeConfirmer
    events/               event types and Emitter interface
    ui/                   terminal renderer, diff coloriser, TTY detection
```

## Development

```bash
make build    # compile to ./bin/forge
make run      # build and run
make clean    # remove ./bin
```

## License

MIT
