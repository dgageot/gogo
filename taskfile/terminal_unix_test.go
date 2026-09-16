//go:build darwin || linux

package taskfile

import (
	"bufio"
	"context"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"testing"
	"time"

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
	if os.Getenv("GOGO_TERMINAL_TEST") != "headless" {
		exe, err := os.Executable()
		require.NoError(t, err)
		cmd := exec.CommandContext(t.Context(), exe, "-test.run=^TestNullStreamsRetainGroupCancellation$")
		cmd.Env = append(os.Environ(), "GOGO_TERMINAL_TEST=headless")
		cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
		output, err := cmd.CombinedOutput()
		require.NoError(t, err, "%s", output)
		return
	}
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

func TestAuxiliaryCommandsCanReadControllingTerminal(t *testing.T) {
	if os.Getenv("GOGO_TERMINAL_TEST") == "foreground" {
		ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
		defer cancel()
		req := ShellCommand{
			Context: ctx,
			Kind:    ShellCommandVar,
			Command: `printf 'READY\n' > /dev/tty; read value < /dev/tty; test "$value" = confirmed`,
		}
		require.True(t, hasForegroundTerminal())
		_, err := newDefaultShellRunner().Output(req)
		require.NoError(t, err)
		return
	}
	exe, err := os.Executable()
	require.NoError(t, err)
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, exe, "-test.run=^TestAuxiliaryCommandsCanReadControllingTerminal$")
	cmd.Env = append(os.Environ(), "GOGO_TERMINAL_TEST=foreground")
	cmd.WaitDelay = time.Second
	master, err := pty.Start(cmd)
	require.NoError(t, err)
	defer master.Close()
	var output strings.Builder
	answered := make(chan error, 1)
	drained := make(chan struct{})
	go func() {
		defer close(drained)
		scanner := bufio.NewScanner(master)
		for scanner.Scan() {
			output.WriteString(scanner.Text() + "\n")
			if scanner.Text() == "READY" {
				_, err := master.WriteString("confirmed\n")
				answered <- err
			}
		}
	}()
	err = cmd.Wait()
	_ = master.Close()
	<-drained
	require.NoError(t, err, "%s", output.String())
	select {
	case err := <-answered:
		require.NoError(t, err)
	default:
		require.FailNow(t, "child never prompted through its controlling terminal")
	}
}
