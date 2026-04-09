package taskfile

import (
	"fmt"
	"strings"
)

// loadSecrets resolves all secret entries by dispatching on the URI scheme.
func loadSecrets(entries []SecretEntry) (map[string]string, error) {
	env := make(map[string]string)

	keychainAuthenticated := false
	var opEntries []SecretEntry

	for _, entry := range entries {
		switch {
		case strings.HasPrefix(entry.Ref, "keychain://"):
			if !keychainAuthenticated {
				if err := authenticateBiometric(); err != nil {
					return nil, err
				}
				keychainAuthenticated = true
			}

			service, key, err := parseKeychainRef(entry.Ref)
			if err != nil {
				return nil, err
			}

			logTask(colorCyan, "keychain", "reading "+key+" from "+service)

			value, err := getSecret(service, key)
			if err != nil {
				return nil, err
			}

			env[entry.Env] = value

		case strings.HasPrefix(entry.Ref, "1password://"):
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

// parseKeychainRef extracts service and key from "keychain://service/key".
func parseKeychainRef(ref string) (service, key string, err error) {
	path, ok := strings.CutPrefix(ref, "keychain://")
	if !ok {
		return "", "", fmt.Errorf("invalid keychain reference %q, expected keychain://service/key", ref)
	}

	service, key, ok = strings.Cut(path, "/")
	if !ok || service == "" || key == "" {
		return "", "", fmt.Errorf("invalid keychain reference %q, expected keychain://service/key", ref)
	}

	return service, key, nil
}
