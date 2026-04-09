package taskfile

import (
	"fmt"
	"os/exec"
	"strings"

	"github.com/ansxuman/go-touchid"
)

func authenticateBiometric() error {
	ok, err := touchid.Auth(touchid.DeviceTypeAny, "gogo needs to access secrets")
	if err != nil {
		return fmt.Errorf("biometric authentication: %w", err)
	}
	if !ok {
		return fmt.Errorf("biometric authentication denied")
	}
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
func SetSecret(service, key, value string) error {
	// Try to update first, then add if not found
	err := exec.Command("security", "add-generic-password", "-s", service, "-a", key, "-w", value, "-U").Run()
	if err != nil {
		return fmt.Errorf("storing secret %q in keychain %q: %w", key, service, err)
	}
	return nil
}
