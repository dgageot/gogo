package taskfile

import (
	"fmt"
	"maps"
	"slices"
	"strings"
)

// Supported URI schemes for the secrets backend. Adding a new backend means
// adding a case in resolveSecretURI and a constant here for the validator.
const secretSchemeOp = "op://"

// supportedSecretSchemes is the list shown in error messages so users know
// which prefixes will work. Keep it in sync with resolveSecretURI.
var supportedSecretSchemes = []string{secretSchemeOp}

// resolveTaskSecrets walks task.Secrets, resolves each reference against
// the config-level secrets map, and returns a flat map of env entries to
// inject into the task's environment.
//
// Today the only supported backend is op://, which simply passes the URI
// through as the env value: the existing op-run path then resolves it when
// the command actually runs (so Touch ID / biometric prompts continue to
// work the way users already expect).
//
// Errors carry the offending name so users can debug a typo quickly.
func (r *Runner) resolveTaskSecrets(task *Task) (map[string]string, error) {
	if len(task.Secrets) == 0 {
		return nil, nil
	}
	out := make(map[string]string, len(task.Secrets))
	for _, name := range task.Secrets {
		uri, ok := r.tf.Secrets[name]
		if !ok {
			return nil, fmt.Errorf("task references unknown secret %q (declare it under top-level `secrets:`)", name)
		}
		entries, err := resolveSecretURI(name, uri)
		if err != nil {
			return nil, fmt.Errorf("resolving secret %q: %w", name, err)
		}
		maps.Copy(out, entries)
	}
	return out, nil
}

// resolveSecretURI dispatches to the appropriate backend based on the URI
// scheme and returns the env entries to inject. Today only op:// is wired
// up; new schemes plug in by adding another case here.
func resolveSecretURI(name, uri string) (map[string]string, error) {
	if strings.HasPrefix(uri, secretSchemeOp) {
		// Pass-through: the URI itself becomes the env value. The existing
		// hasOpSecrets / op run wrapper resolves it when the command runs,
		// so authentication (Touch ID, biometric, etc.) flows through op
		// exactly as it does for inline op:// references in task.env.
		return map[string]string{name: uri}, nil
	}
	return nil, fmt.Errorf("unknown backend in %q (supported: %s)", uri, strings.Join(supportedSecretSchemes, ", "))
}

// validateSecrets checks that every name referenced from any task.Secrets
// list is defined under the top-level `secrets:` map, that every URI declared
// there uses a supported scheme, and that op:// references only appear in env
// or secrets (never in vars).
func validateSecrets(c *Config) error {
	if err := validateVarsDoNotUseSecrets(c.Vars, "root vars"); err != nil {
		return err
	}
	for taskName, task := range c.Tasks {
		if err := validateVarsDoNotUseSecrets(task.Vars, fmt.Sprintf("task %q vars", taskName)); err != nil {
			return err
		}
		for _, cmd := range task.Cmds {
			if err := validateVarsDoNotUseSecrets(cmd.Vars, fmt.Sprintf("task %q call-site vars", taskName)); err != nil {
				return err
			}
		}
		for _, name := range task.Secrets {
			if _, ok := c.Secrets[name]; !ok {
				return fmt.Errorf("task %q references unknown secret %q; declare it under top-level `secrets:`", taskName, name)
			}
		}
	}
	for name, uri := range c.Secrets {
		if !secretSchemeKnown(uri) {
			return fmt.Errorf("secret %q has unknown backend in %q (supported: %s)", name, uri, strings.Join(supportedSecretSchemes, ", "))
		}
	}
	return nil
}

func validateVarsDoNotUseSecrets(vars map[string]Var, where string) error {
	for _, name := range slices.Sorted(maps.Keys(vars)) {
		v := vars[name]
		if strings.HasPrefix(v.Value, secretSchemeOp) || strings.HasPrefix(v.Sh, secretSchemeOp) {
			return fmt.Errorf("%s: variable %q uses %q syntax; secrets are only supported in env or top-level `secrets:`", where, name, secretSchemeOp)
		}
	}
	return nil
}

func secretSchemeKnown(uri string) bool {
	for _, scheme := range supportedSecretSchemes {
		if strings.HasPrefix(uri, scheme) {
			return true
		}
	}
	return false
}
