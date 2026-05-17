---
title: Errors & Troubleshooting
---

# Errors & Troubleshooting

A reference of the error messages gogo emits, what triggers each one, and how to fix it. Errors are grouped by the layer that surfaces them — discovery, parsing, validation, resolution, and execution.

## Discovery

### `no gogo.yaml found`

You ran gogo from a directory that has no `gogo.yaml` and no ancestor with one either. gogo walks up from the current working directory and stops at the **nearest** `gogo.yaml`; if it reaches `/` without finding one, you get this error.

**Fix:** create a `gogo.yaml` at the project root, or `cd` into a directory that has one above it.

When no `gogo.yaml` is found gogo also looks for foreign task files in the
same walk:

- `Taskfile.yml` / `taskfile.yml` / `Taskfile.yaml` / `taskfile.yaml` → shells
  out to [`task`](https://taskfile.dev).
- `mise.toml` → shells out to [`mise run`](https://mise.jdx.dev).

If the matching binary isn't on `PATH` the runner is silently skipped (so a
stray Taskfile in a parent directory doesn't break unrelated invocations).

### `no gogo.yaml found in <dir>`

Variant of the above when an explicit directory was checked.

## Parsing

### `parsing <path>: …`

The YAML failed to parse. The line/column shown comes from the YAML library and points at the offending token.

**Fix:** validate the file with any YAML linter; the most common cause is mixed tabs/spaces or an unquoted value containing `:`.

### `task name <X> must not contain '/' or '\'`

Task names may not contain path separators. They're only allowed in *namespace* prefixes, which gogo synthesizes from `includes:` directory names.

### `task name <X> must not contain '..'`

Same family — task names are checked against a small allow-list to keep namespace resolution unambiguous.

### `task name must not be empty`

Empty key in a `tasks:` map. Usually a stray `- ` or quoting mistake.

## Includes & flatten

### `include <X> must be a subdirectory (absolute paths are not allowed)`
### `include <X> must be a subdirectory (path separators are not allowed)`
### `include <X> must be a subdirectory ('..' is not allowed)`

`includes:` entries are directory names, not paths. They must point to a direct subdirectory of the current task file. For multi-level layouts, chain includes — a top-level `gogo.yaml` includes `frontend/`, whose `frontend/gogo.yaml` includes `web/`, and so on.

### `cyclic include <X> detected in <file>`
### `cyclic flatten <X> detected in <file>`

A → B → A. gogo detects cycles by absolute directory (for `includes:`) or absolute file path (for `flatten:`).

**Fix:** break the cycle. Common cause: a `flatten:` file pulling itself in transitively through a shared helper.

### `resolving include <X> from <file>: …`
### `loading include <X> from <file>: …`

The path resolved but the include couldn't be loaded — either it doesn't exist on disk or the included `gogo.yaml` itself failed to parse. The wrapped error has the underlying cause.

## Defaults & secrets validation

### `top-level default: <X> does not reference any defined task`

Your file declared `default: build` (or similar) but no task named `build` exists. Namespaced names are accepted (`default: backend:build`).

### `task <T> references unknown secret <S>; declare it under top-level secrets:`

The task lists a secret name that has no entry in the top-level `secrets:` map. This is **load-time** validation — you don't have to run the task to see it.

### `secret <S> has unknown backend in <URI> (supported: op://)`

Only `op://` is supported today. Other URI schemes are rejected up front so a typo doesn't silently produce an empty env value at run time.

## Variables & requires

### `task <T> requires variable <V> to be set`

A `requires.vars:` entry is missing. Built-in variables (`GIT_*`, `TASK_FILE_DIR`) satisfy the requirement even when their resolved value is empty — for instance, `requires: { vars: [GIT_TAG] }` is satisfied outside of a tagged commit.

**Fix:** set the variable in `vars:`, in `env:`, or pass it via a sub-task call site.

### `task <T> requires environment variable <E> to be set`

A `requires.env:` entry is unset in the **process** environment (OS env + global dotenv). Per-task `env:` does **not** satisfy the requirement; the check runs against `os.LookupEnv` on purpose, so CI-only variables can be enforced cleanly.

### `resolving variable (sh: <cmd>): …`

A `vars: { X: { sh: <cmd> } }` shell command failed. The wrapped error is the shell's own.

**Fix:** run the command yourself — the failure mode is the same. Common cause: depending on a tool that isn't installed on a clean machine.

## Sources & checksums

### `cyclic source preset <X> (chain: a -> b -> a)`

A user-defined entry in the top-level `sources:` map references itself transitively. The chain in the message tells you exactly where to break it.

### `resolving sources for task <T>: …`

Almost always wraps a cyclic-preset error.

### `computing sources checksum: …`

Filesystem failure while reading a source file (permissions, disappeared between glob and read, etc.). The wrapped error names the file.

## Preconditions

### `task <T>: <msg>`

A `preconditions[]` entry that declared its own `msg:` failed. The task name is preserved so you can find the source quickly in larger files.

### `task <T>: precondition failed: <sh>`

A bare-string precondition failed. The shell command itself is shown verbatim — that's the no-`msg:` fallback.

**Fix:** add a `msg:` to the precondition for a clearer error, or fix the underlying check.

## Watch mode

### `task <T> has no sources, cannot watch`

`gogo -w <task>` requires the task (or one of its transitive dependencies) to declare `sources:` so the watcher knows what to poll. Sources are collected recursively across `deps:`, so adding `sources:` on a leaf task is usually enough.

### `watch interval must be at least <d>`

The `interval:` setting is below gogo's floor. Bump it; the floor exists to keep CPU/IO use reasonable on large repos.

### `invalid interval <X>: …`

The interval is not a valid Go duration string. Use values like `100ms`, `500ms`, `1s`, `2s`.

## 1Password / `op://`

### `uses op:// secrets but the 1Password CLI (op) is not installed: …`

Some part of the resolved environment contained an `op://` URI (either inline in `task.env`, dotenv-sourced, or via `secrets:`), but `op` is not on `PATH`. gogo wraps the command with `op run` only when needed; the check fires only on the affected command.

**Fix:** install `op` from <https://developer.1password.com/docs/cli/get-started/>, or stub the value in local development by overriding the env-var name in `env:` (since `secrets:` always wins, you'll need to comment out the `secrets:` reference for that local override to take effect).

## Aliases & resolution

### `alias <A> is defined by both <T1> and <T2>`

Two tasks declared the same `aliases:` entry. Aliases share a flat global namespace with task names, so collisions must be unique across all included files.

### `task <T> not found`

The task name (or alias) doesn't resolve. Things to check, in order:

1. Typo in the task name.
2. Forgotten namespace prefix when calling from outside the include's directory (`backend:build`, not `build`).
3. The task is defined in a file gogo never loaded — usually a missing `includes:` or `flatten:` entry.

## See also

- [CLI Reference](../cli/) — `--dry` and `--force` semantics for reproducing failures without side effects.
- [Variables](../variables/#resolution--precedence) — the canonical resolution and precedence chains, useful when a value resolves to something unexpected.
- [Secrets](../secrets/) — full op:// flow.
