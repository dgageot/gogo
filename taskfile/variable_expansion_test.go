package taskfile

import (
	"strings"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTaskVarShIsLazyAndScoped(t *testing.T) {
	dir := t.TempDir()
	var gitCalls atomic.Int64
	tf := &Config{
		Dir: dir,
		Tasks: map[string]Task{
			"deploy": {
				Vars: map[string]Var{"GIT_TAG": {Sh: "git rev-parse --short=7 HEAD"}},
				Cmds: []Cmd{{Cmd: `echo "sha-{{.GIT_TAG}}"`}},
			},
			"other": {
				Cmds: []Cmd{{Cmd: `echo "other"`}},
			},
		},
	}
	r := newTestRunner(t, tf, dir)
	r.ShellRunner = &fakeShellRunner{
		outputFunc: func(req ShellCommand) ([]byte, error) {
			gitCalls.Add(1)
			assert.Equal(t, ShellCommandVar, req.Kind)
			assert.Equal(t, "git rev-parse --short=7 HEAD", req.Command)
			return []byte("abc1234\n"), nil
		},
	}
	execs := captureExecs(r)

	require.NoError(t, r.Run("other", ""))
	require.Len(t, *execs, 1)
	assert.Equal(t, `echo "other"`, (*execs)[0].Command)
	assert.Equal(t, int64(0), gitCalls.Load())
	assert.Empty(t, envValue((*execs)[0].Env, "GIT_TAG"))

	r.ResetRan()
	require.NoError(t, r.Run("deploy", ""))
	require.Len(t, *execs, 2)
	assert.Equal(t, `echo "sha-abc1234"`, (*execs)[1].Command)
	assert.Equal(t, int64(1), gitCalls.Load())
	assert.Empty(t, envValue((*execs)[1].Env, "GIT_TAG"))
}

func TestCommandLogShowsExpandedTemplateVars(t *testing.T) {
	dir := t.TempDir()
	tf := &Config{
		Dir: dir,
		Tasks: map[string]Task{
			"deploy": {
				Vars: map[string]Var{"GIT_TAG": {Sh: "git rev-parse --short=7 HEAD"}},
				Cmds: []Cmd{{Cmd: `echo "sha-{{.GIT_TAG}}"`}},
			},
		},
	}
	r := newTestRunner(t, tf, dir)
	r.ShellRunner = &fakeShellRunner{outputFunc: func(ShellCommand) ([]byte, error) {
		return []byte("abc1234\n"), nil
	}}
	var stderr strings.Builder
	r.IO.Stderr = &stderr
	captureExecs(r)

	require.NoError(t, r.Run("deploy", ""))

	assert.Contains(t, stderr.String(), `echo "sha-abc1234"`)
	assert.NotContains(t, stderr.String(), "{{.GIT_TAG}}")
}

func TestShellSyntaxUsesEnvNamespaceAndWarnsForUnusedVar(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("GIT_TAG", "from-process")
	var gitCalls atomic.Int64
	tf := &Config{
		Dir: dir,
		Tasks: map[string]Task{
			"deploy": {
				Vars: map[string]Var{"GIT_TAG": {Sh: "git rev-parse --short=7 HEAD"}},
				Cmds: []Cmd{{Cmd: `echo "sha-${GIT_TAG}"`}},
			},
		},
	}
	r := newTestRunner(t, tf, dir)
	r.ShellRunner = &fakeShellRunner{outputFunc: func(ShellCommand) ([]byte, error) {
		gitCalls.Add(1)
		return []byte("abc1234\n"), nil
	}}
	var stderr strings.Builder
	r.IO.Stderr = &stderr
	execs := captureExecs(r)

	require.NoError(t, r.Run("deploy", ""))

	require.Len(t, *execs, 1)
	assert.Equal(t, `echo "sha-${GIT_TAG}"`, (*execs)[0].Command)
	assert.Equal(t, int64(0), gitCalls.Load())
	assert.Contains(t, stderr.String(), `warning: variable "GIT_TAG" is declared but not used`)
}

func TestTaskEnvOverridesProcessEnvForShellSyntax(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("GIT_TAG", "from-process")
	tf := &Config{
		Dir: dir,
		Tasks: map[string]Task{
			"deploy": {
				Env:  map[string]string{"GIT_TAG": "HEAD"},
				Cmds: []Cmd{{Cmd: `echo "sha-${GIT_TAG}"`}},
			},
		},
	}
	r := newTestRunner(t, tf, dir)
	execs := captureExecs(r)

	require.NoError(t, r.Run("deploy", ""))

	require.Len(t, *execs, 1)
	assert.Equal(t, `echo "sha-${GIT_TAG}"`, (*execs)[0].Command)
	assert.Equal(t, "HEAD", envValue((*execs)[0].Env, "GIT_TAG"))
}

func TestTaskEnvValueCanReadProcessEnv(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("GIT_TAG", "from-process")
	tf := &Config{
		Dir: dir,
		Tasks: map[string]Task{
			"deploy": {
				Env:  map[string]string{"GIT_TAG": "$GIT_TAG"},
				Cmds: []Cmd{{Cmd: `echo "sha-${GIT_TAG}"`}},
			},
		},
	}
	r := newTestRunner(t, tf, dir)
	execs := captureExecs(r)

	require.NoError(t, r.Run("deploy", ""))

	require.Len(t, *execs, 1)
	assert.Equal(t, "from-process", envValue((*execs)[0].Env, "GIT_TAG"))
}

func TestLocalGitCommitVarOverridesBuiltinOnlyForTask(t *testing.T) {
	dir := t.TempDir()
	var outputs []string
	tf := &Config{
		Dir: dir,
		Tasks: map[string]Task{
			"deploy": {
				Vars: map[string]Var{"GIT_COMMIT": {Sh: "git rev-parse --short=7 HEAD"}},
				Cmds: []Cmd{{Cmd: `echo "sha-{{.GIT_COMMIT}}"`}},
			},
			"other": {
				Cmds: []Cmd{{Cmd: `echo "sha-{{.GIT_COMMIT}}"`}},
			},
		},
	}
	r := newTestRunner(t, tf, dir)
	r.ShellRunner = &fakeShellRunner{outputFunc: func(req ShellCommand) ([]byte, error) {
		outputs = append(outputs, req.Command)
		switch req.Command {
		case "git rev-parse --short=7 HEAD":
			return []byte("shortsha\n"), nil
		case "git rev-parse HEAD":
			return []byte("fullsha\n"), nil
		default:
			return []byte("unexpected\n"), nil
		}
	}}
	execs := captureExecs(r)

	require.NoError(t, r.Run("deploy", ""))
	require.NoError(t, r.Run("other", ""))

	require.Len(t, *execs, 2)
	assert.Equal(t, `echo "sha-shortsha"`, (*execs)[0].Command)
	assert.Equal(t, `echo "sha-fullsha"`, (*execs)[1].Command)
	assert.ElementsMatch(t, []string{"git rev-parse --short=7 HEAD", "git rev-parse HEAD"}, outputs)
}

func TestUnusedVarsStaySortedAndLazy(t *testing.T) {
	dir := t.TempDir()
	r := newTestRunner(t, &Config{
		Dir:  dir,
		Vars: map[string]Var{"ROOT": {Sh: "unused root"}},
		NamespaceVars: map[string]map[string]Var{
			"app": {"NAMESPACE": {Sh: "unused namespace"}},
		},
	}, dir)
	r.ShellRunner = &fakeShellRunner{outputFunc: func(ShellCommand) ([]byte, error) {
		assert.Fail(t, "unused shell variable was evaluated")
		return nil, nil
	}}
	task := &Task{
		Vars: map[string]Var{
			"Z":    {Sh: "unused task"},
			"A":    {Sh: "unused task"},
			"USED": {Value: "value"},
		},
		Cmds: []Cmd{{Cmd: "echo {{.USED}}"}},
	}

	vars, unused, err := r.resolveAllVars("app:show", task, dir, map[string]Var{
		"M": {Sh: "unused call-site"},
	})
	require.NoError(t, err)
	assert.Equal(t, "value", vars["USED"])
	assert.Equal(t, []string{"A", "M", "Z"}, unused)
}
