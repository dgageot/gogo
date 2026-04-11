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
    cmd: go build ./...

  test:
    cmd: go test ./...
    sources:
      - "*.go"
      - "cmd/*.go"
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
