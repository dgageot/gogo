package taskfile

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
)

var compileSwiftHelper = sync.OnceValues(func() (string, error) {
	cacheDir, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}

	cacheDir = filepath.Join(cacheDir, "gogo")
	if err := os.MkdirAll(cacheDir, 0o700); err != nil {
		return "", err
	}

	binary := filepath.Join(cacheDir, "keychain-helper")
	source := filepath.Join(cacheDir, "keychain-helper.swift")

	// Recompile only if binary is missing
	if _, err := os.Stat(binary); err == nil {
		return binary, nil
	}

	if err := os.WriteFile(source, []byte(keychainSwiftSource), 0o600); err != nil {
		return "", err
	}

	if out, err := exec.Command("swiftc", "-O", "-o", binary, source).CombinedOutput(); err != nil {
		return "", fmt.Errorf("compiling keychain helper: %w\n%s", err, out)
	}

	return binary, nil
})

const keychainSwiftSource = `import Foundation
import LocalAuthentication
import Security

let args = CommandLine.arguments
guard args.count >= 4 else {
    fputs("usage: {set|get} <service> <key> [value]\n", stderr)
    exit(1)
}

let action = args[1]
let service = args[2]
let key = args[3]

func touchID(_ reason: String) -> Bool {
    let context = LAContext()
    var error: NSError?
    guard context.canEvaluatePolicy(.deviceOwnerAuthenticationWithBiometrics, error: &error) else {
        fputs("Touch ID unavailable: \(error?.localizedDescription ?? "unknown")\n", stderr)
        return false
    }

    let semaphore = DispatchSemaphore(value: 0)
    var success = false
    context.evaluatePolicy(.deviceOwnerAuthenticationWithBiometrics, localizedReason: reason) { ok, _ in
        success = ok
        semaphore.signal()
    }
    semaphore.wait()
    return success
}

func setSecret(service: String, key: String, value: String) -> Bool {
    let query: [String: Any] = [
        kSecClass as String: kSecClassGenericPassword,
        kSecAttrService as String: service,
        kSecAttrAccount as String: key,
        kSecValueData as String: value.data(using: .utf8)!,
    ]

    var status = SecItemAdd(query as CFDictionary, nil)
    if status == errSecDuplicateItem {
        let search: [String: Any] = [
            kSecClass as String: kSecClassGenericPassword,
            kSecAttrService as String: service,
            kSecAttrAccount as String: key,
        ]
        let update: [String: Any] = [
            kSecValueData as String: value.data(using: .utf8)!,
        ]
        status = SecItemUpdate(search as CFDictionary, update as CFDictionary)
    }

    if status != errSecSuccess {
        fputs("Failed to store secret: \(status)\n", stderr)
        return false
    }
    return true
}

// getSecret retrieves a secret from the macOS Keychain via the Swift helper.
func getSecret(service: String, key: String) -> String? {
    let query: [String: Any] = [
        kSecClass as String: kSecClassGenericPassword,
        kSecAttrService as String: service,
        kSecAttrAccount as String: key,
        kSecReturnData as String: true,
        kSecMatchLimit as String: kSecMatchLimitOne,
    ]

    var result: AnyObject?
    let status = SecItemCopyMatching(query as CFDictionary, &result)
    if status != errSecSuccess {
        fputs("Failed to read secret: \(status)\n", stderr)
        return nil
    }

    guard let data = result as? Data, let value = String(data: data, encoding: .utf8) else {
        fputs("Failed to decode secret\n", stderr)
        return nil
    }
    return value
}

switch action {
case "set":
    guard args.count >= 5 else {
        fputs("usage: set <service> <key> <value>\n", stderr)
        exit(1)
    }
    exit(setSecret(service: service, key: key, value: args[4]) ? 0 : 1)
case "get":
    guard touchID("gogo needs to access secrets") else {
        fputs("Authentication failed\n", stderr)
        exit(2)
    }
    guard let value = getSecret(service: service, key: key) else {
        exit(1)
    }
    print(value, terminator: "")
    exit(0)
default:
    fputs("unknown action: \(action)\n", stderr)
    exit(1)
}
`

// authenticateBiometric is a no-op on macOS; Touch ID is triggered per-secret access.
func authenticateBiometric() error {
	return nil
}

// runHelper compiles the Swift keychain helper (if needed) and runs it with the given args.
func runHelper(args ...string) (string, error) {
	helper, err := compileSwiftHelper()
	if err != nil {
		return "", err
	}

	out, err := exec.Command(helper, args...).CombinedOutput()
	if err != nil {
		if msg := strings.TrimSpace(string(out)); msg != "" {
			return "", errors.New(msg)
		}
		return "", err
	}
	return string(out), nil
}

func getSecret(service, key string) (string, error) {
	out, err := runHelper("get", service, key)
	if err != nil {
		return "", fmt.Errorf("reading secret %q from keychain %q: %w", key, service, err)
	}
	return out, nil
}

// SetSecret stores a secret in the macOS Keychain.
func SetSecret(service, key, value string) error {
	if _, err := runHelper("set", service, key, value); err != nil {
		return fmt.Errorf("storing secret %q in keychain %q: %w", key, service, err)
	}
	return nil
}
