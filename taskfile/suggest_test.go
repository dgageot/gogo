package taskfile

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSuggestSingleTypo(t *testing.T) {
	dir := t.TempDir()
	tf := makePrefixTF(dir, "build", "test", "lint")

	runner := newTestRunner(t, tf, dir)

	err := runner.Run("buld", "")
	require.EqualError(t, err, `task "buld" not found, did you mean: build?`)
}

func TestSuggestSubstitution(t *testing.T) {
	dir := t.TempDir()
	tf := makePrefixTF(dir, "deploy", "build")

	runner := newTestRunner(t, tf, dir)

	err := runner.Run("deplor", "")
	require.EqualError(t, err, `task "deplor" not found, did you mean: deploy?`)
}

func TestSuggestNoneWhenTooDifferent(t *testing.T) {
	// Wildly different names produce no suggestion: the error keeps its
	// original shape so tooling that grep'd for "not found" doesn't break.
	dir := t.TempDir()
	tf := makePrefixTF(dir, "build", "test")

	runner := newTestRunner(t, tf, dir)

	err := runner.Run("xyzzy", "")
	require.EqualError(t, err, `task "xyzzy" not found`)
}

func TestSuggestSortedByDistance(t *testing.T) {
	dir := t.TempDir()
	tf := makePrefixTF(dir, "test", "rest", "build")

	runner := newTestRunner(t, tf, dir)

	// "tist" doesn't prefix any task. By Levenshtein it's at distance 1
	// from "test" and 2 from "rest"; only the closest match is shown.
	err := runner.Run("tist", "")
	require.EqualError(t, err, `task "tist" not found, did you mean: test?`)
}

func TestSuggestCaps(t *testing.T) {
	dir := t.TempDir()
	tf := makePrefixTF(dir, "build1", "build2", "build3", "build4", "build5")

	runner := newTestRunner(t, tf, dir)

	// "buld" doesn't prefix any task (each starts with "buil"). It sits
	// at distance 2 from every "buildN", so all five tie. Even with a tie,
	// only one suggestion is shown — alphabetically first wins.
	err := runner.Run("buld", "")
	require.EqualError(t, err, `task "buld" not found, did you mean: build1?`)
}

func TestSuggestIncludesAliases(t *testing.T) {
	dir := t.TempDir()
	tf := &Config{
		Dir: dir,
		Tasks: map[string]Task{
			"github": {Aliases: StringList{"gh"}, Cmds: []Cmd{{Cmd: "gh"}}},
			"build":  {Cmds: []Cmd{{Cmd: "go build"}}},
		},
		DotenvVars: make(map[string]string),
	}

	runner := newTestRunner(t, tf, dir)

	// "gj" is distance 1 from the alias "gh"; the suggestion surfaces the
	// underlying task name ("github"), not the alias.
	err := runner.Run("gj", "")
	require.EqualError(t, err, `task "gj" not found, did you mean: github?`)
}

func TestSuggestSkipsInternalTasks(t *testing.T) {
	dir := t.TempDir()
	tf := &Config{
		Dir: dir,
		Tasks: map[string]Task{
			"_setup": {Cmds: []Cmd{{Cmd: "true"}}},
			"build":  {Cmds: []Cmd{{Cmd: "true"}}},
		},
		DotenvVars: make(map[string]string),
	}

	runner := newTestRunner(t, tf, dir)

	// "_setp" is closer to "_setup" but the latter is internal and must
	// not be suggested. With no other near-matches, no hint is appended.
	err := runner.Run("_setp", "")
	require.EqualError(t, err, `task "_setp" not found`)
}

func TestSuggestDeduplicatesAliasAndTaskName(t *testing.T) {
	dir := t.TempDir()
	tf := &Config{
		Dir: dir,
		Tasks: map[string]Task{
			"install": {Aliases: StringList{"instal"}, Cmds: []Cmd{{Cmd: "true"}}},
		},
		DotenvVars: make(map[string]string),
	}

	runner := newTestRunner(t, tf, dir)

	// Both the task name "install" and the alias "instal" are within
	// distance 2 of the input. The task should appear once.
	err := runner.Run("instll", "")
	require.EqualError(t, err, `task "instll" not found, did you mean: install?`)
}

func TestLevenshteinBasics(t *testing.T) {
	assert.Equal(t, 0, levenshtein("", ""))
	assert.Equal(t, 3, levenshtein("", "abc"))
	assert.Equal(t, 3, levenshtein("abc", ""))
	assert.Equal(t, 0, levenshtein("foo", "foo"))
	assert.Equal(t, 1, levenshtein("kitten", "kittens"))
	assert.Equal(t, 3, levenshtein("kitten", "sitting"))
}
