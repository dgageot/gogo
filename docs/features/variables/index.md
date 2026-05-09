---
title: Variables
---

# Variables

gogo supports variables that can be used in commands via `{{ "{{" }}.VAR}}` templates or `${VAR}` shell expansion.

## Global Variables

Define variables at the top level of your task file:

```yaml
vars:
  BINARY_NAME: myapp
  VERSION: 1.0.0

tasks:
  build:
    cmd: go build -ldflags "-X main.version={{ "{{" }}.VERSION}}" -o {{ "{{" }}.BINARY_NAME}} ./...
```

## Dynamic Variables

Variables can be computed from shell commands:

```yaml
vars:
  GIT_SHA:
    sh: git rev-parse --short HEAD
  DATE:
    sh: date -u +%Y-%m-%dT%H:%M:%SZ

tasks:
  build:
    cmd: go build -ldflags "-X main.sha={{ "{{" }}.GIT_SHA}} -X main.date={{ "{{" }}.DATE}}" ./...
```

## Task-Scoped Variables

Tasks can define their own variables that override global ones:

```yaml
vars:
  ENV: development

tasks:
  deploy:
    vars:
      ENV: production
    cmd: deploy --env {{ "{{" }}.ENV}}
```

## Variable References Inside Variables

A variable's value (or `sh:` command) can reference any other variable, including [built-ins](#built-in-variables). References are resolved transitively at run time and declaration order is irrelevant:

```yaml
vars:
  GIT_TAG: "{{ "{{" }}.GIT_TAG}}"          # built-in
  LDFLAGS: "-X main.Version={{ "{{" }}.GIT_TAG}} -X main.Commit={{ "{{" }}.GIT_COMMIT}}"

tasks:
  build:
    cmd: go build -ldflags '{{ "{{" }}.LDFLAGS}}' .
```

The `sh:` form is template-expanded too, so a shell command can be parameterised by other vars:

```yaml
vars:
  IMAGE: cagent-proxy
  LATEST: { sh: "ls -1 dist/{{ "{{" }}.IMAGE}}-*.tar | sort | tail -1" }
```

Cycles — `A: "{{ "{{" }}.B}}"`, `B: "{{ "{{" }}.A}}"` — short-circuit to the empty string rather than looping forever, mirroring how task `env:` cross-references behave.

## Built-in Variables

| Variable | Description |
|----------|-------------|
| `TASK_FILE_DIR` | The working directory for the task (defaults to the task file directory) |
| `CLI_ARGS` | Extra arguments passed after `--` |
| `GIT_COMMIT` | Full SHA at `HEAD` (empty outside a git repo) |
| `GIT_SHORT_COMMIT` | 7-char SHA at `HEAD` (empty outside a git repo) |
| `GIT_TAG` | Exact-match tag at `HEAD`, or empty if `HEAD` isn't tagged |
| `GIT_BRANCH` | Current branch name; `HEAD` when detached |
| `GIT_DIRTY` | `dirty` if the working tree has changes; empty when clean |

### `TASK_FILE_DIR`

`TASK_FILE_DIR` is the working directory the task runs in — the same value gogo passes as the shell's `cwd`. The rules are simple but the most common foot-gun in gogo, especially with `dir:` and includes, so it's worth a worked example. The full algorithm lives in `Runner.taskDir` (`taskfile/task_execution.go`) and `makeTaskDirAbsolute` (`taskfile/includes.go`).

Three cases, by ascending complexity:

```
project/
├── gogo.yaml
└── frontend/
    └── gogo.yaml
```

```yaml
# project/gogo.yaml
includes:
  - frontend

tasks:
  here:
    cmd: echo $TASK_FILE_DIR        # → /abs/path/to/project

  there:
    dir: tools
    cmd: echo $TASK_FILE_DIR        # → /abs/path/to/project/tools
```

```yaml
# project/frontend/gogo.yaml
tasks:
  build:
    cmd: echo $TASK_FILE_DIR        # → /abs/path/to/project/frontend

  build-src:
    dir: src
    cmd: echo $TASK_FILE_DIR        # → /abs/path/to/project/frontend/src
```

Key takeaways:

- For an **included task**, `TASK_FILE_DIR` is the include's own directory — not the root project directory. This is what users almost always want (`gogo frontend:build` should build *the frontend*).
- A **relative `dir:`** is rebased against whichever file declares the task: relative to the root for root tasks, relative to the include's directory for included tasks. Includes can never "escape" to the root by accident.
- An **absolute `dir:`** is taken verbatim. Useful for tasks that operate on a sibling repository.
- `TASK_FILE_DIR` is *always* an absolute path, even when `dir:` was written as a relative one.
- Sub-task calls (`cmds: - task: X`) recompute `TASK_FILE_DIR` for the child — it reflects the *child's* file/`dir:`, not the parent's.

### Built-in Git Variables

The `GIT_*` variables are resolved lazily on first use and cached for the lifetime of one `gogo` invocation, so a task that doesn't reference any of them never shells out to git. Outside a git repo all five resolve to the empty string — they never raise an error.

A user-defined variable with the same name always wins, so a project can pin (for example) `GIT_COMMIT` in CI for a reproducible build:

```yaml
vars:
  GIT_COMMIT: "{{ "{{" }}.CI_COMMIT_SHA}}"   # take it from the CI env instead of git
```

### Example: ldflags from git metadata

```yaml
tasks:
  build:
    cmd: >
      go build
      -ldflags '-X main.Version={{ "{{" }}.GIT_TAG}} -X main.Commit={{ "{{" }}.GIT_COMMIT}}'
      -o bin/myapp .
```

No `vars:` block, no `sh: git rev-parse HEAD`. The same template can be written shell-style — `${GIT_COMMIT}` — because gogo expands both forms before handing the command to `/bin/sh`.

## CLI Arguments

Arguments after `--` are available as `{{ "{{" }}.CLI_ARGS}}`:

```yaml
tasks:
  test:
    cmd: go test {{ "{{" }}.CLI_ARGS}} ./...
```

```sh
gogo test -- -v -run TestFoo
```

## Environment Variable Expansion

Variables in the task file are expanded from environment variables using `{{ "{{" }}.VAR}}` syntax at parse time:

```yaml
tasks:
  deploy:
    cmd: deploy --region {{ "{{" }}.AWS_REGION}}
```

If `AWS_REGION` is set in the environment, it will be substituted before the task file is processed.

## Task Environment

Tasks can set environment variables for their commands. Values support `${VAR}` expansion from variables and the environment:

```yaml
vars:
  PORT: "8080"

tasks:
  serve:
    env:
      PORT: "${PORT}"
      NODE_ENV: production
    cmd: node server.js
```

### Cross-References Between Env Entries

`env` values can reference other entries in the same `env` block. References are resolved transparently before the command runs:

```yaml
tasks:
  serve:
    env:
      HOST: localhost
      PORT: "8080"
      ADDR: "${HOST}:${PORT}"   # → localhost:8080
    cmd: server --addr $ADDR
```

Lookup order for `${VAR}` inside an env value: another key in the same `env` block first, then task `vars`, then **inherited parent env (when invoked via `cmds: - task: X`)**, then the process environment. Self-cycles or mutual cycles between env keys resolve to the empty string rather than looping forever.

## Parent-to-child Env Propagation

When a task calls another via `cmds: - task: X`, the child inherits the parent's resolved environment — vars, dotenv, and the parent's own `env:` block all flow down. This matches shell-function semantics and makes parent-driven workflows pleasant:

```yaml
tasks:
  smoke:
    env:
      TESTSET_SIZE: "2"
      MIRROR_FS: "1"
    cmds:
      - task: gen      # sees TESTSET_SIZE and MIRROR_FS
      - task: ingest   # same
```

The child's own declarations override the inherited ones on a per-key basis:

```yaml
tasks:
  parent:
    env: { MODE: parent, SHARED: from-parent }
    cmds:
      - task: child
  child:
    env: { MODE: child }              # wins for MODE
    cmd: echo $MODE / $SHARED         # prints: child / from-parent
```

**Deps don't inherit.** Tasks listed in `deps:` run as prerequisites before the parent's body and do *not* see the parent's `env:` block. Only `cmds: - task: X` invocations trigger propagation.

When the same child is called by two different parents (or twice by the same parent with different env), each call site is a distinct execution — gogo bypasses its task-level memoization so the child re-runs with each set of inherited values.

## Resolution & Precedence

Two distinct precedence chains govern how variables work in gogo. Most tasks only need to think about the first one; the second matters when you mix `env:`, `dotenv:`, and `secrets:`.

### 1. Variable lookup (`{{ "{{" }}.VAR}}` and `${VAR}` inside commands)

When gogo expands a reference *inside a command* (and inside a task's own `env` values), it consults these sources in order:

1. Task-scoped `vars` (which already shadow global `vars`)
2. `CLI_ARGS` — only when the lookup is for that name
3. Built-in variables (`GIT_*`, `TASK_FILE_DIR`)
4. The process environment

Var bodies (the `value:` and `sh:` of another variable) use a slightly tighter chain — only other vars and built-ins, no `CLI_ARGS` and no process env. This keeps `vars:` deterministic and free of CLI/shell context.

Unknown `${VAR}` references are left intact for the shell to expand. Unknown `{{ "{{" }}.VAR}}` templates are left verbatim.

### 2. Final task environment (what the command actually sees)

The environment handed to `/bin/sh` is built up in layers, each one overriding the one above on a per-key basis:

1. **`BaseEnv`** — the OS environment plus global `dotenv:` files
2. **Inherited parent env** — only on sub-task calls (`cmds: - task: X`); deps don't inherit
3. **Task `dotenv:`** — never overrides `BaseEnv` (i.e. global dotenv and OS env always win)
4. **Task `vars:`** — promoted into env so commands can read them with `$NAME`
5. **Task `env:`** — with cross-references resolved (see [Cross-References Between Env Entries](#cross-references-between-env-entries))
6. **Task `secrets:`** — **highest precedence**; declaring a secret overrides any same-named placeholder in `env:`

A few non-obvious consequences:

- A task `dotenv:` that names a key already set in the global dotenv (or in the user's shell) is silently ignored — see [Dotenv › Resolution Rules](../dotenv/#resolution-rules).
- Sub-tasks called from `cmds:` see the parent's resolved env (vars + dotenv + `env:` block). Same task name called from two parents with different env runs twice, because gogo bypasses task-level memoization when extra context is in play. See [Parent-to-child Env Propagation](#parent-to-child-env-propagation).
- A task that declares both `env: { OPENAI_API_KEY: dummy }` and `secrets: [OPENAI_API_KEY]` gets the secret value at run time. Use this pattern to keep a clear placeholder for local dev while still resolving the real value in CI.
