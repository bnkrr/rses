package tmux

import (
	"strings"
	"testing"
	"time"

	"github.com/bnkrr/rses/internal/session"
)

func TestSSHArgsForQueryUseNonInteractiveDefaults(t *testing.T) {
	args := strings.Join(sshArgs("ssh-dev", "true", session.RuntimeOptions{}, true), " ")

	for _, want := range []string{
		"-o ConnectTimeout=8",
		"-o BatchMode=yes",
		"-o NumberOfPasswordPrompts=0",
		"ssh-dev true",
	} {
		if !strings.Contains(args, want) {
			t.Fatalf("args = %q, missing %q", args, want)
		}
	}
}

func TestSSHArgsCanDisableQueryBatchMode(t *testing.T) {
	batchMode := false
	runtime := session.RuntimeOptions{
		SSH: session.SSHOptions{
			ConnectTimeout: 3 * time.Second,
			BatchMode:      &batchMode,
			ExtraArgs:      []string{"-F", "none"},
		},
	}

	args := strings.Join(sshArgs("ssh-dev", "true", runtime, true), " ")

	if strings.Contains(args, "BatchMode=yes") || strings.Contains(args, "NumberOfPasswordPrompts") {
		t.Fatalf("args = %q, want batch mode disabled", args)
	}
	for _, want := range []string{"-F none", "-o ConnectTimeout=3", "ssh-dev true"} {
		if !strings.Contains(args, want) {
			t.Fatalf("args = %q, missing %q", args, want)
		}
	}
}
