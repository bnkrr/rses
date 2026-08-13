package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestLoadInlineSession(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, FileName), `
sessions:
  demo:
    backend: tmux
    group: dev
    tags: [one]
    summary: test session
    command:
      argv: ["sleep", "10"]
`)
	specs, err := LoadProject(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(specs) != 1 {
		t.Fatalf("len(specs) = %d", len(specs))
	}
	if specs[0].Name != "demo" || specs[0].Group != "dev" || specs[0].Command.Argv[0] != "sleep" {
		t.Fatalf("unexpected spec: %+v", specs[0])
	}
}

func TestLoadEmptyConfig(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, FileName), "groups: {}\nsessions: {}\n")
	specs, err := LoadProject(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(specs) != 0 {
		t.Fatalf("specs = %+v, want empty config", specs)
	}
}

func TestLocateDirRequiresConfigInExactDirectory(t *testing.T) {
	dir := t.TempDir()
	child := filepath.Join(dir, "child")
	if err := os.Mkdir(child, 0o755); err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(dir, FileName), "sessions: {}\n")
	if _, err := LocateDir(child); err == nil || !strings.Contains(err.Error(), filepath.Join(child, FileName)) {
		t.Fatalf("error = %v, want exact child config path", err)
	}
	source, err := LocateDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if source.Root != dir || source.Path != filepath.Join(dir, FileName) {
		t.Fatalf("source = %+v", source)
	}
}

func TestLoadGroupedSessions(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, FileName), `
sessions:
  standalone:
    backend: tmux
    group: dev
    command:
      argv: ["sleep", "10"]
groups:
  dev:
    sessions:
      remote-api:
        backend: ssh-tmux
        host: dev-box
        command:
          argv: ["api-server"]
      api-tunnel:
        backend: tmux
        depends_on: [remote-api]
        command:
          argv: ["ssh", "dev-box"]
`)
	specs, err := LoadProject(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(specs) != 3 {
		t.Fatalf("len(specs) = %d, want 3", len(specs))
	}
	for _, spec := range specs {
		if spec.Group != "dev" {
			t.Fatalf("session %q group = %q, want dev", spec.Name, spec.Group)
		}
	}
	if specs[0].Name != "api-tunnel" || specs[1].Name != "remote-api" || specs[2].Name != "standalone" {
		t.Fatalf("session order = %q, %q, %q", specs[0].Name, specs[1].Name, specs[2].Name)
	}
}

func TestGroupedSessionRejectsExplicitGroup(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, FileName), `
groups:
  dev:
    sessions:
      api:
        backend: tmux
        group: dev
        command:
          argv: ["sleep", "10"]
`)
	_, err := LoadProject(dir)
	if err == nil || !strings.Contains(err.Error(), `session "api" under group "dev" must not set group explicitly`) {
		t.Fatalf("error = %v, want explicit group rejection", err)
	}
}

func TestLoadRejectsDuplicateSessionAcrossSections(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, FileName), `
sessions:
  api:
    backend: tmux
    command:
      argv: ["sleep", "10"]
groups:
  dev:
    sessions:
      api:
        backend: tmux
        command:
          argv: ["sleep", "20"]
`)
	_, err := LoadProject(dir)
	for _, want := range []string{`session "api" is defined more than once`, "sessions.api", "groups.dev.sessions.api"} {
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Fatalf("error = %v, want %q", err, want)
		}
	}
}

func TestLoadRejectsInvalidGroups(t *testing.T) {
	cases := []struct {
		name string
		body string
		want string
	}{
		{
			name: "missing sessions",
			body: "groups:\n  dev: {}\n",
			want: `group "dev" sessions invalid: missing field`,
		},
		{
			name: "empty sessions",
			body: "groups:\n  dev:\n    sessions: {}\n",
			want: `group "dev" has no sessions`,
		},
		{
			name: "unknown field",
			body: "groups:\n  dev:\n    defaults: {}\n    sessions: {}\n",
			want: `group "dev" has unknown field "defaults"`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			write(t, filepath.Join(dir, FileName), tc.body)
			_, err := LoadProject(dir)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error = %v, want %q", err, tc.want)
			}
		})
	}
}

func TestLoadRuntimeOptions(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, FileName), `
runtime:
  query_timeout: 7s
  operation_timeout: 45s
  ssh:
    connect_timeout: 3s
    batch_mode: false
    extra_args: ["-F", "none"]
sessions:
  demo:
    backend: tmux
    command:
      argv: ["sleep", "10"]
`)
	_, runtime, err := LoadProjectWithRuntime(dir)
	if err != nil {
		t.Fatal(err)
	}
	if runtime.QueryTimeout != 7*time.Second || runtime.OperationTimeout != 45*time.Second {
		t.Fatalf("runtime timeouts = %+v", runtime)
	}
	if runtime.SSH.ConnectTimeout != 3*time.Second || runtime.SSH.BatchMode == nil || *runtime.SSH.BatchMode {
		t.Fatalf("ssh runtime = %+v", runtime.SSH)
	}
	if strings.Join(runtime.SSH.ExtraArgs, " ") != "-F none" {
		t.Fatalf("extra args = %+v", runtime.SSH.ExtraArgs)
	}
}

func TestLoadRejectsDuplicateTmuxWindowNames(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, FileName), `
sessions:
  api/v1:
    backend: tmux
    command:
      argv: ["sleep", "10"]
  api_v1:
    backend: tmux
    command:
      argv: ["sleep", "10"]
`)
	_, err := LoadProject(dir)
	if err == nil || !strings.Contains(err.Error(), `both resolve to tmux window "api_v1"`) {
		t.Fatalf("error = %v, want duplicate tmux window", err)
	}
}

func TestProviderSession(t *testing.T) {
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "providers"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeTestProvider(t, dir, "echo-provider", `#!/bin/sh
cat <<'JSON'
{"command":{"argv":["sleep","10"]}}
JSON
`)
	write(t, filepath.Join(dir, FileName), `
sessions:
  demo:
    backend: tmux
    provider: echo-provider
    group: dev
    tags: [one]
    summary: from config
    depends_on: [dep]
    stop_dependencies: [dep]
    with:
      value: ok
`)
	specs, err := LoadProject(dir)
	if err != nil {
		t.Fatal(err)
	}
	if specs[0].Summary != "from config" || specs[0].Group != "dev" || len(specs[0].DependsOn) != 1 {
		t.Fatalf("unexpected provider output: %+v", specs[0])
	}
	if specs[0].Kind() != "tmux" || specs[0].Command.Argv[0] != "sleep" {
		t.Fatalf("unexpected provider command: %+v", specs[0])
	}
}

func TestProviderStderrDoesNotCorruptOutput(t *testing.T) {
	dir := t.TempDir()
	writeTestProvider(t, dir, "diagnostic-provider", `#!/bin/sh
echo 'provider diagnostic' >&2
echo '{"command":{"argv":["sleep","10"]}}'
`)
	write(t, filepath.Join(dir, FileName), `
sessions:
  demo:
    backend: tmux
    provider: diagnostic-provider
    with: {}
`)

	specs, err := LoadProject(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(specs) != 1 || strings.Join(specs[0].Command.Argv, " ") != "sleep 10" {
		t.Fatalf("unexpected provider command: %+v", specs)
	}
}

func TestProviderRejectsInvalidSessionShapes(t *testing.T) {
	cases := []struct {
		name string
		body string
		want string
	}{
		{
			name: "provider and command",
			body: `
sessions:
  demo:
    backend: tmux
    provider: echo-provider
    command:
      argv: ["sleep", "10"]
    with: {}
`,
			want: "cannot set both provider and command",
		},
		{
			name: "missing backend",
			body: `
sessions:
  demo:
    provider: echo-provider
    with: {}
`,
			want: "requires explicit backend",
		},
		{
			name: "missing with",
			body: `
sessions:
  demo:
    backend: tmux
    provider: echo-provider
`,
			want: "missing with",
		},
		{
			name: "old flat provider fields",
			body: `
sessions:
  demo:
    backend: tmux
    provider: echo-provider
    hostname: tunnel.example.com
    listen: 127.0.0.1:35003
    target: 127.0.0.1:4097
`,
			want: "missing with",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			if err := os.Mkdir(filepath.Join(dir, "providers"), 0o755); err != nil {
				t.Fatal(err)
			}
			writeTestProvider(t, dir, "echo-provider", "#!/bin/sh\necho '{\"command\":{\"argv\":[\"sleep\",\"10\"]}}'\n")
			write(t, filepath.Join(dir, FileName), tc.body)
			_, err := LoadProject(dir)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error = %v, want %q", err, tc.want)
			}
		})
	}
}

func TestRejectsLegacySessionFields(t *testing.T) {
	cases := []string{"type", "start", "stop", "status", "logs"}
	for _, field := range cases {
		t.Run(field, func(t *testing.T) {
			dir := t.TempDir()
			body := `
sessions:
  demo:
    backend: tmux
    command:
      argv: ["sleep", "10"]
`
			body += "    " + field + ": legacy\n"
			write(t, filepath.Join(dir, FileName), body)
			_, err := LoadProject(dir)
			if err == nil || !strings.Contains(err.Error(), "unsupported legacy field") {
				t.Fatalf("error = %v, want legacy field rejection", err)
			}
		})
	}
}

func writeTestProvider(t *testing.T, projectDir, name, script string) {
	t.Helper()
	providerDir := filepath.Join(projectDir, "providers", name)
	if err := os.MkdirAll(providerDir, 0o755); err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(providerDir, "provider.json"), `{"command":["sh","provider.sh"]}`)
	write(t, filepath.Join(providerDir, "provider.sh"), script)
}

func TestLocateUsesXDGConfig(t *testing.T) {
	startDir := t.TempDir()
	xdg := t.TempDir()
	t.Setenv("RSES_CONFIG", "")
	t.Setenv("XDG_CONFIG_HOME", xdg)
	t.Setenv("HOME", t.TempDir())

	path := filepath.Join(xdg, "rses", FileName)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	write(t, path, "sessions: {}\n")

	source, err := Locate(startDir)
	if err != nil {
		t.Fatal(err)
	}
	if source.Path != path || source.Root != filepath.Dir(path) {
		t.Fatalf("source = %+v, want path %s", source, path)
	}
}

func TestLocateUsesLegacyHomeConfig(t *testing.T) {
	startDir := t.TempDir()
	home := t.TempDir()
	t.Setenv("RSES_CONFIG", "")
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(t.TempDir(), "missing"))
	t.Setenv("HOME", home)

	path := filepath.Join(home, ".rses", FileName)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	write(t, path, "sessions: {}\n")

	source, err := Locate(startDir)
	if err != nil {
		t.Fatal(err)
	}
	if source.Path != path || source.Root != filepath.Dir(path) {
		t.Fatalf("source = %+v, want path %s", source, path)
	}
}

func TestLocateReportsSearchedPaths(t *testing.T) {
	startDir := t.TempDir()
	xdg := t.TempDir()
	home := t.TempDir()
	t.Setenv("RSES_CONFIG", "")
	t.Setenv("XDG_CONFIG_HOME", xdg)
	t.Setenv("HOME", home)

	_, err := Locate(startDir)
	if err == nil {
		t.Fatal("expected error")
	}
	message := err.Error()
	for _, want := range []string{
		filepath.Join(startDir, FileName),
		filepath.Join(xdg, "rses", FileName),
		filepath.Join(home, ".rses", FileName),
	} {
		if !strings.Contains(message, want) {
			t.Fatalf("error %q does not contain %q", message, want)
		}
	}
}

func write(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}
