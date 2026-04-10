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
	// Handle "secret set" subcommand before arg parsing
	if len(os.Args) >= 3 && strings.Join(os.Args[1:3], " ") == "secret set" {
		return secretSet(os.Args[3:])
	}

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
	defer runner.ClearSecrets()

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

func listTasks() error {
	_, tf, err := loadTaskfile()
	if err != nil {
		return err
	}

	type entry struct {
		name string
		desc string
	}

	var entries []entry
	maxLen := 0
	for _, name := range slices.Sorted(maps.Keys(tf.Tasks)) {
		task := tf.Tasks[name]
		if task.Desc == "" {
			continue
		}
		desc := task.Desc
		if len(task.Aliases) > 0 {
			desc += " (aliases: " + strings.Join(task.Aliases, ", ") + ")"
		}
		entries = append(entries, entry{name, desc})
		maxLen = max(maxLen, len(name))
	}

	for _, e := range entries {
		fmt.Printf("%-*s  %s\n", maxLen, e.name, e.desc)
	}

	return nil
}

func secretSet(args []string) error {
	if len(args) != 3 {
		return errors.New("usage: gogo secret set <service> <key> <value>")
	}

	service, key, value := args[0], args[1], args[2]

	if err := taskfile.SetSecret(service, key, value); err != nil {
		return err
	}

	fmt.Fprintf(os.Stderr, "Secret %q stored in keychain %q\n", key, service)
	return nil
}
