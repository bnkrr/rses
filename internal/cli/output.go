package cli

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/bnkrr/rses/internal/runtime/tmux"
	"github.com/bnkrr/rses/internal/session"
	"github.com/bnkrr/rses/internal/tablefmt"
)

const (
	listNameWidth    = 24
	listBackendWidth = 10
	listHostWidth    = 18
	listGroupWidth   = 16
	listTagsWidth    = 18
	listSummaryWidth = 48

	statusNameWidth    = 24
	statusStateWidth   = 10
	statusPIDWidth     = 8
	statusUptimeWidth  = 7
	statusSummaryWidth = 64

	defaultTagLimit = 2
)

type listColumns struct {
	Name    int
	Backend int
	Host    int
	Group   int
	Tags    int
	Summary int
}

var defaultListColumns = listColumns{
	Name:    listNameWidth,
	Backend: listBackendWidth,
	Host:    listHostWidth,
	Group:   listGroupWidth,
	Tags:    listTagsWidth,
	Summary: listSummaryWidth,
}

func printSpecs(specs []session.Spec, wide bool) {
	columns := listColumnWidths(specs, wide)
	fmt.Println(formatListHeader(columns))
	for _, spec := range specs {
		fmt.Println(formatListRowWithColumns(spec, wide, columns))
	}
}

func printStatuses(statuses []session.Status, wide bool) {
	printStatusHeader(wide)
	for _, st := range statuses {
		printStatusRow(st, wide)
	}
}

func printStatusHeader(wide bool) {
	_ = wide
	fmt.Println(formatStatusHeader())
}

func printStatusRow(st session.Status, wide bool) {
	fmt.Println(formatStatusRow(st, wide))
	if st.LastError != "" {
		fmt.Printf("  error: %s\n", st.LastError)
	}
}

func formatListHeader(columns listColumns) string {
	return joinColumns(
		tablefmt.PadRight("NAME", columns.Name),
		tablefmt.PadRight("BACKEND", columns.Backend),
		tablefmt.PadRight("HOST", columns.Host),
		tablefmt.PadRight("GROUP", columns.Group),
		tablefmt.PadRight("TAGS", columns.Tags),
		"SUMMARY",
	)
}

func formatListRow(spec session.Spec, wide bool) string {
	return formatListRowWithColumns(spec, wide, defaultListColumns)
}

func formatListRowWithColumns(spec session.Spec, wide bool, columns listColumns) string {
	name := spec.Name
	host := spec.Host
	group := spec.Group
	tags := strings.Join(spec.Tags, ",")
	summary := spec.Summary
	if !wide {
		name = tablefmt.Truncate(name, columns.Name)
		host = tablefmt.Truncate(host, columns.Host)
		group = tablefmt.Truncate(group, columns.Group)
		tags = compactTagsForColumn(spec.Tags, defaultTagLimit, columns.Tags)
		summary = tablefmt.Truncate(summary, columns.Summary)
	}
	return joinColumns(
		tablefmt.PadRight(name, columns.Name),
		tablefmt.PadRight(spec.Kind(), columns.Backend),
		tablefmt.PadRight(host, columns.Host),
		tablefmt.PadRight(group, columns.Group),
		tablefmt.PadRight(tags, columns.Tags),
		summary,
	)
}

func listColumnWidths(specs []session.Spec, wide bool) listColumns {
	columns := defaultListColumns
	if !wide {
		return columns
	}
	for _, spec := range specs {
		columns.Name = maxInt(columns.Name, displayWidth(spec.Name))
		columns.Backend = maxInt(columns.Backend, displayWidth(spec.Kind()))
		columns.Host = maxInt(columns.Host, displayWidth(spec.Host))
		columns.Group = maxInt(columns.Group, displayWidth(spec.Group))
		columns.Tags = maxInt(columns.Tags, displayWidth(strings.Join(spec.Tags, ",")))
	}
	return columns
}

func formatStatusHeader() string {
	return joinColumns(
		tablefmt.PadRight("NAME", statusNameWidth),
		tablefmt.PadRight("STATE", statusStateWidth),
		tablefmt.PadRight("PID", statusPIDWidth),
		tablefmt.PadRight("UPTIME", statusUptimeWidth),
		"SUMMARY",
	)
}

func formatStatusRow(st session.Status, wide bool) string {
	name := st.Name
	summary := statusSummary(st)
	if !wide {
		name = tablefmt.Truncate(name, statusNameWidth)
		summary = tablefmt.Truncate(summary, statusSummaryWidth)
	}
	return joinColumns(
		tablefmt.PadRight(name, statusNameWidth),
		tablefmt.PadRight(string(st.State), statusStateWidth),
		tablefmt.PadRight(fmt.Sprintf("%d", st.PID), statusPIDWidth),
		tablefmt.PadRight(uptime(st), statusUptimeWidth),
		summary,
	)
}

func statusSummary(st session.Status) string {
	if st.State == session.StateUnknown && strings.TrimSpace(st.LastError) != "" {
		return oneLine(st.LastError)
	}
	return st.Summary
}

func oneLine(value string) string {
	return strings.Join(strings.Fields(value), " ")
}

func joinColumns(columns ...string) string {
	return strings.Join(columns, " ")
}

func compactTagsForColumn(tags []string, limit, width int) string {
	if len(tags) == 0 {
		return ""
	}
	if limit > len(tags) {
		limit = len(tags)
	}
	for n := limit; n > 0; n-- {
		value := tablefmt.CompactTags(tags, n)
		if tablefmt.Truncate(value, width) == value {
			return value
		}
	}
	value := tablefmt.CompactTags(tags, 0)
	if tablefmt.Truncate(value, width) == value {
		return value
	}
	return tablefmt.Truncate(value, width)
}

func displayWidth(value string) int {
	width := tablefmt.DisplayWidth(value)
	if width < 0 {
		return 0
	}
	return width
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func printResults(results map[string]string) {
	names := make([]string, 0, len(results))
	for name := range results {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		fmt.Printf("%s: %s\n", name, results[name])
	}
}

func printExplain(w io.Writer, projectDir string, spec session.Spec) {
	stateRoot := tmux.RemoteStateRoot
	if !spec.IsRemote() {
		stateRoot = daemonLocalStateRoot()
	}
	plan, err := tmux.NewPlan(spec, projectDir, stateRoot, 200)
	if err != nil {
		fmt.Fprintf(w, "error: %s\n", err)
		return
	}
	fmt.Fprintf(w, "Session: %s\n", spec.Name)
	fmt.Fprintf(w, "Backend: %s\n", spec.Kind())
	if spec.Host != "" {
		fmt.Fprintf(w, "Host: %s\n", spec.Host)
	}
	fmt.Fprintf(w, "Tmux scope: %s\n", plan.Scope)
	fmt.Fprintf(w, "Tmux window: %s\n", plan.Window)
	if spec.Group != "" {
		fmt.Fprintf(w, "Group: %s\n", spec.Group)
	}
	if len(spec.Tags) > 0 {
		fmt.Fprintf(w, "Tags: %s\n", strings.Join(spec.Tags, ","))
	}
	if spec.Summary != "" {
		fmt.Fprintf(w, "Summary: %s\n", spec.Summary)
	}
	fmt.Fprintln(w)
	fmt.Fprintf(w, "State dir: %s\n", plan.StateDir)
	fmt.Fprintf(w, "Run script: %s\n", plan.RunPath)
	fmt.Fprintf(w, "Log path: %s\n", plan.LogPath)
	fmt.Fprintf(w, "Record: %s\n", plan.RecordPath)
	fmt.Fprintf(w, "Attach: %s\n", tmux.CommandLine(plan.AttachCommand))
	fmt.Fprintln(w)
	fmt.Fprintln(w, "run.sh:")
	writeIndentedBlock(w, plan.RunScript)
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Status script:")
	writeIndentedBlock(w, plan.StatusScript)
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Install script:")
	writeIndentedBlock(w, plan.InstallScript)
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Start script:")
	writeIndentedBlock(w, plan.StartScript)
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Logs script:")
	writeIndentedBlock(w, plan.LogScript)
}

func daemonLocalStateRoot() string {
	if v := os.Getenv("RSES_STATE_DIR"); v != "" {
		return v
	}
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return filepath.Join(os.TempDir(), "rses")
	}
	return filepath.Join(home, ".local", "state", "rses")
}

func writeIndentedBlock(w io.Writer, text string) {
	text = strings.TrimRight(text, "\n")
	if text == "" {
		fmt.Fprintln(w, "  <empty>")
		return
	}
	for _, line := range strings.Split(text, "\n") {
		fmt.Fprintf(w, "  %s\n", line)
	}
}

func uptime(st session.Status) string {
	if st.StartedAt.IsZero() || st.State != session.StateRunning {
		return "-"
	}
	return time.Since(st.StartedAt).Round(time.Second).String()
}

func followFile(path string, out io.Writer) error {
	path = filepath.Clean(path)
	var offset int64
	for {
		file, err := os.Open(path)
		if err != nil {
			if os.IsNotExist(err) {
				time.Sleep(500 * time.Millisecond)
				continue
			}
			return err
		}
		if _, err := file.Seek(offset, io.SeekStart); err != nil {
			_ = file.Close()
			return err
		}
		n, err := io.Copy(out, file)
		_ = file.Close()
		offset += n
		if err != nil {
			return err
		}
		time.Sleep(500 * time.Millisecond)
	}
}
