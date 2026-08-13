package daemon

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/bnkrr/rses/internal/buildinfo"
	"github.com/bnkrr/rses/internal/paths"
	"github.com/bnkrr/rses/internal/session"
)

const ProtocolVersion = 7

var controlRPCDeadline = 2 * time.Second

var startDaemonProcess = startDaemon

type Request struct {
	Action            string                 `json:"action"`
	ProjectDir        string                 `json:"project_dir,omitempty"`
	Specs             []session.Spec         `json:"specs,omitempty"`
	Names             []string               `json:"names,omitempty"`
	SelectedName      string                 `json:"selected_name,omitempty"`
	Group             string                 `json:"group,omitempty"`
	Tags              []string               `json:"tags,omitempty"`
	Host              string                 `json:"host,omitempty"`
	IncludeConfigured bool                   `json:"include_configured,omitempty"`
	Lines             int                    `json:"lines,omitempty"`
	Parallel          int                    `json:"parallel,omitempty"`
	Runtime           session.RuntimeOptions `json:"runtime,omitempty"`
}

type Response struct {
	OK       bool              `json:"ok"`
	Error    string            `json:"error,omitempty"`
	Statuses []session.Status  `json:"statuses,omitempty"`
	Results  map[string]string `json:"results,omitempty"`
	Log      string            `json:"log,omitempty"`
	LogName  string            `json:"log_name,omitempty"`
	LogError string            `json:"log_error,omitempty"`
	Meta     *Meta             `json:"meta,omitempty"`
}

type Meta struct {
	ProtocolVersion int       `json:"protocol_version"`
	BuildID         string    `json:"build_id"`
	ExePath         string    `json:"exe_path"`
	PID             int       `json:"pid"`
	StartedAt       time.Time `json:"started_at"`
}

type OperationEvent struct {
	Type     string            `json:"type"`
	Action   string            `json:"action,omitempty"`
	Name     string            `json:"name,omitempty"`
	State    session.State     `json:"state,omitempty"`
	Status   *session.Status   `json:"status,omitempty"`
	Message  string            `json:"message,omitempty"`
	Error    string            `json:"error,omitempty"`
	Results  map[string]string `json:"results,omitempty"`
	Statuses []session.Status  `json:"statuses,omitempty"`
}

type daemonDialError struct {
	err error
}

func (e daemonDialError) Error() string {
	return e.err.Error()
}

func (e daemonDialError) Unwrap() error {
	return e.err
}

func Call(req Request) (Response, error) {
	conn, err := net.DialTimeout("unix", paths.SocketPath(), 500*time.Millisecond)
	if err != nil {
		return Response{}, daemonDialError{err: err}
	}
	defer conn.Close()
	if timeout := shortRPCDeadline(req); timeout > 0 {
		_ = conn.SetDeadline(time.Now().Add(timeout))
	}

	enc := json.NewEncoder(conn)
	if err := enc.Encode(req); err != nil {
		return Response{}, err
	}

	var resp Response
	if err := json.NewDecoder(bufio.NewReader(conn)).Decode(&resp); err != nil {
		return Response{}, err
	}
	if !resp.OK {
		return resp, errors.New(resp.Error)
	}
	return resp, nil
}

func Stream(req Request, handle func(OperationEvent) error) error {
	conn, err := net.DialTimeout("unix", paths.SocketPath(), 500*time.Millisecond)
	if err != nil {
		return daemonDialError{err: err}
	}
	defer conn.Close()

	if err := json.NewEncoder(conn).Encode(req); err != nil {
		return err
	}
	dec := json.NewDecoder(bufio.NewReader(conn))
	for {
		var event OperationEvent
		if err := dec.Decode(&event); err != nil {
			return err
		}
		if err := handle(event); err != nil {
			return err
		}
		if event.Type == "done" {
			return nil
		}
	}
}

func shortRPCDeadline(req Request) time.Duration {
	switch req.Action {
	case "meta", "shutdown":
		return controlRPCDeadline
	default:
		return 0
	}
}

func EnsureRunning() error {
	if _, err := os.Stat(paths.SocketPath()); err == nil {
		if err := ensureCompatibleDaemon(); err == nil {
			return nil
		} else if !isDialFailure(err) {
			return err
		}
	}

	return startDaemonProcess()
}

func CurrentBuildID() string {
	return buildinfo.Current()
}

func CurrentExePath() string {
	exe, err := os.Executable()
	if err != nil {
		return ""
	}
	return filepath.Clean(exe)
}

func daemonMeta() Meta {
	return Meta{
		ProtocolVersion: ProtocolVersion,
		BuildID:         CurrentBuildID(),
		ExePath:         CurrentExePath(),
		PID:             os.Getpid(),
		StartedAt:       processStartedAt,
	}
}

func ensureCompatibleDaemon() error {
	resp, err := Call(Request{Action: "meta"})
	if err != nil {
		if resp.Error != "" && strings.Contains(resp.Error, "unknown daemon action") {
			return replaceLegacyDaemon()
		}
		return err
	}
	if resp.Meta == nil {
		return fmt.Errorf("running rses daemon returned no metadata; stop it manually and retry")
	}
	if compatibleDaemon(*resp.Meta) {
		return nil
	}
	if err := requestDaemonShutdown(); err != nil {
		return err
	}
	if err := waitForSocketGone(3 * time.Second); err != nil {
		return err
	}
	return startDaemonProcess()
}

func replaceLegacyDaemon() error {
	if _, err := Call(Request{Action: "shutdown"}); err != nil {
		return fmt.Errorf("running rses daemon does not support version negotiation and could not be shut down: %w; stop it manually and retry", err)
	}
	if err := waitForSocketGone(3 * time.Second); err != nil {
		return err
	}
	return startDaemonProcess()
}

func compatibleDaemon(meta Meta) bool {
	return meta.ProtocolVersion == ProtocolVersion && filepath.Clean(meta.ExePath) == CurrentExePath()
}

func requestDaemonShutdown() error {
	if _, err := Call(Request{Action: "shutdown"}); err != nil {
		return fmt.Errorf("replace incompatible daemon: %w", err)
	}
	return nil
}

func waitForSocketGone(timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		conn, err := net.DialTimeout("unix", paths.SocketPath(), 50*time.Millisecond)
		if err != nil {
			return nil
		}
		_ = conn.Close()
		time.Sleep(50 * time.Millisecond)
	}
	return fmt.Errorf("daemon did not shut down")
}

func isDialFailure(err error) bool {
	var dialErr daemonDialError
	return errors.As(err, &dialErr)
}

func startDaemon() error {
	if err := os.MkdirAll(paths.StateDir(), 0o755); err != nil {
		return err
	}
	if err := os.MkdirAll(paths.RuntimeDir(), 0o755); err != nil {
		return err
	}

	exe, err := os.Executable()
	if err != nil {
		return err
	}
	logPath := paths.DaemonLogPath()
	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer logFile.Close()

	cmd := exec.Command(exe, "daemon")
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		return err
	}

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		conn, err := net.DialTimeout("unix", paths.SocketPath(), 100*time.Millisecond)
		if err == nil {
			conn.Close()
			return nil
		}
		time.Sleep(100 * time.Millisecond)
	}
	return fmt.Errorf("daemon did not become ready; see %s", filepath.Clean(logPath))
}
