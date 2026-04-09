package taskfile

import (
	"fmt"
	"runtime"

	"github.com/99designs/keyring"
)

// KeychainEntry maps a keychain secret to an environment variable.
type KeychainEntry struct {
	Key string `yaml:"key"`
	Env string `yaml:"env"`
}

// loadKeychainSecrets opens the keychain and retrieves all requested secrets.
// On macOS, it prompts for Touch ID before accessing secrets.
func loadKeychainSecrets(service string, entries []KeychainEntry) (map[string]string, error) {
	if len(entries) == 0 {
		return nil, nil
	}

	if err := authenticateBiometric(); err != nil {
		return nil, fmt.Errorf("biometric authentication: %w", err)
	}

	ring, err := openKeyring(service)
	if err != nil {
		return nil, fmt.Errorf("opening keychain %q: %w", service, err)
	}

	env := make(map[string]string)
	for _, entry := range entries {
		item, err := ring.Get(entry.Key)
		if err != nil {
			return nil, fmt.Errorf("reading secret %q from keychain %q: %w", entry.Key, service, err)
		}
		env[entry.Env] = string(item.Data)
	}

	return env, nil
}

func openKeyring(service string) (keyring.Keyring, error) {
	cfg := keyring.Config{
		ServiceName: service,
	}

	if runtime.GOOS == "darwin" {
		cfg.AllowedBackends = []keyring.BackendType{keyring.KeychainBackend}
	}

	return keyring.Open(cfg)
}

// SetSecret stores a secret in the keychain.
func SetSecret(service, key, value string) error {
	ring, err := openKeyring(service)
	if err != nil {
		return fmt.Errorf("opening keychain %q: %w", service, err)
	}

	return ring.Set(keyring.Item{
		Key:  key,
		Data: []byte(value),
	})
}
