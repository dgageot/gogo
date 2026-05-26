# AGENTS.md

## Development commands

The project dogfoods itself: a `gogo.yaml` at the repo root drives
day-to-day development. Use the `gogo` binary for all dev workflows — see
`gogo.yaml` for the exact commands each task runs.

| Goal              | Command            |
| ----------------- | ------------------ |
| Build, lint, test | `gogo` (default)   |
| Build the binary  | `gogo build`       |
| Run all tests     | `gogo test`        |
| Run linters       | `gogo lint`        |
| Format Go sources | `gogo format`      |
| Watch + rebuild   | `gogo -w dev`      |
| Cross-compile all | `gogo cross`       |
| Clean artifacts   | `gogo clean`       |

CI (`.github/workflows/ci.yml`) runs four jobs: tests (`go test ./...`),
`golangci-lint`, `govulncheck`, and the multi-platform Docker build. The
`vulncheck` job also runs on a weekly cron so newly disclosed advisories
surface even when the repo is quiet. Dependabot (`.github/dependabot.yml`)
bumps Go modules, GitHub Actions SHAs, and Docker base images weekly — pair
the Actions group with the `ghapin` skill when reviewing those PRs.
Releases (`release.yml`) are tag-driven and publish cross-built binaries via
`gh release create` plus build-provenance attestations.

The Go toolchain version comes from `go.mod` (`go 1.26.3`). Tests run with
`-tests=true` under golangci-lint v2.

## Code style and conventions

- **Linters**: golangci-lint v2 with a long enable list (see
  `.golangci.yml`). Notable settings:
  - `gofumpt` with `extra-rules: true` and `gofmt` rewrites `interface{}` →
    `any`.
  - `gci` enforces three import groups: standard, default, then
    `prefix(github.com/dgageot/gogo)` (custom-order). Match this exactly
    when adding imports.
  - `depguard` denies `github.com/stretchr/testify` from non-`_test.go`
    files.
  - `forbidigo` (tests only) bans `context.Background/TODO()`,
    `os.MkdirTemp/Setenv/Chdir`, and `fmt.Print*` — use the testing
    equivalents (`t.Context()`, `t.TempDir()`, `t.Setenv()`, `t.Chdir()`)
    and write to a buffer instead of stdout.
  - `revive` requires exported-symbol comments (incl. private receivers)
    and package comments; `staticcheck` runs all checks.
  - Disabled gocritic checks: `dupImport`, `hugeParam`, `rangeValCopy`,
    `unnamedResult`, `appendAssign`.
- **Errors**: wrap with `fmt.Errorf("...: %w", err)`. Aggregate parallel
  errors with `errors.Join` (see `runDeps`). `os.ErrNotExist` is matched
  with `errors.Is`.
- **Concurrency**: prefer `sync.Map` + `sync.Once` for memoization (see
  `taskRun`); use `wg.Go` (Go 1.25+) for fan-out work; always clone slices
  you stash on `ShellCommand.Env` (see `cloneShellCommand` in tests).
- **Defaults**: use `cmp.Or(task.Dir, r.tf.Dir)` rather than ad-hoc empty
  checks (see `Runner.taskDir`).
- **Sorted iteration**: when iterating maps for any user-visible or
  determinism-sensitive output, sort with
  `slices.Sorted(maps.Keys(m))` — the codebase does this consistently
  (`runner.go`, `vars.go`, `env.go`, `includes.go`).
- **YAML unmarshalling**: when adding a field that should accept both a
  string and a struct, follow the `Cmd`/`Dep`/`Var`/`Precondition` pattern
  (try string first, then re-unmarshal into a `type plain X` to avoid
  recursion). For a *string-or-list* field (e.g. `sources`, `aliases`),
  use the `StringList` named slice in `taskfile/types.go` instead of
  `[]string` — its `UnmarshalYAML` already handles the single-string case.
- **Logging**: never `fmt.Print` in library code. Use `Runner.logTask` or
  write to the injected `RunnerIO` / `App.Stdout|Stderr`.
- **Comments on exported symbols**: required by `revive`; keep them short
  and descriptive (Martin-Fowler style — say *why*, not *what*).

## Testing guidelines

- Run a single package: `go test ./taskfile -run TestRunWithExtraVars`.
- Force re-run (no cache): `go test --count=1 ./...`.
- Always `testify/require` for fatal preconditions and `testify/assert`
  for non-fatal checks. Never use bare `t.Fatal` / `t.Error` for value
  comparisons.
- Use `t.Context()`, `t.TempDir()`, `t.Setenv()`, `t.Chdir()` — the linter
  enforces this.
- **Test helpers**:
  - `taskfile/testhelper_test.go::writeFiles(t, dir, map[string]string)` —
    writes a tree of files (creates parent dirs).
  - `taskfile/run_test.go::newTestRunner(t, tf, dir)` — returns a `Runner`
    with `BaseEnv = nil` and a `fakeShellRunner` already wired up.
  - `taskfile/run_test.go::fakeShellRunner` — implements `ShellRunner`,
    records every `Run`/`Output` call, and supports custom `runFunc` /
    `outputFunc` injection. `captureExecs(r)` returns a `*[]Execution`
    populated for `ShellCommandTask` calls only.
  - `taskfile/run_test.go::envValue(env, key)` — last-match env lookup
    (mirrors how `/bin/sh` resolves duplicates).
  - `app_test.go::newTestApp(t, dir, args...)` — builds an `App` wired to
    byte buffers; pass `dir = ""` to use the real `os.Getwd`.
- Tests construct `taskfile.Config` literals directly when a YAML round-trip
  isn't part of the contract under test — this is the preferred style
  for runner-level tests. Use `Parse` / `LoadWithIncludes` only when the
  YAML/AST behavior matters.
- Two-row table tests are split into two named tests; reserve table tests
  for genuinely repetitive cases (see `TestShellJoinPreservesBoundaries`
  for an accepted multi-case test).

## Configuration

- **`gogo.yaml`** — repo's own task file (the project eats its own dog food).
  Uses the built-in `go` source preset (see below) so every Go-aware task
  shares one source list. Edit when changing dev workflows.
- **`.golangci.yml`** — single source of truth for lint config; keep
  `gci.sections` in sync if the module path ever changes.
- **`Dockerfile`** — multi-stage cross build using `tonistiigi/xx`
  (`xx-go build`) and `crazymax/osxcross` for darwin targets. CGO is on for
  darwin, off otherwise. Update `GO_VERSION` here when bumping `go.mod`.
- **`.github/workflows/`** — `ci.yml` (test/lint/vulncheck/build) and
  `release.yml` (tag-triggered cross build + GitHub release with build
  provenance attestations). Action SHAs are pinned; use the `ghapin` skill
  when bumping them. `.github/dependabot.yml` opens grouped weekly PRs for
  Go modules, Actions, and Docker base images.
- **Generated/ignored** (`.gitignore`): `.gogo/` (checksum cache),
  `bin/`, `dist/`, `.zig-cache/`.
- **No env vars** are required at runtime; gogo only consumes whatever the
  user puts in their own `gogo.yaml` / dotenv files.
- **Source presets** — built-ins `go` and `go-vendored` live in
  `taskfile/sources.go::builtinSourcePresets`. Users can override or extend
  them via a top-level `sources:` map (`Config.Sources`); user entries win
  on a name collision. Presets compose recursively (`go-vendored`
  references `go`); cycles and unknown preset-shaped names are caught in
  `expandSources`. Anything containing a glob metacharacter or path
  separator is treated as a literal pattern, so `go.mod` / `*.go` work as
  before.
- **`flatten:`** (top-level or per-include) lists YAML files whose tasks
  merge into the *parent's* namespace without a prefix — the agentic-platform
  pattern for splitting one big task file across many. Tasks land at the
  ancestor's namespace (root, or the include dir), `task.Dir` is resolved
  against the ancestor (not the flatten file's dir), and "first defined
  wins" so a parent file can override a flattened task by re-declaring it.
  See `loadFlatten` and `TestFlattenedTaskRunsFromRootDir`.
- **Foreign fallback** (`fallback.go`) — when no `gogo.yaml` is found, gogo
  walks up looking for a `Taskfile.yml`, `mise.toml`, or `Makefile` whose
  runner is on `PATH`, and shells out. Order is fixed by `foreignRunners`
  and the `make` arm intentionally drops `default` and skips the `--`
  separator (since `make` doesn't understand it). Tests stub the package-
  level `fallbackLookPath` / `fallbackRun` hooks rather than the real exec.
- **Internal tasks** — names whose local segment starts with `_`
  (e.g. `_helper`, `cli:_fmt`) are excluded from `--list` and `--complete`
  but still callable explicitly. Use `taskfile.IsInternalTask` for the
  visibility check; `visibleTaskNames` in `main.go` is the only consumer.

## Common development patterns

- **Adding a new top-level CLI flag**: add a tagged field to `args` in
  `main.go`, handle it in `App.Run` before `runner.Run` is reached, and
  add a test in `app_test.go` using `newTestApp`. The current flags are
  `--list`, `--watch`, `--force`, `--dry`, `--completion`, and the hidden
  `--complete` (used by the shell-completion scripts embedded in
  `main.go`).
- **Silencing a task's per-cmd log**: set `silent: true` on the task. It
  suppresses the `[task] cmd` log line for *that task's own* cmds only —
  sub-tasks invoked via `cmds: - task: X` continue to log unless they
  also opt in (see `TestSilentDoesNotPropagateToCalledTasks`). The shell
  command itself still runs and its stdout/stderr are unaffected.
- **Adding a new task field**: add it to `Task` in `taskfile/types.go`,
  decide whether `UnmarshalYAML` needs string-shorthand support, thread
  it through `Runner.run` in the right phase (deps → vars → requires →
  env → preconditions → up-to-date → cmds), and cover it with a literal
  `taskfile.Config` test in `run_test.go` plus a `Parse`-based test in
  `parse_test.go` if YAML shape matters.
- **Touching env/var resolution**: respect the existing precedence
  (`BaseEnv` < parent task env < task dotenv < task vars < task env) and the
  rule that *task dotenv never overrides global dotenv or OS env* (see
  `TestTaskDotenvDoesNotOverrideGlobalDotenv`). Sub-task calls
  (`cmds: - task: X`) propagate the parent's resolved env via
  `Runner.runSubTask` — deps do NOT (they're prerequisites, not sequenced
  sub-calls; see `TestDepsDoNotInheritParentEnv`). Memoization is bypassed
  whenever extraVars or parentEnv is non-nil so two call sites with
  different context don't collapse into one execution. Vars in `vars:`
  resolve lazily through a recursive lookup in `resolveAllVars` (see
  `taskfile/vars.go`); they may reference each other and the built-in
  `GIT_*` family transitively, declaration order is irrelevant, and
  cycles short-circuit to the empty string. The built-in `TASK_FILE_DIR`
  template var is seeded into every task's resolved-vars map and points
  at the task's effective working directory (after `Runner.taskDir`).
  **Vars are namespace-scoped**:
  a var declared in an included `gogo.yaml` lives in
  `Config.NamespaceVars[namespace]` (with its own working dir in
  `Config.NamespaceDirs[namespace]` for `sh:` resolution) and is visible
  only to tasks at or below that namespace — sibling includes never see
  each other's vars (see `TestNamespacedVarsIsolateSiblings`). Root vars
  in `Config.Vars` stay visible everywhere. The most-specific namespace
  wins on collisions, so `proxy:` and `metrics:` can each declare their
  own `LDFLAGS` without clobbering. The built-in `GIT_*` resolver in
  `taskfile/gitvars.go` is wired through `Runner.builtinLookup` and is
  consulted *after* user vars / CLI_ARGS but *before* the process
  environment by `expandVars`, `resolveEnvValue`, and `checkRequires`.
  Unresolved `${VAR}` references survive verbatim through
  `unknownShellVarSpan`; in particular `$2` (awk positional) is **not**
  rewritten to `${2}` (see `TestShVarPreservesAwkPositional`). Watch mode
  must call `ResetRan` between iterations — it also clears the gitVars
  cache so `{{.GIT_DIRTY}}` re-evaluates after each edit.
- **Touching include logic**: cycles must be detected by absolute file
  path (`loadStack`, which tracks both `gogo.yaml` files and `flatten:`
  YAML files), nested namespaces are colon-joined
  (`parent:child:grandchild`), and dotenv files dedupe globally via
  `seenDotenv`. `Namespaces` map keys are absolute dirs and are used by
  namespace-aware task name resolution.
- **Adding a shell call**: route it through `Runner.ShellRunner` so tests
  can intercept it. Tag it with the right `ShellCommandKind` (`Task`,
  `Precondition`, or `Var`).
- **Touching the `op://` path**: the trigger is `hasOpSecrets(env)` over
  the *fully-built* env (so dotenv-sourced secrets count). Don't move the
  check to before env composition. The new top-level `secrets:` block in
  `taskfile/secrets.go` runs as the *last* layer of `buildEnv`, so a
  `secrets: [X]` reference still feeds the existing `op://` detection.
  When the task's stdout *and* stderr are both terminals, gogo passes
  `--no-masking` to `op run` (`opRunArgs` in `taskfile/shell.go`) so
  interactive TUIs keep their TTY — op's default masking pipes the
  streams through itself and breaks anything more elaborate than line
  output. Non-interactive runs (CI, redirected output) keep masking on.
  New backends plug in by adding a `case strings.HasPrefix(uri, ...)`
  branch in `resolveSecretURI` and a scheme constant in
  `supportedSecretSchemes`.
- **Editing watch behavior**: source collection is recursive over deps via
  `collectSources`; remember to `r.ResetRan()` between iterations or the
  memoized first run will be returned forever.