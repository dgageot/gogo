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
func loadSecrets(entries []SecretEntry) (map[string]string, error) {
	env := make(map[string]string)

	keychainAuthenticated := false
	var opEntries []SecretEntry

	for _, entry := range entries {
		switch {
		case strings.HasPrefix(entry.Ref, keychainScheme):
			if !keychainAuthenticated {
				if err := authenticateBiometric(); err != nil {
					return nil, err
				}
				keychainAuthenticated = true
			}

			value, err := resolveKeychainEntry(entry)
			if err != nil {
				return nil, err
			}

			env[entry.Env] = value

		case strings.HasPrefix(entry.Ref, onePasswordScheme):
			opEntries = append(opEntries, entry)

		default:
			return nil, fmt.Errorf("unknown secret scheme in %q", entry.Ref)
		}
	}

	if len(opEntries) > 0 {
		if err := loadOnePasswordSecrets(opEntries, env); err != nil {
			return nil, err
		}
	}

	return env, nil
}

// resolveKeychainEntry reads a single secret from the OS keychain.
func resolveKeychainEntry(entry SecretEntry) (string, error) {
	service, key, err := parseKeychainRef(entry.Ref)
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
