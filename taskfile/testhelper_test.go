package taskfile

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// writeFiles creates files under dir from a path->content map.
// Intermediate directories are created automatically.
func writeFiles(t *testing.T, dir string, files map[string]string) {
	t.Helper()

	for path, content := range files {
		full := filepath.Join(dir, path)
		require.NoError(t, os.MkdirAll(filepath.Dir(full), 0o755))
		require.NoError(t, os.WriteFile(full, []byte(content), 0o644))
	}
}
