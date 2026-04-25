package taskfile

import (
	"fmt"
	"io"
)

// ANSI color escape sequences.
const (
	colorReset  = "\x1b[0m"
	colorGreen  = "\x1b[32m"
	colorYellow = "\x1b[33m"
)

// logTask prints a colored task-prefixed message.
func logTask(w io.Writer, color, name, msg string) {
	fmt.Fprintf(w, "%s[%s]%s %s\n", color, name, colorReset, msg)
}
