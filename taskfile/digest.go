package taskfile

import (
	"crypto/sha256"
	"fmt"
	"io"
	"os"
)

// fileDigest computes the SHA256 digest of a file's identity and content.
// absPath is the on-disk location to read; identity is the bytes mixed in
// alongside the content to detect renames. Callers pass a path relative
// to the task's working directory so the resulting checksum is portable
// across machines (CI cache restore, repo moves, different $HOME) — an
// absolute path would bake in the developer's filesystem layout.
func fileDigest(absPath, identity string) ([sha256.Size]byte, error) {
	var digest [sha256.Size]byte

	f, err := os.Open(absPath)
	if err != nil {
		return digest, fmt.Errorf("opening %s: %w", absPath, err)
	}
	defer f.Close()

	h := sha256.New()
	h.Write([]byte(identity))
	h.Write([]byte{'\n'})
	if _, err := io.Copy(h, f); err != nil {
		return digest, fmt.Errorf("reading %s: %w", absPath, err)
	}

	h.Sum(digest[:0])
	return digest, nil
}
