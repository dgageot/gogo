---
title: gogo
---

<div class="hero">
  <h1>🚀 gogo</h1>
  <p>A simple task runner. Define tasks in YAML, run them from anywhere.</p>
  <div class="hero-buttons">
    <a href="{{ '/getting-started/installation/' | relative_url }}" class="btn btn-primary">Get Started</a>
    <a href="{{ '/features/taskfile-syntax/' | relative_url }}" class="btn btn-secondary">Taskfile Syntax</a>
  </div>
</div>

<div class="features-grid">
  <div class="feature">
    <div class="feature-icon">📄</div>
    <h3>Simple YAML Config</h3>
    <p>Define tasks with commands, dependencies, variables, and environment in a single gogo.yaml file.</p>
  </div>
  <div class="feature">
    <div class="feature-icon">👁️</div>
    <h3>Watch Mode</h3>
    <p>Watch source files and automatically re-run tasks when they change.</p>
  </div>
  <div class="feature">
    <div class="feature-icon">⚡</div>
    <h3>Incremental Builds</h3>
    <p>Source checksums skip tasks that are already up to date.</p>
  </div>
  <div class="feature">
    <div class="feature-icon">🔗</div>
    <h3>Dependencies</h3>
    <p>Tasks can depend on other tasks, with concurrent execution of independent dependencies.</p>
  </div>
  <div class="feature">
    <div class="feature-icon">🔐</div>
    <h3>Secrets</h3>
    <p>Retrieve secrets from the macOS Keychain or 1Password, injected as environment variables.</p>
  </div>
  <div class="feature">
    <div class="feature-icon">📦</div>
    <h3>Includes</h3>
    <p>Split large taskfiles into namespaced modules across subdirectories.</p>
  </div>
</div>

## Quick Example

```yaml
# gogo.yaml
tasks:
  # Build the project
  build:
    cmd: go build ./...

  # Run all tests
  test:
    deps: [build]
    cmd: go test ./...
    sources:
      - "*.go"
      - "cmd/*.go"

  # Format and lint
  lint:
    cmds:
      - gofmt -w .
      - golangci-lint run
```

```sh
gogo build       # run the build task
gogo test        # run tests (builds first)
gogo -w test     # watch and re-run tests on changes
gogo -l          # list tasks with descriptions
```
