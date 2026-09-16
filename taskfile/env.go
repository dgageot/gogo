package taskfile

import (
	"context"
	"fmt"
	"maps"
	"os"
	"regexp"
	"slices"
	"strings"
)

// envPair formats a key-value pair as an environment variable entry.
func envPair(k, v string) string {
	return k + "=" + v
}

// envHasKey reports whether the env slice contains an entry for the given key.
func envHasKey(env []string, key string) bool {
	prefix := key + "="
	return slices.ContainsFunc(env, func(e string) bool {
		return strings.HasPrefix(e, prefix)
	})
}

// envLookup returns the last value for key in env, matching shell duplicate
// handling and the test helper envValue.
func envLookup(env []string, key string) (string, bool) {
	for _, e := range slices.Backward(env) {
		if k, v, ok := strings.Cut(e, "="); ok && k == key {
			return v, true
		}
	}
	return "", false
}

// setEnv sets or replaces an environment variable in the env slice.
func setEnv(env []string, key, value string) []string {
	pair := envPair(key, value)
	prefix := key + "="
	for i, e := range env {
		if strings.HasPrefix(e, prefix) {
			env[i] = pair
			return env
		}
	}
	return append(env, pair)
}

// baseEnvWithDotenv returns os.Environ() augmented with dotenv vars that
// are not already defined in the process environment (OS env wins).
//
// op://-prefixed values inherited from the OS environment are stripped: a
// user who can set the parent process's env must not be able to force
// every task through `op run` (which would trigger 1Password credential
// prompts on the local user). Legitimate op:// references must come from
// the task file's own `env:`, `dotenv:`, or top-level `secrets:` blocks.
func baseEnvWithDotenv(dotenv map[string]string) []string {
	var env []string
	for _, e := range os.Environ() {
		if _, v, ok := strings.Cut(e, "="); ok && strings.HasPrefix(v, "op://") {
			continue
		}
		env = append(env, e)
	}
	for _, k := range slices.Sorted(maps.Keys(dotenv)) {
		if _, exists := os.LookupEnv(k); !exists {
			env = append(env, envPair(k, dotenv[k]))
		}
	}
	return env
}

var shellDefaultPattern = regexp.MustCompile(`\$\{([A-Za-z_][A-Za-z0-9_]*):-([^{}]*)\}`)

// buildEnv composes the environment used to run a task's commands:
//
//  1. start from r.BaseEnv (os env + global dotenv),
//  2. overlay parentEnv (only set when invoked as a sub-task via `cmds:
//     - task: X`; matches shell-function semantics so a parent's `env:`
//     block flows down to its children),
//  3. add task-level dotenv (only for keys not already present),
//  4. overlay task env, resolving any $VAR cross-references from env only,
//  5. overlay call-site env for `task:` sub-calls,
//  6. overlay resolved task secrets (highest precedence; an explicit
//     `secrets: [X]` reference is a stronger signal than a same-named
//     `env: { X: ... }` entry, and is the only way op:// values reach the
//     env when secrets are declared centrally).
func (r *Runner) buildEnv(ctx context.Context, taskName string, task *Task, dir string, parentEnv []string, vars, callEnv map[string]string) ([]string, error) {
	env := slices.Clone(r.BaseEnv)

	// File-level env entries are defaults: root values flow everywhere,
	// namespace values become more specific, and the process environment wins.
	scoped := r.scopedEnv(taskName)
	resolvedScoped := resolveTaskEnv(scoped, env, vars, r.builtins(ctx))
	for _, key := range slices.Sorted(maps.Keys(resolvedScoped)) {
		if !envHasKey(env, key) {
			env = append(env, envPair(key, resolvedScoped[key]))
		}
	}

	// Inherited parent env wins over BaseEnv (a parent's `env: { GOOS: linux }`
	// must override the OS GOOS for child tasks). Child task.dotenv/env then
	// layer on top — the child still gets the final say.
	for _, e := range parentEnv {
		if k, v, ok := strings.Cut(e, "="); ok {
			env = setEnv(env, k, v)
		}
	}

	if len(task.Dotenv) > 0 {
		taskDotenv, err := loadDotenvFiles(dir, task.Dotenv, make(map[string]struct{}))
		if err != nil {
			return nil, fmt.Errorf("loading task dotenv: %w", err)
		}
		for _, k := range slices.Sorted(maps.Keys(taskDotenv)) {
			if !envHasKey(env, k) {
				env = append(env, envPair(k, taskDotenv[k]))
			}
		}
	}

	resolvedTaskEnv := resolveTaskEnv(task.Env, env, vars, r.builtins(ctx))
	for _, k := range slices.Sorted(maps.Keys(task.Env)) {
		env = setEnv(env, k, resolvedTaskEnv[k])
	}

	resolvedCallEnv := resolveTaskEnv(callEnv, env, nil, r.builtins(ctx))
	for _, k := range slices.Sorted(maps.Keys(callEnv)) {
		env = setEnv(env, k, resolvedCallEnv[k])
	}

	secrets, err := r.resolveTaskSecrets(task)
	if err != nil {
		return nil, err
	}
	for _, k := range slices.Sorted(maps.Keys(secrets)) {
		env = setEnv(env, k, secrets[k])
	}

	return env, nil
}

func resolveTaskEnv(taskEnv map[string]string, baseEnv []string, vars map[string]string, builtin func(string) (string, bool)) map[string]string {
	resolved := make(map[string]string, len(taskEnv))
	visiting := make(map[string]struct{})

	var lookup func(string) (string, bool)
	lookup = func(k string) (string, bool) {
		if v, ok := resolved[k]; ok {
			return v, true
		}
		if _, onPath := visiting[k]; onPath {
			if value, ok := envLookup(baseEnv, k); ok {
				return value, true
			}
			if value, ok := os.LookupEnv(k); ok {
				return value, true
			}
			return "", true
		}
		raw, ok := taskEnv[k]
		if !ok {
			if value, ok := envLookup(baseEnv, k); ok {
				return value, true
			}
			return os.LookupEnv(k)
		}
		visiting[k] = struct{}{}
		var v string
		if strings.HasPrefix(raw, "op://") {
			v = raw
		} else {
			v = raw
			if vars != nil {
				v = expandTemplates(v, func(name string) (string, bool) {
					if value, ok := vars[name]; ok {
						return value, true
					}
					if builtin != nil {
						return builtin(name)
					}
					return "", false
				})
			}
			v = expandShellDefaults(v, lookup)
			v = expandShellEnv(v, lookup)
		}
		delete(visiting, k)
		resolved[k] = v
		return v, true
	}

	for _, key := range slices.Sorted(maps.Keys(taskEnv)) {
		lookup(key)
	}
	return resolved
}

func expandShellDefaults(value string, lookup func(string) (string, bool)) string {
	return shellDefaultPattern.ReplaceAllStringFunc(value, func(match string) string {
		parts := shellDefaultPattern.FindStringSubmatch(match)
		if current, ok := lookup(parts[1]); ok && current != "" {
			return current
		}
		return parts[2]
	})
}

// hasOpSecrets reports whether any env entry's value is an op:// reference.
// This runs over the fully-built env (base + dotenv + task env), so any env
// source of an op:// reference triggers op-run wrapping.
func hasOpSecrets(env []string) bool {
	for _, e := range env {
		if _, v, ok := strings.Cut(e, "="); ok && strings.HasPrefix(v, "op://") {
			return true
		}
	}
	return false
}

// scopedEnv collects file-level defaults from the root to the task namespace.
func (r *Runner) scopedEnv(taskName string) map[string]string {
	scoped := maps.Clone(r.tf.Env)
	for _, namespace := range ancestorNamespaces(taskNamespace(taskName)) {
		if scoped == nil {
			scoped = make(map[string]string)
		}
		maps.Copy(scoped, r.tf.NamespaceEnv[namespace])
	}
	// Defaults shadowed by the process/global dotenv are not evaluated:
	// cross-references must see the effective value, not the discarded default.
	for key := range scoped {
		if envHasKey(r.BaseEnv, key) {
			delete(scoped, key)
		}
	}
	return scoped
}
