package taskfile

import (
	"fmt"
	"os/exec"
	"strings"
)

const touchIDSwift = `
import LocalAuthentication
import Foundation
let context = LAContext()
var error: NSError?
guard context.canEvaluatePolicy(.deviceOwnerAuthenticationWithBiometrics, error: &error) else {
    fputs(error?.localizedDescription ?? "Biometrics unavailable", stderr)
    exit(1)
}
let semaphore = DispatchSemaphore(value: 0)
var authOK = false
context.evaluatePolicy(.deviceOwnerAuthenticationWithBiometrics, localizedReason: CommandLine.arguments.count > 1 ? CommandLine.arguments[1] : "authenticate") { success, _ in
    authOK = success
    semaphore.signal()
}
semaphore.wait()
exit(authOK ? 0 : 2)
`

func authenticateBiometric() error {
	cmd := exec.Command("swift", "-e", touchIDSwift, "gogo needs to access secrets")
	out, err := cmd.CombinedOutput()
	if err != nil {
		msg := strings.TrimSpace(string(out))
		if msg != "" {
			return fmt.Errorf("biometric authentication: %s", msg)
		}
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
	err := exec.Command("security", "add-generic-password", "-s", service, "-a", key, "-w", value, "-U").Run()
	if err != nil {
		return fmt.Errorf("storing secret %q in keychain %q: %w", key, service, err)
	}
	return nil
}
