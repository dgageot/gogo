//go:build !cgo

package taskfile

import (
	"context"
	"errors"
)

func loadOnePasswordSecrets(_ context.Context, _, _ map[string]string) error {
	return errors.New("1Password support requires CGO (not available in this build)")
}
