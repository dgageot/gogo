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
	args := os.Args[1:]

	// Parse flags
	if len(args) > 0 {
		switch args[0] {
		case "-l", "--list":
			return listTasks()
		case "-h", "--help":
			printUsage()
			return nil
		}
	}
	dir, err := os.Getwd()
	if err != nil {
		return err
	}

	rootDir, err := taskfile.FindRootDir(dir)
	if err != nil {
		return err
	}

	tf, err := taskfile.LoadWithIncludes(rootDir)
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

	runner := taskfile.NewRunner(tf, dir)
	return runner.Run(taskName, cliArgs)
}

func listTasks() error {
	dir, err := os.Getwd()
	if err != nil {
		return err
	}

	rootDir, err := taskfile.FindRootDir(dir)
	if err != nil {
		return err
	}

	tf, err := taskfile.LoadWithIncludes(rootDir)
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
			if len(task.Aliases) > 0 {
				fmt.Printf("%-*s  %s (aliases: %s)\n", maxLen, name, task.Desc, strings.Join(task.Aliases, ", "))
			} else {
				fmt.Printf("%-*s  %s\n", maxLen, name, task.Desc)
			}
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
  -h, --help      Show this help`)
}
