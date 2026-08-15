package daemon

import (
	"context"
	"sort"
	"strconv"
	"strings"

	"github.com/bnkrr/rses/internal/runtime/tmux"
	"github.com/bnkrr/rses/internal/session"
)

const (
	watchWindowsMarker = "__RSES_WATCH_WINDOWS__"
	watchLogMarker     = "__RSES_WATCH_LOG__"
)

type watchGroupResult struct {
	statuses []session.Status
	logName  string
	log      string
	logErr   string
}

func (s *Server) watchSnapshot(req Request) Response {
	if !req.IncludeConfigured {
		return s.watchSnapshotRecords(req)
	}
	groups := groupWatchSpecs(req.Specs, req.Host)
	if len(groups) == 0 {
		return Response{OK: true}
	}

	ctx := context.Background()
	resultCh := make(chan watchGroupResult, len(groups))
	for _, specs := range groups {
		specs := specs
		go func() {
			resultCh <- watchSnapshotGroup(ctx, req.ProjectDir, specs, req.SelectedName, req.Lines, req.Runtime)
		}()
	}

	statuses := []session.Status{}
	logName := ""
	log := ""
	logErr := ""
	for range groups {
		res := <-resultCh
		statuses = append(statuses, res.statuses...)
		if res.logName != "" {
			logName = res.logName
			log = res.log
			logErr = res.logErr
		}
	}
	sortStatuses(statuses)
	return Response{OK: true, Statuses: statuses, LogName: logName, Log: log, LogError: logErr}
}

func (s *Server) watchSnapshotRecords(req Request) Response {
	records, err := matchingRecords(req)
	if err != nil {
		return Response{OK: false, Error: err.Error()}
	}
	groupsByProject := map[string][]session.Spec{}
	startedByName := map[string]*tmux.RuntimeRecord{}
	projects := []string{}
	for _, record := range records {
		spec := specFromRecord(record)
		projectDir := record.ProjectDir
		record := record
		startedByName[record.Name] = &record
		if _, ok := groupsByProject[projectDir]; !ok {
			projects = append(projects, projectDir)
		}
		groupsByProject[projectDir] = append(groupsByProject[projectDir], spec)
	}
	sort.Strings(projects)
	if len(projects) == 0 {
		return Response{OK: true}
	}

	ctx := context.Background()
	resultCh := make(chan watchGroupResult, len(projects))
	for _, projectDir := range projects {
		specs := groupsByProject[projectDir]
		go func(projectDir string) {
			resultCh <- watchSnapshotGroup(ctx, projectDir, specs, req.SelectedName, req.Lines, req.Runtime)
		}(projectDir)
	}

	statuses := []session.Status{}
	logName := ""
	log := ""
	logErr := ""
	for range projects {
		res := <-resultCh
		statuses = append(statuses, res.statuses...)
		if res.logName != "" {
			logName = res.logName
			log = res.log
			logErr = res.logErr
		}
	}
	for i := range statuses {
		if record := startedByName[statuses[i].Name]; record != nil && record.StartedAt != nil {
			statuses[i].StartedAt = *record.StartedAt
		}
	}
	sortStatuses(statuses)
	return Response{OK: true, Statuses: statuses, LogName: logName, Log: log, LogError: logErr}
}

func groupWatchSpecs(specs []session.Spec, host string) [][]session.Spec {
	groupsByKey := map[string][]session.Spec{}
	keys := []string{}
	for _, spec := range specs {
		if host != "" && (!spec.IsRemote() || spec.Host != host) {
			continue
		}
		key := "local"
		if spec.IsRemote() {
			key = "remote:" + spec.Host
		}
		if _, ok := groupsByKey[key]; !ok {
			keys = append(keys, key)
		}
		groupsByKey[key] = append(groupsByKey[key], spec)
	}
	sort.Strings(keys)
	groups := make([][]session.Spec, 0, len(keys))
	for _, key := range keys {
		groups = append(groups, groupsByKey[key])
	}
	return groups
}

func watchSnapshotGroup(ctx context.Context, projectDir string, specs []session.Spec, selectedName string, lines int, runtime session.RuntimeOptions) watchGroupResult {
	plans := make([]tmux.Plan, 0, len(specs))
	statuses := make([]session.Status, 0, len(specs))
	for _, spec := range specs {
		plan, err := tmuxPlan(projectDir, spec, lines)
		if err != nil {
			statuses = append(statuses, session.Status{
				Name:      spec.Name,
				Group:     spec.Group,
				Tags:      spec.Tags,
				Summary:   spec.Summary,
				State:     session.StateUnknown,
				LastError: err.Error(),
			})
			continue
		}
		plans = append(plans, plan)
	}
	if len(plans) == 0 {
		return watchGroupResult{statuses: statuses}
	}

	script := watchSnapshotScript(plans, selectedName, lines)
	out, err := runQueryPlan(ctx, plans[0].Spec, script, runtime)
	if err != nil {
		msg := trimError(out, err)
		for _, plan := range plans {
			statuses = append(statuses, watchStatusFromPlan(plan, session.StateUnknown, msg))
		}
		res := watchGroupResult{statuses: statuses}
		if selectedName != "" && containsPlan(plans, selectedName) {
			res.logName = selectedName
			res.logErr = msg
		}
		return res
	}

	windows, log := parseWatchSnapshotOutput(out)
	for _, plan := range plans {
		state := session.StateStopped
		if windows[plan.Window] {
			state = session.StateRunning
		}
		statuses = append(statuses, watchStatusFromPlan(plan, state, ""))
	}
	res := watchGroupResult{statuses: statuses}
	if selectedName != "" && containsPlan(plans, selectedName) {
		res.logName = selectedName
		res.log = log
	}
	return res
}

func watchStatusFromPlan(plan tmux.Plan, state session.State, lastErr string) session.Status {
	return session.Status{
		Name:      plan.Spec.Name,
		Group:     plan.Spec.Group,
		Tags:      plan.Spec.Tags,
		Summary:   plan.Spec.Summary,
		State:     state,
		LogPath:   plan.LogPath,
		LastError: lastErr,
	}
}

func watchSnapshotScript(plans []tmux.Plan, selectedName string, lines int) string {
	if lines <= 0 {
		lines = 200
	}
	scope := plans[0].Scope
	var selected *tmux.Plan
	for i := range plans {
		if plans[i].Spec.Name == selectedName {
			selected = &plans[i]
			break
		}
	}

	var b strings.Builder
	b.WriteString("command -v tmux >/dev/null || { echo 'tmux not found' >&2; exit 127; }\n")
	b.WriteString("printf '\\000%s\\000' " + tmux.ShellQuote(watchWindowsMarker) + "\n")
	b.WriteString("tmux list-windows -t " + tmux.ShellQuote(scope) + " -F '#{window_name}' 2>/dev/null || true\n")
	b.WriteString("printf '\\000%s\\000' " + tmux.ShellQuote(watchLogMarker) + "\n")
	if selected != nil {
		b.WriteString("if [ -f " + tmux.ShellQuotePath(selected.LogPath) + " ]; then\n")
		b.WriteString("  tail -n ")
		b.WriteString(intString(lines))
		b.WriteByte(' ')
		b.WriteString(tmux.ShellQuotePath(selected.LogPath))
		b.WriteString("\nelse\n")
		b.WriteString("  tmux capture-pane -p -t " + tmux.ShellQuote(selected.Target) + " -S -")
		b.WriteString(intString(lines))
		b.WriteString(" 2>/dev/null || true\n")
		b.WriteString("fi\n")
	}
	return b.String()
}

func parseWatchSnapshotOutput(out string) (map[string]bool, string) {
	windows := map[string]bool{}
	windowsFrame := watchFrame(watchWindowsMarker)
	logFrame := watchFrame(watchLogMarker)
	windowsStart := strings.Index(out, windowsFrame)
	if windowsStart < 0 {
		return windows, ""
	}
	windowsStart += len(windowsFrame)
	logOffset := strings.Index(out[windowsStart:], logFrame)
	if logOffset < 0 {
		return windows, ""
	}
	for _, line := range strings.Split(out[windowsStart:windowsStart+logOffset], "\n") {
		name := strings.TrimRight(line, "\r")
		if name != "" {
			windows[name] = true
		}
	}
	logStart := windowsStart + logOffset + len(logFrame)
	return windows, out[logStart:]
}

func watchFrame(marker string) string {
	return "\x00" + marker + "\x00"
}

func containsPlan(plans []tmux.Plan, name string) bool {
	for _, plan := range plans {
		if plan.Spec.Name == name {
			return true
		}
	}
	return false
}

func intString(value int) string {
	if value <= 0 {
		value = 200
	}
	return strconv.Itoa(value)
}
