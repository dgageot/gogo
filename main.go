package main

import (
	"fmt"
	"os"
	"slices"
	"strings"

	"github.com/dgageot/gogo/taskfile"
	"maps"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	verbose := false
	args := os.Args[1:]

	// Parse flags
	for len(args) > 0 && strings.HasPrefix(args[0], "-") {
		switch args[0] {
		case "-v", "--verbose":
			verbose = true
		case "-l", "--list":
			return listTasks()
		case "-h", "--help":
			printUsage()
			return nil
		default:
			return fmt.Errorf("unknown flag: %s", args[0])
		}
		args = args[1:]
	}
	dir, err := os.Getwd()
	if err != nil {
		return err
	}

	tf, err := taskfile.LoadWithIncludes(dir)
	if err != nil {
		return err
	}

	// Default task
	taskName := "default"
	if len(args) > 0 {
		taskName = args[0]
	}

	// Collect CLI_ARGS (everything after --)
	var cliArgs string
	for i, arg := range os.Args {
		if arg == "--" && i+1 < len(os.Args) {
			cliArgs = strings.Join(os.Args[i+1:], " ")
			break
		}
	}

	runner := taskfile.NewRunner(tf, verbose)
	return runner.Run(taskName, cliArgs)
}

func listTasks() error {
	dir, err := os.Getwd()
	if err != nil {
		return err
	}

	tf, err := taskfile.LoadWithIncludes(dir)
	if err != nil {
		return err
	}

	names := slices.Sorted(maps.Keys(tf.Tasks))

	maxLen := 0
	for _, name := range names {
		if len(name) > maxLen {
			maxLen = len(name)
		}
	}

	for _, name := range names {
		task := tf.Tasks[name]
		if task.Desc != "" {
			fmt.Printf("%-*s  %s\n", maxLen, name, task.Desc)
		}
	}

	return nil
}

func printUsage() {
	fmt.Println(`gogo - a simple task runner

Usage:
  gogo [flags] [task] [-- args...]

Flags:
  -l, --list      List available tasks
  -v, --verbose   Show commands being run
  -h, --help      Show this help`)
}
