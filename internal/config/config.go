package config

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"gopkg.in/yaml.v3"

	providerpkg "github.com/bnkrr/rses/internal/provider"
	"github.com/bnkrr/rses/internal/runtime/tmux"
	"github.com/bnkrr/rses/internal/session"
)

const FileName = "rses.yaml"

type Source struct {
	Root string
	Path string
}

type File struct {
	Runtime  map[string]any            `yaml:"runtime"`
	Sessions map[string]map[string]any `yaml:"sessions"`
	Groups   map[string]map[string]any `yaml:"groups"`
}

func Locate(startDir string) (Source, error) {
	var candidates []string

	if path := os.Getenv("RSES_CONFIG"); path != "" {
		candidates = append(candidates, path)
	}
	if startDir != "" {
		candidates = append(candidates, filepath.Join(startDir, FileName))
	}
	if configDir, err := os.UserConfigDir(); err == nil && configDir != "" {
		candidates = append(candidates, filepath.Join(configDir, "rses", FileName))
	}
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		candidates = append(candidates, filepath.Join(home, ".rses", FileName))
	}

	seen := map[string]bool{}
	unique := make([]string, 0, len(candidates))
	for _, candidate := range candidates {
		path := filepath.Clean(candidate)
		if seen[path] {
			continue
		}
		seen[path] = true
		unique = append(unique, path)
		if info, err := os.Stat(path); err == nil && !info.IsDir() {
			return Source{Root: filepath.Dir(path), Path: path}, nil
		}
	}

	return Source{}, fmt.Errorf("rses config not found; searched:\n  %s\n\nCreate ./rses.yaml or %s", strings.Join(unique, "\n  "), filepath.Join("~", ".config", "rses", FileName))
}

func LocateDir(dir string) (Source, error) {
	absDir, err := filepath.Abs(dir)
	if err != nil {
		return Source{}, fmt.Errorf("resolve config directory %s: %w", dir, err)
	}
	path := filepath.Join(absDir, FileName)
	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return Source{}, fmt.Errorf("rses config not found at %s", path)
		}
		return Source{}, fmt.Errorf("inspect %s: %w", path, err)
	}
	if info.IsDir() {
		return Source{}, fmt.Errorf("rses config path %s is a directory", path)
	}
	return Source{Root: absDir, Path: path}, nil
}

func LoadProject(projectDir string) ([]session.Spec, error) {
	path := filepath.Join(projectDir, FileName)
	return LoadFile(projectDir, path)
}

func LoadFile(root, path string) ([]session.Spec, error) {
	specs, _, err := LoadFileWithRuntime(root, path)
	return specs, err
}

func LoadProjectWithRuntime(projectDir string) ([]session.Spec, session.RuntimeOptions, error) {
	path := filepath.Join(projectDir, FileName)
	return LoadFileWithRuntime(projectDir, path)
}

func LoadFileWithRuntime(root, path string) ([]session.Spec, session.RuntimeOptions, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, session.RuntimeOptions{}, fmt.Errorf("read %s: %w", path, err)
	}

	var cfg File
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, session.RuntimeOptions{}, fmt.Errorf("parse %s: %w", path, err)
	}
	sessions, err := flattenSessions(cfg)
	if err != nil {
		return nil, session.RuntimeOptions{}, fmt.Errorf("%s invalid: %w", path, err)
	}
	runtime, err := parseRuntime(cfg.Runtime)
	if err != nil {
		return nil, session.RuntimeOptions{}, fmt.Errorf("%s runtime invalid: %w", path, err)
	}

	names := make([]string, 0, len(sessions))
	for name := range sessions {
		names = append(names, name)
	}
	sort.Strings(names)

	specs := make([]session.Spec, 0, len(names))
	for _, name := range names {
		raw := sessions[name]
		spec, err := expandSession(root, name, raw)
		if err != nil {
			return nil, session.RuntimeOptions{}, err
		}
		if err := spec.Validate(); err != nil {
			return nil, session.RuntimeOptions{}, err
		}
		specs = append(specs, spec)
	}
	if err := validateUniqueWindows(specs); err != nil {
		return nil, session.RuntimeOptions{}, err
	}
	return specs, runtime, nil
}

func flattenSessions(cfg File) (map[string]map[string]any, error) {
	out := make(map[string]map[string]any, len(cfg.Sessions))
	sources := make(map[string]string, len(cfg.Sessions))
	for name, raw := range cfg.Sessions {
		out[name] = raw
		sources[name] = fmt.Sprintf("sessions.%s", name)
	}

	groupNames := make([]string, 0, len(cfg.Groups))
	for name := range cfg.Groups {
		groupNames = append(groupNames, name)
	}
	sort.Strings(groupNames)
	for _, groupName := range groupNames {
		if strings.TrimSpace(groupName) == "" {
			return nil, fmt.Errorf("group name is required")
		}
		groupRaw := cfg.Groups[groupName]
		for key := range groupRaw {
			if key != "sessions" {
				return nil, fmt.Errorf("group %q has unknown field %q", groupName, key)
			}
		}
		groupSessions, err := nestedSessionsField(groupRaw, "sessions")
		if err != nil {
			return nil, fmt.Errorf("group %q sessions invalid: %w", groupName, err)
		}
		if len(groupSessions) == 0 {
			return nil, fmt.Errorf("group %q has no sessions", groupName)
		}

		names := make([]string, 0, len(groupSessions))
		for name := range groupSessions {
			names = append(names, name)
		}
		sort.Strings(names)
		for _, name := range names {
			raw := groupSessions[name]
			source := fmt.Sprintf("groups.%s.sessions.%s", groupName, name)
			if previous, ok := sources[name]; ok {
				return nil, fmt.Errorf("session %q is defined more than once: %s and %s", name, previous, source)
			}
			if _, ok := raw["group"]; ok {
				return nil, fmt.Errorf("session %q under group %q must not set group explicitly", name, groupName)
			}
			expanded := make(map[string]any, len(raw)+1)
			for key, value := range raw {
				expanded[key] = value
			}
			expanded["group"] = groupName
			out[name] = expanded
			sources[name] = source
		}
	}
	return out, nil
}

func nestedSessionsField(raw map[string]any, key string) (map[string]map[string]any, error) {
	value, ok := raw[key]
	if !ok {
		return nil, fmt.Errorf("missing field")
	}
	if value == nil {
		return nil, fmt.Errorf("must be a map")
	}
	data, err := yaml.Marshal(value)
	if err != nil {
		return nil, err
	}
	var out map[string]map[string]any
	if err := yaml.Unmarshal(data, &out); err != nil {
		return nil, err
	}
	if out == nil {
		return nil, fmt.Errorf("must be a map")
	}
	return out, nil
}

func validateUniqueWindows(specs []session.Spec) error {
	seen := map[string]string{}
	for _, spec := range specs {
		window := tmux.SafeName(spec.Name)
		if previous, ok := seen[window]; ok {
			return fmt.Errorf("sessions %q and %q both resolve to tmux window %q; rename one session", previous, spec.Name, window)
		}
		seen[window] = spec.Name
	}
	return nil
}

func parseRuntime(raw map[string]any) (session.RuntimeOptions, error) {
	var out session.RuntimeOptions
	if raw == nil {
		return out, nil
	}
	for key := range raw {
		switch key {
		case "query_timeout", "operation_timeout", "ssh":
		default:
			return out, fmt.Errorf("unknown field %q", key)
		}
	}
	var err error
	if out.QueryTimeout, err = durationField(raw, "query_timeout"); err != nil {
		return out, err
	}
	if out.OperationTimeout, err = durationField(raw, "operation_timeout"); err != nil {
		return out, err
	}
	ssh, ok, err := mapField(raw, "ssh")
	if err != nil {
		return out, fmt.Errorf("ssh field invalid: %w", err)
	}
	if ok {
		for key := range ssh {
			switch key {
			case "connect_timeout", "batch_mode", "extra_args":
			default:
				return out, fmt.Errorf("ssh unknown field %q", key)
			}
		}
		if out.SSH.ConnectTimeout, err = durationField(ssh, "connect_timeout"); err != nil {
			return out, fmt.Errorf("ssh.connect_timeout invalid: %w", err)
		}
		if batchMode, ok, err := boolField(ssh, "batch_mode"); err != nil {
			return out, fmt.Errorf("ssh.batch_mode invalid: %w", err)
		} else if ok {
			out.SSH.BatchMode = &batchMode
		}
		if out.SSH.ExtraArgs, err = stringSliceField(ssh, "extra_args"); err != nil {
			return out, fmt.Errorf("ssh.extra_args invalid: %w", err)
		}
	}
	return out, nil
}

func durationField(raw map[string]any, key string) (time.Duration, error) {
	v, ok := raw[key]
	if !ok || v == nil {
		return 0, nil
	}
	switch value := v.(type) {
	case string:
		d, err := time.ParseDuration(value)
		if err != nil {
			return 0, err
		}
		return d, nil
	default:
		return 0, fmt.Errorf("%s must be a duration string like 10s", key)
	}
}

func expandSession(projectDir, name string, raw map[string]any) (session.Spec, error) {
	if err := rejectLegacyFields(name, raw); err != nil {
		return session.Spec{}, err
	}
	if provider, ok := stringField(raw, "provider"); ok && provider != "" {
		return expandProvider(projectDir, name, provider, raw)
	}
	return parseInline(name, raw)
}

func rejectLegacyFields(name string, raw map[string]any) error {
	for _, key := range []string{"type", "start", "stop", "status", "logs"} {
		if _, ok := raw[key]; ok {
			return fmt.Errorf("session %q uses unsupported legacy field %q; use backend and command.argv", name, key)
		}
	}
	return nil
}

func expandProvider(projectDir, name, providerName string, raw map[string]any) (session.Spec, error) {
	if backend, ok := stringField(raw, "backend"); !ok || strings.TrimSpace(backend) == "" {
		return session.Spec{}, fmt.Errorf("session %q provider %q requires explicit backend", name, providerName)
	}
	if _, ok := raw["command"]; ok {
		return session.Spec{}, fmt.Errorf("session %q cannot set both provider and command", name)
	}
	with, ok, err := mapField(raw, "with")
	if err != nil {
		return session.Spec{}, fmt.Errorf("session %q provider %q with field invalid: %w", name, providerName, err)
	}
	if !ok {
		return session.Spec{}, fmt.Errorf("session %q provider %q missing with; provider inputs must be under with", name, providerName)
	}

	command, err := providerpkg.GenerateCommand(projectDir, name, providerName, with)
	if err != nil {
		return session.Spec{}, err
	}
	spec, err := parseInline(name, raw)
	if err != nil {
		return session.Spec{}, err
	}
	spec.Command = command
	return spec, nil
}

func parseInline(name string, raw map[string]any) (session.Spec, error) {
	data, err := yaml.Marshal(raw)
	if err != nil {
		return session.Spec{}, err
	}
	var spec session.Spec
	if err := yaml.Unmarshal(data, &spec); err != nil {
		return session.Spec{}, fmt.Errorf("session %q inline spec invalid: %w", name, err)
	}
	if spec.Name == "" {
		spec.Name = name
	}
	return spec, nil
}

func stringField(raw map[string]any, key string) (string, bool) {
	v, ok := raw[key]
	if !ok || v == nil {
		return "", false
	}
	s, ok := v.(string)
	return s, ok
}

func boolField(raw map[string]any, key string) (bool, bool, error) {
	v, ok := raw[key]
	if !ok || v == nil {
		return false, false, nil
	}
	b, ok := v.(bool)
	if !ok {
		return false, true, fmt.Errorf("must be a boolean")
	}
	return b, true, nil
}

func stringSliceField(raw map[string]any, key string) ([]string, error) {
	v, ok := raw[key]
	if !ok || v == nil {
		return nil, nil
	}
	values, ok := v.([]any)
	if !ok {
		data, err := yaml.Marshal(v)
		if err != nil {
			return nil, err
		}
		if err := yaml.Unmarshal(data, &values); err != nil {
			return nil, err
		}
	}
	out := make([]string, 0, len(values))
	for i, value := range values {
		s, ok := value.(string)
		if !ok || strings.TrimSpace(s) == "" {
			return nil, fmt.Errorf("item %d must be a non-empty string", i)
		}
		out = append(out, s)
	}
	return out, nil
}

func mapField(raw map[string]any, key string) (map[string]any, bool, error) {
	v, ok := raw[key]
	if !ok {
		return nil, false, nil
	}
	if v == nil {
		return nil, true, fmt.Errorf("must be a map")
	}
	if m, ok := v.(map[string]any); ok {
		return m, true, nil
	}
	data, err := yaml.Marshal(v)
	if err != nil {
		return nil, true, err
	}
	var out map[string]any
	if err := yaml.Unmarshal(data, &out); err != nil {
		return nil, true, err
	}
	if out == nil {
		return nil, true, fmt.Errorf("must be a map")
	}
	return out, true, nil
}
