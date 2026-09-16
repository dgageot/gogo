package taskfile

import (
	"errors"
	"io"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTaskCyclesReturnErrors(t *testing.T) {
	for _, tc := range []struct {
		name  string
		tasks map[string]Task
	}{
		{"self dependency", map[string]Task{"a": {Deps: []Dep{{Task: "a"}}}}},
		{"indirect dependency", map[string]Task{"a": {Deps: []Dep{{Task: "b"}}}, "b": {Deps: []Dep{{Task: "a"}}}}},
		{"self call", map[string]Task{"a": {Cmds: []Cmd{{Task: "a"}}}}},
		{"mixed", map[string]Task{"a": {Deps: []Dep{{Task: "b"}}}, "b": {Cmds: []Cmd{{Task: "a"}}}}},
		{"alias", map[string]Task{"a": {Aliases: StringList{"alias"}, Deps: []Dep{{Task: "alias"}}}}},
		{"pattern", map[string]Task{"a": {Deps: []Dep{{Task: "...:a"}}}}},
		{"parallel", map[string]Task{"a": {Deps: []Dep{{Task: "b"}, {Task: "c"}}}, "b": {Deps: []Dep{{Task: "c"}}}, "c": {Deps: []Dep{{Task: "b"}}}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			r := newTestRunner(t, &Config{Dir: dir, Tasks: tc.tasks}, dir)
			r.IO.Stderr = io.Discard
			done := make(chan error, 1)
			go func() { done <- r.Run("a", "") }()
			select {
			case err := <-done:
				require.ErrorContains(t, err, "task cycle")
				assert.Empty(t, r.waits)
			case <-time.After(2 * time.Second):
				require.FailNow(t, "task cycle deadlocked")
			}
		})
	}
}

func TestSkippedTaskDoesNotTraverseCycle(t *testing.T) {
	dir := t.TempDir()
	r := newTestRunner(t, &Config{Dir: dir, Tasks: map[string]Task{
		"a": {If: "false", Deps: []Dep{{Task: "a"}}},
	}}, dir)
	require.NoError(t, r.Run("a", ""))
}

func TestConcurrentCallsToSameSubtaskAreNotCycles(t *testing.T) {
	dir := t.TempDir()
	r := newTestRunner(t, &Config{Dir: dir, Tasks: map[string]Task{
		"a":      {Deps: []Dep{{Task: "b"}, {Task: "c"}}},
		"b":      {Cmds: []Cmd{{Task: "shared"}}},
		"c":      {Cmds: []Cmd{{Task: "shared"}}},
		"shared": {Cmds: []Cmd{{Cmd: "true"}}},
	}}, dir)
	execs := captureExecs(r)
	require.NoError(t, r.Run("a", ""))
	assert.Len(t, *execs, 2)
	assert.Empty(t, r.waits)
}

func TestBoundedRecursiveTaskCallsStillWork(t *testing.T) {
	dir := t.TempDir()
	r := newTestRunner(t, &Config{Dir: dir, Tasks: map[string]Task{
		"count": {Vars: map[string]Var{"N": {Value: "1"}}, Cmds: []Cmd{
			{Cmd: "echo {{.N}}"},
			{If: "test {{.N}} -gt 0", Task: "count", Vars: map[string]Var{"N": {Value: "0"}}},
		}},
	}}, dir)
	r.ShellRunner = &fakeShellRunner{runFunc: func(req ShellCommand) error {
		if req.Kind == ShellCommandCondition && req.Command == "test 0 -gt 0" {
			return errors.New("condition false")
		}
		return nil
	}}
	execs := captureExecs(r)
	require.NoError(t, r.Run("count", ""))
	require.Len(t, *execs, 2)
	assert.Equal(t, "echo 1", (*execs)[0].Command)
	assert.Equal(t, "echo 0", (*execs)[1].Command)
	assert.Empty(t, r.waits)
}
