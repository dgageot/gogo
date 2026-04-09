package taskfile

import (
	"fmt"
)

// KeychainEntry maps a keychain secret to an environment variable.
type KeychainEntry struct {
	Key string `yaml:"key"`
	Env string `yaml:"env"`
}

// loadKeychainSecrets retrieves all requested secrets from the OS credential store.
// On macOS, it prompts for Touch ID before accessing secrets.
func loadKeychainSecrets(service string, entries []KeychainEntry) (map[string]string, error) {
	if len(entries) == 0 {
		return nil, nil
	}

	if err := authenticateBiometric(); err != nil {
		return nil, err
	}

	env := make(map[string]string)
	for _, entry := range entries {
		logTask(colorCyan, "keychain", fmt.Sprintf("reading %q from %q", entry.Key, service))
		value, err := getSecret(service, entry.Key)
		if err != nil {
			return nil, err
		}
		env[entry.Env] = value
	}

	return env, nil
}
