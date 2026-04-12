package taskfile

import (
	"crypto/sha256"
	"io"
	"os"
)

// fileDigest computes the SHA256 digest of a file's path and content.
func fileDigest(path string) [sha256.Size]byte {
	f, err := os.Open(path)
	if err != nil {
		return [sha256.Size]byte{}
	}
	defer f.Close()

	h := sha256.New()
	h.Write([]byte(path))
	h.Write([]byte{'\n'})
	io.Copy(h, f) //nolint:errcheck // best-effort read; unreadable files produce a zero digest

	var digest [sha256.Size]byte
	h.Sum(digest[:0])
	return digest
}
