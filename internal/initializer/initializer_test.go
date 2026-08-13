package initializer

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/bnkrr/rses/internal/config"
	providerpkg "github.com/bnkrr/rses/internal/provider"
)

func TestInitializeWritesLoadableBundle(t *testing.T) {
	target := filepath.Join(t.TempDir(), "config")
	got, err := Initialize(target)
	if err != nil {
		t.Fatal(err)
	}
	if got != target {
		t.Fatalf("target = %q, want %q", got, target)
	}
	specs, err := config.LoadProject(target)
	if err != nil {
		t.Fatal(err)
	}
	if len(specs) != 0 {
		t.Fatalf("specs = %+v, want empty config", specs)
	}
	for _, name := range []string{"ssh-tunnel", "cloudflared-tcp"} {
		if _, err := providerpkg.LoadCommand(filepath.Join(target, "providers", name)); err != nil {
			t.Fatalf("provider %s: %v", name, err)
		}
	}
}

func TestInitializeRefusesExistingBundleFiles(t *testing.T) {
	for _, existing := range []string{"rses.yaml", "providers"} {
		t.Run(existing, func(t *testing.T) {
			target := t.TempDir()
			path := filepath.Join(target, existing)
			if existing == "providers" {
				if err := os.Mkdir(path, 0o755); err != nil {
					t.Fatal(err)
				}
			} else if err := os.WriteFile(path, []byte("keep\n"), 0o644); err != nil {
				t.Fatal(err)
			}
			_, err := Initialize(target)
			if err == nil || !strings.Contains(err.Error(), "refusing to overwrite") {
				t.Fatalf("error = %v, want overwrite refusal", err)
			}
			if _, err := os.Stat(filepath.Join(target, "providers", "ssh-tunnel")); err == nil {
				t.Fatal("partial provider bundle was written")
			}
		})
	}
}

func TestDefaultDirUsesUserConfigDir(t *testing.T) {
	configHome := filepath.Join(t.TempDir(), "xdg")
	t.Setenv("XDG_CONFIG_HOME", configHome)
	got, err := DefaultDir()
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(configHome, "rses")
	if got != want {
		t.Fatalf("default dir = %q, want %q", got, want)
	}
}
