package tui

import (
	"testing"

	"github.com/bnkrr/rses/internal/daemon"
	"github.com/bnkrr/rses/internal/session"
)

func TestWrapLogContentWrapsLongLines(t *testing.T) {
	got := wrapLogContent("abcdefghij", 4)
	want := "abcd\nefgh\nij"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestWrapLogContentPreservesExistingNewlines(t *testing.T) {
	got := wrapLogContent("abcd\nefgh\n", 10)
	want := "abcd\nefgh\n"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestWrapLogContentUsesDisplayWidth(t *testing.T) {
	got := wrapLogContent("你好世界", 4)
	want := "你好\n世界"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestWrapLogContentStripsANSIControlSequences(t *testing.T) {
	got := wrapLogContent("\x1b[93mwarn\x1b[0m\r\n\x1b]0;title\x07ready\x00\n", 80)
	want := "warn\nready\n"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestWrapLogContentPreservesOSC8LinkText(t *testing.T) {
	got := wrapLogContent("\x1b]8;;https://example.com\x1b\\visible text\x1b]8;;\x1b\\\n", 80)
	want := "visible text\n"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestRefreshActiveGroupsSelectedRemoteStartsLocalAndSelectedHost(t *testing.T) {
	m := newModel("/tmp/project", []session.Spec{
		{Name: "local", Backend: "tmux", Command: session.Command{Argv: []string{"true"}}},
		remoteSpec("remote-a", "h1"),
		remoteSpec("remote-b", "h2"),
	}, session.RuntimeOptions{})
	m.selected = 1

	m, _ = m.refreshActiveGroups(true)

	if !m.groups[localGroupKey].inFlight {
		t.Fatalf("local group not in flight")
	}
	if !m.groups["remote:h1"].inFlight {
		t.Fatalf("selected remote host not in flight")
	}
	if m.groups["remote:h2"].inFlight {
		t.Fatalf("unselected remote host in flight")
	}
	if !m.items[1].logLoading {
		t.Fatalf("selected remote log not marked loading")
	}
}

func TestRefreshAllGroupsStartsOnlyVisibleGroups(t *testing.T) {
	m := newModel("/tmp/project", []session.Spec{
		{Name: "local", Backend: "tmux", Command: session.Command{Argv: []string{"true"}}},
		remoteSpec("remote-a", "h1"),
		remoteSpec("remote-b", "h2"),
	}, session.RuntimeOptions{})

	m, _ = m.refreshAllGroups(true)

	for _, key := range []string{localGroupKey, "remote:h1", "remote:h2"} {
		if !m.groups[key].inFlight {
			t.Fatalf("group %s not in flight", key)
		}
	}
	if len(m.groups) != 3 {
		t.Fatalf("groups = %+v, want only visible groups", m.groups)
	}
}

func TestApplySnapshotIgnoresStaleLogResponse(t *testing.T) {
	m := newModel("/tmp/project", []session.Spec{remoteSpec("remote-a", "h1")}, session.RuntimeOptions{})
	group := m.groups["remote:h1"]
	group.seq = 2
	group.inFlight = true

	m, _ = m.applySnapshot(snapshotMsg{
		key: "remote:h1",
		seq: 1,
		resp: daemon.Response{
			OK:      true,
			LogName: "remote-a",
			Log:     "old\n",
		},
	})

	if m.items[0].log != "" {
		t.Fatalf("stale log updated item log to %q", m.items[0].log)
	}
	if !group.inFlight {
		t.Fatalf("stale response cleared current in-flight request")
	}
}

func TestApplySnapshotUpdatesStatusAndSelectedLog(t *testing.T) {
	m := newModel("/tmp/project", []session.Spec{remoteSpec("remote-a", "h1")}, session.RuntimeOptions{})
	group := m.groups["remote:h1"]
	group.seq = 1
	group.inFlight = true
	m.items[0].logLoading = true

	m, _ = m.applySnapshot(snapshotMsg{
		key: "remote:h1",
		seq: 1,
		resp: daemon.Response{
			OK:       true,
			Statuses: []session.Status{{Name: "remote-a", State: session.StateRunning}},
			LogName:  "remote-a",
			Log:      "fresh\n",
		},
	})

	if group.inFlight {
		t.Fatalf("group still in flight")
	}
	if m.items[0].status.State != session.StateRunning {
		t.Fatalf("status = %+v, want running", m.items[0].status)
	}
	if m.items[0].log != "fresh\n" || m.items[0].logLoading {
		t.Fatalf("item log = %q loading=%v, want fresh not loading", m.items[0].log, m.items[0].logLoading)
	}
}

func remoteSpec(name, host string) session.Spec {
	return session.Spec{
		Name:    name,
		Backend: "ssh-tmux",
		Host:    host,
		Command: session.Command{Argv: []string{"true"}},
	}
}
