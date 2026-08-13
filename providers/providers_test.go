package providers_test

import (
	"bytes"
	"encoding/json"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	providerpkg "github.com/bnkrr/rses/internal/provider"
	"github.com/bnkrr/rses/internal/session"
)

func TestCloudflaredProviderOutputsCommandOnly(t *testing.T) {
	in := []byte(`{
		"name": "example-codex",
		"provider": "cloudflared-tcp",
		"root": "/tmp/rses",
		"with": {
			"hostname": "tunnel-a.example.com",
			"target": "tcp://service.example.internal:4096",
			"listen": "127.0.0.1:35003"
		}
	}`)
	out, err := runProviderRaw("cloudflared-tcp", in)
	if err != nil {
		t.Fatalf("provider failed: %v\n%s", err, out)
	}
	var got map[string]json.RawMessage
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatalf("decode provider output: %v\n%s", err, out)
	}
	if len(got) != 1 || got["command"] == nil {
		t.Fatalf("provider output = %s, want command only", out)
	}
	var output struct {
		Command session.Command `json:"command"`
	}
	if err := json.Unmarshal(out, &output); err != nil {
		t.Fatalf("decode command: %v", err)
	}
	want := []string{
		"cloudflared", "access", "tcp",
		"--hostname", "tunnel-a.example.com",
		"--destination", "tcp://service.example.internal:4096",
		"--url", "127.0.0.1:35003",
	}
	if strings.Join(output.Command.Argv, "\x00") != strings.Join(want, "\x00") {
		t.Fatalf("argv = %+v, want %+v", output.Command.Argv, want)
	}
}

func TestSSHTunnelProvider(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want []string
	}{
		{
			name: "local",
			in:   `{"name":"codex","provider":"ssh-tunnel","root":"/tmp/rses","with":{"host":"ssh-edge","mode":"local","listen":"127.0.0.1:35004","target":"127.0.0.1:4097","ssh_args":["-p","2222"]}}`,
			want: []string{"ssh -N", "ExitOnForwardFailure=yes", "-L 127.0.0.1:35004:127.0.0.1:4097", "-p 2222", "ssh-edge", "delay=2", "ran=$((now - started))", "2>&1"},
		},
		{
			name: "SSH args can override defaults",
			in:   `{"name":"codex","provider":"ssh-tunnel","root":"/tmp/rses","with":{"host":"ssh-edge","mode":"local","listen":"127.0.0.1:35004","target":"127.0.0.1:4097","ssh_args":["-o","ExitOnForwardFailure=no"]}}`,
			want: []string{"ssh -N -o ExitOnForwardFailure=no -o ExitOnForwardFailure=yes", "-L 127.0.0.1:35004:127.0.0.1:4097"},
		},
		{
			name: "remote",
			in:   `{"name":"dev","provider":"ssh-tunnel","root":"/tmp/rses","with":{"host":"vps","mode":"remote","listen":"127.0.0.1:18080","target":"127.0.0.1:3000"}}`,
			want: []string{"-R 127.0.0.1:18080:127.0.0.1:3000", "vps"},
		},
		{
			name: "local socket",
			in:   `{"name":"codex","provider":"ssh-tunnel","root":"/tmp/rses","with":{"host":"ssh-edge","mode":"local-socket","listen":"127.0.0.1:35003","target":"/home/remote/.local/state/rses/codex-app-server.sock"}}`,
			want: []string{"-L 127.0.0.1:35003:/home/remote/.local/state/rses/codex-app-server.sock", "ssh-edge"},
		},
		{
			name: "remote socket",
			in:   `{"name":"reverse","provider":"ssh-tunnel","root":"/tmp/rses","with":{"host":"vps","mode":"remote-socket","listen":"/tmp/app.sock","target":"127.0.0.1:3000"}}`,
			want: []string{"-R /tmp/app.sock:127.0.0.1:3000", "vps"},
		},
		{
			name: "SOCKS",
			in:   `{"name":"socks","provider":"ssh-tunnel","root":"/tmp/rses","with":{"host":"vps","mode":"socks","listen":"127.0.0.1:1080"}}`,
			want: []string{"-D 127.0.0.1:1080", "vps"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			output := runProvider(t, "ssh-tunnel", []byte(tc.in))
			if len(output.Command.Argv) != 3 || output.Command.Argv[0] != "sh" || output.Command.Argv[1] != "-c" {
				t.Fatalf("argv = %+v, want sh -c script", output.Command.Argv)
			}
			for _, want := range tc.want {
				if !strings.Contains(output.Command.Argv[2], want) {
					t.Fatalf("script missing %q:\n%s", want, output.Command.Argv[2])
				}
			}
		})
	}
}

func TestSSHTunnelProviderRejectsMissingFields(t *testing.T) {
	for _, in := range []string{
		`{"name":"bad","provider":"ssh-tunnel","root":"/tmp/rses","with":{"mode":"local","listen":"127.0.0.1:1","target":"127.0.0.1:2"}}`,
		`{"name":"bad","provider":"ssh-tunnel","root":"/tmp/rses","with":{"host":"h","mode":"local","listen":"127.0.0.1:1"}}`,
		`{"name":"bad","provider":"ssh-tunnel","root":"/tmp/rses","with":{"host":"h","mode":"local-socket","listen":"127.0.0.1:1","target":"relative.sock"}}`,
		`{"name":"bad","provider":"ssh-tunnel","root":"/tmp/rses","with":{"host":"h","mode":"remote-socket","listen":"relative.sock","target":"127.0.0.1:1"}}`,
		`{"name":"bad","provider":"ssh-tunnel","root":"/tmp/rses","with":{"host":"h","mode":"wat","listen":"127.0.0.1:1"}}`,
	} {
		if out, err := runProviderRaw("ssh-tunnel", []byte(in)); err == nil {
			t.Fatalf("provider succeeded unexpectedly: %s", out)
		}
	}
}

func runProvider(t *testing.T, name string, in []byte) struct {
	Command session.Command `json:"command"`
} {
	t.Helper()
	out, err := runProviderRaw(name, in)
	if err != nil {
		t.Fatalf("provider failed: %v\n%s", err, out)
	}
	var output struct {
		Command session.Command `json:"command"`
	}
	if err := json.Unmarshal(out, &output); err != nil {
		t.Fatalf("decode provider output: %v\n%s", err, out)
	}
	return output
}

func runProviderRaw(name string, in []byte) ([]byte, error) {
	dir := filepath.Join(".", name)
	manifest, err := providerpkg.LoadCommand(dir)
	if err != nil {
		return nil, err
	}
	cmd := exec.Command(manifest[0], manifest[1:]...)
	cmd.Dir = dir
	cmd.Stdin = bytes.NewReader(in)
	return cmd.CombinedOutput()
}
