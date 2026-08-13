package daemon

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	tmuxrt "github.com/bnkrr/rses/internal/runtime/tmux"
	"github.com/bnkrr/rses/internal/session"
)

func TestDaemonMetaIncludesVersionAndProcess(t *testing.T) {
	meta := daemonMeta()
	if meta.ProtocolVersion != ProtocolVersion {
		t.Fatalf("protocol = %d, want %d", meta.ProtocolVersion, ProtocolVersion)
	}
	if meta.PID != os.Getpid() {
		t.Fatalf("pid = %d, want %d", meta.PID, os.Getpid())
	}
	if meta.ExePath == "" {
		t.Fatal("exe path is empty")
	}
	if meta.StartedAt.IsZero() {
		t.Fatal("started_at is zero")
	}
}

func TestCompatibleDaemonRequiresProtocolAndExecutable(t *testing.T) {
	meta := daemonMeta()
	if !compatibleDaemon(meta) {
		t.Fatalf("current daemon meta should be compatible: %+v", meta)
	}
	meta.ProtocolVersion++
	if compatibleDaemon(meta) {
		t.Fatal("protocol mismatch should be incompatible")
	}
	meta = daemonMeta()
	meta.ExePath = "/tmp/other-rses"
	if compatibleDaemon(meta) {
		t.Fatal("exe path mismatch should be incompatible")
	}
}

func TestControlRPCTimeoutIsNotDialFailure(t *testing.T) {
	runtimeDir := t.TempDir()
	t.Setenv("RSES_RUNTIME_DIR", runtimeDir)
	socket := filepath.Join(runtimeDir, "rses.sock")
	ln, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()

	oldDeadline := controlRPCDeadline
	controlRPCDeadline = 25 * time.Millisecond
	defer func() {
		controlRPCDeadline = oldDeadline
	}()

	accepted := make(chan net.Conn, 1)
	go func() {
		conn, err := ln.Accept()
		if err == nil {
			accepted <- conn
		}
	}()

	_, err = Call(Request{Action: "meta"})
	if err == nil {
		t.Fatal("expected meta timeout")
	}
	if isDialFailure(err) {
		t.Fatalf("timeout error was treated as dial failure: %v", err)
	}
	select {
	case conn := <-accepted:
		_ = conn.Close()
	default:
	}
}

func TestEnsureRunningKeepsCompatibleDaemon(t *testing.T) {
	events := startTestDaemon(t, func(req Request) Response {
		switch req.Action {
		case "meta":
			return Response{OK: true, Meta: ptr(daemonMeta())}
		case "shutdown":
			return Response{OK: true}
		default:
			return Response{OK: false, Error: "unexpected action: " + req.Action}
		}
	})
	started := stubStartDaemon(t)

	if err := EnsureRunning(); err != nil {
		t.Fatal(err)
	}
	if *started {
		t.Fatal("compatible daemon should not be restarted")
	}
	if containsEvent(events, "shutdown") {
		t.Fatal("compatible daemon should not receive shutdown")
	}
}

func TestEnsureRunningReplacesProtocolMismatch(t *testing.T) {
	meta := daemonMeta()
	meta.ProtocolVersion++
	events := startTestDaemon(t, func(req Request) Response {
		switch req.Action {
		case "meta":
			return Response{OK: true, Meta: &meta}
		case "shutdown":
			return Response{OK: true}
		default:
			return Response{OK: false, Error: "unexpected action: " + req.Action}
		}
	})
	started := stubStartDaemon(t)

	if err := EnsureRunning(); err != nil {
		t.Fatal(err)
	}
	if !*started {
		t.Fatal("protocol mismatch should start replacement daemon")
	}
	if !containsEvent(events, "shutdown") {
		t.Fatal("protocol mismatch should request daemon shutdown")
	}
}

func TestEnsureRunningReportsLegacyDaemon(t *testing.T) {
	events := startTestDaemon(t, func(req Request) Response {
		if req.Action == "meta" {
			return Response{OK: false, Error: "unknown daemon action: meta"}
		}
		return Response{OK: false, Error: "unknown daemon action: " + req.Action}
	})
	started := stubStartDaemon(t)

	err := EnsureRunning()
	if err == nil {
		t.Fatal("expected legacy daemon error")
	}
	if !strings.Contains(err.Error(), "does not support version negotiation") {
		t.Fatalf("error = %q, want version negotiation guidance", err)
	}
	if !containsEvent(events, "shutdown") {
		t.Fatal("legacy daemon should receive best-effort shutdown")
	}
	if *started {
		t.Fatal("legacy daemon should not be replaced without shutdown support")
	}
}

func TestServerMetaAndShutdownActions(t *testing.T) {
	socket := filepath.Join(t.TempDir(), "rses.sock")
	ln, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	t.Setenv("RSES_RUNTIME_DIR", filepath.Dir(socket))
	server := &Server{shutdown: make(chan struct{})}

	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go server.handle(conn)
		}
	}()

	resp := callSocket(t, socket, Request{Action: "meta"})
	if !resp.OK || resp.Meta == nil || resp.Meta.ProtocolVersion != ProtocolVersion {
		t.Fatalf("meta response = %+v", resp)
	}
	resp = callSocket(t, socket, Request{Action: "shutdown"})
	if !resp.OK {
		t.Fatalf("shutdown response = %+v", resp)
	}
	select {
	case <-server.shutdown:
	case <-time.After(time.Second):
		t.Fatal("shutdown channel was not closed")
	}
}

func TestRemoteStatusFiltersConfiguredSpecsByHost(t *testing.T) {
	logPath := installFakeSSH(t)
	s := &Server{}

	req := Request{
		Action:     "status",
		ProjectDir: "/tmp/rses-remote",
		Specs: []session.Spec{
			remoteSpec("one", "h1"),
			remoteSpec("two", "h2"),
		},
		Host:              "h1",
		IncludeConfigured: true,
	}
	summary, _ := collectOperation(t, func(ctx context.Context, emit eventEmitter) operationSummary {
		return s.status(ctx, req, emit)
	})

	if len(summary.Statuses) != 1 || summary.Statuses[0].Name != "one" || summary.Statuses[0].State != session.StateRunning {
		t.Fatalf("statuses = %+v, want one running h1 status", summary.Statuses)
	}
	calls := readFile(t, logPath)
	if strings.Contains(calls, "h2 ") {
		t.Fatalf("fake ssh calls = %q, want no h2 call", calls)
	}
	if !strings.Contains(calls, "tmux list-windows") {
		t.Fatalf("fake ssh calls = %q, want h1 list-windows", calls)
	}
}

func TestRemoteLogsCaptureTmuxPane(t *testing.T) {
	logPath := installFakeSSH(t)
	s := &Server{}

	resp := s.logs(Request{ProjectDir: "/tmp/rses-remote", Specs: []session.Spec{remoteSpec("one", "h1")}, Lines: 25})

	if !resp.OK || resp.Log != "remote log\n" {
		t.Fatalf("unexpected response: %+v", resp)
	}
	calls := readFile(t, logPath)
	if !strings.Contains(calls, "tail -n 25") {
		t.Fatalf("fake ssh calls = %q, want tail log first", calls)
	}
	if !strings.Contains(calls, "tmux capture-pane -p -t 'rses-") {
		t.Fatalf("fake ssh calls = %q, want capture-pane fallback", calls)
	}
}

func TestWatchSnapshotBatchesRemoteStatusByHostAndReturnsSelectedLog(t *testing.T) {
	logPath := installFakeSSH(t)
	s := &Server{}

	resp := s.watchSnapshot(Request{
		ProjectDir:        "/tmp/rses-remote",
		Specs:             []session.Spec{remoteSpec("one", "h1"), remoteSpec("two", "h1")},
		SelectedName:      "two",
		IncludeConfigured: true,
		Lines:             25,
	})

	if !resp.OK {
		t.Fatalf("response = %+v, want OK", resp)
	}
	if len(resp.Statuses) != 2 {
		t.Fatalf("statuses = %+v, want two statuses", resp.Statuses)
	}
	if resp.Statuses[0].Name != "one" || resp.Statuses[0].State != session.StateRunning {
		t.Fatalf("first status = %+v, want one running", resp.Statuses[0])
	}
	if resp.Statuses[1].Name != "two" || resp.Statuses[1].State != session.StateStopped {
		t.Fatalf("second status = %+v, want two stopped", resp.Statuses[1])
	}
	if resp.LogName != "two" || resp.Log != "remote log\n" {
		t.Fatalf("log = (%q, %q), want selected remote log", resp.LogName, resp.Log)
	}
	calls := readFile(t, logPath)
	if got := strings.Count(calls, "h1 command -v tmux"); got != 1 {
		t.Fatalf("fake ssh calls = %q, want one batched call", calls)
	}
}

func TestWatchSnapshotHostScopeOnlyTouchesRequestedVisibleSpecs(t *testing.T) {
	logPath := installFakeSSH(t)
	s := &Server{}

	resp := s.watchSnapshot(Request{
		ProjectDir:        "/tmp/rses-remote",
		Host:              "h1",
		IncludeConfigured: true,
		Specs: []session.Spec{
			remoteSpec("one", "h1"),
			remoteSpec("two", "h2"),
		},
	})

	if !resp.OK {
		t.Fatalf("response = %+v, want OK", resp)
	}
	if len(resp.Statuses) != 1 || resp.Statuses[0].Name != "one" {
		t.Fatalf("statuses = %+v, want only h1 visible status", resp.Statuses)
	}
	calls := readFile(t, logPath)
	if strings.Contains(calls, "h2 ") {
		t.Fatalf("fake ssh calls = %q, want no h2 call", calls)
	}
}

func TestParseWatchSnapshotOutputPreservesMarkerLinesInLog(t *testing.T) {
	want := "first\n__RSES_WATCH_WINDOWS__\n__RSES_WATCH_LOG__\nlast\n"
	out := fmt.Sprintf("__RSES_WATCH_WINDOWS__\none\n__RSES_WATCH_LOG_BYTES__\n%d\n%s", len(want), want)

	windows, log := parseWatchSnapshotOutput(out)

	if !windows["one"] {
		t.Fatalf("windows = %+v, want one", windows)
	}
	if log != want {
		t.Fatalf("log = %q, want %q", log, want)
	}
}

func TestRemoteStartInstallsInspectableRunScriptInWindow(t *testing.T) {
	logPath := installFakeSSH(t)
	t.Setenv("RSES_FAKE_SSH_WINDOW", "missing")
	spec := remoteSpec("one", "h1")
	spec.Command = session.Command{
		Cwd:  "/home/remote/app",
		Env:  map[string]string{"CODEX_HOME": "/home/remote/.codex"},
		Argv: []string{"/home/remote/.local/bin/codex", "app-server", "--listen", "ws://127.0.0.1:4097"},
	}
	s := &Server{}

	req := Request{Action: "start", ProjectDir: "/tmp/rses-remote", Specs: []session.Spec{spec}}
	summary, _ := collectOperation(t, func(ctx context.Context, emit eventEmitter) operationSummary {
		return s.start(ctx, req, emit)
	})

	if summary.Results["one"] != "started" {
		t.Fatalf("summary = %+v, want started", summary)
	}
	calls := readFile(t, logPath)
	for _, want := range []string{
		"cat > $HOME/'.local/state/rses/scopes/",
		"cd '/home/remote/app'",
		"export CODEX_HOME='/home/remote/.codex'",
		"exec '/home/remote/.local/bin/codex' 'app-server' '--listen' 'ws://127.0.0.1:4097'",
		"tmux new-session -d -s 'rses-",
		"tmux new-window -d -t 'rses-",
		"tmux pipe-pane -o -t 'rses-",
	} {
		if !strings.Contains(calls, want) {
			t.Fatalf("fake ssh calls = %q, missing %q", calls, want)
		}
	}
}

func TestLocalStatusCanRecoverFromRecord(t *testing.T) {
	stateDir := t.TempDir()
	t.Setenv("RSES_STATE_DIR", stateDir)
	logPath := installFakeTmux(t, "demo")
	projectDir := "/tmp/rses-history"
	spec := session.Spec{
		Name:    "demo",
		Backend: "tmux",
		Group:   "old",
		Command: session.Command{Argv: []string{"sleep", "10"}},
	}
	plan, err := tmuxPlan(projectDir, spec, 200)
	if err != nil {
		t.Fatal(err)
	}
	if err := writeLocalRecord(plan, time.Now()); err != nil {
		t.Fatal(err)
	}
	s := &Server{}

	req := Request{Action: "status", Names: []string{"demo"}}
	summary, _ := collectOperation(t, func(ctx context.Context, emit eventEmitter) operationSummary {
		return s.status(ctx, req, emit)
	})

	if len(summary.Statuses) != 1 {
		t.Fatalf("unexpected summary: %+v", summary)
	}
	got := summary.Statuses[0]
	if got.Name != "demo" || got.Group != "old" || got.State != session.StateRunning || got.LogPath == "" {
		t.Logf("fake tmux calls:\n%s", readFile(t, logPath))
		t.Fatalf("status = %+v, want recovered running metadata", got)
	}
}

func TestWatchSnapshotCanRecoverLocalRecordWithoutConfiguredSpecs(t *testing.T) {
	stateDir := t.TempDir()
	t.Setenv("RSES_STATE_DIR", stateDir)
	installFakeTmux(t, "demo")
	projectDir := "/tmp/rses-history"
	spec := session.Spec{
		Name:    "demo",
		Backend: "tmux",
		Group:   "old",
		Command: session.Command{Argv: []string{"sleep", "10"}},
	}
	plan, err := tmuxPlan(projectDir, spec, 200)
	if err != nil {
		t.Fatal(err)
	}
	if err := writeLocalRecord(plan, time.Now()); err != nil {
		t.Fatal(err)
	}
	s := &Server{}

	resp := s.watchSnapshot(Request{Names: []string{"demo"}, SelectedName: "demo"})

	if !resp.OK {
		t.Fatalf("response = %+v, want OK", resp)
	}
	if len(resp.Statuses) != 1 || resp.Statuses[0].Name != "demo" || resp.Statuses[0].State != session.StateRunning {
		t.Fatalf("statuses = %+v, want recovered running status", resp.Statuses)
	}
	if resp.Statuses[0].StartedAt.IsZero() {
		t.Fatalf("status = %+v, want StartedAt from record", resp.Statuses[0])
	}
	if resp.LogName != "demo" || resp.Log != "local log\n" {
		t.Fatalf("log = (%q, %q), want recovered selected log", resp.LogName, resp.Log)
	}
}

func TestStatusRejectsAmbiguousRecordedNameWithoutConfigRoot(t *testing.T) {
	stateDir := t.TempDir()
	t.Setenv("RSES_STATE_DIR", stateDir)
	installFakeTmux(t, "demo")
	for _, root := range []string{"/tmp/one", "/tmp/two"} {
		spec := session.Spec{Name: "demo", Backend: "tmux", Command: session.Command{Argv: []string{"sleep", "10"}}}
		plan, err := tmuxPlan(root, spec, 200)
		if err != nil {
			t.Fatal(err)
		}
		if err := writeLocalRecord(plan, time.Now()); err != nil {
			t.Fatal(err)
		}
	}
	s := &Server{}

	req := Request{Action: "status", Names: []string{"demo"}}
	summary, events := collectOperation(t, func(ctx context.Context, emit eventEmitter) operationSummary {
		return s.status(ctx, req, emit)
	})

	if len(summary.Statuses) != 0 {
		t.Fatalf("expected no statuses for ambiguity, got %+v", summary.Statuses)
	}
	if got := firstFailedError(events); !strings.Contains(got, "ambiguous") {
		t.Fatalf("error = %q, want ambiguity; events=%+v", got, events)
	}
}

func collectOperation(t *testing.T, run func(context.Context, eventEmitter) operationSummary) (operationSummary, []OperationEvent) {
	t.Helper()
	var mu sync.Mutex
	events := []OperationEvent{}
	summary := run(context.Background(), func(event OperationEvent) error {
		mu.Lock()
		defer mu.Unlock()
		events = append(events, event)
		return nil
	})
	return summary, events
}

func firstFailedError(events []OperationEvent) string {
	for _, event := range events {
		if event.Type == "failed" {
			return event.Error
		}
	}
	return ""
}

func installFakeSSH(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	logPath := filepath.Join(dir, "ssh.log")
	ssh := filepath.Join(dir, "ssh")
	if err := os.WriteFile(ssh, []byte(`#!/bin/sh
prev=
host=
script=
for arg in "$@"; do
  host="$prev"
  script="$arg"
  prev="$arg"
done
printf '%s %s\n' "$host" "$script" >> "$RSES_FAKE_SSH_LOG"
case "$script" in
  *'__RSES_WATCH_WINDOWS__'*) printf '__RSES_WATCH_WINDOWS__\none\n__RSES_WATCH_LOG_BYTES__\n11\nremote log\n'; exit 0 ;;
  *'tail -n'*) printf 'remote log\n'; exit 0 ;;
  *capture-pane*) printf 'remote log\n'; exit 0 ;;
  *new-window*) exit 0 ;;
  *new-session*) exit 0 ;;
  *kill-window*) exit 0 ;;
  *'cat >'*) exit 0 ;;
  *'list-windows'*) if [ "$RSES_FAKE_SSH_WINDOW" = "missing" ]; then exit 1; fi; printf 'one\n'; exit 0 ;;
esac
exit 1
	`), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("RSES_FAKE_SSH_LOG", logPath)
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	return logPath
}

func stubStartDaemon(t *testing.T) *bool {
	t.Helper()
	started := false
	oldStart := startDaemonProcess
	startDaemonProcess = func() error {
		started = true
		return nil
	}
	t.Cleanup(func() {
		startDaemonProcess = oldStart
	})
	return &started
}

func startTestDaemon(t *testing.T, respond func(Request) Response) <-chan string {
	t.Helper()
	runtimeDir := t.TempDir()
	t.Setenv("RSES_RUNTIME_DIR", runtimeDir)
	socket := filepath.Join(runtimeDir, "rses.sock")
	ln, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	events := make(chan string, 16)
	done := make(chan struct{})
	go func() {
		defer close(done)
		defer os.Remove(socket)
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			var req Request
			if err := json.NewDecoder(conn).Decode(&req); err != nil {
				_ = conn.Close()
				continue
			}
			events <- req.Action
			resp := respond(req)
			_ = json.NewEncoder(conn).Encode(resp)
			_ = conn.Close()
			if req.Action == "shutdown" {
				_ = ln.Close()
				return
			}
		}
	}()
	t.Cleanup(func() {
		_ = ln.Close()
		select {
		case <-done:
		case <-time.After(time.Second):
			t.Fatal("test daemon did not stop")
		}
	})
	return events
}

func containsEvent(events <-chan string, want string) bool {
	for {
		select {
		case event := <-events:
			if event == want {
				return true
			}
		default:
			return false
		}
	}
}

func installFakeTmux(t *testing.T, window string) string {
	t.Helper()
	dir := t.TempDir()
	logPath := filepath.Join(dir, "tmux.log")
	tmux := filepath.Join(dir, "tmux")
	if err := os.WriteFile(tmux, []byte(fmt.Sprintf(`#!/bin/sh
printf '%%s\n' "$*" >> %s
case "$1" in
  list-windows) printf '%%s\n' %s; exit 0 ;;
  has-session) exit 0 ;;
  new-session) exit 0 ;;
  new-window) exit 0 ;;
  pipe-pane) exit 0 ;;
  capture-pane) printf 'local log\n'; exit 0 ;;
  kill-window) exit 0 ;;
esac
exit 1
`, shellQuoteForTest(logPath), shellQuoteForTest(window))), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(tmux, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	if got, err := exec.LookPath("tmux"); err != nil || got != tmux {
		t.Fatalf("exec.LookPath(tmux) = %q, %v; want %q; mode=%s", got, err, tmux, fileMode(t, tmux))
	}
	out, err := tmuxrt.RunLocal(context.Background(), "command -v tmux", session.DefaultQueryTimeout)
	if err != nil {
		t.Fatalf("fake tmux not discoverable: %v\n%s", err, out)
	}
	if strings.TrimSpace(out) != tmux {
		t.Fatalf("command -v tmux = %q, want %q; PATH=%s", strings.TrimSpace(out), tmux, os.Getenv("PATH"))
	}
	return logPath
}

func shellQuoteForTest(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'"
}

func fileMode(t *testing.T, path string) os.FileMode {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	return info.Mode()
}

func remoteSpec(name, host string) session.Spec {
	return session.Spec{
		Name:    name,
		Backend: "ssh-tmux",
		Host:    host,
		Command: session.Command{Argv: []string{"sleep", "10"}},
	}
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func callSocket(t *testing.T, socket string, req Request) Response {
	t.Helper()
	conn, err := net.Dial("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if err := json.NewEncoder(conn).Encode(req); err != nil {
		t.Fatal(err)
	}
	var resp Response
	if err := json.NewDecoder(conn).Decode(&resp); err != nil {
		t.Fatal(err)
	}
	return resp
}
