//go:build integration

package integration_test

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"
)

const commandTimeout = 60 * time.Second

type harness struct {
	t          *testing.T
	root       string
	project    string
	bin        string
	env        []string
	tmuxScope  string
	countFile  string
	sshLogFile string
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	root := findRepoRoot(t)
	work := t.TempDir()
	bin := filepath.Join(work, "rses")
	fakeBin := filepath.Join(work, "bin")
	if err := os.Mkdir(fakeBin, 0o755); err != nil {
		t.Fatal(err)
	}

	runBuild(t, root, "go", "build", "-buildvcs=false", "-o", bin, "./cmd/rses")
	runBuild(t, root, "go", "build", "-buildvcs=false", "-o", filepath.Join(fakeBin, "ssh"), "./integration/testdata/fake-ssh")

	overrides := map[string]string{
		"RSES_STATE_DIR":   filepath.Join(work, "state"),
		"RSES_RUNTIME_DIR": filepath.Join(work, "runtime"),
		"HOME":             filepath.Join(work, "home"),
		"XDG_CONFIG_HOME":  filepath.Join(work, "xdg"),
		"FAKE_SSH_COUNT":   filepath.Join(work, "fake-ssh.count"),
		"FAKE_SSH_LOG":     filepath.Join(work, "fake-ssh.log"),
		"PATH":             fakeBin + string(os.PathListSeparator) + os.Getenv("PATH"),
	}
	for _, key := range []string{"RSES_STATE_DIR", "RSES_RUNTIME_DIR", "HOME", "XDG_CONFIG_HOME"} {
		if err := os.MkdirAll(overrides[key], 0o755); err != nil {
			t.Fatal(err)
		}
	}

	h := &harness{
		t:          t,
		root:       root,
		project:    filepath.Join(work, "project"),
		bin:        bin,
		env:        environment(overrides),
		countFile:  overrides["FAKE_SSH_COUNT"],
		sshLogFile: overrides["FAKE_SSH_LOG"],
	}
	t.Cleanup(func() {
		h.runCleanup("stop", "fake-ssh-tunnel")
		h.runCleanup("stop", "local-clock")
		h.runCleanup("daemon", "shutdown")
		if h.tmuxScope != "" {
			cmd := exec.Command("tmux", "kill-session", "-t", h.tmuxScope)
			cmd.Env = h.env
			_ = cmd.Run()
		}
	})
	return h
}

func findRepoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("could not find repository root containing go.mod")
		}
		dir = parent
	}
}

func (h *harness) run(args ...string) string {
	h.t.Helper()
	out, err := h.tryRun(args...)
	if err != nil {
		h.t.Fatalf("rses %s failed: %v\n%s", strings.Join(args, " "), err, out)
	}
	return out
}

func (h *harness) runFrom(dir string, args ...string) string {
	h.t.Helper()
	out, err := run(h.env, dir, h.bin, args...)
	if err != nil {
		h.t.Fatalf("rses %s failed: %v\n%s", strings.Join(args, " "), err, out)
	}
	return out
}

func (h *harness) tryRun(args ...string) (string, error) {
	dir := h.project
	if _, err := os.Stat(dir); err != nil {
		dir = h.root
	}
	return run(h.env, dir, h.bin, args...)
}

func (h *harness) runErrorContains(want string, args ...string) {
	h.t.Helper()
	out, err := h.tryRun(args...)
	if err == nil {
		h.t.Fatalf("rses %s succeeded, want error containing %q\n%s", strings.Join(args, " "), want, out)
	}
	requireContains(h.t, out, want)
}

func (h *harness) runCleanup(args ...string) {
	_, _ = h.tryRun(args...)
}

func run(env []string, dir, name string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), commandTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = dir
	cmd.Env = env
	out, err := cmd.CombinedOutput()
	if ctx.Err() != nil {
		return string(out), fmt.Errorf("command timed out: %w", ctx.Err())
	}
	return string(out), err
}

func runBuild(t *testing.T, dir, name string, args ...string) {
	t.Helper()
	out, err := run(os.Environ(), dir, name, args...)
	if err != nil {
		t.Fatalf("%s %s failed: %v\n%s", name, strings.Join(args, " "), err, out)
	}
}

func requireCommands(t *testing.T, names ...string) {
	t.Helper()
	for _, name := range names {
		if _, err := exec.LookPath(name); err != nil {
			t.Fatalf("integration tests require %s: %v", name, err)
		}
	}
}

func environment(overrides map[string]string) []string {
	env := make([]string, 0, len(os.Environ())+len(overrides))
	for _, item := range os.Environ() {
		key, _, _ := strings.Cut(item, "=")
		if _, replaced := overrides[key]; !replaced {
			env = append(env, item)
		}
	}
	keys := make([]string, 0, len(overrides))
	for key := range overrides {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		env = append(env, key+"="+overrides[key])
	}
	return env
}

func eventually(t *testing.T, timeout time.Duration, description string, check func() (bool, string)) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	last := ""
	for time.Now().Before(deadline) {
		if ok, detail := check(); ok {
			return
		} else {
			last = detail
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s; last result: %s", description, last)
}

func commandResult(out string, err error) string {
	if err == nil {
		return out
	}
	return fmt.Sprintf("%v: %s", err, out)
}

func fieldValue(t *testing.T, output, prefix string) string {
	t.Helper()
	for _, line := range strings.Split(output, "\n") {
		if strings.HasPrefix(line, prefix) {
			value := strings.TrimSpace(strings.TrimPrefix(line, prefix))
			if value != "" {
				return value
			}
		}
	}
	t.Fatalf("output missing %q field:\n%s", prefix, output)
	return ""
}

func requireContains(t *testing.T, got, want string) {
	t.Helper()
	if !strings.Contains(got, want) {
		t.Fatalf("output missing %q:\n%s", want, got)
	}
}

func copyFile(t *testing.T, source, destination string) {
	t.Helper()
	data, err := os.ReadFile(source)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(destination, data, 0o644); err != nil {
		t.Fatal(err)
	}
}
