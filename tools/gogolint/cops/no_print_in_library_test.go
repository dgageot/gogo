package cops

import (
	"testing"

	"github.com/dgageot/rubocop-go/coptest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNoPrintInLibraryFlagsPrint(t *testing.T) {
	src := `package taskfile
import "fmt"
func f() { fmt.Println("hi") }
`
	offenses := coptest.RunNamed(t, NewNoPrintInLibrary(), "taskfile/runner.go", src)
	require.Len(t, offenses, 1)
	assert.Equal(t, "Gogo/NoPrintInLibrary", offenses[0].CopName)
}

func TestNoPrintInLibraryAllowsMain(t *testing.T) {
	src := `package main
import "fmt"
func f() { fmt.Println("hi") }
`
	assert.Empty(t, coptest.RunNamed(t, NewNoPrintInLibrary(), "main.go", src))
}

func TestNoPrintInLibrarySkipsTests(t *testing.T) {
	src := `package taskfile
import "fmt"
func f() { fmt.Println("hi") }
`
	assert.Empty(t, coptest.RunNamed(t, NewNoPrintInLibrary(), "taskfile/runner_test.go", src))
}
