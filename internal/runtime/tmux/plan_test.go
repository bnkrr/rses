package tmux

import (
	"strings"
	"testing"

	"github.com/bnkrr/rses/internal/session"
)

func TestNewPlanRendersInspectableScripts(t *testing.T) {
	spec := session.Spec{
		Name:    "demo/app",
		Backend: "ssh-tmux",
		Host:    "ssh-dev",
		Command: session.Command{
			Cwd:  "/home/remote/app",
			Env:  map[string]string{"CODEX_HOME": "/home/remote/.codex"},
			Argv: []string{"/home/remote/.local/bin/codex", "app-server", "--listen", "ws://127.0.0.1:4097"},
		},
	}

	plan, err := NewPlan(spec, "/srv/rses-demo", RemoteStateRoot, 25)
	if err != nil {
		t.Fatal(err)
	}

	for _, want := range []string{
		"$HOME/.local/state/rses/scopes/",
		"/sessions/demo_app",
		"/run.sh",
		"/output.log",
	} {
		if !strings.Contains(plan.StateDir+" "+plan.RunPath+" "+plan.LogPath, want) {
			t.Fatalf("plan paths = %+v, missing %q", plan, want)
		}
	}
	for _, want := range []string{
		"cd '/home/remote/app'",
		"export CODEX_HOME='/home/remote/.codex'",
		"exec '/home/remote/.local/bin/codex' 'app-server' '--listen' 'ws://127.0.0.1:4097'",
	} {
		if !strings.Contains(plan.RunScript, want) {
			t.Fatalf("run script = %q, missing %q", plan.RunScript, want)
		}
	}
	for _, want := range []string{
		"cat > $HOME/'.local/state/rses/scopes/",
		"tmux new-session -d -s 'rses-",
		"tmux new-window -d -t 'rses-",
		"tail -n 25",
		"tmux capture-pane -p -t 'rses-",
	} {
		combined := plan.InstallScript + "\n" + plan.StartScript + "\n" + plan.LogScript
		if !strings.Contains(combined, want) {
			t.Fatalf("plan scripts = %q, missing %q", combined, want)
		}
	}
}
