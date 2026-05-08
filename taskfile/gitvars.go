package taskfile

import (
	"strings"
	"sync"
)

// builtinGitVars enumerates the {{.GIT_*}} names gogo synthesises by shelling
// out to git. Resolution is lazy — none of these commands run until a task
// actually references the name — and the result is cached for the lifetime
// of the Runner. Outside a git repository (or on any error) the value
// resolves to the empty string so missing `.git` never breaks `gogo`.
var builtinGitVars = []string{
	"GIT_COMMIT",       // full SHA at HEAD
	"GIT_SHORT_COMMIT", // 7-char SHA at HEAD
	"GIT_TAG",          // exact-match tag at HEAD; empty when none
	"GIT_BRANCH",       // current branch name; "HEAD" when detached
	"GIT_DIRTY",        // "dirty" if the working tree has changes; empty otherwise
}

// gitVars memoises each built-in git command exactly once per Runner via
// sync.OnceValue, regardless of how many tasks reference the name.
type gitVars struct {
	commit      func() string
	shortCommit func() string
	tag         func() string
	branch      func() string
	dirty       func() string
}

// newGitVars wires the lazy resolvers to the Runner's ShellRunner so tests
// can intercept git invocations the same way they do for `vars: { sh: ... }`.
func newGitVars(dir string, sh ShellRunner) *gitVars {
	once := func(command string) func() string {
		return sync.OnceValue(func() string {
			out, err := sh.Output(ShellCommand{
				Kind:    ShellCommandVar,
				Command: command,
				Dir:     dir,
			})
			if err != nil {
				return ""
			}
			return strings.TrimSpace(string(out))
		})
	}
	return &gitVars{
		commit:      once("git rev-parse HEAD"),
		shortCommit: once("git rev-parse --short=7 HEAD"),
		// `2>/dev/null` keeps git's "no exact match" stderr noise out of the
		// terminal when GIT_TAG is referenced on a commit without a tag.
		tag:    once("git describe --tags --exact-match HEAD 2>/dev/null"),
		branch: once("git rev-parse --abbrev-ref HEAD"),
		// `--porcelain` is stable across git versions; non-empty output means
		// the working tree has uncommitted changes.
		dirty: once(`if [ -n "$(git status --porcelain 2>/dev/null)" ]; then echo dirty; fi`),
	}
}

// lookup returns the value of a built-in git variable. The boolean is true
// only for known names; values may be empty (e.g. outside a git repo, or
// for GIT_TAG when no exact-match tag exists). The empty/known distinction
// matters for `requires.vars`, which must accept an empty built-in as "set".
func (g *gitVars) lookup(name string) (string, bool) {
	if g == nil {
		return "", false
	}
	switch name {
	case "GIT_COMMIT":
		return g.commit(), true
	case "GIT_SHORT_COMMIT":
		return g.shortCommit(), true
	case "GIT_TAG":
		return g.tag(), true
	case "GIT_BRANCH":
		return g.branch(), true
	case "GIT_DIRTY":
		return g.dirty(), true
	}
	return "", false
}
