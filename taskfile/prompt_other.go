//go:build !darwin && !linux

package taskfile

import (
	"context"
	"os"
)

// Other platforms retain their native blocking input behavior.
func waitPromptInput(ctx context.Context, _ *os.File) error {
	return ctx.Err()
}
