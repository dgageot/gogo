---
title: gogo
---

<div class="hero">
  <h1>🚀 gogo</h1>
  <p>A simple task runner. Define tasks in YAML, run them from anywhere.</p>
  <div class="hero-buttons">
    <a href="{{ '/getting-started/installation/' | relative_url }}" class="btn btn-primary">Get Started</a>
    <a href="{{ '/features/task-file-syntax/' | relative_url }}" class="btn btn-secondary">Task File Syntax</a>
  </div>
</div>

<div class="features-grid">
  <div class="feature">
    <div class="feature-icon">📄</div>
    <h3>Simple YAML Config</h3>
    <p>Define tasks with commands, dependencies, variables, and environment in a single <code>gogo.yaml</code>.</p>
  </div>
  <div class="feature">
    <div class="feature-icon">⚡</div>
    <h3>Incremental Builds</h3>
    <p>SHA-256 source checksums or <code>generates:</code> timestamp comparison skip work that is already up to date.</p>
  </div>
  <div class="feature">
    <div class="feature-icon">📦</div>
    <h3>Source Presets</h3>
    <p>Reuse named glob lists (built-in <code>go</code> and <code>go-vendored</code>, or define your own).</p>
  </div>
  <div class="feature">
    <div class="feature-icon">👁️</div>
    <h3>Watch Mode</h3>
    <p>Polls sources at a configurable <code>interval:</code> and re-runs tasks when they change.</p>
  </div>
  <div class="feature">
    <div class="feature-icon">🔗</div>
    <h3>Concurrent Dependencies</h3>
    <p>Independent <code>deps:</code> run in parallel and are deduplicated within a single invocation.</p>
  </div>
  <div class="feature">
    <div class="feature-icon">🧮</div>
    <h3>Variables</h3>
    <p>Template expansion, shell-evaluated <code>sh:</code> values, and built-in <code>{{ "{{" }}.GIT_*}}</code> and <code>{{ "{{" }}.TASK_FILE_DIR}}</code>.</p>
  </div>
  <div class="feature">
    <div class="feature-icon">📨</div>
    <h3>Dotenv</h3>
    <p>Global and per-task <code>.env</code> files, with deterministic precedence rules.</p>
  </div>
  <div class="feature">
    <div class="feature-icon">🔐</div>
    <h3>Secrets</h3>
    <p>Inject 1Password values into tasks using <code>op://</code> references; <code>op run</code> handles auth.</p>
  </div>
  <div class="feature">
    <div class="feature-icon">✅</div>
    <h3>Preconditions &amp; Requires</h3>
    <p>Guard tasks with shell-evaluated checks or required vars / env before they run.</p>
  </div>
  <div class="feature">
    <div class="feature-icon">🖥️</div>
    <h3>Platform Filtering</h3>
    <p>Restrict tasks to specific OS/arch with <code>platforms: [linux/amd64, darwin]</code>.</p>
  </div>
  <div class="feature">
    <div class="feature-icon">📂</div>
    <h3>Includes &amp; Flatten</h3>
    <p>Split task files across subdirectories (<code>includes:</code>) or merge sibling files into one namespace (<code>flatten:</code>).</p>
  </div>
  <div class="feature">
    <div class="feature-icon">🔍</div>
    <h3>Dry Run</h3>
    <p><code>gogo -n &lt;task&gt;</code> prints the full plan without executing commands.</p>
  </div>
</div>

## Quick Example

```yaml
# gogo.yaml
tasks:
  # Build the project
  build:
    cmd: go build ./...
    sources: go        # built-in preset: **/*.go + go.mod + go.sum

  # Run all tests
  test:
    deps: [build]
    cmd: go test ./...
    sources: go

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
