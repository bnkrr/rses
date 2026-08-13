package tablefmt

import "testing"

func TestCompactTags(t *testing.T) {
	cases := []struct {
		name string
		tags []string
		max  int
		want string
	}{
		{name: "empty", tags: nil, max: 2, want: ""},
		{name: "within limit", tags: []string{"local", "opencode"}, max: 2, want: "local,opencode"},
		{name: "overflow", tags: []string{"local", "opencode", "ssh", "tunnel"}, max: 2, want: "local,opencode,+2"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := CompactTags(tc.tags, tc.max); got != tc.want {
				t.Fatalf("CompactTags(%v, %d) = %q, want %q", tc.tags, tc.max, got, tc.want)
			}
		})
	}
}

func TestTruncate(t *testing.T) {
	if got := Truncate("short", 10); got != "short" {
		t.Fatalf("short truncate = %q", got)
	}
	if got := Truncate("abcdefghijklmnopqrstuvwxyz", 10); got != "abcdefg..." {
		t.Fatalf("long truncate = %q, want abcdefg...", got)
	}
	if got := Truncate("abcdef", 2); got != ".." {
		t.Fatalf("tiny truncate = %q, want ..", got)
	}
	if got := Truncate("👩‍💻abcdef", 6); got != "👩‍💻a..." {
		t.Fatalf("grapheme truncate = %q, want %q", got, "👩‍💻a...")
	}
}
