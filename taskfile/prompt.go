package taskfile

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"
)

// confirmPrompt asks the user to confirm a task guarded by `prompt:` and
// returns an error when the answer is anything but yes. Dry runs skip the
// question because nothing will execute anyway, and --yes (AssumeYes)
// auto-confirms so guarded tasks stay scriptable in CI. EOF or an unreadable
// stdin counts as a decline — a non-interactive run must never assume
// consent for a task its author chose to guard.
func (r *Runner) confirmPrompt(ctx context.Context, taskName, prompt string) error {
	if r.DryRun || r.AssumeYes {
		return nil
	}

	// Serialize the whole question/answer exchange: parallel deps and
	// pattern fan-out can prompt concurrently, and interleaved byte-wise
	// reads from the shared stdin could hand one task's "y" to another —
	// silently authorizing a guarded task the user never confirmed.
	select {
	case r.promptSem <- struct{}{}:
		defer func() { <-r.promptSem }()
	case <-ctx.Done():
		return ctx.Err()
	}
	if err := ctx.Err(); err != nil {
		return err
	}

	fmt.Fprintf(r.outputWriter(r.IO.Stderr), "%s[%s]%s %s [y/N]: ", colorYellow, taskName, colorReset, prompt)

	declined := fmt.Errorf("task %q: prompt declined", taskName)
	if r.IO.Stdin == nil {
		return declined
	}
	answer := readAnswer(ctx, r.inputReader())
	if err := ctx.Err(); err != nil {
		return err
	}
	switch strings.ToLower(strings.TrimSpace(answer)) {
	case "y", "yes":
		return nil
	default:
		return declined
	}
}

// readAnswer reads one line, one byte at a time, so nothing past the newline
// is buffered away — with piped input (`printf "y\ninput" | gogo deploy`)
// everything after the answer still belongs to the task's own stdin.
// Injected readers must unblock their own Read on cancellation; io.Reader has
// no cancellation protocol, and an abandoned read could steal a later answer.
func readAnswer(ctx context.Context, r io.Reader) string {
	var line []byte
	buf := make([]byte, 1)
	for ctx.Err() == nil {
		if f, ok := r.(*os.File); ok && ctx.Done() != nil {
			if err := waitPromptInput(ctx, f); err != nil {
				break
			}
		}
		n, err := r.Read(buf)
		if n > 0 {
			if buf[0] == '\n' {
				break
			}
			line = append(line, buf[0])
		}
		if err != nil {
			break
		}
	}
	return string(line)
}
