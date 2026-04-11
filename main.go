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

	arg "github.com/alexflint/go-arg"

	"github.com/dgageot/gogo/taskfile"
)

type args struct {
	List    bool     `arg:"-l,--list" help:"list available tasks"`
	Watch   bool     `arg:"-w,--watch" help:"watch sources and re-run on changes"`
	Force   bool     `arg:"-f,--force" help:"ignore sources and generates (always run)"`
	DryRun  bool     `arg:"-n,--dry" help:"print commands without executing them"`
	Task    string   `arg:"positional" default:"default" help:"task to run"`
	CLIArgs []string `arg:"positional" help:"arguments passed to the task (after --)"`
}

func (args) Description() string {
	return "gogo - a simple task runner"
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	a, err := parseArgs()
	if err != nil {
		return err
	}
	if a == nil {
		return nil // help or version was printed
	}

	if a.List {
		return listTasks()
	}

	dir, tf, err := loadTaskfile()
	if err != nil {
		return err
	}

	cliArgs := strings.Join(a.CLIArgs, " ")
	runner := taskfile.NewRunner(tf, dir)
	runner.DryRun = a.DryRun
	runner.Force = a.Force

	if a.Watch {
		parsed, _ := time.ParseDuration(tf.Interval)
		return runner.Watch(a.Task, cliArgs, cmp.Or(parsed, 500*time.Millisecond))
	}

	return runner.Run(a.Task, cliArgs)
}

// parseArgs parses command-line arguments. Returns nil if help/version was shown.
func parseArgs() (*args, error) {
	var a args
	p, err := arg.NewParser(arg.Config{Program: "gogo"}, &a)
	if err != nil {
		return nil, err
	}

	if err := p.Parse(os.Args[1:]); err != nil {
		switch {
		case errors.Is(err, arg.ErrHelp):
			p.WriteHelp(os.Stdout)
			return nil, nil
		case errors.Is(err, arg.ErrVersion):
			return nil, nil
		default:
			return nil, err
		}
	}

	return &a, nil
}

func loadTaskfile() (string, *taskfile.Taskfile, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return "", nil, err
	}

	rootDir, err := taskfile.FindRootDir(cwd)
	if err != nil {
		return "", nil, err
	}

	tf, err := taskfile.LoadWithIncludes(rootDir)
	if err != nil {
		return "", nil, err
	}

	return cwd, tf, nil
}

// isInternalTask reports whether a task name is internal (starts with _).
// For namespaced tasks like "ns:_helper", the task part after the last colon is checked.
func isInternalTask(name string) bool {
	if i := strings.LastIndex(name, ":"); i >= 0 {
		name = name[i+1:]
	}
	return strings.HasPrefix(name, "_")
}

func listTasks() error {
	_, tf, err := loadTaskfile()
	if err != nil {
		return err
	}

	sortedNames := slices.Sorted(maps.Keys(tf.Tasks))

	// First pass: compute max name length for alignment
	maxLen := 0
	for _, name := range sortedNames {
		if tf.Tasks[name].Desc != "" && !isInternalTask(name) {
			maxLen = max(maxLen, len(name))
		}
	}

	// Second pass: print tasks with descriptions
	for _, name := range sortedNames {
		task := tf.Tasks[name]
		if task.Desc == "" || isInternalTask(name) {
			continue
		}
		desc := task.Desc
		if len(task.Aliases) > 0 {
			desc += " (aliases: " + strings.Join(task.Aliases, ", ") + ")"
		}
		fmt.Printf("%-*s  %s\n", maxLen, name, desc)
	}

	return nil
}
