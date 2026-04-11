//go:build unix

package taskfile

import (
	"crypto/sha256"
	"syscall"
	"unsafe"
)

// fileDigest computes the SHA256 digest of a file's path and content.
// On Unix, it uses raw syscalls to avoid os.Open overhead (fstat + poller registration).
func fileDigest(path string) [sha256.Size]byte {
	fd, err := syscall.Open(path, syscall.O_RDONLY, 0)
	if err != nil {
		return [sha256.Size]byte{}
	}
	defer syscall.Close(fd)

	h := sha256.New()
	h.Write(unsafe.Slice(unsafe.StringData(path), len(path)))
	h.Write([]byte{'\n'})

	var buf [32 * 1024]byte
	for {
		n, err := syscall.Read(fd, buf[:])
		if n > 0 {
			h.Write(buf[:n])
		}
		if err != nil || n == 0 {
			break
		}
	}

	var digest [sha256.Size]byte
	h.Sum(digest[:0])
	return digest
}
