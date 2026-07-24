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
	Yes        bool     `arg:"-y,--yes" help:"auto-confirm task prompts"`
	Completion string   `arg:"--completion" help:"print shell completion script (bash|zsh|fish)"`
	Complete   bool     `arg:"--complete,hidden"`
	Tasks      []string `arg:"positional" help:"tasks to run in sequence"`
	// CLIArgs is populated by parseArgs from the tail after `--`; it must
	// not be exposed as a flag (`arg:"-"`) or go-arg would advertise a
	// stray `--cliargs` option that would be silently overwritten.
	CLIArgs []string `arg:"-"`
}

func (args) Description() string {
	return "gogo - a simple task runner"
}

// App is the gogo command-line application. External dependencies (args, I/O,
// and working-directory lookup) are injected so Run can be driven from tests
// without touching os.Args, process stdio, or the real cwd.
type App struct {
	Args   []string               // command-line args, without program name
	Stdin  io.Reader              // interactive input (task prompts); nil means no input
	Stdout io.Writer              // user-visible output (help, --list, completion scripts)
	Stderr io.Writer              // error messages
	Getwd  func() (string, error) // working directory lookup
}

func main() {
	app := &App{
		Args:   os.Args[1:],
		Stdin:  os.Stdin,
		Stdout: os.Stdout,
		Stderr: os.Stderr,
		Getwd:  os.Getwd,
	}
	if err := app.Run(context.Background()); err != nil {
		if !errors.Is(err, errSilent) {
			fmt.Fprintln(app.Stderr, err)
		}
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
		return a.listTasks(ctx)
	}

	dir, tf, err := a.loadConfig()
	if err != nil {
		if handled, fbErr := a.tryForeignFallback(ctx, parsed); handled {
			return fbErr
		}
		return err
	}

	cliArgs := shellJoin(parsed.CLIArgs)
	runner, err := taskfile.NewRunner(tf, dir)
	if err != nil {
		return err
	}
	// Stdin is wired only when provided so embedders that leave it nil keep
	// the runner's default (the process stdin).
	if a.Stdin != nil {
		runner.IO.Stdin = a.Stdin
	}
	runner.IO.Stdout = a.Stdout
	runner.IO.Stderr = a.Stderr
	runner.DryRun = parsed.DryRun
	runner.Force = parsed.Force
	runner.AssumeYes = parsed.Yes

	taskNames := defaultTaskNames(parsed.Tasks, tf)

	if parsed.Watch {
		if len(taskNames) != 1 {
			return errors.New("--watch requires exactly one task")
		}
		sigCtx, stop := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
		defer stop()

		interval, err := watchInterval(tf.Interval)
		if err != nil {
			return err
		}

		err = runner.Watch(sigCtx, taskNames[0], cliArgs, interval)
		if err != nil && sigCtx.Err() != nil {
			return nil // graceful shutdown
		}
		return a.handleRunError(tf, err)
	}

	// Run each task in sequence as if it were a separate gogo invocation.
	// ResetRan between iterations so dependencies re-run for the next task —
	// `gogo clean install` must run `clean` then `install` (and its `clean`
	// dep again, if any), not collapse them via memoization.
	for i, name := range taskNames {
		if i > 0 {
			runner.ResetRan()
		}
		if err := a.handleRunError(tf, runner.Run(name, cliArgs)); err != nil {
			return err
		}
	}
	return nil
}

// handleRunError augments a TaskNotFoundError with the human-friendly task
// listing so a typo prints — in one shot — both what the user *could* have
// run and the closest match we know about. When the unresolved name matches
// the local segment of one or more namespaced tasks (e.g. `gogo dev` in a
// parent that only defines `frontend:dev` and `backend:dev`), the listing is
// narrowed to just those candidates rather than the whole index. Other
// errors pass through untouched so the existing error surface is unchanged.
func (a *App) handleRunError(tf *taskfile.Config, err error) error {
	var nfe *taskfile.TaskNotFoundError
	if !errors.As(err, &nfe) {
		return err
	}
	listings := gatherTaskListings(tf)
	if matches := gatherNamespacedMatches(tf, nfe.Name); len(matches) > 0 {
		listings = matches
	}
	writeTaskListings(a.Stderr, listings)
	fmt.Fprintf(a.Stderr, "%stask: %s%s\n", ansiRed, nfe.Error(), ansiReset)
	return errSilent
}

// errSilent signals that the app already printed the user-facing error
// itself and main shouldn't print it again. The exit code stays non-zero.
var errSilent = errors.New("")

// defaultTaskNames picks the tasks to run when no positional args were
// given. A top-level `default:` field in the task file wins over the
// implicit "task literally named default" convention, which lets users
// skip the `default:` trampoline entirely.
func defaultTaskNames(parsed []string, tf *taskfile.Config) []string {
	if len(parsed) > 0 {
		return parsed
	}
	if tf.Default != "" {
		return []string{tf.Default}
	}
	return []string{"default"}
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

// watchInterval parses the task file's `interval` setting, falling back to a
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
//
// We pre-split args at the first standalone `--` so multiple positional task
// names work: `gogo clean install` runs both, while `gogo test -- -v` still
// forwards `-v` as `{{.CLI_ARGS}}`. go-arg's own `--` handling can't do this
// because it would absorb everything after `--` into the same positional
// slice as the task names.
func (a *App) parseArgs() (*args, error) {
	head, tail := splitAtDoubleDash(a.Args)

	var parsed args
	p, err := arg.NewParser(arg.Config{Program: "gogo"}, &parsed)
	if err != nil {
		return nil, err
	}

	if err := p.Parse(head); err != nil {
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

	parsed.CLIArgs = tail
	return &parsed, nil
}

// splitAtDoubleDash splits args at the first standalone `--`. The separator
// itself is dropped. If `--` is absent, tail is nil.
func splitAtDoubleDash(args []string) (head, tail []string) {
	for i, a := range args {
		if a == "--" {
			return args[:i], args[i+1:]
		}
	}
	return args, nil
}

func (a *App) loadConfig() (string, *taskfile.Config, error) {
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
func visibleTaskNames(tf *taskfile.Config) []string {
	var names []string
	for _, name := range slices.Sorted(maps.Keys(tf.Tasks)) {
		if !taskfile.IsInternalTask(name) {
			names = append(names, name)
		}
	}
	return names
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
	_, tf, err := a.loadConfig()
	if err != nil {
		return // silently fail during completion
	}

	for _, name := range visibleTaskNames(tf) {
		fmt.Fprintln(a.Stdout, name)
	}
}

// listTasks renders the colorized, column-aligned task index for `--list`.
// When no gogo.yaml is found, listing is delegated to a colocated foreign
// task file's runner (e.g. `task --list`) so users don't have to learn a
// second listing flag per tool.
func (a *App) listTasks(ctx context.Context) error {
	_, tf, err := a.loadConfig()
	if err != nil {
		if handled, fbErr := a.tryForeignListFallback(ctx); handled {
			return fbErr
		}
		return err
	}
	writeTaskListings(a.Stdout, gatherTaskListings(tf))
	return nil
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

// ANSI escape codes used to colorize CLI output.
const (
	ansiReset = "\x1b[0m"
	ansiGreen = "\x1b[32m"
	ansiCyan  = "\x1b[36m"
	ansiRed   = "\x1b[31m"
)

// taskListing is one row in the --list output, plus the metadata needed to
// render and align it consistently with siblings.
type taskListing struct {
	name    string
	desc    string
	aliases string // pre-formatted "(aliases: a, b)" or empty
}

// newTaskListing builds the listing row for one task: the first line of its
// description (trimmed) plus the pre-formatted alias cell.
func newTaskListing(name string, task taskfile.Task) taskListing {
	desc, _, _ := strings.Cut(task.Desc, "\n")
	row := taskListing{name: name, desc: strings.TrimSpace(desc)}
	if len(task.Aliases) > 0 {
		row.aliases = "(aliases: " + strings.Join(task.Aliases, ", ") + ")"
	}
	return row
}

// gatherTaskListings returns the rows that --list and the unknown-task hint
// should print, in declaration-friendly alphabetical order. Tasks without a
// description are intentionally omitted: we surface only what the author
// chose to advertise. Multi-line descriptions are truncated to their first
// line so each task stays on a single, aligned row.
func gatherTaskListings(tf *taskfile.Config) []taskListing {
	var entries []taskListing
	for _, name := range visibleTaskNames(tf) {
		row := newTaskListing(name, tf.Tasks[name])
		if row.desc == "" {
			continue
		}
		entries = append(entries, row)
	}
	return entries
}

// gatherNamespacedMatches returns the listings for visible tasks whose local
// segment (the part after the last colon) equals the unresolved name. This
// powers the `gogo dev` case in a parent of several sub-projects: when no
// root `dev` exists but `frontend:dev` and `backend:dev` do, we point the
// user at those concrete tasks instead of dumping the whole index. Unlike
// gatherTaskListings, tasks without a description are kept — the user asked
// for this exact name, so every namespace that provides it is worth showing.
// The name must be a bare segment (no colon of its own) so we only narrow
// when the user typed an unqualified task name. Returns nil when there's
// nothing to narrow to, in which case the caller falls back to the full
// listing.
func gatherNamespacedMatches(tf *taskfile.Config, name string) []taskListing {
	if name == "" || strings.Contains(name, ":") {
		return nil
	}
	var matches []taskListing
	for _, taskName := range visibleTaskNames(tf) {
		if !strings.Contains(taskName, ":") || lastSegment(taskName) != name {
			continue
		}
		matches = append(matches, newTaskListing(taskName, tf.Tasks[taskName]))
	}
	return matches
}

// lastSegment returns the part of a colon-joined task name after the final
// colon — the task's local name within its namespace.
func lastSegment(name string) string {
	if i := strings.LastIndex(name, ":"); i >= 0 {
		return name[i+1:]
	}
	return name
}

// writeTaskListings prints rows in three aligned columns: a green
// `* name` cell, the description, and an optional cyan `(aliases: ...)`
// cell. Widths are computed from the un-colored text so ANSI escapes don't
// corrupt column alignment.
func writeTaskListings(w io.Writer, entries []taskListing) {
	if len(entries) == 0 {
		return
	}

	nameWidth, descWidth := 0, 0
	for _, e := range entries {
		nameWidth = max(nameWidth, len(e.name))
		descWidth = max(descWidth, len(e.desc))
	}

	for _, e := range entries {
		namePad := strings.Repeat(" ", nameWidth-len(e.name))
		if e.aliases == "" {
			fmt.Fprintf(w, "* %s%s%s%s  %s\n", ansiGreen, e.name, ansiReset, namePad, e.desc)
			continue
		}
		descPad := strings.Repeat(" ", descWidth-len(e.desc))
		fmt.Fprintf(w, "* %s%s%s%s  %s%s  %s%s%s\n",
			ansiGreen, e.name, ansiReset, namePad,
			e.desc, descPad,
			ansiCyan, e.aliases, ansiReset)
	}
}
