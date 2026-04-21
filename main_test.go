package main

import (
	"os/exec"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestShellQuote(t *testing.T) {
	assert.Equal(t, "''", shellQuote(""))
	assert.Equal(t, "'hello'", shellQuote("hello"))
	assert.Equal(t, "'hello world'", shellQuote("hello world"))
	assert.Equal(t, `'it'\''s'`, shellQuote("it's"))
}

func TestShellJoinPreservesBoundaries(t *testing.T) {
	cases := [][]string{
		{"hello world", "foo"},
		{"a", "b c", "d"},
		{"with 'quote", "ok"},
		{"$VAR;echo pwned", "safe"},
		{},
	}

	for _, in := range cases {
		joined := shellJoin(in)

		// Echo each argument on its own line through /bin/sh, then split back.
		script := "for a in " + joined + `; do printf '%s\n' "$a"; done`
		out, err := exec.Command("/bin/sh", "-c", script).Output()
		require.NoError(t, err, "script: %s", script)

		var got []string
		if s := strings.TrimRight(string(out), "\n"); s != "" {
			got = strings.Split(s, "\n")
		}
		assert.Equal(t, append([]string(nil), in...), got, "round-trip failed for %q", in)
	}
}

func TestWatchInterval(t *testing.T) {
	d, err := watchInterval("")
	require.NoError(t, err)
	assert.Equal(t, "500ms", d.String())

	d, err = watchInterval("2s")
	require.NoError(t, err)
	assert.Equal(t, "2s", d.String())

	_, err = watchInterval("not-a-duration")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not-a-duration")
}
