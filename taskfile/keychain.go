package taskfile

import (
	"fmt"
	"strings"
)

// loadSecrets resolves all secret entries by dispatching on the URI scheme.
func loadSecrets(entries []SecretEntry) (map[string]string, error) {
	env := make(map[string]string)

	// Group keychain entries to authenticate once.
	var keychainEntries []SecretEntry
	var opEntries []SecretEntry

	for _, entry := range entries {
		switch {
		case strings.HasPrefix(entry.Ref, "keychain://"):
			keychainEntries = append(keychainEntries, entry)
		case strings.HasPrefix(entry.Ref, "1password://"):
			opEntries = append(opEntries, entry)
		default:
			return nil, fmt.Errorf("unknown secret scheme in %q", entry.Ref)
		}
	}

	if len(keychainEntries) > 0 {
		if err := loadKeychainSecrets(keychainEntries, env); err != nil {
			return nil, err
		}
	}

	if len(opEntries) > 0 {
		if err := loadOnePasswordSecrets(opEntries, env); err != nil {
			return nil, err
		}
	}

	return env, nil
}

// loadKeychainSecrets retrieves secrets from the OS credential store.
// Each ref has the form keychain://service/key.
func loadKeychainSecrets(entries []SecretEntry, env map[string]string) error {
	if err := authenticateBiometric(); err != nil {
		return err
	}

	for _, entry := range entries {
		service, key, err := parseKeychainRef(entry.Ref)
		if err != nil {
			return err
		}

		logTask(colorCyan, "keychain", "reading "+key+" from "+service)

		value, err := getSecret(service, key)
		if err != nil {
			return err
		}

		env[entry.Env] = value
	}

	return nil
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
