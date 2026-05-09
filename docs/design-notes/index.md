---
title: Design Notes & Non-Goals
---

# Design Notes & Non-Goals

gogo borrows a lot of vocabulary from [`go-task/task`](https://taskfile.dev) and Make, but it makes deliberate trade-offs that diverge from both. This page collects those choices in one place so you can decide quickly whether gogo fits your project — and so you're not surprised by behavior that looks familiar but isn't.

## Watch mode is polling, not `fsnotify`

`gogo -w` runs every `interval:` (default `500ms`) and recomputes the SHA-256 over each task's resolved sources. If the digest changes, the task re-runs.

- **Why polling:** zero kernel-level dependency, identical behavior across macOS / Linux / Windows / containers / virtualised filesystems, no special handling for editors that swap-then-rename. The whole watcher is ~50 lines.
- **Cost:** O(files in `sources:`) stat + read per interval. For Go projects of <100k lines this is invisible. For very large repos (or lots of binary assets), tune `interval:` upward and trim `sources:` patterns.
- **Implication:** there is no inotify-style "instant" feedback — there is always at least one `interval` of latency. If you need sub-100ms reaction you want a different tool.

## Glob support is custom, not [doublestar](https://github.com/bmatcuk/doublestar)

Source / generates patterns are matched by gogo's own walker, not a full doublestar implementation:

- One `**` per pattern. After it, the suffix is matched against each file's **basename** (not full path).
- Hidden directories (names starting with `.`) are skipped during recursive walks.
- Standard `filepath.Glob` (`*`, `?`, `[…]`) applies to non-recursive patterns.

This covers ~95% of real-world cases (`**/*.go`, `**/*.proto`, `vendor/**`) without pulling in a parser. See [Sources & Checksums › Glob Patterns](../sources-checksums/#glob-patterns) for the full rules and a worked counter-example.

## `cmds:` propagates env to sub-tasks; `deps:` does not

Two ways to invoke another task:

- **`cmds: - task: X`** — sequential, sees the parent's resolved environment (vars, dotenv, `env:` block). Same task name called twice with different env runs twice.
- **`deps: [X]`** — parallel prerequisites, do **not** inherit the parent's `env:`. Memoized — each unique task name runs exactly once per `gogo` invocation.

This is unusual (most task runners treat the two as interchangeable) and intentional: `deps:` are *prerequisites* whose output should be independent of who triggered them, while `cmds: - task:` calls are sequenced sub-routines whose semantics should match calling a shell function. See [Variables › Parent-to-child Env Propagation](../variables/#parent-to-child-env-propagation).

## Built-in source presets are Go-specific

The shipped presets are `go` (`**/*.go` + `go.mod` + `go.sum`) and `go-vendored` (`go` + `vendor/**`). User-defined presets compose with the built-ins (`sources: { lint: [go, .golangci.yml] }`). There are no built-ins for Node, Python, Rust, etc. — projects in those languages declare their own preset map at the top of `gogo.yaml`.

## Only `op://` secrets are supported today

The `secrets:` block validates the URI scheme at load time. The single supported scheme is `op://` (1Password CLI). Other backends (`aws-creds://`, `keychain://`, …) are forward-looking but not implemented — a typo or an experimental scheme fails fast with `unknown backend in "X" (supported: op://)`. New backends plug in by adding a case in `taskfile/secrets.go::resolveSecretURI`. See [Secrets](../secrets/) for the runtime flow.

## No remote includes

`includes:` accept a direct subdirectory of the file that declares them — nothing else. No `https://`, no `git@`, no symlinked sibling repos. This keeps loading deterministic and offline-capable, and makes cycle detection trivial (compare absolute paths).

If you want to share tasks across repos, the answer today is a flat repository layout where the shared tasks live alongside the consumers, or a `flatten:` file checked into a vendored directory. Network-loaded includes are explicitly out of scope.

## No conditional execution beyond preconditions

There is no `when:`, no `if:`, no `unless:`. The escape hatch is a `precondition:` that exits non-zero — failed preconditions abort the task with a clear error. For "skip silently if X" semantics, the task should run a guard command directly inside its `cmd:`.

This keeps the data model tiny: a task is `(deps, env, vars, sources, generates, cmds, preconditions, requires, platforms)` — full stop, with no DSL for control flow. The trade-off is that complex orchestration belongs in a real shell script the task calls.

## No template language beyond `{{ "{{" }}.VAR}}` substitution

Templates are pure variable substitution. No conditionals, no loops, no functions, no pipelines. If you find yourself wanting `{{ "{{" }}if eq .GOOS "linux"}}…{{ "{{" }}end}}`, the answer is either:

- A `platforms:` filter on the task itself, or
- Multiple tasks plus a precondition that selects between them, or
- Conditional logic inside the `cmd:` (the shell already has `case`).

## Memoization is per `gogo` invocation, not per repository

A task runs at most once per `gogo` call (with the [exceptions](../variables/#parent-to-child-env-propagation) for `cmds: - task: X` with different env). There is no on-disk job ledger, no remote build cache, no content-addressed memoization. The only persistent state is the per-task source checksum under `.gogo/checksum/`, which decides "rebuild or not" on the *next* invocation.

If you need shared / distributed caching, use a tool like Bazel, Buck2, or Earthly — gogo is built for the local-developer-loop slot.

## Comparison cheat-sheet

| | gogo | go-task/task | Make |
|---|---|---|---|
| Task file format | YAML (`gogo.yaml`) | YAML (`Taskfile.yml`) | Make DSL |
| Watch mode | Built-in, polling | Built-in, polling (with `--watch`) | None |
| Source/output staleness | SHA-256 checksum or mtime | mtime + checksum | mtime |
| Includes | Direct subdirectories only | Local files & directories | `include` directive |
| Glob in sources | Custom one-`**` matcher | doublestar | n/a (rules use file targets) |
| Env propagation to sub-tasks | `cmds: - task:` propagates, `deps:` doesn't | All sub-task calls inherit | Variables are global |
| Secrets backend | `op://` (1Password CLI) | n/a | n/a |
| Conditional execution | Preconditions only | `status:`, `preconditions:`, `requires:` | `ifeq`, `ifdef`, `ifneq` |
| Template language | Plain `{{ "{{" }}.VAR}}` substitution | Go templates with sprig | Make functions (`$(if …)`) |
| Cross-platform | Single binary, `/bin/sh`-only commands | Single binary, `/bin/sh`-only commands | POSIX shell + Make |

When in doubt: gogo is opinionated about the developer-loop slot. If you need scriptable conditionals, remote caching, or fan-in/fan-out execution graphs, reach for a different tool. If you want a single binary that builds, lints, tests, watches, and shells out 1Password secrets — and stays out of your way — gogo is the trade-off you're looking for.
