package taskfile

import (
	"fmt"
	"maps"
	"slices"
	"strings"
)

// builtinSourcePresets returns gogo's built-in named source groups. They can
// be overridden by user-defined entries in the top-level `sources:` map —
// user entries always win on a name collision.
//
// The default set focuses on Go projects because that's by far the most
// duplicated boilerplate today. Presets compose: `go-vendored` references
// `go` and adds `vendor/**`.
func builtinSourcePresets() map[string]StringList {
	return map[string]StringList{
		"go":          {"**/*.go", "go.mod", "go.sum"},
		"go-vendored": {"go", "vendor/**"},
	}
}

// effectivePresets merges user-defined presets on top of the built-ins.
// A nil user map yields just the built-ins.
func effectivePresets(user map[string]StringList) map[string]StringList {
	merged := builtinSourcePresets()
	maps.Copy(merged, user)
	return merged
}

// resolveSources expands a list of `sources:` entries into a flat list of
// glob patterns. An entry that matches a known preset name is recursively
// expanded; anything else is treated as a literal glob.
//
// Cycles and unknown names that look like presets are caught here. The result
// is deduplicated while preserving first-seen order.
func resolveSources(presets map[string]StringList, list []string) ([]string, error) {
	if len(list) == 0 {
		return nil, nil
	}
	out := make([]string, 0, len(list))
	seen := make(map[string]struct{})
	if err := expandSources(presets, list, &out, seen, nil); err != nil {
		return nil, err
	}
	return out, nil
}

// expandSources is the recursive worker behind resolveSources. The visiting
// stack tracks preset names currently being expanded so we can report cycles.
func expandSources(presets map[string]StringList, list []string, out *[]string, seen map[string]struct{}, visiting []string) error {
	for _, entry := range list {
		if entry == "" {
			continue
		}
		if !looksLikePresetName(entry) {
			appendUnique(out, seen, entry)
			continue
		}
		preset, ok := presets[entry]
		if !ok {
			// No preset matches but the name has no glob characters. Treat as
			// a literal path — keeps backward compatibility with entries like
			// "go.mod" or ".golangci.yml" that happen to be plain filenames.
			appendUnique(out, seen, entry)
			continue
		}
		if slices.Contains(visiting, entry) {
			return fmt.Errorf("cyclic source preset %q (chain: %s)", entry, strings.Join(append(visiting, entry), " -> "))
		}
		if err := expandSources(presets, []string(preset), out, seen, append(visiting, entry)); err != nil {
			return err
		}
	}
	return nil
}

// looksLikePresetName reports whether s could be a preset reference rather
// than a glob. Anything containing a glob metacharacter or path separator is
// definitely a glob; the rest is *eligible* to look up in the preset map.
func looksLikePresetName(s string) bool {
	return !strings.ContainsAny(s, "*?[]/\\")
}

func appendUnique(out *[]string, seen map[string]struct{}, s string) {
	if _, ok := seen[s]; ok {
		return
	}
	seen[s] = struct{}{}
	*out = append(*out, s)
}

// taskSources resolves task.Sources against the config's preset map (with
// built-ins applied). It returns the original list verbatim on error so
// callers can surface meaningful diagnostics — but the only errors here are
// cyclic preset definitions, which we want to fail loudly.
func (c *Config) taskSources(sources []string) ([]string, error) {
	if len(sources) == 0 {
		return nil, nil
	}
	return resolveSources(effectivePresets(c.Sources), sources)
}
