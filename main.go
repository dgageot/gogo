package main

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"maps"
	"os"
	"os/signal"
	"slices"
	"strings"
	"time"

	arg "github.com/alexflint/go-arg"

	"github.com/dgageot/gogo/taskfile"
)

type args struct {
	List       bool     `arg:"-l,--list" help:"list available tasks"`
	Watch      bool     `arg:"-w,--watch" help:"watch sources and re-run on changes"`
	Force      bool     `arg:"-f,--force" help:"ignore sources and generates (always run)"`
	DryRun     bool     `arg:"-n,--dry" help:"print commands without executing them"`
	Completion string   `arg:"--completion" help:"print shell completion script (bash|zsh|fish)"`
	Complete   bool     `arg:"--complete,hidden"`
	Task       string   `arg:"positional" default:"default" help:"task to run"`
	CLIArgs    []string `arg:"positional" help:"arguments passed to the task (after --)"`
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

	if a.Completion != "" {
		return printCompletionScript(a.Completion)
	}

	if a.Complete {
		printTaskNames()
		return nil
	}

	if a.List {
		return listTasks()
	}

	dir, tf, err := loadTaskfile()
	if err != nil {
		return err
	}

	cliArgs := strings.Join(a.CLIArgs, " ")
	runner, err := taskfile.NewRunner(tf, dir)
	if err != nil {
		return err
	}
	runner.DryRun = a.DryRun
	runner.Force = a.Force

	if a.Watch {
		ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
		defer stop()

		parsed, _ := time.ParseDuration(tf.Interval)
		err := runner.Watch(ctx, a.Task, cliArgs, cmp.Or(parsed, 500*time.Millisecond))
		if err != nil && ctx.Err() != nil {
			return nil // graceful shutdown
		}
		return err
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

// visibleTaskNames returns sorted task names, excluding internal tasks.
func visibleTaskNames(tf *taskfile.Taskfile) []string {
	var names []string
	for _, name := range slices.Sorted(maps.Keys(tf.Tasks)) {
		if !isInternalTask(name) {
			names = append(names, name)
		}
	}
	return names
}

// isInternalTask reports whether a task name is internal (starts with _).
// For namespaced tasks like "ns:_helper", the task part after the last colon is checked.
func isInternalTask(name string) bool {
	if i := strings.LastIndex(name, ":"); i >= 0 {
		name = name[i+1:]
	}
	return strings.HasPrefix(name, "_")
}

func printCompletionScript(shell string) error {
	switch shell {
	case "bash":
		fmt.Print(bashCompletion)
	case "zsh":
		fmt.Print(zshCompletion)
	case "fish":
		fmt.Print(fishCompletion)
	default:
		return fmt.Errorf("unsupported shell: %s (valid: bash, zsh, fish)", shell)
	}
	return nil
}

func printTaskNames() {
	_, tf, err := loadTaskfile()
	if err != nil {
		return // silently fail during completion
	}

	for _, name := range visibleTaskNames(tf) {
		fmt.Println(name)
	}
}

const bashCompletion = `_gogo_completions() {
	local cur="${COMP_WORDS[COMP_CWORD]}"
	COMPREPLY=($(compgen -W "$(gogo --complete 2>/dev/null)" -- "$cur"))
}
complete -F _gogo_completions gogo
`

const zshCompletion = `#compdef gogo
_gogo() {
	local -a tasks
	tasks=("${(@f)$(gogo --complete 2>/dev/null)}")
	_describe 'task' tasks
}
compdef _gogo gogo
`

const fishCompletion = `complete -c gogo -f -a '(gogo --complete 2>/dev/null)'
`

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
	for _, name := range visibleTaskNames(tf) {
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
