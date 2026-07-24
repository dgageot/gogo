package taskfile

import (
	"fmt"
	"io"
	"strings"
)

// confirmPrompt asks the user to confirm a task guarded by `prompt:` and
// returns an error when the answer is anything but yes. Dry runs skip the
// question because nothing will execute anyway, and --yes (AssumeYes)
// auto-confirms so guarded tasks stay scriptable in CI. EOF or an unreadable
// stdin counts as a decline — a non-interactive run must never assume
// consent for a task its author chose to guard.
func (r *Runner) confirmPrompt(taskName, prompt string) error {
	if r.DryRun || r.AssumeYes {
		return nil
	}

	// Serialize the whole question/answer exchange: parallel deps and
	// pattern fan-out can prompt concurrently, and interleaved byte-wise
	// reads from the shared stdin could hand one task's "y" to another —
	// silently authorizing a guarded task the user never confirmed.
	r.promptMu.Lock()
	defer r.promptMu.Unlock()

	fmt.Fprintf(r.IO.Stderr, "%s[%s]%s %s [y/N]: ", colorYellow, taskName, colorReset, prompt)

	declined := fmt.Errorf("task %q: prompt declined", taskName)
	if r.IO.Stdin == nil {
		return declined
	}
	switch strings.ToLower(strings.TrimSpace(readAnswer(r.IO.Stdin))) {
	case "y", "yes":
		return nil
	default:
		return declined
	}
}

// readAnswer reads one line, one byte at a time, so nothing past the newline
// is buffered away — with piped input (`printf "y\ninput" | gogo deploy`)
// everything after the answer still belongs to the task's own stdin.
func readAnswer(r io.Reader) string {
	var line []byte
	buf := make([]byte, 1)
	for {
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
