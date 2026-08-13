package session

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"
)

const (
	DefaultQueryTimeout      = 10 * time.Second
	DefaultOperationTimeout  = 30 * time.Second
	DefaultSSHConnectTimeout = 8 * time.Second
)

type RuntimeOptions struct {
	QueryTimeout     time.Duration `json:"query_timeout,omitempty"`
	OperationTimeout time.Duration `json:"operation_timeout,omitempty"`
	SSH              SSHOptions    `json:"ssh,omitempty"`
}

type SSHOptions struct {
	ConnectTimeout time.Duration `json:"connect_timeout,omitempty"`
	BatchMode      *bool         `json:"batch_mode,omitempty"`
	ExtraArgs      []string      `json:"extra_args,omitempty"`
}

type Command struct {
	Argv []string          `json:"argv" yaml:"argv"`
	Env  map[string]string `json:"env,omitempty" yaml:"env,omitempty"`
	Cwd  string            `json:"cwd,omitempty" yaml:"cwd,omitempty"`
}

func (o RuntimeOptions) QueryTimeoutOrDefault() time.Duration {
	if o.QueryTimeout > 0 {
		return o.QueryTimeout
	}
	return DefaultQueryTimeout
}

func (o RuntimeOptions) OperationTimeoutOrDefault() time.Duration {
	if o.OperationTimeout > 0 {
		return o.OperationTimeout
	}
	return DefaultOperationTimeout
}

func (o RuntimeOptions) SSHConnectTimeoutOrDefault() time.Duration {
	if o.SSH.ConnectTimeout > 0 {
		return o.SSH.ConnectTimeout
	}
	return DefaultSSHConnectTimeout
}

func (o RuntimeOptions) SSHBatchModeOrDefault() bool {
	if o.SSH.BatchMode != nil {
		return *o.SSH.BatchMode
	}
	return true
}

type Spec struct {
	Name             string   `json:"name" yaml:"name"`
	Backend          string   `json:"backend,omitempty" yaml:"backend,omitempty"`
	Group            string   `json:"group,omitempty" yaml:"group,omitempty"`
	Tags             []string `json:"tags,omitempty" yaml:"tags,omitempty"`
	Summary          string   `json:"summary,omitempty" yaml:"summary,omitempty"`
	DependsOn        []string `json:"depends_on,omitempty" yaml:"depends_on,omitempty"`
	StopDependencies []string `json:"stop_dependencies,omitempty" yaml:"stop_dependencies,omitempty"`
	Host             string   `json:"host,omitempty" yaml:"host,omitempty"`
	TmuxSession      string   `json:"tmux_session,omitempty" yaml:"tmux_session,omitempty"`
	StateDir         string   `json:"state_dir,omitempty" yaml:"state_dir,omitempty"`
	Command          Command  `json:"command,omitempty" yaml:"command,omitempty"`
}

type State string

const (
	StateStopped  State = "stopped"
	StateStarting State = "starting"
	StateRunning  State = "running"
	StateExited   State = "exited"
	StateFailed   State = "failed"
	StateUnknown  State = "unknown"
)

type Status struct {
	Name       string    `json:"name"`
	Group      string    `json:"group,omitempty"`
	Tags       []string  `json:"tags,omitempty"`
	Summary    string    `json:"summary,omitempty"`
	State      State     `json:"state"`
	PID        int       `json:"pid,omitempty"`
	StartedAt  time.Time `json:"started_at,omitempty"`
	ExitedAt   time.Time `json:"exited_at,omitempty"`
	ExitCode   int       `json:"exit_code,omitempty"`
	LogPath    string    `json:"log_path,omitempty"`
	LastError  string    `json:"last_error,omitempty"`
	StartCount int       `json:"start_count,omitempty"`
}

func (s Spec) Validate() error {
	if strings.TrimSpace(s.Name) == "" {
		return fmt.Errorf("session name is required")
	}
	switch s.Kind() {
	case "tmux":
		return s.validateTmux(false)
	case "ssh-tmux":
		return s.validateTmux(true)
	default:
		return fmt.Errorf("session %q has unsupported backend %q", s.Name, s.Kind())
	}
}

func (s Spec) Kind() string {
	return strings.TrimSpace(s.Backend)
}

func (s Spec) IsRemote() bool {
	return s.Kind() == "ssh-tmux"
}

func (s Spec) validateTmux(remote bool) error {
	if remote && strings.TrimSpace(s.Host) == "" {
		return fmt.Errorf("session %q missing host", s.Name)
	}
	if !remote && strings.TrimSpace(s.Host) != "" {
		return fmt.Errorf("session %q host is only valid for ssh-tmux", s.Name)
	}
	if len(s.Command.Argv) == 0 {
		return fmt.Errorf("session %q missing command.argv", s.Name)
	}
	for _, arg := range s.Command.Argv {
		if strings.TrimSpace(arg) == "" {
			return fmt.Errorf("session %q has empty command argv item", s.Name)
		}
	}
	if s.Command.Cwd != "" && !filepath.IsAbs(s.Command.Cwd) {
		return fmt.Errorf("session %q command.cwd must be an absolute path", s.Name)
	}
	if s.StateDir != "" && !filepath.IsAbs(s.StateDir) {
		return fmt.Errorf("session %q state_dir must be an absolute path", s.Name)
	}
	if err := validateEnv(s.Name, s.Command.Env); err != nil {
		return err
	}
	return nil
}

func validateEnv(sessionName string, env map[string]string) error {
	for key := range env {
		if !validEnvName(key) {
			return fmt.Errorf("session %q has invalid env name %q", sessionName, key)
		}
	}
	return nil
}

func validEnvName(value string) bool {
	if value == "" {
		return false
	}
	for i, r := range value {
		if r == '_' || (r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z') || (i > 0 && r >= '0' && r <= '9') {
			continue
		}
		return false
	}
	first := value[0]
	return first == '_' || (first >= 'A' && first <= 'Z') || (first >= 'a' && first <= 'z')
}

func (s Spec) MatchesNames(names []string) bool {
	if len(names) == 0 {
		return true
	}
	for _, name := range names {
		if s.Name == name {
			return true
		}
	}
	return false
}

func (s Spec) MatchesGroup(group string) bool {
	return group == "" || s.Group == group
}

func (s Spec) MatchesTags(tags []string) bool {
	if len(tags) == 0 {
		return true
	}
	have := map[string]bool{}
	for _, tag := range s.Tags {
		have[tag] = true
	}
	for _, tag := range tags {
		if !have[tag] {
			return false
		}
	}
	return true
}

func (s Spec) MatchesHost(host string) bool {
	return host == "" || !s.IsRemote() || s.Host == host
}

func SafeLogName(name string) string {
	name = strings.TrimSpace(name)
	var b strings.Builder
	for _, r := range name {
		if r == '/' || r == '\\' || r == ':' || r == 0 {
			b.WriteByte('_')
			continue
		}
		b.WriteRune(r)
	}
	out := b.String()
	out = strings.TrimLeft(out, "._")
	if out == "" || out == "." || out == ".." {
		return "session"
	}
	return filepath.Base(out)
}
