package taskfile

// Taskfile represents a parsed gogo.yaml (or legacy Taskfile.yml).
type Taskfile struct {
	Version    string            `yaml:"version"`
	Includes   []string          `yaml:"includes"`
	Dotenv     []string          `yaml:"dotenv"`
	Vars       map[string]Var    `yaml:"vars"`
	Tasks      map[string]Task   `yaml:"tasks"`
	Dir        string            `yaml:"-"`
	Interval   string            `yaml:"interval"`
	Namespaces map[string]string `yaml:"-"` // dir -> namespace
	DotenvVars map[string]string `yaml:"-"` // resolved dotenv variables
}

// Task represents a single task definition.
type Task struct {
	Cmds          []Cmd             `yaml:"cmds"`
	Deps          []Dep             `yaml:"deps"`
	Dir           string            `yaml:"dir"`
	Dotenv        []string          `yaml:"dotenv"`
	Env           map[string]string `yaml:"env"`
	Vars          map[string]Var    `yaml:"vars"`
	Sources       StringList        `yaml:"sources"`
	Generates     StringList        `yaml:"generates"`
	Aliases       StringList        `yaml:"aliases"`
	Platforms     StringList        `yaml:"platforms"`
	Requires      Requires          `yaml:"requires"`
	Preconditions []Precondition    `yaml:"preconditions"`
	Desc          string            `yaml:"-"` // set from YAML comments, not from a field
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

// Cmd represents a command in a task. It can be a simple string or a task reference.
type Cmd struct {
	Cmd  string         `yaml:"cmd"`
	Task string         `yaml:"task"`
	Vars map[string]Var `yaml:"vars"`
}

// isSet returns true if the Cmd has a command or task reference.
func (c *Cmd) isSet() bool {
	return c.Cmd != "" || c.Task != ""
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
