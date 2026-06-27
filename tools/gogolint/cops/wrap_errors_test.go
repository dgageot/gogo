package cops

import (
	"testing"

	"github.com/dgageot/rubocop-go/coptest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWrapErrorsFlagsVerbOnError(t *testing.T) {
	src := `package p
import "fmt"
func f(err error) error { return fmt.Errorf("oops: %v", err) }
`
	offenses := coptest.RunTyped(t, NewWrapErrors(), src)
	require.Len(t, offenses, 1)
	assert.Equal(t, "Gogo/WrapErrors", offenses[0].CopName)
}

func TestWrapErrorsAllowsWrapVerb(t *testing.T) {
	src := `package p
import "fmt"
func f(err error) error { return fmt.Errorf("oops: %w", err) }
`
	assert.Empty(t, coptest.RunTyped(t, NewWrapErrors(), src))
}

func TestWrapErrorsIgnoresNonErrorArgs(t *testing.T) {
	src := `package p
import "fmt"
func f(name string) error { return fmt.Errorf("bad name %q", name) }
`
	assert.Empty(t, coptest.RunTyped(t, NewWrapErrors(), src))
}
