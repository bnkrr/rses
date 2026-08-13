package tmux

import (
	"context"
	"fmt"
	"os/exec"
	"time"

	"github.com/bnkrr/rses/internal/session"
)

func RunLocal(ctx context.Context, script string, timeout time.Duration) (string, error) {
	if timeout <= 0 {
		timeout = session.DefaultOperationTimeout
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "sh", "-c", script)
	out, err := cmd.CombinedOutput()
	if ctx.Err() == context.DeadlineExceeded {
		return string(out), fmt.Errorf("local tmux command timed out")
	}
	return string(out), err
}

func RunSSH(ctx context.Context, host, script string, runtime session.RuntimeOptions, query bool) (string, error) {
	timeout := runtime.OperationTimeoutOrDefault()
	if query {
		timeout = runtime.QueryTimeoutOrDefault()
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	args := sshArgs(host, script, runtime, query)
	cmd := exec.CommandContext(ctx, "ssh", args...)
	out, err := cmd.CombinedOutput()
	if ctx.Err() == context.DeadlineExceeded {
		return string(out), fmt.Errorf("ssh %s timed out", host)
	}
	return string(out), err
}

func sshArgs(host, script string, runtime session.RuntimeOptions, query bool) []string {
	args := append([]string{}, runtime.SSH.ExtraArgs...)
	connectTimeout := runtime.SSHConnectTimeoutOrDefault()
	if connectTimeout > 0 {
		args = append(args, "-o", "ConnectTimeout="+sshDurationSeconds(connectTimeout))
	}
	if query && runtime.SSHBatchModeOrDefault() {
		args = append(args, "-o", "BatchMode=yes", "-o", "NumberOfPasswordPrompts=0")
	}
	args = append(args, host, script)
	return args
}

func sshDurationSeconds(value time.Duration) string {
	seconds := int(value / time.Second)
	if value%time.Second != 0 {
		seconds++
	}
	if seconds <= 0 {
		seconds = 1
	}
	return fmt.Sprintf("%d", seconds)
}
