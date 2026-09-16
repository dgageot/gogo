//go:build darwin || linux

package taskfile

import (
	"context"
	"os"
	"testing"

	"github.com/creack/pty"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPseudoTerminalIsDetected(t *testing.T) {
	master, slave, err := pty.Open()
	require.NoError(t, err)
	defer master.Close()
	defer slave.Close()
	assert.True(t, isTerminal(slave))
	assert.Contains(t, opRunArgs(ShellCommand{Stdout: slave, Stderr: slave}), "--no-masking")
}

func TestNullStreamsRetainGroupCancellation(t *testing.T) {
	null, err := os.OpenFile(os.DevNull, os.O_RDWR, 0)
	require.NoError(t, err)
	defer null.Close()
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	for _, req := range []ShellCommand{
		{Context: ctx, Stdin: null},
		{Context: ctx, Stdout: null},
		{Context: ctx, Stderr: null},
	} {
		cmd, err := newDefaultShellRunner().shellExecCommand(req)
		require.NoError(t, err)
		require.NotNil(t, cmd.SysProcAttr)
		assert.True(t, cmd.SysProcAttr.Setpgid)
	}
}
