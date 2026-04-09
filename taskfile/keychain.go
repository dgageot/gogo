package taskfile

import (
	"fmt"
	"strings"
)

// loadSecrets resolves all secret entries by dispatching on the URI scheme.
func loadSecrets(entries []SecretEntry) (map[string]string, error) {
	env := make(map[string]string)

	keychainAuthenticated := false

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
			// 1Password entries are handled in batch for client caching
			continue

		default:
			return nil, fmt.Errorf("unknown secret scheme in %q", entry.Ref)
		}
	}

	// Collect 1Password entries and process them in batch
	var opEntries []SecretEntry
	for _, entry := range entries {
		if strings.HasPrefix(entry.Ref, "1password://") {
			opEntries = append(opEntries, entry)
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
	path := strings.TrimPrefix(ref, "keychain://")

	service, key, ok := strings.Cut(path, "/")
	if !ok || service == "" || key == "" {
		return "", "", fmt.Errorf("invalid keychain reference %q, expected keychain://service/key", ref)
	}

	return service, key, nil
}
