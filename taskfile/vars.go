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
	return templatePattern.ReplaceAllStringFunc(s, func(match string) string {
		name := templatePattern.FindStringSubmatch(match)[1]
		if val, ok := os.LookupEnv(name); ok {
			return val
		}
		return match
	})
}

// expandConfigEnvTemplates expands {{.VAR}} env-variable references in every
// user-configurable string field of a parsed Config. Map keys (task names,
// var names, env keys) are left untouched so an environment value can never
// alter the document's structure.
func expandConfigEnvTemplates(c *Config) {
	expandStringSlice(c.Includes)
	expandStringSlice(c.Flatten)
	expandStringSlice(c.Dotenv)
	c.Interval = expandEnvTemplates(c.Interval)
	expandVarsMap(c.Vars)
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

func expandVarsMap(m map[string]Var) {
	for k, v := range m {
		v.Value = expandEnvTemplates(v.Value)
		v.Sh = expandEnvTemplates(v.Sh)
		m[k] = v
	}
}

func expandTaskTemplates(t *Task) {
	t.Dir = expandEnvTemplates(t.Dir)
	expandStringSlice(t.Sources)
	expandStringSlice(t.Generates)
	expandStringSlice(t.Aliases)
	expandStringSlice(t.Platforms)
	expandStringSlice(t.Dotenv)
	expandStringSlice(t.Requires.Vars)
	expandStringSlice(t.Requires.Env)
	for k, v := range t.Env {
		t.Env[k] = expandEnvTemplates(v)
	}
	expandVarsMap(t.Vars)
	for i, c := range t.Cmds {
		c.Cmd = expandEnvTemplates(c.Cmd)
		c.Task = expandEnvTemplates(c.Task)
		expandVarsMap(c.Vars)
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

// resolveAllVars computes the effective variables for a task. Vars are
// resolved lazily and may reference each other (transitively) via {{.OTHER}}
// or ${OTHER} — declaration order in YAML is irrelevant. Cycles resolve to
// the empty string (matching how task.env cross-references behave).
//
// Precedence on a name collision: extra (call-site) > task > global. Within
// each layer the value is template-expanded against everything below it,
// then against the built-in lookup (e.g. {{.GIT_COMMIT}}).
func (r *Runner) resolveAllVars(task *Task, dir string, extraVars []map[string]Var) (map[string]string, error) {
	type source struct {
		v   Var
		dir string // working directory for `sh:` evaluation
	}

	// Build a unified source map. Later assignments win; we layer global
	// vars first, then task vars, then any call-site extra vars.
	sources := make(map[string]source, len(r.tf.Vars)+len(task.Vars))
	for k, v := range r.tf.Vars {
		sources[k] = source{v, r.tf.Dir}
	}
	for k, v := range task.Vars {
		sources[k] = source{v, dir}
	}
	for _, ev := range extraVars {
		for k, v := range ev {
			sources[k] = source{v, dir}
		}
	}

	resolved := map[string]string{
		"TASK_FILE_DIR": dir,
	}
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
		// Cycles short-circuit to the empty string. Treating the value as
		// "resolved" lets the rest of the expansion finish so the user gets
		// a complete (if mostly empty) value to debug from.
		if _, onPath := visiting[key]; onPath {
			return "", true
		}
		s, ok := sources[key]
		if !ok {
			// Unknown user-vars fall through to gogo built-ins. CLI_ARGS, OS
			// env and the cliArgs fallback are NOT consulted here — those
			// only apply when expanding command strings, not var bodies.
			return r.builtinLookup(key)
		}
		visiting[key] = struct{}{}
		defer delete(visiting, key)

		var value string
		if s.v.Sh != "" {
			// Expand template references inside the shell command itself so
			// patterns like `sh: echo {{.IMAGE}}-{{.GIT_TAG}}.tar` work.
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

	// Force-resolve every declared name so the returned map is complete:
	// downstream consumers (env composition, requires, expandVars on cmd
	// strings) read from a flat map and don't go through this lookup again.
	for _, name := range slices.Sorted(maps.Keys(sources)) {
		lookup(name)
		if firstErr != nil {
			return nil, firstErr
		}
	}
	return resolved, nil
}

// expandTemplates substitutes ${VAR} and {{.VAR}} references in s using the
// supplied lookup. Unknown ${VAR} references are left for the shell to
// expand; unknown {{.VAR}} templates are left verbatim. Both forms share a
// single lookup so callers can layer vars / CLI_ARGS / built-ins / env in
// whatever priority makes sense for their context.
func expandTemplates(s string, lookup func(string) (string, bool)) string {
	// Expand ${VAR} first. This won't touch {{.VAR}} templates since they
	// don't start with $. Unknown variables are left as ${KEY} for the shell.
	s = os.Expand(s, func(key string) string {
		if val, ok := lookup(key); ok {
			return val
		}
		return "${" + key + "}"
	})

	// Then replace {{.VAR}} templates. Unknown templates are left as-is so
	// run-time behavior matches expandConfigEnvTemplates at parse time.
	return templatePattern.ReplaceAllStringFunc(s, func(match string) string {
		key := templatePattern.FindStringSubmatch(match)[1]
		if val, ok := lookup(key); ok {
			return val
		}
		return match
	})
}

// expandVars substitutes template and shell variables in a command string.
// {{.VAR}} and ${VAR} are both resolved from task variables, CLI_ARGS, the
// optional builtin lookup (currently {{.GIT_*}}), and finally the process
// environment. Unknown ${VAR} references are left for the shell to expand.
// Unknown {{.VAR}} templates are left verbatim (matching
// expandConfigEnvTemplates at parse time).
//
// CLI_ARGS resolves through the same lookup as any other variable: a value
// in `vars` (e.g. a call-site `vars: { CLI_ARGS: -f }`) wins, and the cliArgs
// argument is used only as a fallback default. Env never satisfies CLI_ARGS,
// because the cliArgs fallback is always considered "found" (even when empty).
//
// builtin may be nil. When non-nil it is consulted *after* user vars and
// CLI_ARGS but *before* the process environment, so a user-defined var with
// the same name as a built-in (e.g. GIT_COMMIT) always wins.
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
		return os.LookupEnv(key)
	})
}

// expandCLIArgsOnly substitutes only CLI_ARGS in a command string, leaving
// every other ${VAR} and {{.VAR}} reference untouched. It is used to render
// the per-cmd log line: CLI_ARGS comes from the user's invocation and is safe
// to surface, while other variables may carry secrets sourced from env or
// dotenv files and are deliberately not expanded in logs.
//
// CLI_ARGS resolution mirrors expandVars: a value in `vars` (typically a
// call-site `vars: { CLI_ARGS: -f }`) wins over the cliArgs fallback.
func expandCLIArgsOnly(s string, vars map[string]string, cliArgs string) string {
	val := cliArgs
	if v, ok := vars["CLI_ARGS"]; ok {
		val = v
	}

	s = os.Expand(s, func(key string) string {
		if key == "CLI_ARGS" {
			return val
		}
		return "${" + key + "}"
	})

	return templatePattern.ReplaceAllStringFunc(s, func(match string) string {
		if templatePattern.FindStringSubmatch(match)[1] == "CLI_ARGS" {
			return val
		}
		return match
	})
}
