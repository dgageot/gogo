//go:build !darwin

package taskfile

// authenticateBiometric is a no-op on non-macOS platforms.
func authenticateBiometric() error {
	return nil
}
