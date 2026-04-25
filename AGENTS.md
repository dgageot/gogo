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
| Cross-compile all | `gogo build-cross` |
| Clean artifacts   | `gogo clean`       |

CI (`.github/workflows/ci.yml`) runs tests, `golangci-lint`, and the
multi-platform Docker build. Releases (`release.yml`) are tag-driven and
publish cross-built binaries via `gh release create`.

The Go toolchain version comes from `go.mod` (`go 1.26.2`). Tests run with
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
  recursion).
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
- Tests construct `Taskfile` literals directly when a YAML round-trip
  isn't part of the contract under test — this is the preferred style
  for runner-level tests. Use `Parse` / `LoadWithIncludes` only when the
  YAML/AST behavior matters.
- Two-row table tests are split into two named tests; reserve table tests
  for genuinely repetitive cases (see `TestShellJoinPreservesBoundaries`
  for an accepted multi-case test).

## Configuration

- **`gogo.yaml`** — repo's own taskfile (the project eats its own dog food).
  Edit when changing dev workflows.
- **`.golangci.yml`** — single source of truth for lint config; keep
  `gci.sections` in sync if the module path ever changes.
- **`Dockerfile`** — multi-stage cross build using `tonistiigi/xx`
  (`xx-go build`) and `crazymax/osxcross` for darwin targets. CGO is on for
  darwin, off otherwise. Update `GO_VERSION` here when bumping `go.mod`.
- **`.github/workflows/`** — `ci.yml` (test/lint/build) and `release.yml`
  (tag-triggered cross build + GitHub release). Action SHAs are pinned;
  use the `ghapin` skill when bumping them.
- **Generated/ignored** (`.gitignore`): `.gogo/` (checksum cache),
  `bin/`, `dist/`, `.zig-cache/`.
- **No env vars** are required at runtime; gogo only consumes whatever the
  user puts in their own `gogo.yaml` / dotenv files.

## Common development patterns

- **Adding a new top-level CLI flag**: add a tagged field to `args` in
  `main.go`, handle it in `App.Run` before `runner.Run` is reached, and
  add a test in `app_test.go` using `newTestApp`.
- **Adding a new task field**: add it to `Task` in `taskfile/types.go`,
  decide whether `UnmarshalYAML` needs string-shorthand support, thread
  it through `Runner.run` in the right phase (deps → vars → requires →
  env → preconditions → up-to-date → cmds), and cover it with a literal
  `Taskfile` test in `run_test.go` plus a `Parse`-based test in
  `parse_test.go` if YAML shape matters.
- **Touching env/var resolution**: respect the existing precedence
  (`BaseEnv` < task dotenv < task vars < task env) and the rule that
  *task dotenv never overrides global dotenv or OS env* (see
  `TestTaskDotenvDoesNotOverrideGlobalDotenv`).
- **Touching include logic**: cycles must be detected by absolute dir
  (`includeStack`), nested namespaces are colon-joined
  (`parent:child:grandchild`), and dotenv files dedupe globally via
  `seenDotenv`. `Namespaces` map keys are absolute dirs and are used by
  namespace-aware task name resolution.
- **Adding a shell call**: route it through `Runner.ShellRunner` so tests
  can intercept it. Tag it with the right `ShellCommandKind` (`Task`,
  `Precondition`, or `Var`).
- **Touching the `op://` path**: the trigger is `hasOpSecrets(env)` over
  the *fully-built* env (so dotenv-sourced secrets count). Don't move the
  check to before env composition.
- **Editing watch behavior**: source collection is recursive over deps via
  `collectSources`; remember to `r.ResetRan()` between iterations or the
  memoized first run will be returned forever.