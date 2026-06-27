package cops

import (
	"testing"

	"github.com/dgageot/rubocop-go/coptest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const secretSchemeHeader = `package taskfile
import "strings"
const secretSchemeOp = "op://"
`

func TestSecretSchemeInSyncFlagsMissingFromSlice(t *testing.T) {
	// Const is dispatched in resolveSecretURI but absent from the slice.
	src := secretSchemeHeader + `
var supportedSecretSchemes = []string{}
func resolveSecretURI(name, uri string) error {
	if strings.HasPrefix(uri, secretSchemeOp) {
		return nil
	}
	return nil
}
`
	offenses := coptest.RunNamed(t, NewSecretSchemeInSync(), "taskfile/secrets.go", src)
	require.Len(t, offenses, 1)
	assert.Contains(t, offenses[0].Message, "supportedSecretSchemes")
}

func TestSecretSchemeInSyncFlagsMissingFromDispatch(t *testing.T) {
	// Const is listed in the slice but never dispatched in resolveSecretURI.
	src := secretSchemeHeader + `
var supportedSecretSchemes = []string{secretSchemeOp}
func resolveSecretURI(name, uri string) error { return nil }
`
	offenses := coptest.RunNamed(t, NewSecretSchemeInSync(), "taskfile/secrets.go", src)
	require.Len(t, offenses, 1)
	assert.Contains(t, offenses[0].Message, "resolveSecretURI")
}

func TestSecretSchemeInSyncAllowsConsistent(t *testing.T) {
	src := secretSchemeHeader + `
var supportedSecretSchemes = []string{secretSchemeOp}
func resolveSecretURI(name, uri string) error {
	if strings.HasPrefix(uri, secretSchemeOp) {
		return nil
	}
	return nil
}
`
	assert.Empty(t, coptest.RunNamed(t, NewSecretSchemeInSync(), "taskfile/secrets.go", src))
}
