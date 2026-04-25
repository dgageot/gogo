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

// expandTemplates replaces {{.VAR}} patterns with environment variable values.
func expandTemplates(data []byte) []byte {
	return templatePattern.ReplaceAllFunc(data, func(match []byte) []byte {
		name := string(templatePattern.FindSubmatch(match)[1])
		if val, ok := os.LookupEnv(name); ok {
			return []byte(val)
		}
		return match
	})
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
		"TASKFILE_DIR": taskDir,
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
// {{.VAR}} templates are left verbatim (matching expandTemplates at parse time).
func expandVars(s string, vars map[string]string, cliArgs string) string {
	lookup := func(key string) (string, bool) {
		if key == "CLI_ARGS" {
			return cliArgs, true
		}
		if val, ok := vars[key]; ok {
			return val, true
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
	// run-time behavior matches expandTemplates at parse time.
	return templatePattern.ReplaceAllStringFunc(s, func(match string) string {
		key := templatePattern.FindStringSubmatch(match)[1]
		if val, ok := lookup(key); ok {
			return val
		}
		return match
	})
}
