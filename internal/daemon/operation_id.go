package daemon

import (
	"github.com/bnkrr/rses/internal/runtime/tmux"
	"github.com/bnkrr/rses/internal/session"
)

type operationID struct {
	Scope  string
	Window string
}

func (id operationID) String() string {
	if id.Scope == "" {
		return id.Window
	}
	if id.Window == "" {
		return id.Scope
	}
	return id.Scope + ":" + id.Window
}

func operationIDForSpec(projectDir string, spec session.Spec) operationID {
	return operationID{
		Scope:  tmux.ScopeForProject(projectDir),
		Window: tmux.SafeName(spec.Name),
	}
}

func operationIDForRecord(record tmux.RuntimeRecord) operationID {
	id := operationID{Scope: record.Scope, Window: record.Window}
	if id.Scope != "" && id.Window != "" {
		return id
	}
	return operationID{
		Scope:  tmux.ScopeForProject(record.ProjectDir),
		Window: tmux.SafeName(record.Name),
	}
}
