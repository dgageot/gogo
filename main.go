package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"maps"
	"os"
	"os/signal"
	"slices"
	"strings"
	"syscall"
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

// App is the gogo command-line application. External dependencies (args, I/O,
// and working-directory lookup) are injected so Run can be driven from tests
// without touching os.Args, process stdio, or the real cwd.
type App struct {
	Args   []string               // command-line args, without program name
	Stdout io.Writer              // user-visible output (help, --list, completion scripts)
	Stderr io.Writer              // error messages
	Getwd  func() (string, error) // working directory lookup
}

func main() {
	app := &App{
		Args:   os.Args[1:],
		Stdout: os.Stdout,
		Stderr: os.Stderr,
		Getwd:  os.Getwd,
	}
	if err := app.Run(context.Background()); err != nil {
		fmt.Fprintln(app.Stderr, err)
		os.Exit(1)
	}
}

// Run executes the gogo CLI with the configured args and I/O.
func (a *App) Run(ctx context.Context) error {
	parsed, err := a.parseArgs()
	if err != nil {
		return err
	}
	if parsed == nil {
		return nil // help or version was printed
	}

	if parsed.Completion != "" {
		return a.printCompletionScript(parsed.Completion)
	}

	if parsed.Complete {
		a.printTaskNames()
		return nil
	}

	if parsed.List {
		return a.listTasks()
	}

	dir, tf, err := a.loadTaskfile()
	if err != nil {
		return err
	}

	cliArgs := shellJoin(parsed.CLIArgs)
	runner, err := taskfile.NewRunner(tf, dir)
	if err != nil {
		return err
	}
	runner.IO.Stdout = a.Stdout
	runner.IO.Stderr = a.Stderr
	runner.DryRun = parsed.DryRun
	runner.Force = parsed.Force

	if parsed.Watch {
		sigCtx, stop := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
		defer stop()

		interval, err := watchInterval(tf.Interval)
		if err != nil {
			return err
		}

		err = runner.Watch(sigCtx, parsed.Task, cliArgs, interval)
		if err != nil && sigCtx.Err() != nil {
			return nil // graceful shutdown
		}
		return err
	}

	return runner.Run(parsed.Task, cliArgs)
}

// shellJoin quotes each CLI argument so it survives splicing into a
// /bin/sh command line as a single word. This preserves argument
// boundaries that strings.Join would otherwise lose.
func shellJoin(args []string) string {
	quoted := make([]string, len(args))
	for i, a := range args {
		quoted[i] = shellQuote(a)
	}
	return strings.Join(quoted, " ")
}

// shellQuote wraps a single argument in single quotes. An embedded single
// quote is written as the standard sh idiom: a closing quote, an escaped
// quote, and a re-opening quote.
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// watchInterval parses the taskfile's `interval` setting, falling back to a
// sensible default when unset. Invalid values are surfaced as errors rather
// than silently ignored.
func watchInterval(raw string) (time.Duration, error) {
	const defaultInterval = 500 * time.Millisecond
	if raw == "" {
		return defaultInterval, nil
	}
	d, err := time.ParseDuration(raw)
	if err != nil {
		return 0, fmt.Errorf("invalid interval %q: %w", raw, err)
	}
	return d, nil
}

// parseArgs parses command-line arguments. Returns nil if help/version was shown.
func (a *App) parseArgs() (*args, error) {
	var parsed args
	p, err := arg.NewParser(arg.Config{Program: "gogo"}, &parsed)
	if err != nil {
		return nil, err
	}

	if err := p.Parse(a.Args); err != nil {
		switch {
		case errors.Is(err, arg.ErrHelp):
			p.WriteHelp(a.Stdout)
			return nil, nil
		case errors.Is(err, arg.ErrVersion):
			return nil, nil
		default:
			return nil, err
		}
	}

	return &parsed, nil
}

func (a *App) loadTaskfile() (string, *taskfile.Taskfile, error) {
	cwd, err := a.Getwd()
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

func (a *App) printCompletionScript(shell string) error {
	switch shell {
	case "bash":
		fmt.Fprint(a.Stdout, bashCompletion)
	case "zsh":
		fmt.Fprint(a.Stdout, zshCompletion)
	case "fish":
		fmt.Fprint(a.Stdout, fishCompletion)
	default:
		return fmt.Errorf("unsupported shell: %s (valid: bash, zsh, fish)", shell)
	}
	return nil
}

func (a *App) printTaskNames() {
	_, tf, err := a.loadTaskfile()
	if err != nil {
		return // silently fail during completion
	}

	for _, name := range visibleTaskNames(tf) {
		fmt.Fprintln(a.Stdout, name)
	}
}

const bashCompletion = `_gogo_completions() {
	# Reconstruct the current word treating ':' as part of it.
	# Bash includes ':' in COMP_WORDBREAKS, which would otherwise break
	# completion of namespaced tasks like 'assistant:evals'.
	local cur="${COMP_WORDS[COMP_CWORD]}"
	if [[ "$cur" == ":" && $COMP_CWORD -ge 1 ]]; then
		cur="${COMP_WORDS[COMP_CWORD-1]}:"
	elif [[ $COMP_CWORD -ge 2 && "${COMP_WORDS[COMP_CWORD-1]}" == ":" ]]; then
		cur="${COMP_WORDS[COMP_CWORD-2]}:${cur}"
	fi

	COMPREPLY=($(compgen -W "$(gogo --complete 2>/dev/null)" -- "$cur"))

	# Strip the prefix up to and including the last ':' from each completion,
	# since bash will only insert the suffix after the last word break.
	if [[ "$cur" == *:* ]]; then
		local i prefix="${cur%:*}:"
		for i in "${!COMPREPLY[@]}"; do
			COMPREPLY[i]="${COMPREPLY[i]#$prefix}"
		done
	fi
}
complete -F _gogo_completions gogo
`

const zshCompletion = `#compdef gogo
_gogo() {
	local -a tasks
	tasks=("${(@f)$(gogo --complete 2>/dev/null)}")
	# Use compadd directly because _describe treats ':' as a value/description
	# separator, which would break namespaced tasks like 'assistant:evals'.
	compadd -a tasks
}
compdef _gogo gogo
`

const fishCompletion = `complete -c gogo -f -a '(gogo --complete 2>/dev/null)'
`

func (a *App) listTasks() error {
	_, tf, err := a.loadTaskfile()
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
		fmt.Fprintf(a.Stdout, "%-*s  %s\n", maxLen, e.name, e.desc)
	}

	return nil
}
