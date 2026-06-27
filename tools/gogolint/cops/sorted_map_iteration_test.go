package cops

import (
	"testing"

	"github.com/dgageot/rubocop-go/coptest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSortedMapIterationFlagsAppendFromMap(t *testing.T) {
	src := `package p
func keys(m map[string]int) []string {
	var out []string
	for k := range m {
		out = append(out, k)
	}
	return out
}
`
	offenses := coptest.RunTyped(t, NewSortedMapIteration(), src)
	require.Len(t, offenses, 1)
	assert.Equal(t, "Gogo/SortedMapIteration", offenses[0].CopName)
}

func TestSortedMapIterationAllowsSortedResult(t *testing.T) {
	// range+append is fine when the function sorts before returning.
	src := `package p
import "slices"
func keys(m map[string]int) []string {
	var out []string
	for k := range m {
		out = append(out, k)
	}
	slices.Sort(out)
	return out
}
`
	assert.Empty(t, coptest.RunTyped(t, NewSortedMapIteration(), src))
}

func TestSortedMapIterationAllowsMapToMap(t *testing.T) {
	// Writing into another map is order-independent.
	src := `package p
func copyMap(m map[string]int) map[string]int {
	out := map[string]int{}
	for k, v := range m {
		out[k] = v
	}
	return out
}
`
	assert.Empty(t, coptest.RunTyped(t, NewSortedMapIteration(), src))
}

func TestSortedMapIterationAllowsSliceRange(t *testing.T) {
	src := `package p
func dup(s []string) []string {
	var out []string
	for _, v := range s {
		out = append(out, v)
	}
	return out
}
`
	assert.Empty(t, coptest.RunTyped(t, NewSortedMapIteration(), src))
}
