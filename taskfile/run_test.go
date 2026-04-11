package taskfile

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRunWithExtraVars(t *testing.T) {
	dir := t.TempDir()
	output := filepath.Join(dir, "output.txt")

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
					{Cmd: "printf ${MSG} > " + output},
				},
			},
		},
		DotenvVars: make(map[string]string),
	}

	runner := NewRunner(tf, dir)
	err := runner.Run("caller", "")
	require.NoError(t, err)

	got, err := os.ReadFile(output)
	require.NoError(t, err)
	assert.Equal(t, "from-caller", string(got))
}

func TestRunWithExtraVarsOverridesTaskVars(t *testing.T) {
	dir := t.TempDir()
	output := filepath.Join(dir, "output.txt")

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
					{Cmd: "printf ${MSG} > " + output},
				},
			},
		},
		DotenvVars: make(map[string]string),
	}

	runner := NewRunner(tf, dir)
	err := runner.Run("caller", "")
	require.NoError(t, err)

	got, err := os.ReadFile(output)
	require.NoError(t, err)
	assert.Equal(t, "overridden", string(got))
}

func TestRunWithExtraVarsShell(t *testing.T) {
	dir := t.TempDir()
	output := filepath.Join(dir, "output.txt")

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
					{Cmd: "printf ${MSG} > " + output},
				},
			},
		},
		DotenvVars: make(map[string]string),
	}

	runner := NewRunner(tf, dir)
	err := runner.Run("caller", "")
	require.NoError(t, err)

	got, err := os.ReadFile(output)
	require.NoError(t, err)
	assert.Equal(t, "dynamic", string(got))
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

	runner := NewRunner(tf, dir)
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

	runner := NewRunner(tf, dir)
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

	runner := NewRunner(tf, dir)
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
	runner := NewRunner(tf, dir)
	err := runner.Run("deploy", "")
	assert.NoError(t, err)
}

func TestRunDeduplicatesDeps(t *testing.T) {
	dir := t.TempDir()
	counter := filepath.Join(dir, "counter.txt")

	// shared is a dep of both a and b; it should only run once
	tf := &Taskfile{
		Dir: dir,
		Tasks: map[string]Task{
			"all": {
				Deps: []Dep{{Task: "a"}, {Task: "b"}},
			},
			"a": {
				Deps: []Dep{{Task: "shared"}},
				Cmds: []Cmd{{Cmd: "true"}},
			},
			"b": {
				Deps: []Dep{{Task: "shared"}},
				Cmds: []Cmd{{Cmd: "true"}},
			},
			"shared": {
				Cmds: []Cmd{{Cmd: "echo x >> " + counter}},
			},
		},
		DotenvVars: make(map[string]string),
	}

	runner := NewRunner(tf, dir)
	err := runner.Run("all", "")
	require.NoError(t, err)

	data, err := os.ReadFile(counter)
	require.NoError(t, err)
	assert.Equal(t, "x\n", string(data))
}
