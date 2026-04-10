//go:build !darwin

package taskfile

import (
	"fmt"

	"github.com/99designs/keyring"
)

// authenticateBiometric is a no-op on non-macOS platforms.
func authenticateBiometric() error {
	return nil
}

// getSecret retrieves a secret from the OS credential store.
func getSecret(service, key string) (string, error) {
	ring, err := keyring.Open(keyring.Config{ServiceName: service})
	if err != nil {
		return "", fmt.Errorf("opening keychain %q: %w", service, err)
	}

	item, err := ring.Get(key)
	if err != nil {
		return "", fmt.Errorf("reading secret %q from keychain %q: %w", key, service, err)
	}

	return string(item.Data), nil
}

// SetSecret stores a secret in the OS credential store.
func SetSecret(service, key, value string) error {
	ring, err := keyring.Open(keyring.Config{ServiceName: service})
	if err != nil {
		return fmt.Errorf("opening keychain %q: %w", service, err)
	}

	if err := ring.Set(keyring.Item{
		Key:   key,
		Label: service + ": " + key,
		Data:  []byte(value),
	}); err != nil {
		return fmt.Errorf("storing secret %q in keychain %q: %w", key, service, err)
	}

	return nil
}
