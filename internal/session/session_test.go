package session

import "testing"

func TestSpecValidateRequiresBackend(t *testing.T) {
	spec := Spec{Name: "demo"}
	if err := spec.Validate(); err == nil {
		t.Fatal("expected validation error")
	}
}

func TestTmuxValidateUsesCommandArgv(t *testing.T) {
	spec := Spec{
		Name:    "local",
		Backend: "tmux",
		Command: Command{Argv: []string{"sleep", "10"}},
	}
	if err := spec.Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestSSHTmuxValidateUsesCommandArgv(t *testing.T) {
	spec := Spec{
		Name:    "remote",
		Backend: "ssh-tmux",
		Host:    "ssh-dev",
		Command: Command{Argv: []string{"sleep", "10"}},
	}
	if err := spec.Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestLegacyRemoteTmuxTypeIsRejected(t *testing.T) {
	spec := Spec{
		Name:    "remote",
		Host:    "ssh-dev",
		Command: Command{Argv: []string{"sleep", "10"}},
	}
	if spec.Kind() != "" || spec.IsRemote() {
		t.Fatalf("kind = %q remote = %v, want empty local-unknown", spec.Kind(), spec.IsRemote())
	}
	if err := spec.Validate(); err == nil {
		t.Fatal("expected validation error")
	}
}

func TestSSHTmuxValidateRequiresHost(t *testing.T) {
	spec := Spec{
		Name:    "remote",
		Backend: "ssh-tmux",
		Command: Command{Argv: []string{"sleep", "10"}},
	}
	if err := spec.Validate(); err == nil {
		t.Fatal("expected validation error")
	}
}

func TestSSHTmuxValidateRequiresAbsoluteCwd(t *testing.T) {
	spec := Spec{
		Name:    "remote",
		Backend: "ssh-tmux",
		Host:    "ssh-dev",
		Command: Command{Cwd: "relative", Argv: []string{"sleep", "10"}},
	}
	if err := spec.Validate(); err == nil {
		t.Fatal("expected validation error")
	}
}

func TestSpecValidateRejectsInvalidEnvName(t *testing.T) {
	spec := Spec{
		Name:    "demo",
		Backend: "tmux",
		Command: Command{Argv: []string{"sleep", "10"}, Env: map[string]string{"BAD-NAME": "x"}},
	}
	if err := spec.Validate(); err == nil {
		t.Fatal("expected validation error")
	}
}

func TestMatchesTags(t *testing.T) {
	spec := Spec{Name: "demo", Tags: []string{"db", "dev"}}
	if !spec.MatchesTags([]string{"db"}) {
		t.Fatal("expected single tag match")
	}
	if !spec.MatchesTags([]string{"db", "dev"}) {
		t.Fatal("expected all tags match")
	}
	if spec.MatchesTags([]string{"db", "web"}) {
		t.Fatal("unexpected tag match")
	}
}

func TestSafeLogName(t *testing.T) {
	got := SafeLogName("../a/b:c")
	if got != "a_b_c" {
		t.Fatalf("SafeLogName() = %q", got)
	}
}
