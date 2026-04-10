# gogo

A simple task runner for Go projects.

## Installation

```sh
go install github.com/dgageot/gogo@latest
```

## Usage

Create a `taskfile.yaml` in your project root:

```yaml
default:
  cmd: echo "Hello, World!"

build:
  cmd: go build ./...

test:
  cmd: go test ./...
  sources:
    - "**/*.go"
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

## License

Apache License 2.0 — see [LICENSE](LICENSE) for details.
