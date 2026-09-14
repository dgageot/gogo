package taskfile

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTopLevelEnvProvidesOverridableDefaults(t *testing.T) {
	dir := t.TempDir()
	tf := &Config{
		Dir: dir,
		Env: map[string]string{
			"AWS_PROFILE": "${AWS_PROFILE:-Docker-Main/AIAgentTeam}",
			"URL":         "https://example.test:8443",
		},
		Tasks: map[string]Task{"run": {Cmds: []Cmd{{Cmd: "true"}}}},
	}
	r := newTestRunner(t, tf, dir)
	r.BaseEnv = nil
	execs := captureExecs(r)

	require.NoError(t, r.Run("run", ""))
	require.Len(t, *execs, 1)
	assert.Equal(t, "Docker-Main/AIAgentTeam", envValue((*execs)[0].Env, "AWS_PROFILE"))
	assert.Equal(t, "https://example.test:8443", envValue((*execs)[0].Env, "URL"))

	r.ResetRan()
	r.BaseEnv = []string{"AWS_PROFILE=custom"}
	require.NoError(t, r.Run("run", ""))
	assert.Equal(t, "custom", envValue((*execs)[1].Env, "AWS_PROFILE"))
}

func TestNamespaceEnvIsScopedAndMostSpecificWins(t *testing.T) {
	dir := t.TempDir()
	tf := &Config{
		Dir: dir,
		Env: map[string]string{"ROOT": "yes", "MODE": "root"},
		NamespaceEnv: map[string]map[string]string{
			"api":       {"MODE": "api"},
			"api:admin": {"MODE": "admin"},
			"worker":    {"MODE": "worker"},
		},
		Tasks: map[string]Task{
			"api:admin:run": {Cmds: []Cmd{{Cmd: "true"}}},
			"worker:run":    {Cmds: []Cmd{{Cmd: "true"}}},
		},
	}
	r := newTestRunner(t, tf, dir)
	execs := captureExecs(r)

	require.NoError(t, r.Run("api:admin:run", ""))
	require.NoError(t, r.Run("worker:run", ""))
	assert.Equal(t, "admin", envValue((*execs)[0].Env, "MODE"))
	assert.Equal(t, "worker", envValue((*execs)[1].Env, "MODE"))
	assert.Equal(t, "yes", envValue((*execs)[0].Env, "ROOT"))
}

func TestLoadWithIncludesAndFlattenScopesEnv(t *testing.T) {
	dir := t.TempDir()
	writeFiles(t, dir, map[string]string{
		"gogo.yaml": `version: "1"
env: {ROOT: root}
includes: [api]
`,
		"api/gogo.yaml": `version: "1"
env: {MODE: parent}
flatten: [shared.yml]
tasks:
  run: {cmd: true}
`,
		"api/shared.yml": `version: "1"
env: {MODE: flattened, FLAT: yes}
`,
	})

	tf, err := LoadWithIncludes(dir)
	require.NoError(t, err)
	assert.Equal(t, "root", tf.Env["ROOT"])
	assert.Equal(t, "parent", tf.NamespaceEnv["api"]["MODE"])
	assert.Equal(t, "yes", tf.NamespaceEnv["api"]["FLAT"])
}
