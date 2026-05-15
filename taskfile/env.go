package taskfile

import (
	"fmt"
	"maps"
	"os"
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
func baseEnvWithDotenv(dotenv map[string]string) []string {
	env := os.Environ()
	for _, k := range slices.Sorted(maps.Keys(dotenv)) {
		if _, exists := os.LookupEnv(k); !exists {
			env = append(env, envPair(k, dotenv[k]))
		}
	}
	return env
}

// buildEnv composes the environment used to run a task's commands:
//
//  1. start from r.BaseEnv (os env + global dotenv),
//  2. overlay parentEnv (only set when invoked as a sub-task via `cmds:
//     - task: X`; matches shell-function semantics so a parent's `env:`
//     block flows down to its children),
//  3. add task-level dotenv (only for keys not already present),
//  4. overlay task vars,
//  5. overlay task env, resolving any ${VAR} cross-references,
//  6. overlay resolved task secrets (highest precedence; an explicit
//     `secrets: [X]` reference is a stronger signal than a same-named
//     `env: { X: ... }` entry, and is the only way op:// values reach the
//     env when secrets are declared centrally).
func (r *Runner) buildEnv(task *Task, dir string, vars map[string]string, parentEnv []string) ([]string, error) {
	env := slices.Clone(r.BaseEnv)

	// Inherited parent env wins over BaseEnv (a parent's `env: { GOOS: linux }`
	// must override the OS GOOS for child tasks). Child task.dotenv/vars/env
	// then layer on top — the child still gets the final say.
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

	for _, k := range slices.Sorted(maps.Keys(vars)) {
		env = setEnv(env, k, vars[k])
	}

	for _, k := range slices.Sorted(maps.Keys(task.Env)) {
		env = setEnv(env, k, resolveEnvValue(k, task.Env, vars, r.builtinLookup))
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

// resolveEnvValue expands references in task.Env[key], transparently following
// cross-references to other task.Env keys. Cycles (self- or mutual) resolve to
// the empty string.
//
// Both ${VAR} and {{.VAR}} forms are supported so env values follow the same
// variable syntax as commands and var bodies. Literal op:// secret references
// are left untouched for the op-run wrapper to resolve at command execution
// time.
//
// builtin may be nil. When non-nil it is consulted *after* task.Env and task
// vars, but *before* the process environment, so a task-level override of a
// built-in name (e.g. GIT_COMMIT) wins.
func resolveEnvValue(key string, taskEnv, vars map[string]string, builtin func(string) (string, bool)) string {
	resolved := make(map[string]string)
	visiting := make(map[string]struct{})

	var lookup func(string) (string, bool)
	lookup = func(k string) (string, bool) {
		if v, ok := resolved[k]; ok {
			return v, true
		}
		if _, onPath := visiting[k]; onPath {
			return "", true
		}
		raw, ok := taskEnv[k]
		if !ok {
			if v, ok := vars[k]; ok {
				return v, true
			}
			if builtin != nil {
				if v, ok := builtin(k); ok {
					return v, true
				}
			}
			return os.LookupEnv(k)
		}
		visiting[k] = struct{}{}
		var v string
		if strings.HasPrefix(raw, "op://") {
			v = raw
		} else {
			v = expandTemplates(raw, lookup)
		}
		delete(visiting, k)
		resolved[k] = v
		return v, true
	}

	v, _ := lookup(key)
	return v
}

// hasOpSecrets reports whether any env entry's value is an op:// reference.
// This runs over the fully-built env (base + dotenv + vars + task env), so
// any source of an op:// reference triggers op-run wrapping.
func hasOpSecrets(env []string) bool {
	for _, e := range env {
		if _, v, ok := strings.Cut(e, "="); ok && strings.HasPrefix(v, "op://") {
			return true
		}
	}
	return false
}
