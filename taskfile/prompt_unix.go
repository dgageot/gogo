//go:build darwin || linux

package taskfile

import (
	"context"
	"errors"
	"os"

	"golang.org/x/sys/unix"
)

// waitPromptInput polls without changing the shared descriptor's blocking mode.
// Checking before each byte also makes partial answers cancellable.
func waitPromptInput(ctx context.Context, f *os.File) error {
	raw, err := f.SyscallConn()
	if err != nil {
		return err
	}
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		var ready int
		var pollErr error
		if err := raw.Control(func(fd uintptr) {
			fds := []unix.PollFd{{Fd: int32(fd), Events: unix.POLLIN}}
			ready, pollErr = unix.Poll(fds, 50)
		}); err != nil {
			return err
		}
		if errors.Is(pollErr, unix.EINTR) {
			continue
		}
		if pollErr != nil {
			return pollErr
		}
		// HUP/ERR/NVAL also need a Read to observe EOF or the underlying error.
		// In particular, macOS reports NVAL for /dev/null.
		if ready > 0 {
			return ctx.Err()
		}
	}
}
