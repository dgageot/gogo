package cops

import (
	"testing"

	"github.com/dgageot/rubocop-go/coptest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestResetRanClearsGitCacheFlagsMissingField(t *testing.T) {
	// Resets runs but forgets gitOnce and gitVars.
	src := `package taskfile
import "sync"
type Runner struct {
	runs    sync.Map
	gitOnce sync.Once
	gitVars *int
}
func (r *Runner) ResetRan() {
	r.runs = sync.Map{}
}
`
	offenses := coptest.RunNamed(t, NewResetRanClearsGitCache(), "taskfile/runner.go", src)
	require.Len(t, offenses, 2) // gitOnce and gitVars
	for _, o := range offenses {
		assert.Equal(t, "Gogo/ResetRanClearsGitCache", o.CopName)
	}
}

func TestResetRanClearsGitCacheAllowsComplete(t *testing.T) {
	src := `package taskfile
import "sync"
type Runner struct {
	runs    sync.Map
	gitOnce sync.Once
	gitVars *int
}
func (r *Runner) ResetRan() {
	r.runs = sync.Map{}
	r.gitOnce = sync.Once{}
	r.gitVars = nil
}
`
	assert.Empty(t, coptest.RunNamed(t, NewResetRanClearsGitCache(), "taskfile/runner.go", src))
}
