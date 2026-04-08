package taskfile

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"

	yaml "github.com/goccy/go-yaml"
)

// Taskfile represents a parsed Taskfile.yml.
type Taskfile struct {
	Version    string             `yaml:"version"`
	Includes   map[string]Include `yaml:"includes"`
	Dotenv     []string           `yaml:"dotenv"`
	Vars       map[string]Var     `yaml:"vars"`
	Tasks      map[string]Task    `yaml:"tasks"`
	Dir        string             `yaml:"-"`
	Interval   string             `yaml:"interval"`
	Namespaces map[string]string  `yaml:"-"` // dir -> namespace
}

// Include represents an included taskfile reference.
type Include struct {
	Taskfile string `yaml:"taskfile"`
	Dir      string `yaml:"dir"`
}

// UnmarshalYAML allows Include to be either a string or a map.
func (i *Include) UnmarshalYAML(unmarshal func(interface{}) error) error {
	var s string
	if err := unmarshal(&s); err == nil {
		i.Taskfile = s
		return nil
	}
	type plain Include
	return unmarshal((*plain)(i))
}

// Task represents a single task definition.
type Task struct {
	Desc    string            `yaml:"desc"`
	Cmds    []Cmd             `yaml:"cmds"`
	Deps    []Dep             `yaml:"deps"`
	Dir     string            `yaml:"dir"`
	Env     map[string]string `yaml:"env"`
	Vars    map[string]Var    `yaml:"vars"`
	Cmd     Cmd               `yaml:"cmd"`
	Sources []string          `yaml:"sources"`
	Watch   bool              `yaml:"watch"`
	Aliases []string          `yaml:"aliases"`
}

// Cmd represents a command in a task. It can be a simple string or a task reference.
type Cmd struct {
	Cmd  string            `yaml:"cmd"`
	Task string            `yaml:"task"`
	Vars map[string]Var    `yaml:"vars"`
}

// UnmarshalYAML allows Cmd to be either a string or a map.
func (c *Cmd) UnmarshalYAML(unmarshal func(interface{}) error) error {
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
	Task string
}

// UnmarshalYAML allows Dep to be either a string or a map.
func (d *Dep) UnmarshalYAML(unmarshal func(interface{}) error) error {
	var s string
	if err := unmarshal(&s); err == nil {
		d.Task = s
		return nil
	}
	type depMap struct {
		Task string `yaml:"task"`
	}
	var m depMap
	if err := unmarshal(&m); err != nil {
		return err
	}
	d.Task = m.Task
	return nil
}

// Var represents a variable value. It can be a static string or a shell command.
type Var struct {
	Value string
	Sh    string
}

// UnmarshalYAML allows Var to be either a string or a map with sh.
func (v *Var) UnmarshalYAML(unmarshal func(interface{}) error) error {
	var s string
	if err := unmarshal(&s); err == nil {
		v.Value = s
		return nil
	}
	type varMap struct {
		Sh string `yaml:"sh"`
	}
	var m varMap
	if err := unmarshal(&m); err != nil {
		return err
	}
	v.Sh = m.Sh
	return nil
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

	var tf Taskfile
	if err := yaml.UnmarshalWithOptions(expandTemplates(data), &tf, yaml.Strict()); err != nil {
		return nil, fmt.Errorf("parsing %s:\n%s", path, yaml.FormatError(err, true, true))
	}

	tf.Dir = dir
	if tf.Tasks == nil {
		tf.Tasks = make(map[string]Task)
	}

	return &tf, nil
}

func findTaskfile(dir string) string {
	for _, name := range []string{"Taskfile.yml", "Taskfile.yaml"} {
		path := filepath.Join(dir, name)
		if _, err := os.Stat(path); err == nil {
			return path
		}
	}
	return ""
}

// FindRootDir walks up from dir to find the topmost directory containing a Taskfile.
// It first finds the nearest Taskfile, then keeps walking up to find any ancestor
// that also has a Taskfile (to support running from included subdirectories).
func FindRootDir(dir string) (string, error) {
	dir, err := filepath.Abs(dir)
	if err != nil {
		return "", err
	}

	// Find the nearest Taskfile first
	found := ""
	current := dir
	for {
		if findTaskfile(current) != "" {
			found = current
			break
		}
		parent := filepath.Dir(current)
		if parent == current {
			return "", fmt.Errorf("no Taskfile found")
		}
		current = parent
	}

	// Keep walking up to find the topmost Taskfile
	for {
		parent := filepath.Dir(found)
		if parent == found {
			break
		}
		if findTaskfile(parent) != "" {
			found = parent
		} else {
			break
		}
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

// LoadWithIncludes parses a Taskfile and resolves all includes into a flat task map.
func LoadWithIncludes(dir string) (*Taskfile, error) {
	tf, err := Parse(dir)
	if err != nil {
		return nil, err
	}

	tf.Namespaces = make(map[string]string)

	for namespace, inc := range tf.Includes {
		incDir := inc.Dir
		if incDir == "" {
			incDir = filepath.Dir(inc.Taskfile)
		}
		if !filepath.IsAbs(incDir) {
			incDir = filepath.Join(dir, incDir)
		}

		child, err := Parse(incDir)
		if err != nil {
			return nil, fmt.Errorf("loading include %q: %w", namespace, err)
		}

		tf.Namespaces[incDir] = namespace

		for name, task := range child.Tasks {
			qualifiedName := namespace + ":" + name
			// Resolve relative dir to the child's directory
			if task.Dir == "" {
				task.Dir = child.Dir
			} else if !filepath.IsAbs(task.Dir) {
				task.Dir = filepath.Join(child.Dir, task.Dir)
			}
			tf.Tasks[qualifiedName] = task
		}
	}

	return tf, nil
}
