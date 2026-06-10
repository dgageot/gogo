package taskfile

// Config represents a parsed gogo.yaml.
type Config struct {
	Version       string                    `yaml:"version"`
	Includes      []string                  `yaml:"includes"`
	Flatten       []string                  `yaml:"flatten"` // YAML files whose tasks merge into this config without a namespace
	Dotenv        []string                  `yaml:"dotenv"`
	Default       string                    `yaml:"default"` // task to run when none is given on the CLI; replaces the convention of declaring a `default` task
	Vars          map[string]Var            `yaml:"vars"`    // vars declared at the root (namespace "")
	Sources       map[string]StringList     `yaml:"sources"` // named source presets, referenced from task.Sources by name
	Secrets       map[string]string         `yaml:"secrets"` // named secret URIs (op://, aws-creds://, ...), referenced from task.Secrets
	Tasks         map[string]Task           `yaml:"tasks"`
	Dir           string                    `yaml:"-"`
	Interval      string                    `yaml:"interval"`
	Namespaces    map[string]string         `yaml:"-"` // dir -> namespace
	NamespaceDirs map[string]string         `yaml:"-"` // namespace -> dir (where each include's gogo.yaml lives, used to resolve `sh:` vars in their own working dir)
	NamespaceVars map[string]map[string]Var `yaml:"-"` // namespace -> vars declared at that namespace; not visible to sibling namespaces
	DotenvVars    map[string]string         `yaml:"-"` // resolved dotenv variables
}

// Task represents a single task definition.
type Task struct {
	Cmds          []Cmd             `yaml:"cmds"`
	Deps          []Dep             `yaml:"deps"`
	Dir           string            `yaml:"dir"`
	Dotenv        []string          `yaml:"dotenv"`
	Env           map[string]string `yaml:"env"`
	Vars          map[string]Var    `yaml:"vars"`
	Secrets       StringList        `yaml:"secrets"` // names referencing entries in Config.Secrets
	Sources       StringList        `yaml:"sources"`
	Generates     StringList        `yaml:"generates"`
	Aliases       StringList        `yaml:"aliases"`
	Platforms     StringList        `yaml:"platforms"`
	Requires      Requires          `yaml:"requires"`
	Preconditions []Precondition    `yaml:"preconditions"`
	Prompt        string            `yaml:"prompt"` // confirmation message shown before the task runs
	Silent        bool              `yaml:"silent"` // when true, suppress the per-cmd "[task] cmd" log line
	Desc          string            `yaml:"-"`      // set from YAML comments, not from a field
}

// UnmarshalYAML normalizes the singular "cmd" field into the "cmds" list.
func (t *Task) UnmarshalYAML(unmarshal func(any) error) error {
	type plain Task
	raw := struct {
		Plain plain `yaml:",inline"`
		Cmd   Cmd   `yaml:"cmd"`
	}{}
	if err := unmarshal(&raw); err != nil {
		return err
	}

	*t = Task(raw.Plain)
	if raw.Cmd.isSet() {
		t.Cmds = []Cmd{raw.Cmd}
	}
	return nil
}

// Precondition defines a shell command that must succeed before a task runs.
type Precondition struct {
	Sh  string `yaml:"sh"`
	Msg string `yaml:"msg"`
}

// UnmarshalYAML allows Precondition to be either a string (shell command) or a map.
func (p *Precondition) UnmarshalYAML(unmarshal func(any) error) error {
	var s string
	if err := unmarshal(&s); err == nil {
		p.Sh = s
		return nil
	}
	type plain Precondition
	return unmarshal((*plain)(p))
}

// Requires defines variables and environment variables that must be set for a task to run.
type Requires struct {
	Vars []string `yaml:"vars"`
	Env  []string `yaml:"env"`
}

// StringList is a []string that can be unmarshalled from either a single string or a list.
type StringList []string

// UnmarshalYAML allows StringList to be either a string or a sequence.
func (sl *StringList) UnmarshalYAML(unmarshal func(any) error) error {
	var s string
	if err := unmarshal(&s); err == nil {
		*sl = []string{s}
		return nil
	}
	var list []string
	if err := unmarshal(&list); err != nil {
		return err
	}
	*sl = list
	return nil
}

// Cmd represents a command in a task. It can be a simple string, a task
// reference, or a deferred cleanup command.
type Cmd struct {
	Cmd         string         `yaml:"cmd"`
	Task        string         `yaml:"task"`
	Defer       string         `yaml:"defer"`        // shell command run after the task's cmds, even on failure
	IgnoreError bool           `yaml:"ignore_error"` // when true, a failure of this cmd doesn't stop the task (shell cmds only; not honored on task: or defer: entries)
	Vars        map[string]Var `yaml:"vars"`
}

// isSet returns true if the Cmd has a command, task reference, or deferred command.
func (c *Cmd) isSet() bool {
	return c.Cmd != "" || c.Task != "" || c.Defer != ""
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
