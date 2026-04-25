package taskfile

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeShellRunner struct {
	mu         sync.Mutex
	runs       []ShellCommand
	outputs    []ShellCommand
	execs      *[]Execution
	runFunc    func(ShellCommand) error
	outputFunc func(ShellCommand) ([]byte, error)
}

func (f *fakeShellRunner) Run(req ShellCommand) error {
	f.mu.Lock()
	f.runs = append(f.runs, cloneShellCommand(req))
	if f.execs != nil && req.Kind == ShellCommandTask {
		*f.execs = append(*f.execs, Execution{
			Task:     req.TaskName,
			Command:  req.Command,
			Dir:      req.Dir,
			Env:      slices.Clone(req.Env),
			UseOpRun: req.UseOpRun,
		})
	}
	f.mu.Unlock()

	if f.runFunc != nil {
		return f.runFunc(req)
	}
	if req.Command == "false" {
		return errors.New("exit status 1")
	}
	return nil
}

func (f *fakeShellRunner) Output(req ShellCommand) ([]byte, error) {
	f.mu.Lock()
	f.outputs = append(f.outputs, cloneShellCommand(req))
	f.mu.Unlock()

	if f.outputFunc != nil {
		return f.outputFunc(req)
	}
	if req.Command == "exit 1" {
		return nil, errors.New("exit status 1")
	}
	return []byte("sh:" + req.Command), nil
}

func cloneShellCommand(req ShellCommand) ShellCommand {
	req.Env = slices.Clone(req.Env)
	return req
}

func (f *fakeShellRunner) runsSnapshot() []ShellCommand {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.runs)
}

func (f *fakeShellRunner) outputsSnapshot() []ShellCommand {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.outputs)
}

func captureExecs(r *Runner) *[]Execution {
	execs := []Execution{}
	if shell, ok := r.ShellRunner.(*fakeShellRunner); ok {
		shell.execs = &execs
		return &execs
	}
	r.ShellRunner = &fakeShellRunner{execs: &execs}
	return &execs
}

func newTestRunner(t *testing.T, tf *Taskfile, dir string) *Runner {
	t.Helper()

	r, err := NewRunner(tf, dir)
	require.NoError(t, err)

	r.BaseEnv = nil
	r.ShellRunner = &fakeShellRunner{}
	return r
}

// envValue returns the last value for key in an env slice, or "" if not found.
func envValue(env []string, key string) string {
	for i := len(env) - 1; i >= 0; i-- {
		if k, v, ok := strings.Cut(env[i], "="); ok && k == key {
			return v
		}
	}
	return ""
}

func TestRunWithExtraVars(t *testing.T) {
	dir := t.TempDir()

	tf := &Taskfile{
		Dir: dir,
		Tasks: map[string]Task{
			"caller": {
				Cmds: []Cmd{
					{Task: "callee", Vars: map[string]Var{
						"MSG": {Value: "from-caller"},
					}},
				},
			},
			"callee": {
				Cmds: []Cmd{
					{Cmd: "printf ${MSG}"},
				},
			},
		},
		DotenvVars: make(map[string]string),
	}

	runner := newTestRunner(t, tf, dir)
	execs := captureExecs(runner)

	err := runner.Run("caller", "")
	require.NoError(t, err)

	require.Len(t, *execs, 1)
	assert.Equal(t, "callee", (*execs)[0].Task)
	assert.Equal(t, "printf from-caller", (*execs)[0].Command)
}

func TestRunWithExtraVarsOverridesTaskVars(t *testing.T) {
	dir := t.TempDir()

	tf := &Taskfile{
		Dir: dir,
		Tasks: map[string]Task{
			"caller": {
				Cmds: []Cmd{
					{Task: "callee", Vars: map[string]Var{
						"MSG": {Value: "overridden"},
					}},
				},
			},
			"callee": {
				Vars: map[string]Var{
					"MSG": {Value: "default"},
				},
				Cmds: []Cmd{
					{Cmd: "printf ${MSG}"},
				},
			},
		},
		DotenvVars: make(map[string]string),
	}

	runner := newTestRunner(t, tf, dir)
	execs := captureExecs(runner)

	err := runner.Run("caller", "")
	require.NoError(t, err)

	require.Len(t, *execs, 1)
	assert.Equal(t, "printf overridden", (*execs)[0].Command)
}

func TestRunWithExtraVarsShell(t *testing.T) {
	dir := t.TempDir()

	tf := &Taskfile{
		Dir: dir,
		Tasks: map[string]Task{
			"caller": {
				Cmds: []Cmd{
					{Task: "callee", Vars: map[string]Var{
						"MSG": {Sh: "echo dynamic"},
					}},
				},
			},
			"callee": {
				Cmds: []Cmd{
					{Cmd: "printf ${MSG}"},
				},
			},
		},
		DotenvVars: make(map[string]string),
	}

	runner := newTestRunner(t, tf, dir)
	execs := captureExecs(runner)

	err := runner.Run("caller", "")
	require.NoError(t, err)

	require.Len(t, *execs, 1)
	assert.Equal(t, "printf sh:echo dynamic", (*execs)[0].Command)
}

func TestRequiresVarsMissing(t *testing.T) {
	dir := t.TempDir()
	tf := &Taskfile{
		Dir: dir,
		Tasks: map[string]Task{
			"deploy": {
				Requires: Requires{Vars: []string{"VERSION"}},
				Cmds:     []Cmd{{Cmd: "echo deploying"}},
			},
		},
		DotenvVars: make(map[string]string),
	}

	runner := newTestRunner(t, tf, dir)
	captureExecs(runner)

	err := runner.Run("deploy", "")
	assert.EqualError(t, err, `task "deploy" requires variable "VERSION" to be set`)
}

func TestRequiresVarsProvided(t *testing.T) {
	dir := t.TempDir()
	tf := &Taskfile{
		Dir: dir,
		Tasks: map[string]Task{
			"deploy": {
				Requires: Requires{Vars: []string{"VERSION"}},
				Vars:     map[string]Var{"VERSION": {Value: "1.0"}},
				Cmds:     []Cmd{{Cmd: "true"}},
			},
		},
		DotenvVars: make(map[string]string),
	}

	runner := newTestRunner(t, tf, dir)
	captureExecs(runner)

	err := runner.Run("deploy", "")
	assert.NoError(t, err)
}

func TestRequiresEnvMissing(t *testing.T) {
	dir := t.TempDir()
	tf := &Taskfile{
		Dir: dir,
		Tasks: map[string]Task{
			"deploy": {
				Requires: Requires{Env: []string{"DEPLOY_TOKEN"}},
				Cmds:     []Cmd{{Cmd: "echo deploying"}},
			},
		},
		DotenvVars: make(map[string]string),
	}

	runner := newTestRunner(t, tf, dir)
	captureExecs(runner)

	err := runner.Run("deploy", "")
	assert.EqualError(t, err, `task "deploy" requires environment variable "DEPLOY_TOKEN" to be set`)
}

func TestRequiresEnvProvided(t *testing.T) {
	dir := t.TempDir()
	tf := &Taskfile{
		Dir: dir,
		Tasks: map[string]Task{
			"deploy": {
				Requires: Requires{Env: []string{"DEPLOY_TOKEN"}},
				Cmds:     []Cmd{{Cmd: "true"}},
			},
		},
		DotenvVars: make(map[string]string),
	}

	t.Setenv("DEPLOY_TOKEN", "secret")
	runner := newTestRunner(t, tf, dir)
	captureExecs(runner)

	err := runner.Run("deploy", "")
	assert.NoError(t, err)
}

func TestRunDeduplicatesDeps(t *testing.T) {
	dir := t.TempDir()

	tf := &Taskfile{
		Dir: dir,
		Tasks: map[string]Task{
			"all": {
				Deps: []Dep{{Task: "a"}, {Task: "b"}},
			},
			"a": {
				Deps: []Dep{{Task: "shared"}},
				Cmds: []Cmd{{Cmd: "echo a"}},
			},
			"b": {
				Deps: []Dep{{Task: "shared"}},
				Cmds: []Cmd{{Cmd: "echo b"}},
			},
			"shared": {
				Cmds: []Cmd{{Cmd: "echo shared"}},
			},
		},
		DotenvVars: make(map[string]string),
	}

	runner := newTestRunner(t, tf, dir)
	execs := captureExecs(runner)

	err := runner.Run("all", "")
	require.NoError(t, err)

	// "shared" should appear exactly once despite being a dep of both a and b
	sharedCount := 0
	for _, e := range *execs {
		if e.Task == "shared" {
			sharedCount++
		}
	}
	assert.Equal(t, 1, sharedCount)
}

func TestMatchesPlatform(t *testing.T) {
	// Empty list matches everything
	assert.True(t, matchesPlatform(nil))
	assert.True(t, matchesPlatform([]string{}))

	// Current OS matches
	assert.True(t, matchesPlatform([]string{runtime.GOOS}))

	// Current OS/ARCH matches
	assert.True(t, matchesPlatform([]string{runtime.GOOS + "/" + runtime.GOARCH}))

	// Wrong OS doesn't match
	assert.False(t, matchesPlatform([]string{"plan9"}))

	// Wrong OS/ARCH doesn't match
	assert.False(t, matchesPlatform([]string{"plan9/mips"}))

	// One matching entry is enough
	assert.True(t, matchesPlatform([]string{"plan9", runtime.GOOS}))
}

func TestPlatformSkipsTask(t *testing.T) {
	dir := t.TempDir()

	tf := &Taskfile{
		Dir: dir,
		Tasks: map[string]Task{
			"build": {
				Platforms: []string{"plan9"},
				Cmds:      []Cmd{{Cmd: "echo build"}},
			},
		},
		DotenvVars: make(map[string]string),
	}

	runner := newTestRunner(t, tf, dir)
	execs := captureExecs(runner)

	err := runner.Run("build", "")
	require.NoError(t, err)
	assert.Empty(t, *execs, "task should have been skipped")
}

func TestWatchRejectsTooSmallInterval(t *testing.T) {
	dir := t.TempDir()
	tf := &Taskfile{
		Dir: dir,
		Tasks: map[string]Task{
			"build": {
				Sources: []string{"*.go"},
				Cmds:    []Cmd{{Cmd: "true"}},
			},
		},
		DotenvVars: make(map[string]string),
	}

	runner := newTestRunner(t, tf, dir)
	err := runner.Watch(t.Context(), "build", "", 0)
	require.EqualError(t, err, "watch interval must be at least 10ms")
}

func TestPlatformRunsTask(t *testing.T) {
	dir := t.TempDir()

	tf := &Taskfile{
		Dir: dir,
		Tasks: map[string]Task{
			"build": {
				Platforms: []string{runtime.GOOS},
				Cmds:      []Cmd{{Cmd: "echo build"}},
			},
		},
		DotenvVars: make(map[string]string),
	}

	runner := newTestRunner(t, tf, dir)
	execs := captureExecs(runner)

	err := runner.Run("build", "")
	require.NoError(t, err)

	require.Len(t, *execs, 1)
	assert.Equal(t, "echo build", (*execs)[0].Command)
}

func TestExecutionOrder(t *testing.T) {
	dir := t.TempDir()

	tf := &Taskfile{
		Dir: dir,
		Tasks: map[string]Task{
			"all": {
				Cmds: []Cmd{
					{Cmd: "echo step1"},
					{Cmd: "echo step2"},
					{Task: "sub"},
					{Cmd: "echo step3"},
				},
			},
			"sub": {
				Cmds: []Cmd{{Cmd: "echo sub-step"}},
			},
		},
		DotenvVars: make(map[string]string),
	}

	runner := newTestRunner(t, tf, dir)
	execs := captureExecs(runner)

	err := runner.Run("all", "")
	require.NoError(t, err)

	require.Len(t, *execs, 4)
	assert.Equal(t, "echo step1", (*execs)[0].Command)
	assert.Equal(t, "echo step2", (*execs)[1].Command)
	assert.Equal(t, "echo sub-step", (*execs)[2].Command)
	assert.Equal(t, "echo step3", (*execs)[3].Command)
}

func TestTaskEnvPassedToExecution(t *testing.T) {
	dir := t.TempDir()

	tf := &Taskfile{
		Dir: dir,
		Tasks: map[string]Task{
			"build": {
				Env:  map[string]string{"GOOS": "linux", "GOARCH": "amd64"},
				Cmds: []Cmd{{Cmd: "go build"}},
			},
		},
		DotenvVars: make(map[string]string),
	}

	runner := newTestRunner(t, tf, dir)
	execs := captureExecs(runner)

	err := runner.Run("build", "")
	require.NoError(t, err)

	require.Len(t, *execs, 1)
	assert.Equal(t, "linux", envValue((*execs)[0].Env, "GOOS"))
	assert.Equal(t, "amd64", envValue((*execs)[0].Env, "GOARCH"))
}

func TestTaskDir(t *testing.T) {
	dir := t.TempDir()
	subdir := filepath.Join(dir, "sub")
	writeFiles(t, dir, map[string]string{"sub/.keep": ""})

	tf := &Taskfile{
		Dir: dir,
		Tasks: map[string]Task{
			"build": {
				Dir:  "sub",
				Cmds: []Cmd{{Cmd: "make"}},
			},
		},
		DotenvVars: make(map[string]string),
	}

	runner := newTestRunner(t, tf, dir)
	execs := captureExecs(runner)

	err := runner.Run("build", "")
	require.NoError(t, err)

	require.Len(t, *execs, 1)
	assert.Equal(t, subdir, (*execs)[0].Dir)
}

func TestOpSecretsDetection(t *testing.T) {
	dir := t.TempDir()

	tf := &Taskfile{
		Dir: dir,
		Tasks: map[string]Task{
			"deploy": {
				Env:  map[string]string{"TOKEN": "op://vault/item/field"},
				Cmds: []Cmd{{Cmd: "deploy"}},
			},
		},
		DotenvVars: make(map[string]string),
	}

	runner := newTestRunner(t, tf, dir)
	execs := captureExecs(runner)

	err := runner.Run("deploy", "")
	require.NoError(t, err)

	require.Len(t, *execs, 1)
	assert.True(t, (*execs)[0].UseOpRun)
}

func TestGlobalVarsInEnv(t *testing.T) {
	dir := t.TempDir()

	tf := &Taskfile{
		Dir:  dir,
		Vars: map[string]Var{"VERSION": {Value: "1.2.3"}},
		Tasks: map[string]Task{
			"build": {
				Cmds: []Cmd{{Cmd: "echo ${VERSION}"}},
			},
		},
		DotenvVars: make(map[string]string),
	}

	runner := newTestRunner(t, tf, dir)
	execs := captureExecs(runner)

	err := runner.Run("build", "")
	require.NoError(t, err)

	require.Len(t, *execs, 1)
	assert.Equal(t, "echo 1.2.3", (*execs)[0].Command)
	assert.Equal(t, "1.2.3", envValue((*execs)[0].Env, "VERSION"))
}

func TestTaskNotFound(t *testing.T) {
	dir := t.TempDir()
	tf := &Taskfile{
		Dir:        dir,
		Tasks:      map[string]Task{},
		DotenvVars: make(map[string]string),
	}

	runner := newTestRunner(t, tf, dir)
	captureExecs(runner)

	err := runner.Run("nonexistent", "")
	assert.EqualError(t, err, `task "nonexistent" not found`)
}

func TestAliasCollisionDetected(t *testing.T) {
	dir := t.TempDir()
	tf := &Taskfile{
		Dir: dir,
		Tasks: map[string]Task{
			"cli:github":    {Aliases: StringList{"gh"}, Cmds: []Cmd{{Cmd: "gh cli"}}},
			"server:github": {Aliases: StringList{"gh"}, Cmds: []Cmd{{Cmd: "gh server"}}},
		},
		DotenvVars: make(map[string]string),
	}

	_, err := NewRunner(tf, dir)
	require.EqualError(t, err, `alias "gh" is defined by both "cli:github" and "server:github"`)
}

func TestAliasResolution(t *testing.T) {
	dir := t.TempDir()
	tf := &Taskfile{
		Dir: dir,
		Tasks: map[string]Task{
			"github": {
				Aliases: StringList{"gh"},
				Cmds:    []Cmd{{Cmd: "gh auth status"}},
			},
		},
		DotenvVars: make(map[string]string),
	}

	runner := newTestRunner(t, tf, dir)
	execs := captureExecs(runner)

	err := runner.Run("gh", "")
	require.NoError(t, err)

	require.Len(t, *execs, 1)
	assert.Equal(t, "github", (*execs)[0].Task)
	assert.Equal(t, "gh auth status", (*execs)[0].Command)
}

func TestNamespaceResolution(t *testing.T) {
	dir := t.TempDir()
	writeFiles(t, dir, map[string]string{"cli/.keep": ""})
	cliDir := filepath.Join(dir, "cli")

	tf := &Taskfile{
		Dir: dir,
		Tasks: map[string]Task{
			"cli:build": {
				Dir:  cliDir,
				Cmds: []Cmd{{Cmd: "go build"}},
			},
		},
		Namespaces: map[string]string{cliDir: "cli"},
		DotenvVars: make(map[string]string),
	}

	runner := newTestRunner(t, tf, cliDir)
	execs := captureExecs(runner)

	err := runner.Run("build", "")
	require.NoError(t, err)

	require.Len(t, *execs, 1)
	assert.Equal(t, "cli:build", (*execs)[0].Task)
}

func TestDryRunSkipsExecution(t *testing.T) {
	dir := t.TempDir()
	tf := &Taskfile{
		Dir: dir,
		Tasks: map[string]Task{
			"build": {
				Cmds: []Cmd{{Cmd: "go build"}, {Cmd: "go install"}},
			},
		},
		DotenvVars: make(map[string]string),
	}

	runner := newTestRunner(t, tf, dir)
	runner.DryRun = true
	execs := captureExecs(runner)

	err := runner.Run("build", "")
	require.NoError(t, err)
	assert.Empty(t, *execs)
}

func TestCLIArgsExpansion(t *testing.T) {
	dir := t.TempDir()
	tf := &Taskfile{
		Dir: dir,
		Tasks: map[string]Task{
			"test": {
				Cmds: []Cmd{{Cmd: "go test ${CLI_ARGS}"}},
			},
		},
		DotenvVars: make(map[string]string),
	}

	runner := newTestRunner(t, tf, dir)
	execs := captureExecs(runner)

	err := runner.Run("test", "-v -run TestFoo")
	require.NoError(t, err)

	require.Len(t, *execs, 1)
	assert.Equal(t, "go test -v -run TestFoo", (*execs)[0].Command)
}

func TestTaskfileDirVar(t *testing.T) {
	dir := t.TempDir()
	tf := &Taskfile{
		Dir: dir,
		Tasks: map[string]Task{
			"show": {
				Cmds: []Cmd{{Cmd: "echo ${TASKFILE_DIR}"}},
			},
		},
		DotenvVars: make(map[string]string),
	}

	runner := newTestRunner(t, tf, dir)
	execs := captureExecs(runner)

	err := runner.Run("show", "")
	require.NoError(t, err)

	require.Len(t, *execs, 1)
	assert.Equal(t, "echo "+dir, (*execs)[0].Command)
	assert.Equal(t, dir, envValue((*execs)[0].Env, "TASKFILE_DIR"))
}

func TestTaskVarsOverrideGlobalVars(t *testing.T) {
	dir := t.TempDir()
	tf := &Taskfile{
		Dir:  dir,
		Vars: map[string]Var{"MODE": {Value: "global"}},
		Tasks: map[string]Task{
			"build": {
				Vars: map[string]Var{"MODE": {Value: "local"}},
				Cmds: []Cmd{{Cmd: "echo ${MODE}"}},
			},
		},
		DotenvVars: make(map[string]string),
	}

	runner := newTestRunner(t, tf, dir)
	execs := captureExecs(runner)

	err := runner.Run("build", "")
	require.NoError(t, err)

	require.Len(t, *execs, 1)
	assert.Equal(t, "echo local", (*execs)[0].Command)
}

func TestEnvExpansionReferencesVars(t *testing.T) {
	dir := t.TempDir()
	tf := &Taskfile{
		Dir: dir,
		Tasks: map[string]Task{
			"build": {
				Vars: map[string]Var{"BASE": {Value: "/opt"}},
				Env:  map[string]string{"MY_PATH": "${BASE}/bin"},
				Cmds: []Cmd{{Cmd: "run"}},
			},
		},
		DotenvVars: make(map[string]string),
	}

	runner := newTestRunner(t, tf, dir)
	execs := captureExecs(runner)

	err := runner.Run("build", "")
	require.NoError(t, err)

	require.Len(t, *execs, 1)
	assert.Equal(t, "/opt/bin", envValue((*execs)[0].Env, "MY_PATH"))
}

func TestResetRanAllowsRerun(t *testing.T) {
	dir := t.TempDir()
	tf := &Taskfile{
		Dir: dir,
		Tasks: map[string]Task{
			"build": {
				Cmds: []Cmd{{Cmd: "go build"}},
			},
		},
		DotenvVars: make(map[string]string),
	}

	runner := newTestRunner(t, tf, dir)
	execs := captureExecs(runner)

	require.NoError(t, runner.Run("build", ""))
	require.Len(t, *execs, 1)

	// Second run is deduplicated
	require.NoError(t, runner.Run("build", ""))
	require.Len(t, *execs, 1)

	// After reset, runs again
	runner.ResetRan()
	require.NoError(t, runner.Run("build", ""))
	assert.Len(t, *execs, 2)
}

func TestCommandFailureStopsExecution(t *testing.T) {
	dir := t.TempDir()
	tf := &Taskfile{
		Dir: dir,
		Tasks: map[string]Task{
			"build": {
				Cmds: []Cmd{
					{Cmd: "echo step1"},
					{Cmd: "echo step2"},
					{Cmd: "echo step3"},
				},
			},
		},
		DotenvVars: make(map[string]string),
	}

	runner := newTestRunner(t, tf, dir)
	boom := errors.New("boom")
	var execs []string
	shell := &fakeShellRunner{
		runFunc: func(req ShellCommand) error {
			execs = append(execs, req.Command)
			if req.Command == "echo step2" {
				return boom
			}
			return nil
		},
	}
	runner.ShellRunner = shell

	err := runner.Run("build", "")
	require.ErrorIs(t, err, boom)
	assert.Equal(t, []string{"echo step1", "echo step2"}, execs)
}

func TestDepFailurePreventsTask(t *testing.T) {
	dir := t.TempDir()
	tf := &Taskfile{
		Dir: dir,
		Tasks: map[string]Task{
			"all": {
				Deps: []Dep{{Task: "failing"}},
				Cmds: []Cmd{{Cmd: "echo should-not-run"}},
			},
			"failing": {
				Cmds: []Cmd{{Cmd: "exit 1"}},
			},
		},
		DotenvVars: make(map[string]string),
	}

	runner := newTestRunner(t, tf, dir)
	boom := errors.New("dep failed")
	var execs []string
	shell := &fakeShellRunner{
		runFunc: func(req ShellCommand) error {
			execs = append(execs, req.TaskName+":"+req.Command)
			if req.TaskName == "failing" {
				return boom
			}
			return nil
		},
	}
	runner.ShellRunner = shell

	err := runner.Run("all", "")
	require.ErrorIs(t, err, boom)
	assert.Equal(t, []string{"failing:exit 1"}, execs)
}

func TestTaskWithOnlyDeps(t *testing.T) {
	dir := t.TempDir()
	tf := &Taskfile{
		Dir: dir,
		Tasks: map[string]Task{
			"all": {
				Deps: []Dep{{Task: "a"}, {Task: "b"}},
			},
			"a": {Cmds: []Cmd{{Cmd: "echo a"}}},
			"b": {Cmds: []Cmd{{Cmd: "echo b"}}},
		},
		DotenvVars: make(map[string]string),
	}

	runner := newTestRunner(t, tf, dir)
	execs := captureExecs(runner)

	err := runner.Run("all", "")
	require.NoError(t, err)

	// Only deps run, "all" has no cmds
	assert.Len(t, *execs, 2)
	tasks := []string{(*execs)[0].Task, (*execs)[1].Task}
	assert.ElementsMatch(t, []string{"a", "b"}, tasks)
}

func TestExpandVarsTemplateAndShell(t *testing.T) {
	vars := map[string]string{"NAME": "world"}

	assert.Equal(t, "hello world", expandVars("hello ${NAME}", vars, ""))
	assert.Equal(t, "hello world", expandVars("hello {{.NAME}}", vars, ""))
	assert.Equal(t, "hello ${UNKNOWN}", expandVars("hello ${UNKNOWN}", vars, ""))
	assert.Equal(t, "hello {{.UNKNOWN}}", expandVars("hello {{.UNKNOWN}}", vars, ""))
}

func TestExpandVarsCLIArgs(t *testing.T) {
	assert.Equal(t, "test -v", expandVars("test ${CLI_ARGS}", nil, "-v"))
}

func TestNoOpSecretsUseOpRunFalse(t *testing.T) {
	dir := t.TempDir()
	tf := &Taskfile{
		Dir: dir,
		Tasks: map[string]Task{
			"build": {
				Env:  map[string]string{"FOO": "bar"},
				Cmds: []Cmd{{Cmd: "go build"}},
			},
		},
		DotenvVars: make(map[string]string),
	}

	runner := newTestRunner(t, tf, dir)
	execs := captureExecs(runner)

	err := runner.Run("build", "")
	require.NoError(t, err)

	require.Len(t, *execs, 1)
	assert.False(t, (*execs)[0].UseOpRun)
}

func TestDotenvVarsInBaseEnv(t *testing.T) {
	dir := t.TempDir()
	tf := &Taskfile{
		Dir:        dir,
		Tasks:      map[string]Task{"build": {Cmds: []Cmd{{Cmd: "go build"}}}},
		DotenvVars: map[string]string{"DB_HOST": "localhost", "DB_PORT": "5432"},
	}

	runner := newTestRunner(t, tf, dir)
	// Set BaseEnv to only dotenv vars for a clean test
	runner.BaseEnv = []string{"DB_HOST=localhost", "DB_PORT=5432"}
	execs := captureExecs(runner)

	err := runner.Run("build", "")
	require.NoError(t, err)

	require.Len(t, *execs, 1)
	assert.Equal(t, "localhost", envValue((*execs)[0].Env, "DB_HOST"))
	assert.Equal(t, "5432", envValue((*execs)[0].Env, "DB_PORT"))
}

func TestDedupDoesNotApplyWithExtraVars(t *testing.T) {
	dir := t.TempDir()
	tf := &Taskfile{
		Dir: dir,
		Tasks: map[string]Task{
			"caller": {
				Cmds: []Cmd{
					{Task: "greet", Vars: map[string]Var{"MSG": {Value: "hello"}}},
					{Task: "greet", Vars: map[string]Var{"MSG": {Value: "goodbye"}}},
				},
			},
			"greet": {
				Cmds: []Cmd{{Cmd: "echo ${MSG}"}},
			},
		},
		DotenvVars: make(map[string]string),
	}

	runner := newTestRunner(t, tf, dir)
	execs := captureExecs(runner)

	err := runner.Run("caller", "")
	require.NoError(t, err)

	require.Len(t, *execs, 2)
	assert.Equal(t, "echo hello", (*execs)[0].Command)
	assert.Equal(t, "echo goodbye", (*execs)[1].Command)
}

func TestDefaultDirIsTaskfileDir(t *testing.T) {
	dir := t.TempDir()
	tf := &Taskfile{
		Dir: dir,
		Tasks: map[string]Task{
			"build": {Cmds: []Cmd{{Cmd: "make"}}},
		},
		DotenvVars: make(map[string]string),
	}

	runner := newTestRunner(t, tf, dir)
	execs := captureExecs(runner)

	require.NoError(t, runner.Run("build", ""))

	require.Len(t, *execs, 1)
	assert.Equal(t, dir, (*execs)[0].Dir)
}

func TestSourcesChecksumSkipsUpToDate(t *testing.T) {
	dir := t.TempDir()
	writeFiles(t, dir, map[string]string{"main.go": "package main"})

	tf := &Taskfile{
		Dir: dir,
		Tasks: map[string]Task{
			"build": {
				Sources: StringList{"*.go"},
				Cmds:    []Cmd{{Cmd: "go build"}},
			},
		},
		DotenvVars: make(map[string]string),
	}

	runner := newTestRunner(t, tf, dir)
	execs := captureExecs(runner)

	// First run executes
	require.NoError(t, runner.Run("build", ""))
	require.Len(t, *execs, 1)

	// Second run (after ResetRan) is skipped because checksum matches
	runner.ResetRan()
	require.NoError(t, runner.Run("build", ""))
	assert.Len(t, *execs, 1)
}

func TestSourcesChecksumRerunsOnChange(t *testing.T) {
	dir := t.TempDir()
	writeFiles(t, dir, map[string]string{"main.go": "package main"})

	tf := &Taskfile{
		Dir: dir,
		Tasks: map[string]Task{
			"build": {
				Sources: StringList{"*.go"},
				Cmds:    []Cmd{{Cmd: "go build"}},
			},
		},
		DotenvVars: make(map[string]string),
	}

	runner := newTestRunner(t, tf, dir)
	execs := captureExecs(runner)

	require.NoError(t, runner.Run("build", ""))
	require.Len(t, *execs, 1)

	// Change source and re-run
	writeFiles(t, dir, map[string]string{"main.go": "package main // changed"})
	runner.ResetRan()
	require.NoError(t, runner.Run("build", ""))
	assert.Len(t, *execs, 2)
}

func TestForceIgnoresSources(t *testing.T) {
	dir := t.TempDir()
	writeFiles(t, dir, map[string]string{"main.go": "package main"})

	tf := &Taskfile{
		Dir: dir,
		Tasks: map[string]Task{
			"build": {
				Sources: StringList{"*.go"},
				Cmds:    []Cmd{{Cmd: "go build"}},
			},
		},
		DotenvVars: make(map[string]string),
	}

	runner := newTestRunner(t, tf, dir)
	runner.Force = true
	execs := captureExecs(runner)

	require.NoError(t, runner.Run("build", ""))
	runner.ResetRan()
	require.NoError(t, runner.Run("build", ""))
	assert.Len(t, *execs, 2)
}

func TestGeneratesSkipsUpToDate(t *testing.T) {
	dir := t.TempDir()
	writeFiles(t, dir, map[string]string{
		"main.go": "package main",
		"main":    "binary",
	})
	sourceTime := time.Now().Add(-time.Hour)
	require.NoError(t, os.Chtimes(filepath.Join(dir, "main.go"), sourceTime, sourceTime))
	outputTime := time.Now().Add(time.Hour)
	require.NoError(t, os.Chtimes(filepath.Join(dir, "main"), outputTime, outputTime))

	tf := &Taskfile{
		Dir: dir,
		Tasks: map[string]Task{
			"build": {
				Sources:   StringList{"*.go"},
				Generates: StringList{"main"},
				Cmds:      []Cmd{{Cmd: "go build"}},
			},
		},
		DotenvVars: make(map[string]string),
	}

	runner := newTestRunner(t, tf, dir)
	execs := captureExecs(runner)

	require.NoError(t, runner.Run("build", ""))
	assert.Empty(t, *execs)
}

func TestMultipleDepFailures(t *testing.T) {
	dir := t.TempDir()
	tf := &Taskfile{
		Dir: dir,
		Tasks: map[string]Task{
			"all": {
				Deps: []Dep{{Task: "fail1"}, {Task: "fail2"}},
			},
			"fail1": {Cmds: []Cmd{{Cmd: "cmd1"}}},
			"fail2": {Cmds: []Cmd{{Cmd: "cmd2"}}},
		},
		DotenvVars: make(map[string]string),
	}

	err1 := errors.New("fail1 error")
	err2 := errors.New("fail2 error")

	runner := newTestRunner(t, tf, dir)
	shell := &fakeShellRunner{
		runFunc: func(req ShellCommand) error {
			switch req.TaskName {
			case "fail1":
				return err1
			case "fail2":
				return err2
			default:
				return nil
			}
		},
	}
	runner.ShellRunner = shell

	err := runner.Run("all", "")
	require.Error(t, err)
	// Both errors should be joined
	require.ErrorIs(t, err, err1)
	require.ErrorIs(t, err, err2)
}

func TestAbsoluteTaskDir(t *testing.T) {
	dir := t.TempDir()
	writeFiles(t, dir, map[string]string{"absolute/.keep": ""})
	absDir := filepath.Join(dir, "absolute")

	tf := &Taskfile{
		Dir: dir,
		Tasks: map[string]Task{
			"build": {
				Dir:  absDir,
				Cmds: []Cmd{{Cmd: "make"}},
			},
		},
		DotenvVars: make(map[string]string),
	}

	runner := newTestRunner(t, tf, dir)
	execs := captureExecs(runner)

	require.NoError(t, runner.Run("build", ""))

	require.Len(t, *execs, 1)
	assert.Equal(t, absDir, (*execs)[0].Dir)
}

func TestBuildEnvNoDuplicateKeys(t *testing.T) {
	dir := t.TempDir()
	tf := &Taskfile{
		Dir:  dir,
		Vars: map[string]Var{"FOO": {Value: "from-var"}},
		Tasks: map[string]Task{
			"build": {
				Env:  map[string]string{"FOO": "from-env"},
				Cmds: []Cmd{{Cmd: "run"}},
			},
		},
		DotenvVars: make(map[string]string),
	}

	runner := newTestRunner(t, tf, dir)
	runner.BaseEnv = []string{"FOO=from-base"}
	execs := captureExecs(runner)

	require.NoError(t, runner.Run("build", ""))

	require.Len(t, *execs, 1)
	count := 0
	for _, e := range (*execs)[0].Env {
		if strings.HasPrefix(e, "FOO=") {
			count++
		}
	}
	assert.Equal(t, 1, count, "FOO should appear exactly once in env")
	assert.Equal(t, "from-env", envValue((*execs)[0].Env, "FOO"))
}

func TestEnvPrecedenceOrder(t *testing.T) {
	dir := t.TempDir()
	tf := &Taskfile{
		Dir:  dir,
		Vars: map[string]Var{"FOO": {Value: "from-var"}},
		Tasks: map[string]Task{
			"build": {
				Env:  map[string]string{"FOO": "from-env"},
				Cmds: []Cmd{{Cmd: "run"}},
			},
		},
		DotenvVars: make(map[string]string),
	}

	runner := newTestRunner(t, tf, dir)
	runner.BaseEnv = []string{"FOO=from-base"}
	execs := captureExecs(runner)

	require.NoError(t, runner.Run("build", ""))

	require.Len(t, *execs, 1)
	// Task.Env is appended last, so envValue (which reads last match) returns it
	assert.Equal(t, "from-env", envValue((*execs)[0].Env, "FOO"))
}

func TestTemplateVarsThroughRunner(t *testing.T) {
	dir := t.TempDir()
	tf := &Taskfile{
		Dir: dir,
		Tasks: map[string]Task{
			"greet": {
				Vars: map[string]Var{"NAME": {Value: "world"}},
				Cmds: []Cmd{{Cmd: "echo {{.NAME}}"}},
			},
		},
		DotenvVars: make(map[string]string),
	}

	runner := newTestRunner(t, tf, dir)
	execs := captureExecs(runner)

	require.NoError(t, runner.Run("greet", ""))

	require.Len(t, *execs, 1)
	assert.Equal(t, "echo world", (*execs)[0].Command)
}

func TestMixedTemplateAndShellExpansion(t *testing.T) {
	vars := map[string]string{"A": "1", "B": "2"}
	result := expandVars("{{.A}} and ${B}", vars, "")
	assert.Equal(t, "1 and 2", result)
}

func TestTaskLevelDotenvThroughRunner(t *testing.T) {
	dir := t.TempDir()
	writeFiles(t, dir, map[string]string{".env.task": "TASK_SECRET=abc\n"})

	tf := &Taskfile{
		Dir: dir,
		Tasks: map[string]Task{
			"deploy": {
				Dotenv: []string{".env.task"},
				Cmds:   []Cmd{{Cmd: "deploy"}},
			},
		},
		DotenvVars: make(map[string]string),
	}

	runner := newTestRunner(t, tf, dir)
	execs := captureExecs(runner)

	require.NoError(t, runner.Run("deploy", ""))

	require.Len(t, *execs, 1)
	assert.Equal(t, "abc", envValue((*execs)[0].Env, "TASK_SECRET"))
}

func TestNamespaceResolutionPicksMostSpecific(t *testing.T) {
	dir := t.TempDir()
	writeFiles(t, dir, map[string]string{"cli/.keep": "", "cli/utils/.keep": ""})
	cliDir := filepath.Join(dir, "cli")
	cliUtilsDir := filepath.Join(dir, "cli", "utils")

	tf := &Taskfile{
		Dir: dir,
		Tasks: map[string]Task{
			"cli:build":       {Dir: cliDir, Cmds: []Cmd{{Cmd: "go build cli"}}},
			"cli:utils:build": {Dir: cliUtilsDir, Cmds: []Cmd{{Cmd: "go build utils"}}},
		},
		Namespaces: map[string]string{cliDir: "cli", cliUtilsDir: "cli:utils"},
		DotenvVars: make(map[string]string),
	}

	runner := newTestRunner(t, tf, cliUtilsDir)
	execs := captureExecs(runner)

	err := runner.Run("build", "")
	require.NoError(t, err)

	require.Len(t, *execs, 1)
	assert.Equal(t, "cli:utils:build", (*execs)[0].Task, "should resolve to deepest namespace")
}

func TestNamespaceResolutionSelfPrefix(t *testing.T) {
	// When running a sub-Taskfile as its own root, a task name qualified with
	// the directory's basename (e.g. "proxy:deploy-prod" from inside proxy/)
	// should resolve to the bare task ("deploy-prod"). This matches the name
	// the task has when the parent Taskfile is the root.
	dir := t.TempDir()
	writeFiles(t, dir, map[string]string{"proxy/.keep": ""})
	proxyDir := filepath.Join(dir, "proxy")

	tf := &Taskfile{
		Dir: proxyDir,
		Tasks: map[string]Task{
			"deploy-prod": {Cmds: []Cmd{{Cmd: "deploy"}}},
		},
		DotenvVars: make(map[string]string),
	}

	runner := newTestRunner(t, tf, proxyDir)
	execs := captureExecs(runner)

	err := runner.Run("proxy:deploy-prod", "")
	require.NoError(t, err)

	require.Len(t, *execs, 1)
	assert.Equal(t, "deploy-prod", (*execs)[0].Task)
	assert.Equal(t, "deploy", (*execs)[0].Command)
}

func TestNamespaceResolutionMiss(t *testing.T) {
	dir := t.TempDir()
	writeFiles(t, dir, map[string]string{"cli/.keep": "", "other/.keep": ""})
	cliDir := filepath.Join(dir, "cli")
	otherDir := filepath.Join(dir, "other")

	tf := &Taskfile{
		Dir: dir,
		Tasks: map[string]Task{
			"cli:build": {
				Dir:  cliDir,
				Cmds: []Cmd{{Cmd: "go build"}},
			},
		},
		Namespaces: map[string]string{cliDir: "cli"},
		DotenvVars: make(map[string]string),
	}

	// cwd is "other" which doesn't match the "cli" namespace
	runner := newTestRunner(t, tf, otherDir)
	captureExecs(runner)

	err := runner.Run("build", "")
	assert.EqualError(t, err, `task "build" not found`)
}

func TestCLIArgsTemplateExpansion(t *testing.T) {
	result := expandVars("test {{.CLI_ARGS}}", nil, "-v")
	assert.Equal(t, "test -v", result)
}

func TestHasOpSecrets(t *testing.T) {
	assert.False(t, hasOpSecrets(nil))
	assert.False(t, hasOpSecrets([]string{"FOO=bar"}))
	assert.True(t, hasOpSecrets([]string{"TOKEN=op://vault/item/field"}))
	assert.True(t, hasOpSecrets([]string{"FOO=bar", "TOKEN=op://vault/item/field"}))
}

func TestOpSecretsInDotenvTriggerOpRun(t *testing.T) {
	dir := t.TempDir()
	writeFiles(t, dir, map[string]string{".env.task": "TOKEN=op://vault/item/field\n"})

	tf := &Taskfile{
		Dir: dir,
		Tasks: map[string]Task{
			"deploy": {
				Dotenv: []string{".env.task"},
				Cmds:   []Cmd{{Cmd: "deploy"}},
			},
		},
		DotenvVars: make(map[string]string),
	}

	runner := newTestRunner(t, tf, dir)
	execs := captureExecs(runner)

	require.NoError(t, runner.Run("deploy", ""))

	require.Len(t, *execs, 1)
	assert.True(t, (*execs)[0].UseOpRun, "op:// references in dotenv must wrap execution in op run")
}

func TestMatchesPlatformArchOnly(t *testing.T) {
	assert.True(t, matchesPlatform([]string{runtime.GOARCH}))
	assert.False(t, matchesPlatform([]string{"mips"}))
}

func TestExpandVarsFromEnv(t *testing.T) {
	t.Setenv("GOGO_TEST_VAR", "from-env")
	result := expandVars("echo ${GOGO_TEST_VAR}", nil, "")
	assert.Equal(t, "echo from-env", result)
}

func TestExpandVarsTemplateFromEnv(t *testing.T) {
	t.Setenv("GOGO_TEST_VAR", "from-env")
	result := expandVars("echo {{.GOGO_TEST_VAR}}", nil, "")
	assert.Equal(t, "echo from-env", result)
}

func TestInlineTaskCallFailure(t *testing.T) {
	dir := t.TempDir()
	tf := &Taskfile{
		Dir: dir,
		Tasks: map[string]Task{
			"parent": {
				Cmds: []Cmd{
					{Cmd: "echo before"},
					{Task: "child"},
					{Cmd: "echo after"},
				},
			},
			"child": {
				Cmds: []Cmd{{Cmd: "fail"}},
			},
		},
		DotenvVars: make(map[string]string),
	}

	boom := errors.New("child failed")
	runner := newTestRunner(t, tf, dir)
	var execs []string
	shell := &fakeShellRunner{
		runFunc: func(req ShellCommand) error {
			execs = append(execs, req.TaskName+":"+req.Command)
			if req.TaskName == "child" {
				return boom
			}
			return nil
		},
	}
	runner.ShellRunner = shell

	err := runner.Run("parent", "")
	require.ErrorIs(t, err, boom)
	assert.Equal(t, []string{"parent:echo before", "child:fail"}, execs)
}

func TestBuildEnvDotenvError(t *testing.T) {
	dir := t.TempDir()
	// Create an invalid dotenv file
	writeFiles(t, dir, map[string]string{".env.bad": "BAD-KEY=value\n"})

	tf := &Taskfile{
		Dir: dir,
		Tasks: map[string]Task{
			"build": {
				Dotenv: []string{".env.bad"},
				Cmds:   []Cmd{{Cmd: "go build"}},
			},
		},
		DotenvVars: make(map[string]string),
	}

	runner := newTestRunner(t, tf, dir)
	captureExecs(runner)

	err := runner.Run("build", "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "loading task dotenv")
}

func TestDedupPropagatesErrorToWaitingGoroutine(t *testing.T) {
	dir := t.TempDir()
	tf := &Taskfile{
		Dir: dir,
		Tasks: map[string]Task{
			"all": {
				Deps: []Dep{{Task: "a"}, {Task: "b"}},
			},
			"a": {
				Deps: []Dep{{Task: "shared"}},
				Cmds: []Cmd{{Cmd: "echo a"}},
			},
			"b": {
				Deps: []Dep{{Task: "shared"}},
				Cmds: []Cmd{{Cmd: "echo b"}},
			},
			"shared": {
				Cmds: []Cmd{{Cmd: "fail"}},
			},
		},
		DotenvVars: make(map[string]string),
	}

	boom := errors.New("shared failed")
	runner := newTestRunner(t, tf, dir)
	shell := &fakeShellRunner{
		runFunc: func(req ShellCommand) error {
			if req.TaskName == "shared" {
				return boom
			}
			return nil
		},
	}
	runner.ShellRunner = shell

	err := runner.Run("all", "")
	require.Error(t, err)
	// Both a and b should get the error from shared
	assert.ErrorIs(t, err, boom)
}

func TestTaskDotenvDoesNotOverrideGlobalDotenv(t *testing.T) {
	dir := t.TempDir()
	writeFiles(t, dir, map[string]string{
		".env":      "SHARED_KEY=from-global\n",
		".env.task": "SHARED_KEY=from-task\nTASK_ONLY=task-value\n",
	})

	tf := &Taskfile{
		Dir: dir,
		Tasks: map[string]Task{
			"build": {
				Dotenv: []string{".env.task"},
				Cmds:   []Cmd{{Cmd: "run"}},
			},
		},
		DotenvVars: map[string]string{"SHARED_KEY": "from-global"},
	}

	runner := newTestRunner(t, tf, dir)
	runner.BaseEnv = []string{"SHARED_KEY=from-global"}
	execs := captureExecs(runner)

	require.NoError(t, runner.Run("build", ""))
	require.Len(t, *execs, 1)

	// SHARED_KEY should appear exactly once (from global dotenv)
	count := 0
	for _, e := range (*execs)[0].Env {
		if strings.HasPrefix(e, "SHARED_KEY=") {
			count++
		}
	}
	assert.Equal(t, 1, count, "SHARED_KEY should not be duplicated")
	assert.Equal(t, "from-global", envValue((*execs)[0].Env, "SHARED_KEY"))
	assert.Equal(t, "task-value", envValue((*execs)[0].Env, "TASK_ONLY"))
}

func TestTaskDotenvDoesNotOverrideOSEnv(t *testing.T) {
	dir := t.TempDir()
	writeFiles(t, dir, map[string]string{".env": "MY_VAR=from-dotenv\n"})

	tf := &Taskfile{
		Dir: dir,
		Tasks: map[string]Task{
			"build": {
				Dotenv: []string{".env"},
				Cmds:   []Cmd{{Cmd: "run"}},
			},
		},
		DotenvVars: make(map[string]string),
	}

	// Set OS env var — dotenv should NOT override it
	t.Setenv("MY_VAR", "from-os")

	runner := newTestRunner(t, tf, dir)
	runner.BaseEnv = []string{"MY_VAR=from-os"}
	execs := captureExecs(runner)

	require.NoError(t, runner.Run("build", ""))

	require.Len(t, *execs, 1)
	// MY_VAR should NOT contain "from-dotenv" since OS env takes precedence
	assert.NotEqual(t, "from-dotenv", envValue((*execs)[0].Env, "MY_VAR"))
}

func TestDryRunWithTaskReference(t *testing.T) {
	dir := t.TempDir()
	tf := &Taskfile{
		Dir: dir,
		Tasks: map[string]Task{
			"parent": {
				Cmds: []Cmd{
					{Cmd: "echo before"},
					{Task: "child"},
					{Cmd: "echo after"},
				},
			},
			"child": {
				Cmds: []Cmd{{Cmd: "echo child"}},
			},
		},
		DotenvVars: make(map[string]string),
	}

	runner := newTestRunner(t, tf, dir)
	runner.DryRun = true
	execs := captureExecs(runner)

	require.NoError(t, runner.Run("parent", ""))
	// DryRun skips Cmd execution but task references still run (they go through Run)
	assert.Empty(t, *execs)
}

func TestRequiresBothVarsAndEnv(t *testing.T) {
	dir := t.TempDir()
	tf := &Taskfile{
		Dir: dir,
		Tasks: map[string]Task{
			"deploy": {
				Requires: Requires{
					Vars: []string{"VERSION"},
					Env:  []string{"TOKEN"},
				},
				Vars: map[string]Var{"VERSION": {Value: "1.0"}},
				Cmds: []Cmd{{Cmd: "deploy"}},
			},
		},
		DotenvVars: make(map[string]string),
	}

	t.Setenv("TOKEN", "secret")
	runner := newTestRunner(t, tf, dir)
	execs := captureExecs(runner)

	require.NoError(t, runner.Run("deploy", ""))
	assert.Len(t, *execs, 1)
}

func TestEnvEntriesReferenceEachOther(t *testing.T) {
	dir := t.TempDir()
	tf := &Taskfile{
		Dir: dir,
		Tasks: map[string]Task{
			"build": {
				Env:  map[string]string{"BASE": "/opt", "BIN": "${BASE}/bin"},
				Cmds: []Cmd{{Cmd: "run"}},
			},
		},
		DotenvVars: make(map[string]string),
	}

	runner := newTestRunner(t, tf, dir)
	execs := captureExecs(runner)

	require.NoError(t, runner.Run("build", ""))

	require.Len(t, *execs, 1)
	assert.Equal(t, "/opt/bin", envValue((*execs)[0].Env, "BIN"))
}

func TestEnvReverseAlphabeticalReference(t *testing.T) {
	dir := t.TempDir()
	tf := &Taskfile{
		Dir: dir,
		Tasks: map[string]Task{
			"build": {
				Env:  map[string]string{"Z_ROOT": "/opt", "A_PATH": "${Z_ROOT}/bin"},
				Cmds: []Cmd{{Cmd: "run"}},
			},
		},
		DotenvVars: make(map[string]string),
	}

	runner := newTestRunner(t, tf, dir)
	execs := captureExecs(runner)

	require.NoError(t, runner.Run("build", ""))

	require.Len(t, *execs, 1)
	assert.Equal(t, "/opt/bin", envValue((*execs)[0].Env, "A_PATH"))
	assert.Equal(t, "/opt", envValue((*execs)[0].Env, "Z_ROOT"))
}

func TestEnvMutualRecursionDoesNotOverflow(t *testing.T) {
	dir := t.TempDir()
	tf := &Taskfile{
		Dir: dir,
		Tasks: map[string]Task{
			"build": {
				Env:  map[string]string{"A": "${B}", "B": "${A}"},
				Cmds: []Cmd{{Cmd: "run"}},
			},
		},
		DotenvVars: make(map[string]string),
	}

	runner := newTestRunner(t, tf, dir)
	execs := captureExecs(runner)

	require.NoError(t, runner.Run("build", ""))

	require.Len(t, *execs, 1)
	assert.Empty(t, envValue((*execs)[0].Env, "A"))
	assert.Empty(t, envValue((*execs)[0].Env, "B"))
}

func TestEnvExpandsFromOSEnv(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("GOGO_BASE", "/opt")

	tf := &Taskfile{
		Dir: dir,
		Tasks: map[string]Task{
			"build": {
				Env:  map[string]string{"FULL_PATH": "${GOGO_BASE}/bin"},
				Cmds: []Cmd{{Cmd: "run"}},
			},
		},
		DotenvVars: make(map[string]string),
	}

	runner := newTestRunner(t, tf, dir)
	execs := captureExecs(runner)

	require.NoError(t, runner.Run("build", ""))

	require.Len(t, *execs, 1)
	assert.Equal(t, "/opt/bin", envValue((*execs)[0].Env, "FULL_PATH"))
}

func TestPreconditionSeesTaskEnv(t *testing.T) {
	dir := t.TempDir()
	tf := &Taskfile{
		Dir: dir,
		Tasks: map[string]Task{
			"deploy": {
				Env: map[string]string{"DEPLOY_READY": "1"},
				Preconditions: []Precondition{
					{Sh: `test -n "$DEPLOY_READY"`, Msg: "DEPLOY_READY not set"},
				},
				Cmds: []Cmd{{Cmd: "deploy"}},
			},
		},
		DotenvVars: make(map[string]string),
	}

	runner := newTestRunner(t, tf, dir)
	execs := captureExecs(runner)

	require.NoError(t, runner.Run("deploy", ""))
	assert.Len(t, *execs, 1)
}

func TestPreconditionPasses(t *testing.T) {
	dir := t.TempDir()
	tf := &Taskfile{
		Dir: dir,
		Tasks: map[string]Task{
			"deploy": {
				Preconditions: []Precondition{
					{Sh: "true"},
				},
				Cmds: []Cmd{{Cmd: "echo deploying"}},
			},
		},
		DotenvVars: make(map[string]string),
	}

	runner := newTestRunner(t, tf, dir)
	execs := captureExecs(runner)

	err := runner.Run("deploy", "")
	require.NoError(t, err)
	assert.Len(t, *execs, 1)
}

func TestPreconditionFails(t *testing.T) {
	dir := t.TempDir()
	tf := &Taskfile{
		Dir: dir,
		Tasks: map[string]Task{
			"deploy": {
				Preconditions: []Precondition{
					{Sh: "false", Msg: "this should fail"},
				},
				Cmds: []Cmd{{Cmd: "echo deploying"}},
			},
		},
		DotenvVars: make(map[string]string),
	}

	runner := newTestRunner(t, tf, dir)
	execs := captureExecs(runner)

	err := runner.Run("deploy", "")
	require.EqualError(t, err, `task "deploy": this should fail`)
	assert.Empty(t, *execs)
}

func TestPreconditionFailsWithDefaultMessage(t *testing.T) {
	dir := t.TempDir()
	tf := &Taskfile{
		Dir: dir,
		Tasks: map[string]Task{
			"deploy": {
				Preconditions: []Precondition{
					{Sh: "false"},
				},
				Cmds: []Cmd{{Cmd: "echo deploying"}},
			},
		},
		DotenvVars: make(map[string]string),
	}

	runner := newTestRunner(t, tf, dir)
	execs := captureExecs(runner)

	err := runner.Run("deploy", "")
	require.EqualError(t, err, `task "deploy": precondition failed: false`)
	assert.Empty(t, *execs)
}

func TestPreconditionStringShorthand(t *testing.T) {
	dir := t.TempDir()
	tf := &Taskfile{
		Dir: dir,
		Tasks: map[string]Task{
			"deploy": {
				Preconditions: []Precondition{
					{Sh: "true"},
					{Sh: "false"},
				},
				Cmds: []Cmd{{Cmd: "echo deploying"}},
			},
		},
		DotenvVars: make(map[string]string),
	}

	runner := newTestRunner(t, tf, dir)
	execs := captureExecs(runner)

	err := runner.Run("deploy", "")
	require.Error(t, err)
	assert.Empty(t, *execs)
}

func TestSourcesChecksumErrorPropagates(t *testing.T) {
	dir := t.TempDir()

	tf := &Taskfile{
		Dir: dir,
		Tasks: map[string]Task{
			"build": {
				Sources: StringList{"[invalid"},
				Cmds:    []Cmd{{Cmd: "go build"}},
			},
		},
		DotenvVars: make(map[string]string),
	}

	runner := newTestRunner(t, tf, dir)
	captureExecs(runner)

	err := runner.Run("build", "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "computing sources checksum")
}

func TestResolveVarShellError(t *testing.T) {
	dir := t.TempDir()
	tf := &Taskfile{
		Dir: dir,
		Tasks: map[string]Task{
			"build": {
				Vars: map[string]Var{"VER": {Sh: "exit 1"}},
				Cmds: []Cmd{{Cmd: "echo ${VER}"}},
			},
		},
		DotenvVars: make(map[string]string),
	}

	runner, err := NewRunner(tf, dir)
	require.NoError(t, err)
	runner.BaseEnv = nil
	captureExecs(runner)

	err = runner.Run("build", "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "resolving variable")
}

func TestSourcesNoMatchAlwaysRuns(t *testing.T) {
	dir := t.TempDir()

	tf := &Taskfile{
		Dir: dir,
		Tasks: map[string]Task{
			"build": {
				Sources: StringList{"*.go"},
				Cmds:    []Cmd{{Cmd: "go build"}},
			},
		},
		DotenvVars: make(map[string]string),
	}

	runner := newTestRunner(t, tf, dir)
	execs := captureExecs(runner)

	// First run with no matching files still executes
	require.NoError(t, runner.Run("build", ""))
	require.Len(t, *execs, 1)

	// Second run with still no matching files must also execute (not be skipped)
	runner.ResetRan()
	require.NoError(t, runner.Run("build", ""))
	assert.Len(t, *execs, 2)
}

func TestShellRunnerRunsPreconditionsWithoutRunningRealShell(t *testing.T) {
	dir := t.TempDir()
	tf := &Taskfile{
		Dir: dir,
		Tasks: map[string]Task{
			"deploy": {
				Env: map[string]string{"DEPLOY_READY": "1"},
				Preconditions: []Precondition{
					{Sh: `test -n "$DEPLOY_READY"`},
				},
				Cmds: []Cmd{{Cmd: "deploy"}},
			},
		},
		DotenvVars: make(map[string]string),
	}

	runner := newTestRunner(t, tf, dir)
	shell := &fakeShellRunner{}
	runner.ShellRunner = shell
	execs := captureExecs(runner)

	require.NoError(t, runner.Run("deploy", ""))

	runs := shell.runsSnapshot()
	require.Len(t, runs, 2)
	assert.Equal(t, ShellCommandPrecondition, runs[0].Kind)
	assert.Equal(t, `test -n "$DEPLOY_READY"`, runs[0].Command)
	assert.Equal(t, "1", envValue(runs[0].Env, "DEPLOY_READY"))
	assert.Equal(t, ShellCommandTask, runs[1].Kind)
	assert.Len(t, *execs, 1)
}

func TestShellRunnerResolvesShellVariablesWithoutRunningRealShell(t *testing.T) {
	dir := t.TempDir()
	tf := &Taskfile{
		Dir: dir,
		Tasks: map[string]Task{
			"build": {
				Vars: map[string]Var{"VERSION": {Sh: "git describe --tags"}},
				Cmds: []Cmd{{Cmd: "echo ${VERSION}"}},
			},
		},
		DotenvVars: make(map[string]string),
	}

	runner := newTestRunner(t, tf, dir)
	shell := &fakeShellRunner{
		outputFunc: func(req ShellCommand) ([]byte, error) {
			require.Equal(t, ShellCommandVar, req.Kind)
			require.Equal(t, "git describe --tags", req.Command)
			require.Equal(t, dir, req.Dir)
			return []byte("v1.2.3\n"), nil
		},
	}
	runner.ShellRunner = shell
	execs := captureExecs(runner)

	require.NoError(t, runner.Run("build", ""))

	outputs := shell.outputsSnapshot()
	require.Len(t, outputs, 1)
	assert.Equal(t, ShellCommandVar, outputs[0].Kind)
	require.Len(t, *execs, 1)
	assert.Equal(t, "echo v1.2.3", (*execs)[0].Command)
}

func TestPreconditionFailureStopsBeforeUpToDateCheck(t *testing.T) {
	dir := t.TempDir()
	writeFiles(t, dir, map[string]string{"main.go": "package main"})
	tf := &Taskfile{
		Dir: dir,
		Tasks: map[string]Task{
			"build": {
				Preconditions: []Precondition{{Sh: "false", Msg: "not ready"}},
				Sources:       StringList{"*.go"},
				Cmds:          []Cmd{{Cmd: "go build"}},
			},
		},
		DotenvVars: make(map[string]string),
	}

	runner := newTestRunner(t, tf, dir)
	execs := captureExecs(runner)

	require.EqualError(t, runner.Run("build", ""), `task "build": not ready`)
	assert.Empty(t, *execs)
	assert.NoFileExists(t, checksumPath(dir, "build"))
}

func TestRunnerLogsToInjectedStderr(t *testing.T) {
	dir := t.TempDir()
	tf := &Taskfile{
		Dir: dir,
		Tasks: map[string]Task{
			"build": {
				Cmds: []Cmd{{Cmd: "go build"}},
			},
		},
		DotenvVars: make(map[string]string),
	}

	runner := newTestRunner(t, tf, dir)
	var stderr strings.Builder
	runner.IO.Stderr = &stderr
	captureExecs(runner)

	require.NoError(t, runner.Run("build", ""))
	assert.Contains(t, stderr.String(), "[build]")
	assert.Contains(t, stderr.String(), "go build")
}

func TestRunnerPassesInjectedIOToShellCommands(t *testing.T) {
	dir := t.TempDir()
	tf := &Taskfile{
		Dir: dir,
		Tasks: map[string]Task{
			"build": {
				Cmds: []Cmd{{Cmd: "go build"}},
			},
		},
		DotenvVars: make(map[string]string),
	}

	runner := newTestRunner(t, tf, dir)
	stdin := strings.NewReader("input")
	var stdout strings.Builder
	var stderr strings.Builder
	runner.IO = RunnerIO{Stdin: stdin, Stdout: &stdout, Stderr: &stderr}
	shell := &fakeShellRunner{}
	runner.ShellRunner = shell

	require.NoError(t, runner.Run("build", ""))

	runs := shell.runsSnapshot()
	require.Len(t, runs, 1)
	assert.Same(t, stdin, runs[0].Stdin)
	assert.Same(t, &stdout, runs[0].Stdout)
	assert.Same(t, &stderr, runs[0].Stderr)
}
