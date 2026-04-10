package taskfile

import (
	"fmt"
	"strings"
)

const (
	keychainScheme    = "keychain://"
	onePasswordScheme = "1password://"
)

// loadSecrets resolves all secret entries by dispatching on the URI scheme.
func loadSecrets(entries map[string]string) (map[string]string, error) {
	env := make(map[string]string)

	keychainAuthenticated := false
	opEntries := make(map[string]string)

	for name, ref := range entries {
		switch {
		case strings.HasPrefix(ref, keychainScheme):
			if !keychainAuthenticated {
				if err := authenticateBiometric(); err != nil {
					return nil, err
				}
				keychainAuthenticated = true
			}

			value, err := resolveKeychainRef(ref)
			if err != nil {
				return nil, err
			}

			env[name] = value

		case strings.HasPrefix(ref, onePasswordScheme):
			opEntries[name] = ref

		default:
			return nil, fmt.Errorf("unknown secret scheme in %q", ref)
		}
	}

	if len(opEntries) > 0 {
		if err := loadOnePasswordSecrets(opEntries, env); err != nil {
			return nil, err
		}
	}

	return env, nil
}

// resolveKeychainRef reads a single secret from the OS keychain.
func resolveKeychainRef(ref string) (string, error) {
	service, key, err := parseKeychainRef(ref)
	if err != nil {
		return "", err
	}

	logTask(colorCyan, "keychain", "reading "+key+" from "+service)

	return getSecret(service, key)
}

// parseKeychainRef extracts service and key from "keychain://service/key".
func parseKeychainRef(ref string) (service, key string, err error) {
	path, ok := strings.CutPrefix(ref, keychainScheme)
	if !ok {
		return "", "", fmt.Errorf("invalid keychain reference %q, expected keychain://service/key", ref)
	}

	service, key, ok = strings.Cut(path, "/")
	if !ok || service == "" || key == "" {
		return "", "", fmt.Errorf("invalid keychain reference %q, expected keychain://service/key", ref)
	}

	return service, key, nil
}
