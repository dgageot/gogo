package taskfile

import (
	"context"
	"fmt"
	"maps"
	"slices"
	"strings"
)

const (
	keychainScheme    = "keychain://"
	onePasswordScheme = "1password://"
)

// loadSecrets resolves all secret entries by dispatching on the URI scheme.
func loadSecrets(entries map[string]string) (map[string]string, error) {
	secrets := make(map[string]string)

	keychainAuthenticated := false
	opEntries := make(map[string]string)

	for _, name := range slices.Sorted(maps.Keys(entries)) {
		ref := entries[name]
		switch {
		case strings.HasPrefix(ref, keychainScheme):
			if !keychainAuthenticated {
				if err := authenticateBiometric(); err != nil {
					return nil, err
				}
				keychainAuthenticated = true
			}

			service, key, err := parseKeychainRef(ref)
			if err != nil {
				return nil, err
			}

			logTask(colorCyan, "keychain", "reading "+key+" from "+service)

			value, err := getSecret(service, key)
			if err != nil {
				return nil, err
			}

			secrets[name] = value

		case strings.HasPrefix(ref, onePasswordScheme):
			opEntries[name] = ref

		default:
			return nil, fmt.Errorf("unknown secret scheme in %q", ref)
		}
	}

	if len(opEntries) > 0 {
		return secrets, loadOnePasswordSecrets(context.Background(), opEntries, secrets)
	}

	return secrets, nil
}

// parseKeychainRef extracts service and key from "keychain://service/key".
func parseKeychainRef(ref string) (service, key string, err error) {
	const expected = "expected keychain://service/key"

	path, ok := strings.CutPrefix(ref, keychainScheme)
	if !ok {
		return "", "", fmt.Errorf("invalid keychain reference %q, %s", ref, expected)
	}

	service, key, ok = strings.Cut(path, "/")
	if !ok || service == "" || key == "" {
		return "", "", fmt.Errorf("invalid keychain reference %q, %s", ref, expected)
	}

	return service, key, nil
}
