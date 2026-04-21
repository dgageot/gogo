package taskfile

import (
	"crypto/sha256"
	"fmt"
	"io"
	"os"
)

// fileDigest computes the SHA256 digest of a file's path and content.
func fileDigest(path string) ([sha256.Size]byte, error) {
	var digest [sha256.Size]byte

	f, err := os.Open(path)
	if err != nil {
		return digest, fmt.Errorf("opening %s: %w", path, err)
	}
	defer f.Close()

	h := sha256.New()
	h.Write([]byte(path))
	h.Write([]byte{'\n'})
	if _, err := io.Copy(h, f); err != nil {
		return digest, fmt.Errorf("reading %s: %w", path, err)
	}

	h.Sum(digest[:0])
	return digest, nil
}
