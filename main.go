package main

import (
	"cmp"
	"errors"
	"fmt"
	"maps"
	"os"
	"slices"
	"strings"
	"time"

	"github.com/dgageot/gogo/taskfile"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	args := os.Args[1:]

	// Handle subcommands
	if len(args) >= 3 && args[0] == "secret" && args[1] == "set" {
		return secretSet(args[2:])
	}

	// Parse flags
	watch := false
	var filtered []string
	for _, arg := range args {
		switch arg {
		case "-l", "--list":
			return listTasks()
		case "-h", "--help":
			printUsage()
			return nil
		case "-w", "--watch":
			watch = true
		default:
			filtered = append(filtered, arg)
		}
	}
	args = filtered

	dir, tf, err := loadTaskfile()
	if err != nil {
		return err
	}

	taskName := "default"
	if len(args) > 0 {
		taskName = args[0]
	}

	// Collect CLI_ARGS (everything after --)
	var cliArgs string
	if i := slices.Index(os.Args, "--"); i >= 0 && i+1 < len(os.Args) {
		cliArgs = strings.Join(os.Args[i+1:], " ")
	}

	runner := taskfile.NewRunner(tf, dir)

	if watch {
		var parsed time.Duration
		if tf.Interval != "" {
			parsed, _ = time.ParseDuration(tf.Interval)
		}
		interval := cmp.Or(parsed, 500*time.Millisecond)
		return runner.Watch(taskName, cliArgs, interval)
	}

	return runner.Run(taskName, cliArgs)
}

func loadTaskfile() (string, *taskfile.Taskfile, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", nil, err
	}

	rootDir, err := taskfile.FindRootDir(dir)
	if err != nil {
		return "", nil, err
	}

	tf, err := taskfile.LoadWithIncludes(rootDir)
	if err != nil {
		return "", nil, err
	}

	return dir, tf, nil
}

func listTasks() error {
	_, tf, err := loadTaskfile()
	if err != nil {
		return err
	}

	names := slices.Sorted(maps.Keys(tf.Tasks))

	// Filter to only tasks with descriptions
	names = slices.DeleteFunc(names, func(name string) bool {
		return tf.Tasks[name].Desc == ""
	})

	if len(names) == 0 {
		return nil
	}

	maxLen := len(slices.MaxFunc(names, func(a, b string) int {
		return cmp.Compare(len(a), len(b))
	}))

	for _, name := range names {
		task := tf.Tasks[name]
		if len(task.Aliases) > 0 {
			fmt.Printf("%-*s  %s (aliases: %s)\n", maxLen, name, task.Desc, strings.Join(task.Aliases, ", "))
		} else {
			fmt.Printf("%-*s  %s\n", maxLen, name, task.Desc)
		}
	}

	return nil
}

func printUsage() {
	fmt.Println(`gogo - a simple task runner

Usage:
  gogo [flags] [task] [-- args...]
  gogo secret set <service> <key> <value>

Flags:
  -l, --list      List available tasks
  -w, --watch     Watch sources and re-run on changes
  -h, --help      Show this help

Commands:
  secret set      Store a secret in the OS keychain`)
}

func secretSet(args []string) error {
	if len(args) != 3 {
		return errors.New("usage: gogo secret set <service> <key> <value>")
	}

	service, key, value := args[0], args[1], args[2]

	if err := taskfile.SetSecret(service, key, value); err != nil {
		return fmt.Errorf("storing secret: %w", err)
	}

	fmt.Fprintf(os.Stderr, "Secret %q stored in keychain %q\n", key, service)
	return nil
}
