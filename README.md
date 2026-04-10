# gogo

A simple task runner.

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

## Secrets

gogo can retrieve secrets from the macOS Keychain or 1Password and inject them as environment variables into your tasks. Secrets are resolved lazily and protected by Touch ID on macOS.

```yaml
secrets:
  API_KEY: keychain://myservice/api-key

deploy:
  secrets: [API_KEY]
  cmd: deploy --api-key $API_KEY
```

## License

Apache License 2.0 — see [LICENSE](LICENSE) for details.
