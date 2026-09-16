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

func TestFileEnvTemplatesResolveLazyVars(t *testing.T) {
	dir := t.TempDir()
	r := newTestRunner(t, &Config{
		Dir:           dir,
		Vars:          map[string]Var{"VERSION": {Value: "v1"}, "UNUSED": {Sh: "must not run"}},
		Env:           map[string]string{"IMAGE": "app:{{.VERSION}}"},
		NamespaceEnv:  map[string]map[string]string{"api": {"TAG": "{{.LOCAL}}"}},
		NamespaceVars: map[string]map[string]Var{"api": {"LOCAL": {Sh: "echo local"}}},
		Tasks: map[string]Task{
			"api:build": {Cmds: []Cmd{{Cmd: "echo $IMAGE $TAG"}}},
			"other":     {Cmds: []Cmd{{Cmd: "echo $IMAGE"}}},
		},
	}, dir)
	shell := &fakeShellRunner{outputFunc: func(req ShellCommand) ([]byte, error) {
		assert.Equal(t, "echo local", req.Command)
		return []byte("local"), nil
	}}
	r.ShellRunner = shell
	execs := captureExecs(r)
	require.NoError(t, r.Run("api:build", ""))
	require.NoError(t, r.Run("other", ""))
	assert.Equal(t, "app:v1", envValue((*execs)[0].Env, "IMAGE"))
	assert.Equal(t, "local", envValue((*execs)[0].Env, "TAG"))
	assert.Empty(t, envValue((*execs)[1].Env, "TAG"))
	assert.Len(t, shell.outputsSnapshot(), 1)
}

func TestFileEnvCrossReferencesUseProcessOverrides(t *testing.T) {
	dir := t.TempDir()
	r := newTestRunner(t, &Config{
		Dir:          dir,
		Env:          map[string]string{"HOST": "{{.UNUSED}}", "URL": "https://$HOST"},
		Vars:         map[string]Var{"UNUSED": {Sh: "must not execute"}},
		NamespaceEnv: map[string]map[string]string{"api": {"HOST": "namespace-default"}},
		Tasks: map[string]Task{
			"build":     {Cmds: []Cmd{{Cmd: "true"}}},
			"api:build": {Cmds: []Cmd{{Cmd: "true"}}},
		},
	}, dir)
	r.BaseEnv = []string{"HOST=production.example"}
	shell := &fakeShellRunner{}
	r.ShellRunner = shell
	execs := captureExecs(r)
	require.NoError(t, r.Run("build", ""))
	require.NoError(t, r.Run("api:build", ""))
	for _, exec := range *execs {
		assert.Equal(t, "production.example", envValue(exec.Env, "HOST"))
		assert.Equal(t, "https://production.example", envValue(exec.Env, "URL"))
	}
	assert.Empty(t, shell.outputsSnapshot())
}

func TestScopedEnvCrossReferenceResolvesOverlaidDefault(t *testing.T) {
	dir := t.TempDir()
	r := newTestRunner(t, &Config{
		Dir:   dir,
		Vars:  map[string]Var{"VERSION": {Value: "v1"}},
		Env:   map[string]string{"TAG": "{{.VERSION}}", "IMAGE": "app:$TAG"},
		Tasks: map[string]Task{"show": {Env: map[string]string{"TAG": "local"}, Cmds: []Cmd{{Cmd: "true"}}}},
	}, dir)
	execs := captureExecs(r)
	require.NoError(t, r.Run("show", ""))
	assert.Equal(t, "local", envValue((*execs)[0].Env, "TAG"))
	assert.Equal(t, "app:v1", envValue((*execs)[0].Env, "IMAGE"))
}

func TestShellDefaultsExpandSourceOnly(t *testing.T) {
	for _, tc := range []struct{ name, value, want string }{
		{"plain reference", "$TOKEN", "abc$OTHER"},
		{"default reference", "${TOKEN:-fallback}", "abc$OTHER"},
		{"fallback reference", "${MISSING:-$TOKEN}", "abc$OTHER"},
		{"empty value", "${EMPTY:-$OTHER}", "changed"},
		{"literal fallback", "${MISSING:-fallback}", "fallback"},
		{"multiple references", "$OTHER/${TOKEN:-fallback}/$OTHER/${MISSING:-$TOKEN}", "changed/abc$OTHER/changed/abc$OTHER"},
		{"unused fallback", "${TOKEN:-$OTHER}", "abc$OTHER"},
		{"special dollar", "${SPECIAL:-fallback}", "$$$2${OTHER}"},
		{"nested missing", "${MISSING:-${ALSO_MISSING:-8080}}", "8080"},
		{"nested value", "${MISSING:-${TOKEN:-fallback}}", "abc$OTHER"},
		{"nested unused", "${TOKEN:-${MISSING:-$OTHER}}", "abc$OTHER"},
		{"nested empty", "${EMPTY:-${MISSING:-$TOKEN}}", "abc$OTHER"},
		{"literal braces", "${MISSING:-{literal}}", "{literal}"},
		{"escaped dollar", "$${MISSING:-fallback}", "$${MISSING:-fallback}"},
		{"positional", "$2/${MISSING:-fallback}", "$2/fallback"},
		{"trailing dollar", "${MISSING:-fallback}$", "fallback$"},
		{"incomplete parameter", "${MISSING:-${TOKEN}", "${MISSING:-${TOKEN}"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			env := resolveTaskEnv(map[string]string{"COPY": tc.value}, []string{"TOKEN=abc$OTHER", "OTHER=changed", "EMPTY=", "SPECIAL=$$$2${OTHER}"}, nil, nil)
			assert.Equal(t, tc.want, env["COPY"])
		})
	}
}

func TestShellDefaultPreservesCrossReferencedValue(t *testing.T) {
	env := resolveTaskEnv(map[string]string{"COPY": "${TOKEN:-fallback}", "TOKEN": "$RAW"}, []string{"RAW=abc$OTHER", "OTHER=changed"}, nil, nil)
	assert.Equal(t, "abc$OTHER", env["COPY"])
	assert.Equal(t, "abc$OTHER", env["TOKEN"])
}

func TestUnusedDefaultDoesNotResolveFallback(t *testing.T) {
	var lookedUp []string
	value := expandShellDefaults("${SET:-${UNUSED:-fallback}}", func(name string) (string, bool) {
		lookedUp = append(lookedUp, name)
		return "value$OTHER", true
	})
	assert.Equal(t, "value$OTHER", value)
	assert.Equal(t, []string{"SET"}, lookedUp)
}
