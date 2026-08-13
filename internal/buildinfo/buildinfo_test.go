package buildinfo

import "testing"

func TestCurrentPrefersInjectedVersion(t *testing.T) {
	previous := Version
	Version = "v0.1.0-test"
	t.Cleanup(func() { Version = previous })

	if got := Current(); got != "v0.1.0-test" {
		t.Fatalf("Current() = %q, want injected version", got)
	}
}
