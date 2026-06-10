package taskfile

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParse(t *testing.T) {
	dir := t.TempDir()
	writeFiles(t, dir, map[string]string{
		"gogo.yaml": `version: "1"
includes:
  - cli
tasks:
  todo:
    cmd: open TODO.md
`,
		"cli/gogo.yaml": `version: "1"
tasks:
  # Build the docker-ai CLI
  build:
    cmd: go build .
  cli_internal:
    cmd: echo hidden
  # Install the docker-ai CLI
  install:
    deps:
      - build
    cmd: go install .
  # GitHub helper
  github:
    aliases:
      - gh
    cmd: gh auth status
`,
	})

	tf, err := LoadWithIncludes(dir)
	require.NoError(t, err)

	assert.Equal(t, "1", tf.Version)
	assert.NotEmpty(t, tf.Tasks)

	task, ok := tf.Tasks["todo"]
	require.True(t, ok)
	assert.Empty(t, task.Desc)

	task, ok = tf.Tasks["cli:build"]
	require.True(t, ok)
	assert.Equal(t, "Build the docker-ai CLI", task.Desc)

	task, ok = tf.Tasks["cli:install"]
	require.True(t, ok)
	assert.Len(t, task.Deps, 1)
	assert.Equal(t, "cli:build", task.Deps[0].Task)
	assert.Equal(t, "Install the docker-ai CLI", task.Desc)

	task, ok = tf.Tasks["cli:github"]
	require.True(t, ok)
	assert.Equal(t, StringList{"gh"}, task.Aliases)
	assert.Equal(t, "GitHub helper", task.Desc)
}

func TestLoadWithIncludesNested(t *testing.T) {
	dir := t.TempDir()
	writeFiles(t, dir, map[string]string{
		"gogo.yaml": `version: "1"
includes:
  - cli
`,
		"cli/gogo.yaml": `version: "1"
includes:
  - nested
tasks:
  build:
    cmd: go build .
`,
		"cli/nested/gogo.yaml": `version: "1"
tasks:
  test:
    cmd: go test ./...
`,
	})

	tf, err := LoadWithIncludes(dir)
	require.NoError(t, err)

	assert.Contains(t, tf.Tasks, "cli:build")
	assert.Contains(t, tf.Tasks, "cli:nested:test")
}

func TestLoadWithIncludesRejectsParentEscape(t *testing.T) {
	// `includes:` must point at a direct subdirectory, so `..` (or any path
	// that climbs out of the parent) is rejected. This makes parent-escape
	// cycles unreachable: an include cycle would require a parent jump.
	dir := t.TempDir()
	writeFiles(t, dir, map[string]string{
		"gogo.yaml": `version: "1"
includes:
  - cli
`,
		"cli/gogo.yaml": `version: "1"
includes:
  - root
`,
		"cli/root/gogo.yaml": `version: "1"
includes:
  - ../..
`,
	})

	_, err := LoadWithIncludes(dir)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "must be a subdirectory")
	assert.Contains(t, err.Error(), filepath.Join(dir, "cli", "root", "gogo.yaml"))
}

func TestLoadWithIncludesRejectsNonSubdirectory(t *testing.T) {
	cases := map[string]string{
		"parent":      "..",
		"sibling":     "../sibling",
		"absolute":    "/etc",
		"nested-path": "sub/dir",
		"backslash":   `sub\dir`,
	}
	for label, badName := range cases {
		t.Run(label, func(t *testing.T) {
			dir := t.TempDir()
			writeFiles(t, dir, map[string]string{
				"gogo.yaml": fmt.Sprintf("version: \"1\"\nincludes:\n  - %q\n", badName),
			})

			_, err := LoadWithIncludes(dir)
			require.Error(t, err)
			assert.Contains(t, err.Error(), "must be a subdirectory")
		})
	}
}

func TestLoadWithIncludesMissingMentionsParentFile(t *testing.T) {
	dir := t.TempDir()
	writeFiles(t, dir, map[string]string{
		"gogo.yaml": `version: "1"
includes:
  - backend
`,
		"backend/gogo.yaml": `version: "1"
includes:
  - data
`,
	})

	_, err := LoadWithIncludes(dir)
	require.Error(t, err)
	assert.Contains(t, err.Error(), `loading include "backend:data"`)
	assert.Contains(t, err.Error(), "from "+filepath.Join(dir, "backend", "gogo.yaml"))
	assert.Contains(t, err.Error(), "no gogo.yaml found")
}

func TestLoadWithIncludesPreservesChildVars(t *testing.T) {
	dir := t.TempDir()
	writeFiles(t, dir, map[string]string{
		"gogo.yaml": `version: "1"
includes:
  - child
tasks:
  hello:
    cmd: echo hello
`,
		"child/gogo.yaml": `version: "1"
vars:
  VERSION: "1.2.3"
tasks:
  build:
    cmd: echo {{.VERSION}}
`,
	})

	tf, err := LoadWithIncludes(dir)
	require.NoError(t, err)

	// Child vars are scoped to their include's namespace, not leaked into the
	// root — a sibling include can declare its own VERSION without clashing.
	assert.NotContains(t, tf.Vars, "VERSION", "included vars must not leak into the root namespace")
	require.Contains(t, tf.NamespaceVars, "child")
	assert.Contains(t, tf.NamespaceVars["child"], "VERSION")
	assert.Equal(t, "1.2.3", tf.NamespaceVars["child"]["VERSION"].Value)
	assert.Equal(t, filepath.Join(dir, "child"), tf.NamespaceDirs["child"])
}

func TestLoadWithIncludesNestedNamespaceCollision(t *testing.T) {
	dir := t.TempDir()
	writeFiles(t, dir, map[string]string{
		"gogo.yaml": `version: "1"
includes:
  - cli
  - server
`,
		"cli/gogo.yaml": `version: "1"
includes:
  - utils
tasks:
  build:
    cmd: go build ./cli
`,
		"cli/utils/gogo.yaml": `version: "1"
tasks:
  fmt:
    cmd: gofmt ./cli/utils
`,
		"server/gogo.yaml": `version: "1"
includes:
  - utils
tasks:
  build:
    cmd: go build ./server
`,
		"server/utils/gogo.yaml": `version: "1"
tasks:
  fmt:
    cmd: gofmt ./server/utils
`,
	})

	tf, err := LoadWithIncludes(dir)
	require.NoError(t, err)

	assert.Contains(t, tf.Tasks, "cli:build")
	assert.Contains(t, tf.Tasks, "server:build")
	assert.Contains(t, tf.Tasks, "cli:utils:fmt", "nested include under cli should be namespaced as cli:utils:fmt")
	assert.Contains(t, tf.Tasks, "server:utils:fmt", "nested include under server should be namespaced as server:utils:fmt")
}

func TestExpandEnvTemplatesValueOnly(t *testing.T) {
	t.Setenv("MY_VAR", "hello")

	assert.Equal(t, "value: hello", expandEnvTemplates("value: {{.MY_VAR}}"))
	assert.Equal(t, "value: hello", expandEnvTemplates("value: {{ .MY_VAR }}"))
	assert.Equal(t, "value: {{.UNSET_VAR}}", expandEnvTemplates("value: {{.UNSET_VAR}}"))
}

func TestParseEnvExpansionAppliesToValuesNotStructure(t *testing.T) {
	// An env value containing YAML-structural characters (newline, ':', '-')
	// must NOT be able to inject new tasks or fields. Cmd strings are now
	// expanded at run time, so we exercise a parse-time-expanded field
	// (task.Dir) here — the substitution still happens after YAML parsing,
	// so the parser sees the original document either way.
	t.Setenv("GOGO_INJECT", "injected\n  evil:\n    cmd: rm -rf /")

	dir := t.TempDir()
	writeFiles(t, dir, map[string]string{
		"gogo.yaml": `version: "1"
tasks:
  build:
    dir: "{{.GOGO_INJECT}}"
    cmd: echo hi
`,
	})

	tf, err := Parse(dir)
	require.NoError(t, err)

	// The malicious env value lands inside the dir string verbatim — no
	// 'evil' task is conjured into existence.
	assert.NotContains(t, tf.Tasks, "evil")
	require.Contains(t, tf.Tasks, "build")
	assert.Equal(t,
		"injected\n  evil:\n    cmd: rm -rf /",
		tf.Tasks["build"].Dir,
	)
}

func TestParseEnvExpansionInVariousFields(t *testing.T) {
	// Verify that {{.VAR}} in fields that are *not* re-expanded at run time
	// (task.Dir, task.Sources, task.Aliases, etc.) still gets the value
	// from the process environment at parse time. Run-time-expanded fields
	// (vars, env values, cmd strings) are intentionally left untouched at
	// parse time so the run-time precedence (user vars > env) wins — see
	// TestEnvTemplateDoesNotReferenceUserVarAfterParse for the regression that
	// shaped this split.
	t.Setenv("GOGO_DIR", "backend")
	t.Setenv("GOGO_GLOB", "*.go")

	dir := t.TempDir()
	writeFiles(t, dir, map[string]string{
		"gogo.yaml": `version: "1"
tasks:
  build:
    dir: "{{.GOGO_DIR}}"
    sources:
      - "{{.GOGO_GLOB}}"
    cmd: go build
`,
	})

	tf, err := Parse(dir)
	require.NoError(t, err)

	build := tf.Tasks["build"]
	assert.Equal(t, "backend", build.Dir)
	assert.Equal(t, StringList{"*.go"}, build.Sources)
}

func TestParsePreconditions(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "gogo.yaml"), []byte(`version: "1"
tasks:
  deploy:
    preconditions:
      - sh: test -n "$DOCKER_HUB_USER"
        msg: DOCKER_HUB_USER is not set
      - test -n "$DOCKER_HUB_PASSWORD"
    cmd: echo deploying
`), 0o644))

	tf, err := Parse(dir)
	require.NoError(t, err)

	task := tf.Tasks["deploy"]
	require.Len(t, task.Preconditions, 2)
	assert.Equal(t, `test -n "$DOCKER_HUB_USER"`, task.Preconditions[0].Sh)
	assert.Equal(t, "DOCKER_HUB_USER is not set", task.Preconditions[0].Msg)
	assert.Equal(t, `test -n "$DOCKER_HUB_PASSWORD"`, task.Preconditions[1].Sh)
	assert.Empty(t, task.Preconditions[1].Msg)
}

func TestParseDeferCmd(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "gogo.yaml"), []byte(`version: "1"
tasks:
  test:
    cmds:
      - docker compose up -d
      - defer: docker compose down
      - go test ./...
`), 0o644))

	tf, err := Parse(dir)
	require.NoError(t, err)

	task := tf.Tasks["test"]
	require.Len(t, task.Cmds, 3)
	assert.Equal(t, "docker compose up -d", task.Cmds[0].Cmd)
	assert.Equal(t, "docker compose down", task.Cmds[1].Defer)
	assert.Equal(t, "go test ./...", task.Cmds[2].Cmd)
}

func TestParseIgnoreError(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "gogo.yaml"), []byte(`version: "1"
tasks:
  clean:
    cmds:
      - cmd: rm bin/app
        ignore_error: true
      - echo done
`), 0o644))

	tf, err := Parse(dir)
	require.NoError(t, err)

	task := tf.Tasks["clean"]
	require.Len(t, task.Cmds, 2)
	assert.Equal(t, "rm bin/app", task.Cmds[0].Cmd)
	assert.True(t, task.Cmds[0].IgnoreError)
	assert.False(t, task.Cmds[1].IgnoreError, "ignore_error defaults to false")
}

func TestParsePromptField(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "gogo.yaml"), []byte(`version: "1"
tasks:
  deploy:
    prompt: Deploy to production?
    cmd: echo deploying
  build:
    cmd: go build
`), 0o644))

	tf, err := Parse(dir)
	require.NoError(t, err)

	assert.Equal(t, "Deploy to production?", tf.Tasks["deploy"].Prompt)
	assert.Empty(t, tf.Tasks["build"].Prompt)
}

func TestParseSilentField(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "gogo.yaml"), []byte(`version: "1"
tasks:
  setup:
    silent: true
    cmds:
      - echo step1
      - echo step2
  build:
    cmds:
      - go build
`), 0o644))

	tf, err := Parse(dir)
	require.NoError(t, err)

	assert.True(t, tf.Tasks["setup"].Silent)
	assert.False(t, tf.Tasks["build"].Silent, "silent defaults to false")
}

func TestApplyTaskComments(t *testing.T) {
	yamlData := []byte(`version: "3"
tasks:
  # Build the project
  build:
    cmd: go build
  # Run all the tests
  test:
    cmd: go test
  deploy:
    cmd: deploy.sh
`)

	tf := &Config{
		Tasks: map[string]Task{
			"build":  {Cmds: []Cmd{{Cmd: "go build"}}},
			"test":   {Cmds: []Cmd{{Cmd: "go test"}}},
			"deploy": {Cmds: []Cmd{{Cmd: "deploy.sh"}}},
		},
	}

	applyTaskComments(tf, yamlData)

	assert.Equal(t, "Build the project", tf.Tasks["build"].Desc)
	assert.Equal(t, "Run all the tests", tf.Tasks["test"].Desc)
	assert.Empty(t, tf.Tasks["deploy"].Desc)
}

func TestLoadWithIncludesKeepsExternalTaskReferencesUnchanged(t *testing.T) {
	dir := t.TempDir()
	writeFiles(t, dir, map[string]string{
		"gogo.yaml": `version: "1"
includes:
  - cli
tasks:
  shared:
    cmd: echo root
`,
		"cli/gogo.yaml": `version: "1"
tasks:
  build:
    deps:
      - shared
    cmds:
      - task: shared
      - task: test
  test:
    cmd: go test ./...
`,
	})

	tf, err := LoadWithIncludes(dir)
	require.NoError(t, err)

	build := tf.Tasks["cli:build"]
	require.Len(t, build.Deps, 1)
	assert.Equal(t, "shared", build.Deps[0].Task)
	require.Len(t, build.Cmds, 2)
	assert.Equal(t, "shared", build.Cmds[0].Task)
	assert.Equal(t, "cli:test", build.Cmds[1].Task)
}

func TestLoadWithIncludesMakesTaskDirsAbsolute(t *testing.T) {
	dir := t.TempDir()
	writeFiles(t, dir, map[string]string{
		"gogo.yaml": `version: "1"
includes:
  - cli
`,
		"cli/gogo.yaml": `version: "1"
tasks:
  build:
    dir: src
    cmd: go build .
  test:
    cmd: go test ./...
`,
	})

	tf, err := LoadWithIncludes(dir)
	require.NoError(t, err)

	assert.Equal(t, filepath.Join(dir, "cli", "src"), tf.Tasks["cli:build"].Dir)
	assert.Equal(t, filepath.Join(dir, "cli"), tf.Tasks["cli:test"].Dir)
}

func TestLoadWithFlattenMergesTasksUnnamespaced(t *testing.T) {
	dir := t.TempDir()
	writeFiles(t, dir, map[string]string{
		"gogo.yaml": `version: "1"
flatten:
  - tasks/lint.yml
  - tasks/test.yml
tasks:
  default:
    cmd: echo root
`,
		"tasks/lint.yml": `version: "1"
tasks:
  lint:backend:
    cmd: golangci-lint run
  lint:frontend:
    cmd: eslint .
`,
		"tasks/test.yml": `version: "1"
tasks:
  test:
    cmd: go test ./...
`,
	})

	tf, err := LoadWithIncludes(dir)
	require.NoError(t, err)

	// Tasks land in the root namespace verbatim — colons in names are preserved.
	assert.Contains(t, tf.Tasks, "default")
	assert.Contains(t, tf.Tasks, "lint:backend")
	assert.Contains(t, tf.Tasks, "lint:frontend")
	assert.Contains(t, tf.Tasks, "test")

	assert.Equal(t, "golangci-lint run", tf.Tasks["lint:backend"].Cmds[0].Cmd)
	assert.Equal(t, "eslint .", tf.Tasks["lint:frontend"].Cmds[0].Cmd)
}

func TestLoadWithFlattenResolvesTaskDirAgainstRoot(t *testing.T) {
	// A `dir: backend` written inside a flatten file should mean "backend at
	// the project root", not relative to where the YAML file happens to live.
	dir := t.TempDir()
	writeFiles(t, dir, map[string]string{
		"gogo.yaml": `version: "1"
flatten:
  - tasks/utils.yml
`,
		"tasks/utils.yml": `version: "1"
tasks:
  build:
    dir: backend
    cmd: go build
`,
	})

	tf, err := LoadWithIncludes(dir)
	require.NoError(t, err)

	assert.Equal(t, filepath.Join(dir, "backend"), tf.Tasks["build"].Dir)
}

func TestLoadWithFlattenRootTaskWinsCollision(t *testing.T) {
	// A task defined in the parent file beats a same-named task in a flatten file.
	dir := t.TempDir()
	writeFiles(t, dir, map[string]string{
		"gogo.yaml": `version: "1"
flatten:
  - tasks/extras.yml
tasks:
  build:
    cmd: from-root
`,
		"tasks/extras.yml": `version: "1"
tasks:
  build:
    cmd: from-flatten
  test:
    cmd: from-flatten
`,
	})

	tf, err := LoadWithIncludes(dir)
	require.NoError(t, err)

	assert.Equal(t, "from-root", tf.Tasks["build"].Cmds[0].Cmd, "root task should win")
	assert.Equal(t, "from-flatten", tf.Tasks["test"].Cmds[0].Cmd)
}

func TestLoadWithFlattenFirstFlattenFileWinsCollision(t *testing.T) {
	// When two flatten files both define a task and the parent doesn't, the
	// first file listed wins (mirrors mergeVars).
	dir := t.TempDir()
	writeFiles(t, dir, map[string]string{
		"gogo.yaml": `version: "1"
flatten:
  - tasks/a.yml
  - tasks/b.yml
`,
		"tasks/a.yml": `version: "1"
tasks:
  build:
    cmd: from-a
`,
		"tasks/b.yml": `version: "1"
tasks:
  build:
    cmd: from-b
`,
	})

	tf, err := LoadWithIncludes(dir)
	require.NoError(t, err)

	assert.Equal(t, "from-a", tf.Tasks["build"].Cmds[0].Cmd)
}

func TestLoadWithFlattenMergesGlobalVars(t *testing.T) {
	dir := t.TempDir()
	writeFiles(t, dir, map[string]string{
		"gogo.yaml": `version: "1"
flatten:
  - tasks/extras.yml
vars:
  ROOT_ONLY: from-root
`,
		"tasks/extras.yml": `version: "1"
vars:
  FLAT_ONLY: from-flat
  ROOT_ONLY: should-lose
tasks:
  noop:
    cmd: "true"
`,
	})

	tf, err := LoadWithIncludes(dir)
	require.NoError(t, err)

	assert.Equal(t, "from-root", tf.Vars["ROOT_ONLY"].Value, "root vars win conflicts")
	assert.Equal(t, "from-flat", tf.Vars["FLAT_ONLY"].Value, "flatten vars are merged in")
}

func TestLoadWithFlattenLocalTaskReferencesNotRewritten(t *testing.T) {
	// At root, a `task: foo` reference in a flattened file refers to the same
	// `foo` that's now in the root — no rewriting is needed or wanted.
	dir := t.TempDir()
	writeFiles(t, dir, map[string]string{
		"gogo.yaml": `version: "1"
flatten:
  - tasks/extras.yml
`,
		"tasks/extras.yml": `version: "1"
tasks:
  helper:
    cmd: helper
  build:
    cmds:
      - task: helper
      - go build
`,
	})

	tf, err := LoadWithIncludes(dir)
	require.NoError(t, err)

	build := tf.Tasks["build"]
	require.Len(t, build.Cmds, 2)
	assert.Equal(t, "helper", build.Cmds[0].Task)
}

func TestLoadWithFlattenSupportsTaskComments(t *testing.T) {
	dir := t.TempDir()
	writeFiles(t, dir, map[string]string{
		"gogo.yaml": `version: "1"
flatten:
  - tasks/lint.yml
`,
		"tasks/lint.yml": `version: "1"
tasks:
  # Run all linters
  lint:
    cmd: golangci-lint run
`,
	})

	tf, err := LoadWithIncludes(dir)
	require.NoError(t, err)

	assert.Equal(t, "Run all linters", tf.Tasks["lint"].Desc)
}

func TestLoadWithFlattenNested(t *testing.T) {
	// A flatten file can itself flatten another file. All tasks land in root.
	dir := t.TempDir()
	writeFiles(t, dir, map[string]string{
		"gogo.yaml": `version: "1"
flatten:
  - tasks/a.yml
`,
		"tasks/a.yml": `version: "1"
flatten:
  - sub/b.yml
tasks:
  a-task:
    cmd: a
`,
		"tasks/sub/b.yml": `version: "1"
tasks:
  b-task:
    cmd: b
`,
	})

	tf, err := LoadWithIncludes(dir)
	require.NoError(t, err)

	assert.Contains(t, tf.Tasks, "a-task")
	assert.Contains(t, tf.Tasks, "b-task")
}

func TestLoadWithFlattenRejectsCycles(t *testing.T) {
	dir := t.TempDir()
	writeFiles(t, dir, map[string]string{
		"gogo.yaml": `version: "1"
flatten:
  - a.yml
`,
		"a.yml": `version: "1"
flatten:
  - b.yml
`,
		"b.yml": `version: "1"
flatten:
  - a.yml
`,
	})

	_, err := LoadWithIncludes(dir)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "cyclic flatten")
	assert.Contains(t, err.Error(), "a.yml")
}

func TestLoadWithFlattenMissingFile(t *testing.T) {
	dir := t.TempDir()
	writeFiles(t, dir, map[string]string{
		"gogo.yaml": `version: "1"
flatten:
  - tasks/missing.yml
`,
	})

	_, err := LoadWithIncludes(dir)
	require.Error(t, err)
	assert.Contains(t, err.Error(), `loading flatten "tasks/missing.yml"`)
}

func TestLoadWithFlattenInsideIncludeUsesIncludeNamespace(t *testing.T) {
	// A flatten file pulled in from inside a namespaced include should have
	// its tasks land at that include's namespace, not at the root.
	dir := t.TempDir()
	writeFiles(t, dir, map[string]string{
		"gogo.yaml": `version: "1"
includes:
  - cli
`,
		"cli/gogo.yaml": `version: "1"
flatten:
  - helpers.yml
tasks:
  build:
    cmd: go build
`,
		"cli/helpers.yml": `version: "1"
tasks:
  helper:
    cmd: do-helper
`,
	})

	tf, err := LoadWithIncludes(dir)
	require.NoError(t, err)

	assert.Contains(t, tf.Tasks, "cli:build")
	assert.Contains(t, tf.Tasks, "cli:helper", "flatten inside an include should use that include's namespace")
	assert.NotContains(t, tf.Tasks, "helper")
}

func TestLoadWithFlattenAbsolutePath(t *testing.T) {
	dir := t.TempDir()
	extras := filepath.Join(dir, "shared", "extras.yml")
	writeFiles(t, dir, map[string]string{
		"shared/extras.yml": `version: "1"
tasks:
  shared-task:
    cmd: echo shared
`,
	})
	require.NoError(t, os.WriteFile(filepath.Join(dir, "gogo.yaml"), fmt.Appendf(nil, `version: "1"
flatten:
  - %s
`, extras), 0o644))

	tf, err := LoadWithIncludes(dir)
	require.NoError(t, err)

	assert.Contains(t, tf.Tasks, "shared-task")
}

func TestParseFlattenField(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "gogo.yaml"), []byte(`version: "1"
flatten:
  - tasks/a.yml
  - tasks/b.yml
`), 0o644))

	tf, err := Parse(dir)
	require.NoError(t, err)
	assert.Equal(t, []string{"tasks/a.yml", "tasks/b.yml"}, tf.Flatten)
}

func TestParseRejectsUnsafeTaskNames(t *testing.T) {
	// Task names appear in checksum file paths, log lines and shell
	// completions. We allow only letters, digits, '-', '_' and ':'; anything
	// else — path separators, control characters, terminal escapes, shell
	// metacharacters — must be rejected at parse time.
	cases := map[string]string{
		"slash":          "foo/bar",
		"backslash":      `foo\bar`,
		"parent":         "..",
		"traversal":      "../etc/passwd",
		"dotdot-mid":     "foo..bar",
		"space":          "foo bar",
		"newline":        "foo\nbar",
		"tab":            "foo\tbar",
		"escape":         "foo\x1b[31mred",
		"null":           "foo\x00bar",
		"backtick":       "foo`whoami`",
		"dollar":         "$(whoami)",
		"semicolon":      "foo;rm",
		"pipe":           "foo|rm",
		"glob":           "foo*",
		"question":       "foo?",
		"quote":          `foo"bar`,
		"singlequote":    "foo'bar",
		"unicode":        "foo\u202ebar",
		"leading-colon":  ":build",
		"trailing-colon": "build:",
		"double-colon":   "foo::bar",
		"dot":            "foo.bar",
	}
	for label, name := range cases {
		t.Run(label, func(t *testing.T) {
			dir := t.TempDir()
			writeFiles(t, dir, map[string]string{
				"gogo.yaml": fmt.Sprintf("version: \"1\"\ntasks:\n  %q:\n    cmd: echo hi\n", name),
			})

			_, err := Parse(dir)
			require.Error(t, err)
			assert.Contains(t, err.Error(), "task name")
		})
	}
}

func TestParseAcceptsValidTaskNames(t *testing.T) {
	names := []string{
		"build",
		"build-all",
		"build_all",
		"_internal",
		"cli:build",
		"cli:utils:fmt",
		"go121",
		"BuildIt",
	}
	for _, name := range names {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			writeFiles(t, dir, map[string]string{
				"gogo.yaml": fmt.Sprintf("version: \"1\"\ntasks:\n  %q:\n    cmd: echo hi\n", name),
			})

			tf, err := Parse(dir)
			require.NoError(t, err)
			assert.Contains(t, tf.Tasks, name)
		})
	}
}

func TestLoadWithFlattenRejectsUnsafeTaskName(t *testing.T) {
	dir := t.TempDir()
	writeFiles(t, dir, map[string]string{
		"gogo.yaml": `version: "1"
flatten:
  - tasks/extras.yml
`,
		"tasks/extras.yml": `version: "1"
tasks:
  "../escape":
    cmd: echo hi
`,
	})

	_, err := LoadWithIncludes(dir)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "task name")
}
