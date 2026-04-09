package taskfile

import (
	"fmt"
	"os/exec"
	"strings"
)

// authenticateBiometric is a no-op on macOS because the Keychain
// prompts for Touch ID when accessing protected secrets.
func authenticateBiometric() error {
	return nil
}

func getSecret(service, key string) (string, error) {
	out, err := exec.Command("security", "find-generic-password", "-s", service, "-a", key, "-w").Output()
	if err != nil {
		return "", fmt.Errorf("reading secret %q from keychain %q: %w", key, service, err)
	}
	return strings.TrimRight(string(out), "\n"), nil
}

// SetSecret stores a secret in the macOS Keychain.
// The -T "" flag ensures no application is trusted by default,
// so every access requires user authorization (Touch ID on supported Macs).
func SetSecret(service, key, value string) error {
	// Delete first to reset the ACL (update preserves the old ACL)
	_ = exec.Command("security", "delete-generic-password", "-s", service, "-a", key).Run()

	err := exec.Command("security", "add-generic-password", "-s", service, "-a", key, "-w", value, "-T", "").Run()
	if err != nil {
		return fmt.Errorf("storing secret %q in keychain %q: %w", key, service, err)
	}
	return nil
}
