package cli

import (
	"bytes"
	"flag"
	"io"
	"strings"
	"testing"

	"github.com/bnkrr/rses/internal/session"
)

func TestParseInterspersedAllowsFlagsAfterNames(t *testing.T) {
	fs := flag.NewFlagSet("start", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	noWatch := fs.Bool("no-watch", false, "")
	group := fs.String("group", "", "")

	if err := parseInterspersed(fs, []string{"demo", "--no-watch", "--group", "home"}, "group"); err != nil {
		t.Fatal(err)
	}
	if !*noWatch {
		t.Fatal("no-watch = false, want true")
	}
	if *group != "home" {
		t.Fatalf("group = %q, want home", *group)
	}
	if got := fs.Args(); len(got) != 1 || got[0] != "demo" {
		t.Fatalf("args = %+v, want [demo]", got)
	}
}

func TestParseGlobalConfigDirectory(t *testing.T) {
	for _, args := range [][]string{{"-C", "/tmp/project", "list"}, {"-C=/tmp/project", "list"}} {
		dir, rest, err := parseGlobalOptions(args)
		if err != nil {
			t.Fatal(err)
		}
		if dir != "/tmp/project" || strings.Join(rest, " ") != "list" {
			t.Fatalf("parseGlobalOptions(%q) = %q, %q", args, dir, rest)
		}
	}
}

func TestParseGlobalConfigDirectoryRequiresValue(t *testing.T) {
	for _, args := range [][]string{{"-C"}, {"-C="}} {
		if _, _, err := parseGlobalOptions(args); err == nil {
			t.Fatalf("parseGlobalOptions(%q) succeeded", args)
		}
	}
}

func TestParseInterspersedAllowsRepeatedTags(t *testing.T) {
	fs := flag.NewFlagSet("list", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	var tags tagValues
	fs.Var(&tags, "tag", "")

	if err := parseInterspersed(fs, []string{"demo", "--tag", "ssh", "--tag", "tunnel,home"}, "tag"); err != nil {
		t.Fatal(err)
	}
	want := []string{"ssh", "tunnel", "home"}
	if strings.Join(tags.Values(), ",") != strings.Join(want, ",") {
		t.Fatalf("tags = %+v, want %+v", tags.Values(), want)
	}
	if got := fs.Args(); len(got) != 1 || got[0] != "demo" {
		t.Fatalf("args = %+v, want [demo]", got)
	}
}

func TestParseInterspersedAllowsWideAfterName(t *testing.T) {
	fs := flag.NewFlagSet("list", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	wide := fs.Bool("wide", false, "")

	if err := parseInterspersed(fs, []string{"demo", "--wide"}, "wide"); err != nil {
		t.Fatal(err)
	}
	if !*wide {
		t.Fatal("wide = false, want true")
	}
	if got := fs.Args(); len(got) != 1 || got[0] != "demo" {
		t.Fatalf("args = %+v, want [demo]", got)
	}
}

func TestFormatListRowCompactsTagsByDefault(t *testing.T) {
	spec := session.Spec{
		Name:    "galileo-agent-opencode-with-long-name",
		Backend: "tmux",
		Group:   "galileo-agent-with-long-group",
		Tags:    []string{"local", "opencode", "ssh", "tunnel"},
		Summary: strings.Repeat("summary-", 20),
	}

	row := formatListRow(spec, false)

	for _, want := range []string{"tmux", "local,opencode,+2", "..."} {
		if !strings.Contains(row, want) {
			t.Fatalf("row = %q, missing %q", row, want)
		}
	}
	if strings.Contains(row, "ssh,tunnel") {
		t.Fatalf("row = %q, want compact tags", row)
	}
}

func TestFormatListRowPreservesHiddenTagCount(t *testing.T) {
	spec := session.Spec{
		Name:    "galileo-agent-opencode",
		Backend: "tmux",
		Tags:    []string{"local", "galileo-agent", "opencode"},
	}

	row := formatListRow(spec, false)

	if !strings.Contains(row, "local,+2") {
		t.Fatalf("row = %q, want compact tags preserving hidden count", row)
	}
	if strings.Contains(row, "local,galileo-a...") {
		t.Fatalf("row = %q, hidden tag count was truncated", row)
	}
}

func TestFormatListRowWideShowsFullTagsAndSummary(t *testing.T) {
	spec := session.Spec{
		Name:    "wide-demo",
		Backend: "tmux",
		Tags:    []string{"local", "opencode", "ssh", "tunnel"},
		Summary: strings.Repeat("summary-", 12),
	}

	row := formatListRow(spec, true)

	if !strings.Contains(row, "local,opencode,ssh,tunnel") {
		t.Fatalf("row = %q, want full tags", row)
	}
	if !strings.Contains(row, spec.Summary) {
		t.Fatalf("row = %q, want full summary %q", row, spec.Summary)
	}
}

func TestFormatListWideColumnsExpandForLongTags(t *testing.T) {
	specs := []session.Spec{
		{
			Name:    "wide-demo",
			Backend: "tmux",
			Tags:    []string{"local", "galileo-agent", "opencode"},
			Summary: "summary text",
		},
	}
	columns := listColumnWidths(specs, true)

	if columns.Tags < len("local,galileo-agent,opencode") {
		t.Fatalf("tags width = %d, want enough room for full tags", columns.Tags)
	}
	row := formatListRowWithColumns(specs[0], true, columns)
	if !strings.Contains(row, "local,galileo-agent,opencode summary text") {
		t.Fatalf("row = %q, want separated full tags before summary", row)
	}
}

func TestFormatStatusRowWideShowsFullSummary(t *testing.T) {
	status := session.Status{
		Name:    "status-demo",
		State:   session.StateRunning,
		Summary: strings.Repeat("status-summary-", 10),
	}

	defaultRow := formatStatusRow(status, false)
	wideRow := formatStatusRow(status, true)

	if !strings.Contains(defaultRow, "...") {
		t.Fatalf("default row = %q, want truncated summary", defaultRow)
	}
	if !strings.Contains(wideRow, status.Summary) {
		t.Fatalf("wide row = %q, want full summary %q", wideRow, status.Summary)
	}
}

func TestFormatStatusRowShowsUnknownLastError(t *testing.T) {
	status := session.Status{
		Name:      "remote",
		State:     session.StateUnknown,
		Summary:   "configured summary",
		LastError: "ssh host timed out\ntry again",
	}

	row := formatStatusRow(status, false)

	if !strings.Contains(row, "ssh host timed out try again") {
		t.Fatalf("row = %q, want last error", row)
	}
	if strings.Contains(row, "configured summary") {
		t.Fatalf("row = %q, want error instead of configured summary", row)
	}
}

func TestPrintExplainIncludesDebuggingDetails(t *testing.T) {
	spec := session.Spec{
		Name:    "remote-web",
		Backend: "ssh-tmux",
		Host:    "ssh-dev",
		Command: session.Command{Argv: []string{"sh", "-lc", "npm run dev"}},
	}

	var out bytes.Buffer
	printExplain(&out, "/tmp/config-root", spec)

	for _, want := range []string{
		"Session: remote-web",
		"Backend: ssh-tmux",
		"Tmux scope: rses-",
		"Tmux window: remote-web",
		"State dir: $HOME/.local/state/rses/scopes/",
		"Attach: 'ssh' '-t' 'ssh-dev' 'tmux' 'attach' '-t' 'rses-",
		"run.sh:",
		"exec 'sh' '-lc' 'npm run dev'",
		"Install script:",
		"Start script:",
		"Logs script:",
	} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("explain output = %q, missing %q", out.String(), want)
		}
	}
}
