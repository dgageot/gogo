# gogo

A simple task runner.

## Installation

```sh
go install github.com/dgageot/gogo@latest
```

## Usage

Create a `gogo.yaml` in your project root:

```yaml
tasks:
  default:
    cmd: echo "Hello, World!"

  build:
    cmd: go build -o bin/myapp ./...
    sources: go            # built-in preset: **/*.go + go.mod + go.sum
    generates:
      - bin/myapp

  test:
    cmd: go test ./...
    sources: go
```

Run a task:

```sh
gogo build
gogo test
```

List available tasks:

```sh
gogo -l
```

Watch sources and re-run on changes:

```sh
gogo -w test
```

Dry run — see what would execute:

```sh
gogo -n build
```

## Features

A quick tour. Each item links to the canonical docs page.

- **Incremental builds** — SHA-256 source checksums or `generates:` timestamp comparison ([Sources & Checksums](https://dgageot.github.io/gogo/features/sources-checksums/))
- **Source presets** — reuse named glob lists; built-in `go` / `go-vendored`, or define your own ([Sources & Checksums](https://dgageot.github.io/gogo/features/sources-checksums/#presets))
- **Watch mode** — polls sources at a configurable `interval:` and re-runs ([Watch Mode](https://dgageot.github.io/gogo/features/watch-mode/))
- **Concurrent dependencies** with deduplication ([Dependencies](https://dgageot.github.io/gogo/features/dependencies/))
- **Variables** — template expansion, `sh:` shell-evaluated values, built-in `{{.GIT_*}}` and `{{.TASK_FILE_DIR}}` ([Variables](https://dgageot.github.io/gogo/features/variables/))
- **Dotenv** — global and per-task, with deterministic precedence ([Dotenv](https://dgageot.github.io/gogo/features/dotenv/))
- **1Password secrets** via `op://` references and a top-level `secrets:` block ([Secrets](https://dgageot.github.io/gogo/features/secrets/))
- **Preconditions & requires** to guard tasks before they run ([Preconditions](https://dgageot.github.io/gogo/features/preconditions/))
- **Deferred cleanup** — `defer:` cmds run after the task, in reverse order, even on failure
- **Error tolerance** — `ignore_error: true` lets a task continue past a failing cmd
- **Platform filtering** to restrict tasks to specific OS/arch
- **Includes & flatten** to split task files across subdirectories ([Includes](https://dgageot.github.io/gogo/features/includes/))
- **Dry run** mode (`gogo -n`) to preview the plan ([CLI Reference](https://dgageot.github.io/gogo/features/cli/))

**Full documentation:** <https://dgageot.github.io/gogo/>

## Secrets

gogo integrates with [1Password CLI](https://developer.1password.com/docs/cli/) to inject secrets into tasks. Use `op://` references in your task environment:

```yaml
tasks:
  deploy:
    env:
      DB_PASSWORD: op://vault/item/field
    cmd: deploy --password $DB_PASSWORD
```

When `op://` values are detected, gogo wraps the command with `op run` which resolves secrets and handles authentication (including Touch ID).

## License

Apache License 2.0 — see [LICENSE](LICENSE) for details.
