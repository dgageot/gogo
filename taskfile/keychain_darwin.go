package taskfile

// authenticateBiometric is a no-op on macOS because the Keychain
// already prompts the user for access when reading secrets.
func authenticateBiometric() error {
	return nil
}
