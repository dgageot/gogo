package taskfile

import (
	"fmt"
	"os"
)

// ANSI color escape sequences.
const (
	colorReset  = "\033[0m"
	colorGreen  = "\033[32m"
	colorYellow = "\033[33m"
	colorCyan   = "\033[36m"
)

// logTask prints a colored task-prefixed message to stderr.
func logTask(color, name, msg string) {
	fmt.Fprintf(os.Stderr, "%s[%s]%s %s\n", color, name, colorReset, msg)
}
