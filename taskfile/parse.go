package taskfile

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	yaml "github.com/goccy/go-yaml"
	"github.com/goccy/go-yaml/ast"
	"github.com/goccy/go-yaml/parser"
)

// KeychainConfig describes which keychain service to read secrets from.
type KeychainConfig struct {
	Service string          `yaml:"service"`
	Secrets []KeychainEntry `yaml:"secrets"`
}

// Taskfile represents a parsed gogo.yaml (or legacy Taskfile.yml).
type Taskfile struct {
	Version    string            `yaml:"version"`
	Includes   []string          `yaml:"includes"`
	Dotenv     []string          `yaml:"dotenv"`
	Keychain   *KeychainConfig   `yaml:"keychain"`
	Vars       map[string]Var    `yaml:"vars"`
	Tasks      map[string]Task   `yaml:"tasks"`
	Dir        string            `yaml:"-"`
	Interval   string            `yaml:"interval"`
	Namespaces map[string]string `yaml:"-"` // dir -> namespace
	DotenvVars map[string]string `yaml:"-"` // resolved dotenv variables
	SecretVars map[string]string `yaml:"-"` // resolved keychain secrets
}

// Task represents a single task definition.
type Task struct {
	Cmds    []Cmd             `yaml:"cmds"`
	Deps    []Dep             `yaml:"deps"`
	Dir     string            `yaml:"dir"`
	Env     map[string]string `yaml:"env"`
	Vars    map[string]Var    `yaml:"vars"`
	Cmd     Cmd               `yaml:"cmd"`
	Sources []string          `yaml:"sources"`
	Aliases []string          `yaml:"aliases"`
	Desc    string            `yaml:"-"` // set from YAML comments, not from a field
}

// Cmd represents a command in a task. It can be a simple string or a task reference.
type Cmd struct {
	Cmd  string            `yaml:"cmd"`
	Task string            `yaml:"task"`
	Vars map[string]Var    `yaml:"vars"`
}

// isEmpty reports whether the command is empty.
func (c Cmd) isEmpty() bool {
	return c.Cmd == "" && c.Task == ""
}

// UnmarshalYAML allows Cmd to be either a string or a map.
func (c *Cmd) UnmarshalYAML(unmarshal func(any) error) error {
	var s string
	if err := unmarshal(&s); err == nil {
		c.Cmd = s
		return nil
	}
	type plain Cmd
	return unmarshal((*plain)(c))
}

// Dep represents a task dependency.
type Dep struct {
	Task string `yaml:"task"`
}

// UnmarshalYAML allows Dep to be either a string or a map.
func (d *Dep) UnmarshalYAML(unmarshal func(any) error) error {
	var s string
	if err := unmarshal(&s); err == nil {
		d.Task = s
		return nil
	}
	type plain Dep
	return unmarshal((*plain)(d))
}

// Var represents a variable value. It can be a static string or a shell command.
type Var struct {
	Value string `yaml:"value"`
	Sh    string `yaml:"sh"`
}

// UnmarshalYAML allows Var to be either a string or a map with sh.
func (v *Var) UnmarshalYAML(unmarshal func(any) error) error {
	var s string
	if err := unmarshal(&s); err == nil {
		v.Value = s
		return nil
	}
	type plain Var
	return unmarshal((*plain)(v))
}

// Parse reads and parses a Taskfile from the given directory.
func Parse(dir string) (*Taskfile, error) {
	path := findTaskfile(dir)
	if path == "" {
		return nil, fmt.Errorf("no Taskfile found in %s", dir)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", path, err)
	}

	expandedData := expandTemplates(data)

	var tf Taskfile
	if err := yaml.UnmarshalWithOptions(expandedData, &tf, yaml.Strict()); err != nil {
		return nil, fmt.Errorf("parsing %s:\n%s", path, yaml.FormatError(err, true, true))
	}

	tf.Dir = dir
	if tf.Tasks == nil {
		tf.Tasks = make(map[string]Task)
	}

	// Extract comments from AST to use as task descriptions
	applyTaskComments(&tf, expandedData)

	return &tf, nil
}

func findTaskfile(dir string) string {
	for _, name := range []string{"gogo.yaml", "Taskfile.yml", "Taskfile.yaml"} {
		path := filepath.Join(dir, name)
		if _, err := os.Stat(path); err == nil {
			return path
		}
	}
	return ""
}

// FindRootDir walks up from dir to find the topmost directory containing a Taskfile.
func FindRootDir(dir string) (string, error) {
	dir, err := filepath.Abs(dir)
	if err != nil {
		return "", err
	}

	found := ""
	for current := dir; ; {
		if findTaskfile(current) != "" {
			found = current
		}
		parent := filepath.Dir(current)
		if parent == current {
			break
		}
		current = parent
	}

	if found == "" {
		return "", errors.New("no Taskfile found")
	}

	return found, nil
}

var templatePattern = regexp.MustCompile(`\{\{\s*\.([A-Za-z_][A-Za-z0-9_]*)\s*\}\}`)

// expandTemplates replaces {{.VAR}} patterns with environment variable values.
func expandTemplates(data []byte) []byte {
	return templatePattern.ReplaceAllFunc(data, func(match []byte) []byte {
		name := templatePattern.FindSubmatch(match)[1]
		if val, ok := os.LookupEnv(string(name)); ok {
			return []byte(val)
		}
		return match
	})
}

// applyTaskComments parses the YAML AST to extract comments above task keys
// and uses them as task descriptions when no explicit desc is set.
func applyTaskComments(tf *Taskfile, data []byte) {
	file, err := parser.ParseBytes(data, parser.ParseComments)
	if err != nil {
		return
	}

	for _, doc := range file.Docs {
		mapping, ok := doc.Body.(*ast.MappingNode)
		if !ok {
			continue
		}

		taskMapping := findTasksMapping(mapping)
		if taskMapping == nil {
			continue
		}

		for _, taskMV := range taskMapping.Values {
			taskKey, ok := taskMV.Key.(*ast.StringNode)
			if !ok {
				continue
			}

			task, exists := tf.Tasks[taskKey.Value]
			if !exists {
				continue
			}

			if desc := extractCommentText(taskMV); desc != "" {
				task.Desc = desc
				tf.Tasks[taskKey.Value] = task
			}
		}
		break
	}
}

func findTasksMapping(mapping *ast.MappingNode) *ast.MappingNode {
	for _, mv := range mapping.Values {
		key, ok := mv.Key.(*ast.StringNode)
		if !ok || key.Value != "tasks" {
			continue
		}
		taskMapping, ok := mv.Value.(*ast.MappingNode)
		if !ok {
			return nil
		}
		return taskMapping
	}
	return nil
}

func extractCommentText(node *ast.MappingValueNode) string {
	comment := node.GetComment()
	if comment == nil {
		return ""
	}

	var b strings.Builder
	for _, c := range comment.Comments {
		text := strings.TrimSpace(strings.TrimPrefix(c.Token.Value, "#"))
		if text != "" {
			if b.Len() > 0 {
				b.WriteByte(' ')
			}
			b.WriteString(text)
		}
	}
	return b.String()
}

// LoadWithIncludes parses a Taskfile and resolves all includes into a flat task map.
func LoadWithIncludes(dir string) (*Taskfile, error) {
	tf, err := Parse(dir)
	if err != nil {
		return nil, err
	}

	tf.Namespaces = make(map[string]string)

	// Load dotenv files, deduplicating across includes
	seen := make(map[string]struct{})
	dotenvVars, err := loadDotenvFiles(dir, tf.Dotenv, seen)
	if err != nil {
		return nil, fmt.Errorf("loading dotenv: %w", err)
	}

	for _, namespace := range tf.Includes {
		incDir := filepath.Join(dir, namespace)

		child, err := Parse(incDir)
		if err != nil {
			return nil, fmt.Errorf("loading include %q: %w", namespace, err)
		}

		tf.Namespaces[incDir] = namespace

		// Load child dotenv files, deduplicating with parent
		childDotenv, err := loadDotenvFiles(incDir, child.Dotenv, seen)
		if err != nil {
			return nil, fmt.Errorf("loading dotenv for include %q: %w", namespace, err)
		}
		// Merge child dotenv (parent takes precedence)
		for k, v := range childDotenv {
			if _, exists := dotenvVars[k]; !exists {
				dotenvVars[k] = v
			}
		}

		for name, task := range child.Tasks {
			qualifiedName := namespace + ":" + name
			// Resolve relative dir to the child's directory
			if !filepath.IsAbs(task.Dir) {
				task.Dir = filepath.Join(child.Dir, task.Dir)
			}
			tf.Tasks[qualifiedName] = task
		}
	}

	tf.DotenvVars = dotenvVars

	// Load keychain secrets
	if tf.Keychain != nil {
		secrets, err := loadKeychainSecrets(tf.Keychain.Service, tf.Keychain.Secrets)
		if err != nil {
			return nil, fmt.Errorf("loading keychain secrets: %w", err)
		}
		tf.SecretVars = secrets
	}

	return tf, nil
}
