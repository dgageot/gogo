package taskfile

import (
	"fmt"
	"os"
)

// ANSI color escape sequences.
const (
	colorReset  = "\x1b[0m"
	colorGreen  = "\x1b[32m"
	colorYellow = "\x1b[33m"
	colorCyan   = "\x1b[36m"
)

// logTask prints a colored task-prefixed message to stderr.
func logTask(color, name, msg string) {
	fmt.Fprintln(os.Stderr, color+"["+name+"]"+colorReset, msg)
}
