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
	if len(os.Args) >= 3 && os.Args[1] == "secret" && os.Args[2] == "set" {
		return secretSet(os.Args[3:])
	}

	var a args
	p, err := arg.NewParser(arg.Config{Program: "gogo"}, &a)
	if err != nil {
		return err
	}

	if err := p.Parse(os.Args[1:]); err != nil {
		switch {
		case errors.Is(err, arg.ErrHelp):
			p.WriteHelp(os.Stdout)
			return nil
		case errors.Is(err, arg.ErrVersion):
			return nil
		default:
			return err
		}
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

	if a.Watch {
		parsed, _ := time.ParseDuration(tf.Interval)
		return runner.Watch(a.Task, cliArgs, cmp.Or(parsed, 500*time.Millisecond))
	}

	return runner.Run(a.Task, cliArgs)
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

	names := slices.Sorted(maps.Keys(tf.Tasks))
	names = slices.DeleteFunc(names, func(name string) bool {
		return tf.Tasks[name].Desc == ""
	})

	if len(names) == 0 {
		return nil
	}

	var maxLen int
	for _, name := range names {
		maxLen = max(maxLen, len(name))
	}

	for _, name := range names {
		task := tf.Tasks[name]
		aliases := ""
		if len(task.Aliases) > 0 {
			aliases = " (aliases: " + strings.Join(task.Aliases, ", ") + ")"
		}
		fmt.Printf("%-*s  %s%s\n", maxLen, name, task.Desc, aliases)
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
