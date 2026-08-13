//go:build integration

package integration_test

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestCLIWorkflow(t *testing.T) {
	requireCommands(t, "go", "python3", "tmux")
	h := newHarness(t)

	out := h.runFrom(h.root, "init", h.project)
	requireContains(t, out, "initialized rses configuration at "+h.project)
	for _, path := range []string{
		filepath.Join(h.project, "rses.yaml"),
		filepath.Join(h.project, "providers", "ssh-tunnel", "provider.json"),
		filepath.Join(h.project, "providers", "cloudflared-tcp", "provider.json"),
	} {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("initialized file %s: %v", path, err)
		}
	}
	h.run("-C", h.project, "list")
	h.runErrorContains("refusing to overwrite", "init", h.project)

	copyFile(t, filepath.Join(h.root, "integration", "testdata", "rses.yaml"), filepath.Join(h.project, "rses.yaml"))
	list := h.run("-C", h.project, "list")
	for _, want := range []string{"local-clock", "fake-ssh-tunnel", "demo"} {
		requireContains(t, list, want)
	}

	if got := strings.TrimSpace(h.run("version")); got != "rses dev" {
		t.Fatalf("version output = %q, want %q", got, "rses dev")
	}
	if got := strings.TrimSpace(h.run("--version")); got != "rses dev" {
		t.Fatalf("--version output = %q, want %q", got, "rses dev")
	}

	explain := h.run("explain", "local-clock")
	h.tmuxScope = fieldValue(t, explain, "Tmux scope:")
	h.run("start", "--no-watch", "local-clock")
	eventually(t, 10*time.Second, "local session to run", func() (bool, string) {
		out, err := h.tryRun("status", "local-clock")
		return err == nil && strings.Contains(out, "running"), commandResult(out, err)
	})
	eventually(t, 5*time.Second, "local logs", func() (bool, string) {
		out, err := h.tryRun("logs", "local-clock")
		return err == nil && strings.TrimSpace(out) != "", commandResult(out, err)
	})

	meta := h.run("daemon", "meta")
	for _, want := range []string{"protocol_version:", "build_id:", "pid:"} {
		requireContains(t, meta, want)
	}
	h.run("daemon", "shutdown")
	requireContains(t, h.run("status", "local-clock"), "running")

	h.run("start", "--no-watch", "fake-ssh-tunnel")
	eventually(t, 12*time.Second, "SSH tunnel to reconnect", func() (bool, string) {
		data, err := os.ReadFile(h.countFile)
		if err != nil {
			return false, err.Error()
		}
		count, err := strconv.Atoi(strings.TrimSpace(string(data)))
		return err == nil && count >= 2, strings.TrimSpace(string(data))
	})
	sshLog, err := os.ReadFile(h.sshLogFile)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"BatchMode=yes", "127.0.0.1:45999:127.0.0.1:46000", "fake-host"} {
		requireContains(t, string(sshLog), want)
	}
	requireContains(t, h.run("logs", "fake-ssh-tunnel"), "starting ssh tunnel:")

	stop := h.run("stop", "fake-ssh-tunnel")
	progress := strings.Index(stop, "stopping fake-ssh-tunnel...")
	result := strings.Index(stop, "fake-ssh-tunnel: stopped")
	if progress < 0 || result < 0 || progress >= result {
		t.Fatalf("stop output did not stream progress before result:\n%s", stop)
	}

	if err := os.Rename(filepath.Join(h.project, "rses.yaml"), filepath.Join(h.project, "rses.yaml.removed")); err != nil {
		t.Fatal(err)
	}
	h.runErrorContains("rses config not found", "-C", h.project, "list")
	requireContains(t, h.run("status", "local-clock"), "running")
	h.run("logs", "local-clock")
	h.run("stop", "local-clock")
	requireContains(t, h.run("status", "local-clock"), "stopped")
}
