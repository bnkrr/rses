package provider

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGenerateCommandLabelsStdoutAndStderrOnFailure(t *testing.T) {
	root := t.TempDir()
	writeProvider(t, root, "failing", `#!/bin/sh
echo 'partial output'
echo 'failure detail' >&2
exit 7
`)

	_, err := GenerateCommand(root, "demo", "failing", map[string]any{})
	if err == nil {
		t.Fatal("expected provider failure")
	}
	for _, want := range []string{"stderr:\nfailure detail", "stdout:\npartial output"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("error %q does not contain %q", err, want)
		}
	}
}

func TestGenerateCommandRejectsForbiddenOutputFields(t *testing.T) {
	root := t.TempDir()
	writeProvider(t, root, "bad-output", `#!/bin/sh
echo '{"backend":"tmux","command":{"argv":["sleep","10"]}}'
`)

	_, err := GenerateCommand(root, "demo", "bad-output", map[string]any{})
	if err == nil || !strings.Contains(err.Error(), `forbidden field "backend"`) {
		t.Fatalf("error = %v, want forbidden field", err)
	}
}

func writeProvider(t *testing.T, root, name, script string) {
	t.Helper()
	dir := filepath.Join(root, "providers", name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "provider.json"), []byte(`{"command":["sh","provider.sh"]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "provider.sh"), []byte(script), 0o644); err != nil {
		t.Fatal(err)
	}
}
