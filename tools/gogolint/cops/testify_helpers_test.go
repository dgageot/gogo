package cops

import (
	"testing"

	"github.com/dgageot/rubocop-go/coptest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTestifyHelpersFlagsBareFatal(t *testing.T) {
	src := `package p
import "testing"
func TestX(t *testing.T) {
	if 1 != 2 {
		t.Fatalf("nope")
	}
}
`
	offenses := coptest.RunNamed(t, NewTestifyHelpers(), "foo_test.go", src)
	require.Len(t, offenses, 1)
	assert.Equal(t, "Gogo/TestifyHelpers", offenses[0].CopName)
}

func TestTestifyHelpersAllowsErrError(t *testing.T) {
	// err.Error() must not be confused with a t.Error testing call.
	src := `package p
import "testing"
func TestX(t *testing.T) {
	var err error
	_ = err.Error()
}
`
	assert.Empty(t, coptest.RunNamed(t, NewTestifyHelpers(), "foo_test.go", src))
}

func TestTestifyHelpersSkipsNonTestFiles(t *testing.T) {
	src := `package p
type T struct{}
func (t *T) Error(string) {}
func use(t *T) { t.Error("x") }
`
	assert.Empty(t, coptest.RunNamed(t, NewTestifyHelpers(), "foo.go", src))
}
