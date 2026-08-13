package daemon

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/bnkrr/rses/internal/paths"
	"github.com/bnkrr/rses/internal/runtime/tmux"
	"github.com/bnkrr/rses/internal/session"
)

func stopSpec(ctx context.Context, projectDir string, spec session.Spec, runtime session.RuntimeOptions) (string, session.Status) {
	st := tmuxStatus(ctx, projectDir, spec, runtime)
	if st.State != session.StateRunning {
		return "not running", st
	}
	plan, err := tmuxPlan(projectDir, spec, 200)
	if err != nil {
		st.State = session.StateUnknown
		st.LastError = err.Error()
		return "failed: " + st.LastError, st
	}
	out, err := runOperationPlan(ctx, spec, plan.StopScript, runtime)
	if err != nil {
		st.State = session.StateUnknown
		st.LastError = trimError(out, err)
		return "failed: " + st.LastError, st
	}
	st = tmuxStatus(ctx, projectDir, spec, runtime)
	if st.State == session.StateRunning {
		st.State = session.StateUnknown
		st.LastError = "tmux window still exists after stop"
		return "failed: " + st.LastError, st
	}
	return "stopped", st
}

func tmuxStatus(ctx context.Context, projectDir string, spec session.Spec, runtime session.RuntimeOptions) session.Status {
	plan, err := tmuxPlan(projectDir, spec, 200)
	if err != nil {
		return session.Status{
			Name:      spec.Name,
			Group:     spec.Group,
			Tags:      spec.Tags,
			Summary:   spec.Summary,
			State:     session.StateUnknown,
			LastError: err.Error(),
		}
	}
	st := session.Status{
		Name:    spec.Name,
		Group:   spec.Group,
		Tags:    spec.Tags,
		Summary: spec.Summary,
		State:   session.StateUnknown,
		LogPath: plan.LogPath,
	}
	out, err := runQueryPlan(ctx, spec, plan.StatusScript, runtime)
	if err == nil {
		st.State = session.StateRunning
		return st
	}
	if stoppedStatus(out, err) {
		st.State = session.StateStopped
		return st
	}
	st.LastError = trimError(out, err)
	return st
}

func statusFromRecord(ctx context.Context, record tmux.RuntimeRecord, runtime session.RuntimeOptions) session.Status {
	spec := specFromRecord(record)
	st := tmuxStatus(ctx, record.ProjectDir, spec, runtime)
	if record.StartedAt != nil {
		st.StartedAt = *record.StartedAt
	}
	return st
}

func specFromRecord(record tmux.RuntimeRecord) session.Spec {
	return session.Spec{
		Name:    record.Name,
		Backend: record.Kind,
		Host:    record.Host,
		Group:   record.Group,
		Tags:    record.Tags,
		Summary: record.Summary,
		Command: session.Command{Argv: []string{"true"}},
	}
}

func tmuxPlan(projectDir string, spec session.Spec, lines int) (tmux.Plan, error) {
	stateRoot := paths.StateDir()
	if spec.IsRemote() {
		stateRoot = tmux.RemoteStateRoot
	}
	return tmux.NewPlan(spec, projectDir, stateRoot, lines)
}

func runQueryPlan(ctx context.Context, spec session.Spec, script string, runtime session.RuntimeOptions) (string, error) {
	if spec.IsRemote() {
		return tmux.RunSSH(ctx, spec.Host, script, runtime, true)
	}
	return tmux.RunLocal(ctx, script, runtime.QueryTimeoutOrDefault())
}

func runOperationPlan(ctx context.Context, spec session.Spec, script string, runtime session.RuntimeOptions) (string, error) {
	if spec.IsRemote() {
		return tmux.RunSSH(ctx, spec.Host, script, runtime, false)
	}
	return tmux.RunLocal(ctx, script, runtime.OperationTimeoutOrDefault())
}

func stoppedStatus(out string, err error) bool {
	if err == nil {
		return false
	}
	if strings.Contains(err.Error(), "timed out") {
		return false
	}
	out = strings.TrimSpace(out)
	return out == "" || strings.Contains(out, "can't find") || strings.Contains(out, "no server running")
}

func trimError(out string, err error) string {
	out = strings.TrimSpace(out)
	if out != "" {
		return out
	}
	return err.Error()
}

func writeLocalRecord(plan tmux.Plan, startedAt time.Time) error {
	record := tmux.RuntimeRecord{
		Name:       plan.Spec.Name,
		ProjectDir: plan.ProjectDir,
		Kind:       plan.Spec.Kind(),
		Group:      plan.Spec.Group,
		Tags:       plan.Spec.Tags,
		Summary:    plan.Spec.Summary,
		Scope:      plan.Scope,
		Window:     plan.Window,
		StateDir:   plan.StateDir,
		RunPath:    plan.RunPath,
		LogPath:    plan.LogPath,
		StartedAt:  &startedAt,
	}
	recordJSON, err := tmux.RecordJSON(record)
	if err != nil {
		return err
	}
	recordMu.Lock()
	defer recordMu.Unlock()
	if err := os.MkdirAll(plan.StateDir, 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(plan.StateDir, "record-*.tmp")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	if _, err := tmp.Write([]byte(recordJSON)); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpPath)
		return err
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpPath)
		return err
	}
	if err := os.Chmod(tmpPath, 0o600); err != nil {
		_ = os.Remove(tmpPath)
		return err
	}
	if err := os.Rename(tmpPath, filepath.Clean(plan.RecordPath)); err != nil {
		_ = os.Remove(tmpPath)
		return err
	}
	return nil
}
