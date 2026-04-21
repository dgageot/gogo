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
//  2. add task-level dotenv (only for keys not already present),
//  3. overlay task vars,
//  4. overlay task env, resolving any ${VAR} cross-references.
func (r *Runner) buildEnv(task *Task, dir string, vars map[string]string) ([]string, error) {
	env := slices.Clone(r.BaseEnv)

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
		env = setEnv(env, k, resolveEnvValue(k, task.Env, vars))
	}

	return env, nil
}

// resolveEnvValue expands ${VAR} references in task.Env[key], transparently
// following cross-references to other task.Env keys. Cycles (self- or mutual)
// resolve to the empty string.
func resolveEnvValue(key string, taskEnv, vars map[string]string) string {
	resolved := make(map[string]string)
	visiting := make(map[string]struct{})

	var lookup func(string) string
	lookup = func(k string) string {
		if v, ok := resolved[k]; ok {
			return v
		}
		if _, onPath := visiting[k]; onPath {
			return ""
		}
		raw, ok := taskEnv[k]
		if !ok {
			if v, ok := vars[k]; ok {
				return v
			}
			return os.Getenv(k)
		}
		visiting[k] = struct{}{}
		v := os.Expand(raw, lookup)
		delete(visiting, k)
		resolved[k] = v
		return v
	}

	return lookup(key)
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
