package selector

import (
	"testing"

	"github.com/bnkrr/rses/internal/session"
)

func TestSelectStartStopRequiresExplicitSelector(t *testing.T) {
	specs := []session.Spec{{Name: "one"}, {Name: "two"}}

	_, err := SelectStartStop(specs, nil, "", nil, false, "start", false, "")
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestSelectStartStopAllowsAll(t *testing.T) {
	specs := []session.Spec{{Name: "one"}, {Name: "two"}}

	selected, err := SelectStartStop(specs, nil, "", nil, true, "start", false, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(selected) != 2 {
		t.Fatalf("len(selected) = %d, want 2", len(selected))
	}
}

func TestSelectStartStopRejectsAllWithSelector(t *testing.T) {
	specs := []session.Spec{{Name: "one"}, {Name: "two"}}

	_, err := SelectStartStop(specs, []string{"one"}, "", nil, true, "stop", false, "")
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestSelectStartStopRequiresAllTags(t *testing.T) {
	specs := []session.Spec{
		{Name: "one", Tags: []string{"ssh", "tunnel"}},
		{Name: "two", Tags: []string{"ssh"}},
	}

	selected, err := SelectStartStop(specs, nil, "", []string{"ssh", "tunnel"}, false, "start", false, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(selected) != 1 || selected[0].Name != "one" {
		t.Fatalf("selected = %+v, want one", selected)
	}
}

func TestSelectStartStopExcludesRemoteByDefault(t *testing.T) {
	specs := []session.Spec{
		{Name: "local"},
		{Name: "remote", Backend: "ssh-tmux", Host: "h1"},
	}

	selected, err := SelectStartStop(specs, nil, "", nil, true, "start", false, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(selected) != 1 || selected[0].Name != "local" {
		t.Fatalf("selected = %+v, want only local", selected)
	}
}

func TestSelectStartStopIncludesRemoteWithHostFilter(t *testing.T) {
	specs := []session.Spec{
		{Name: "local"},
		{Name: "one", Backend: "ssh-tmux", Host: "h1"},
		{Name: "two", Backend: "ssh-tmux", Host: "h2"},
	}

	selected, err := SelectStartStop(specs, nil, "", nil, true, "start", true, "h1")
	if err != nil {
		t.Fatal(err)
	}
	if len(selected) != 2 || selected[0].Name != "local" || selected[1].Name != "one" {
		t.Fatalf("selected = %+v, want local and h1 remote", selected)
	}
}

func TestExpandStartDependenciesRejectsRemoteDependencyWithoutRemoteFlag(t *testing.T) {
	specs := []session.Spec{
		{Name: "local", DependsOn: []string{"remote"}},
		{Name: "remote", Backend: "ssh-tmux", Host: "h1"},
	}

	_, err := ExpandStartDependencies(specs[:1], specs, false, "")
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestExpandStartDependenciesOrdersDependenciesFirst(t *testing.T) {
	specs := []session.Spec{
		{Name: "app", DependsOn: []string{"remote"}},
		{Name: "remote", Backend: "ssh-tmux", Host: "h1"},
	}

	selected, err := ExpandStartDependencies(specs[:1], specs, true, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(selected) != 2 || selected[0].Name != "remote" || selected[1].Name != "app" {
		t.Fatalf("selected = %+v, want remote before app", selected)
	}
}

func TestExpandStartDependenciesRespectsHostFilter(t *testing.T) {
	specs := []session.Spec{
		{Name: "app", DependsOn: []string{"remote"}},
		{Name: "remote", Backend: "ssh-tmux", Host: "h2"},
	}

	_, err := ExpandStartDependencies(specs[:1], specs, true, "h1")
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestProjectDirForRequestClearsRootWhenSelectorDoesNotMatchConfig(t *testing.T) {
	root := ProjectDirForRequest("/tmp/config-root", nil, true, []string{"old-name"}, "", nil, false)
	if root != "" {
		t.Fatalf("root = %q, want empty", root)
	}
}

func TestProjectDirForRequestKeepsRootForConfiguredMatch(t *testing.T) {
	root := ProjectDirForRequest("/tmp/config-root", []session.Spec{{Name: "demo"}}, true, []string{"demo"}, "", nil, false)
	if root != "/tmp/config-root" {
		t.Fatalf("root = %q, want config root", root)
	}
}
