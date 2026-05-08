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

// resolveAllVars computes the effective variables for a task, including extra vars from call sites.
func (r *Runner) resolveAllVars(task *Task, dir string, extraVars []map[string]Var) (map[string]string, error) {
	vars, err := r.resolveVars(task, dir)
	if err != nil {
		return nil, err
	}

	for _, ev := range extraVars {
		if err := r.addVars(vars, ev, dir); err != nil {
			return nil, err
		}
	}

	return vars, nil
}

// resolveVars computes the effective variables for a task.
func (r *Runner) resolveVars(task *Task, taskDir string) (map[string]string, error) {
	resolved := map[string]string{
		"TASK_FILE_DIR": taskDir,
	}
	if err := r.addVars(resolved, r.tf.Vars, r.tf.Dir); err != nil {
		return nil, err
	}
	if err := r.addVars(resolved, task.Vars, taskDir); err != nil {
		return nil, err
	}
	return resolved, nil
}

// addVars resolves each Var in src (sorted for determinism) and writes it into dst.
func (r *Runner) addVars(dst map[string]string, src map[string]Var, dir string) error {
	for _, k := range slices.Sorted(maps.Keys(src)) {
		v, err := r.resolveVar(src[k], dir)
		if err != nil {
			return err
		}
		dst[k] = v
	}
	return nil
}

// resolveVar evaluates a single variable, running a shell command if needed.
func (r *Runner) resolveVar(v Var, dir string) (string, error) {
	if v.Sh == "" {
		return v.Value, nil
	}

	out, err := r.ShellRunner.Output(ShellCommand{
		Kind:    ShellCommandVar,
		Command: v.Sh,
		Dir:     dir,
	})
	if err != nil {
		return "", fmt.Errorf("resolving variable (sh: %s): %w", v.Sh, err)
	}
	return strings.TrimSpace(string(out)), nil
}

// expandVars substitutes template and shell variables in a command string.
// {{.VAR}} and ${VAR} are both resolved from task variables, CLI_ARGS, and environment.
// Unknown ${VAR} references are left for the shell to expand. Unknown
// {{.VAR}} templates are left verbatim (matching expandConfigEnvTemplates at parse time).
//
// CLI_ARGS resolves through the same lookup as any other variable: a value
// in `vars` (e.g. a call-site `vars: { CLI_ARGS: -f }`) wins, and the cliArgs
// argument is used only as a fallback default. Env never satisfies CLI_ARGS,
// because the cliArgs fallback is always considered "found" (even when empty).
func expandVars(s string, vars map[string]string, cliArgs string) string {
	lookup := func(key string) (string, bool) {
		if val, ok := vars[key]; ok {
			return val, true
		}
		if key == "CLI_ARGS" {
			return cliArgs, true
		}
		return os.LookupEnv(key)
	}

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
