//go:build !darwin

package taskfile

import (
	"fmt"

	"github.com/99designs/keyring"
)

func authenticateBiometric() error {
	return nil
}

func getSecret(service, key string) (string, error) {
	ring, err := openKeyring(service)
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
	ring, err := openKeyring(service)
	if err != nil {
		return fmt.Errorf("opening keychain %q: %w", service, err)
	}

	return ring.Set(keyring.Item{
		Key:   key,
		Label: service + ": " + key,
		Data:  []byte(value),
	})
}

func openKeyring(service string) (keyring.Keyring, error) {
	return keyring.Open(keyring.Config{
		ServiceName: service,
	})
}
