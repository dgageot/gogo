package taskfile

import (
	"fmt"
	"maps"
	"os"
	"regexp"
	"slices"
	"strings"
)

var templatePattern = regexp.MustCompile(`\{\{\s*\.([A-Za-z_][A-Za-z0-9_]*)\s*\}\}`)

// expandEnvTemplates replaces {{.VAR}} references in s with values from the
// process environment. Unknown templates are left verbatim. This is the
// post-parse equivalent of substituting at the byte level: it only touches
// user-supplied string values, never YAML structure or map keys, so an env
// value containing newlines, quotes, or `:` cannot inject new tasks/fields.
func expandEnvTemplates(s string) string {
	return expandTemplates(s, os.LookupEnv)
}

// expandConfigEnvTemplates expands {{.VAR}} env-variable references in every
// user-configurable string field of a parsed Config that is *not* re-expanded
// at run time. Map keys (task names, var names, env keys) are left untouched
// so an environment value can never alter the document's structure.
//
// Fields that go through the run-time resolver (Runner.resolveAllVars,
// resolveEnvValue, expandVars) are deliberately *not* substituted here.
// Substituting them at parse time would inject os.Getenv("VAR") ahead of the
// run-time precedence chain and blur the gogo-var / environment namespaces.
func expandConfigEnvTemplates(c *Config) {
	expandStringSlice(c.Includes)
	expandStringSlice(c.Flatten)
	expandStringSlice(c.Dotenv)
	c.Interval = expandEnvTemplates(c.Interval)
	for name, t := range c.Tasks {
		expandTaskTemplates(&t)
		c.Tasks[name] = t
	}
}

// expandStringSlice rewrites each entry in place. Works for both []string
// and named slice types like StringList because slice headers share storage.
func expandStringSlice(s []string) {
	for i, v := range s {
		s[i] = expandEnvTemplates(v)
	}
}

func expandTaskTemplates(t *Task) {
	t.Dir = expandEnvTemplates(t.Dir)
	// Task-level `if:` never goes through the run-time resolver (it runs
	// before vars are resolved, like preconditions), so it gets the same
	// parse-time env substitution. Cmd-level `if:` goes through expandVars
	// at run time and is deliberately left alone here.
	t.If = expandEnvTemplates(t.If)
	expandStringSlice(t.Sources)
	expandStringSlice(t.Generates)
	expandStringSlice(t.Status)
	expandStringSlice(t.Aliases)
	expandStringSlice(t.Platforms)
	expandStringSlice(t.Dotenv)
	expandStringSlice(t.Requires.Vars)
	expandStringSlice(t.Requires.Env)
	for i, c := range t.Cmds {
		c.Task = expandEnvTemplates(c.Task)
		t.Cmds[i] = c
	}
	for i, d := range t.Deps {
		d.Task = expandEnvTemplates(d.Task)
		t.Deps[i] = d
	}
	for i, p := range t.Preconditions {
		p.Sh = expandEnvTemplates(p.Sh)
		p.Msg = expandEnvTemplates(p.Msg)
		t.Preconditions[i] = p
	}
}

// taskNamespace returns the colon-delimited namespace prefix of a resolved
// task name (everything before the last colon). The root namespace is the
// empty string. A bare task name like "build" lives in the root.
func taskNamespace(name string) string {
	if i := strings.LastIndex(name, ":"); i >= 0 {
		return name[:i]
	}
	return ""
}

// ancestorNamespaces returns all namespaces from the outermost include down
// to and including ns itself, e.g. "a:b:c" -> ["a", "a:b", "a:b:c"]. The
// root namespace is intentionally not included because root vars are
// already pulled from Config.Vars separately. An empty ns yields an empty
// slice. The order matters: callers overlay later entries on top of
// earlier ones so the most-specific namespace wins on key collisions.
func ancestorNamespaces(ns string) []string {
	if ns == "" {
		return nil
	}
	parts := strings.Split(ns, ":")
	out := make([]string, 0, len(parts))
	for i := 1; i <= len(parts); i++ {
		out = append(out, strings.Join(parts[:i], ":"))
	}
	return out
}

// resolveAllVars computes the variables that are actually used by a task.
// Vars are resolved lazily and may reference each other transitively via
// {{.OTHER}} templates; declaration order in YAML is irrelevant. Cycles
// resolve to the empty string (matching task.env cross-references).
//
// Precedence on a name collision: extra (call-site) > task > namespace
// (most-specific wins) > root global. Within each layer the value is
// template-expanded against everything below it, then against the built-in
// lookup (e.g. {{.GIT_COMMIT}}).
func (r *Runner) resolveAllVars(taskName string, task *Task, dir string, extraVars []map[string]Var) (map[string]string, []string, error) {
	type sourceScope int

	const (
		scopeRoot sourceScope = iota
		scopeNamespace
		scopeTask
		scopeExtra
	)

	type source struct {
		v     Var
		dir   string // working directory for `sh:` evaluation
		scope sourceScope
	}

	// Build a unified source map. Later assignments win; we layer root vars
	// first, then ancestor-namespace vars, then task vars, then call-site vars.
	sources := make(map[string]source, len(r.tf.Vars)+len(task.Vars))
	for k, v := range r.tf.Vars {
		sources[k] = source{v: v, dir: r.tf.Dir, scope: scopeRoot}
	}
	for _, ns := range ancestorNamespaces(taskNamespace(taskName)) {
		nsDir := r.tf.NamespaceDirs[ns]
		if nsDir == "" {
			nsDir = r.tf.Dir
		}
		for k, v := range r.tf.NamespaceVars[ns] {
			sources[k] = source{v: v, dir: nsDir, scope: scopeNamespace}
		}
	}
	for k, v := range task.Vars {
		sources[k] = source{v: v, dir: dir, scope: scopeTask}
	}
	for _, ev := range extraVars {
		for k, v := range ev {
			sources[k] = source{v: v, dir: dir, scope: scopeExtra}
		}
	}

	resolved := map[string]string{
		"TASK_FILE_DIR": dir,
	}
	used := make(map[string]struct{})
	visiting := make(map[string]struct{})
	var firstErr error

	var lookup func(key string) (string, bool)
	lookup = func(key string) (string, bool) {
		if firstErr != nil {
			return "", false
		}
		if v, ok := resolved[key]; ok {
			return v, true
		}
		if _, onPath := visiting[key]; onPath {
			return "", true
		}
		s, ok := sources[key]
		if !ok {
			return r.builtinLookup(key)
		}
		used[key] = struct{}{}
		visiting[key] = struct{}{}
		defer delete(visiting, key)

		var value string
		if s.v.Sh != "" {
			cmdLine := expandTemplates(s.v.Sh, lookup)
			out, err := r.ShellRunner.Output(ShellCommand{
				Kind:    ShellCommandVar,
				Command: cmdLine,
				Dir:     s.dir,
			})
			if err != nil {
				firstErr = fmt.Errorf("resolving variable (sh: %s): %w", s.v.Sh, err)
				return "", true
			}
			value = strings.TrimSpace(string(out))
		} else {
			value = expandTemplates(s.v.Value, lookup)
		}
		resolved[key] = value
		return value, true
	}

	for _, name := range referencedVars(task) {
		lookup(name)
		if firstErr != nil {
			return nil, nil, firstErr
		}
	}

	unused := make([]string, 0)
	for _, name := range slices.Sorted(maps.Keys(sources)) {
		s := sources[name]
		if s.scope != scopeTask && s.scope != scopeExtra {
			continue
		}
		if _, ok := used[name]; !ok {
			unused = append(unused, name)
		}
	}
	return resolved, unused, nil
}

func referencedVars(task *Task) []string {
	refs := make(map[string]struct{})
	for _, name := range task.Requires.Vars {
		refs[name] = struct{}{}
	}
	for _, cmd := range task.Cmds {
		for _, name := range templateNames(cmd.Cmd) {
			refs[name] = struct{}{}
		}
		for _, name := range templateNames(cmd.Defer) {
			refs[name] = struct{}{}
		}
		for _, name := range templateNames(cmd.If) {
			refs[name] = struct{}{}
		}
		for _, v := range cmd.Vars {
			for _, name := range templateNames(v.Value) {
				refs[name] = struct{}{}
			}
			for _, name := range templateNames(v.Sh) {
				refs[name] = struct{}{}
			}
		}
	}
	out := slices.Sorted(maps.Keys(refs))
	return out
}

func templateNames(s string) []string {
	matches := templatePattern.FindAllStringSubmatch(s, -1)
	if len(matches) == 0 {
		return nil
	}
	refs := make(map[string]struct{}, len(matches))
	for _, match := range matches {
		refs[match[1]] = struct{}{}
	}
	return slices.Sorted(maps.Keys(refs))
}

// expandTemplates substitutes only {{.VAR}} references in s using the supplied
// lookup. Unknown templates are left verbatim. Shell-style $VAR/${VAR}
// references are intentionally left untouched here: they belong to the
// environment namespace and are interpreted by the shell (or by env-specific
// expansion in resolveEnvValue).
func expandTemplates(s string, lookup func(string) (string, bool)) string {
	return templatePattern.ReplaceAllStringFunc(s, func(match string) string {
		key := templatePattern.FindStringSubmatch(match)[1]
		if val, ok := lookup(key); ok {
			return val
		}
		return match
	})
}

// expandShellEnv substitutes $VAR/${VAR} references in s using lookup.
// Unknown references are preserved so the downstream shell (or awk/sed/jq)
// can interpret them.
func expandShellEnv(s string, lookup func(string) (string, bool)) string {
	return os.Expand(s, func(key string) string {
		if val, ok := lookup(key); ok {
			return val
		}
		return unknownShellVarSpan(key)
	})
}

// unknownShellVarSpan returns the literal text that should replace an
// unresolved `$key` reference so the downstream shell (or awk/sed/jq)
// gets the chance to interpret it. os.Expand's tokenizer reports both
// `${KEY}` and `$KEY` through the same callback with no way to tell
// them apart, so we always emit `${KEY}` — except for the shell-special
// single-character names ('*', '#', '$', '@', '!', '?', '-', '0'-'9'),
// which we emit *without* braces. That preserves `$2` (awk's positional
// reference) verbatim, which previously got mangled into `${2}` and
// crashed awk with a syntax error.
func unknownShellVarSpan(key string) string {
	if len(key) == 1 && isShellSpecialChar(key[0]) {
		return "$" + key
	}
	return "${" + key + "}"
}

// isShellSpecialChar mirrors os.Expand's getShellName logic: these are the
// one-character names that `$` recognises in shell (positional params and
// other special variables). Matching here keeps unknownShellVarSpan and
// os.Expand in lockstep.
func isShellSpecialChar(c byte) bool {
	switch c {
	case '*', '#', '$', '@', '!', '?', '-':
		return true
	}
	return c >= '0' && c <= '9'
}

// expandVars substitutes gogo template variables in a command string.
// {{.VAR}} resolves from task variables, CLI_ARGS, and optional built-ins.
// Shell-style $VAR/${VAR} references are left untouched for the shell to
// resolve from the task environment.
func expandVars(s string, vars map[string]string, cliArgs string, builtin func(string) (string, bool)) string {
	return expandTemplates(s, func(key string) (string, bool) {
		if val, ok := vars[key]; ok {
			return val, true
		}
		if key == "CLI_ARGS" {
			return cliArgs, true
		}
		if builtin != nil {
			if val, ok := builtin(key); ok {
				return val, true
			}
		}
		return "", false
	})
}
