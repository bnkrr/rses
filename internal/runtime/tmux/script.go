package tmux

import (
	"crypto/sha256"
	"fmt"
	"strings"
)

func StatusScript(scope, window string) string {
	return fmt.Sprintf(
		"command -v tmux >/dev/null || { echo 'tmux not found' >&2; exit 127; }\n"+
			"tmux list-windows -t %s -F '#{window_name}' 2>/dev/null | grep -Fx -- %s >/dev/null",
		ShellQuote(scope),
		ShellQuote(window),
	)
}

func StartScript(plan Plan) string {
	return strings.Join([]string{
		"command -v tmux >/dev/null || { echo 'tmux not found' >&2; exit 127; }",
		plan.InstallScript,
		"if ! tmux has-session -t " + ShellQuote(plan.Scope) + " 2>/dev/null; then tmux new-session -d -s " + ShellQuote(plan.Scope) + " -n rses-control; fi",
		"tmux set-environment -t " + ShellQuote(plan.Scope) + " PATH \"$PATH\" 2>/dev/null || true",
		"if " + plan.StatusScript + "; then exit 0; fi",
		"tmux new-window -d -t " + ShellQuote(plan.Scope) + " -n " + ShellQuote(plan.Window) + " -- /bin/sh " + ShellQuotePath(plan.RunPath),
		"(tmux pipe-pane -o -t " + ShellQuote(plan.Target) + " " + ShellQuote("cat >> "+ShellQuotePath(plan.LogPath)) + " || true)",
	}, "\n")
}

func StopScript(scope, window string) string {
	return "command -v tmux >/dev/null || { echo 'tmux not found' >&2; exit 127; }\n" +
		"tmux kill-window -t " + ShellQuote(scope+":"+window)
}

func LogScript(logPath, scope, window string, lines int) string {
	if lines <= 0 {
		lines = 200
	}
	return fmt.Sprintf(
		"command -v tmux >/dev/null || { echo 'tmux not found' >&2; exit 127; }\n"+
			"if [ -f %s ]; then tail -n %d %s; else tmux capture-pane -p -t %s -S -%d; fi",
		ShellQuotePath(logPath),
		lines,
		ShellQuotePath(logPath),
		ShellQuote(scope+":"+window),
		lines,
	)
}

func InstallScript(plan Plan, runBody, recordBody string) string {
	if runBody == "" {
		runBody = RunScript(plan.Spec)
	}
	if recordBody == "" {
		recordBody = plan.RecordJSON
	}
	runDelimiter := delimiter("RSES_RUN", runBody)
	recordDelimiter := delimiter("RSES_RECORD", recordBody)
	return strings.Join([]string{
		"umask 077",
		"mkdir -p " + ShellQuotePath(plan.StateDir),
		"cat > " + ShellQuotePath(plan.RunPath) + " <<'" + runDelimiter + "'",
		runBody,
		runDelimiter,
		"chmod 700 " + ShellQuotePath(plan.RunPath),
		"cat > " + ShellQuotePath(plan.RecordPath) + " <<'" + recordDelimiter + "'",
		recordBody,
		recordDelimiter,
	}, "\n")
}

func delimiter(prefix, body string) string {
	sum := sha256.Sum256([]byte(body))
	return fmt.Sprintf("%s_%x", prefix, sum[:8])
}
