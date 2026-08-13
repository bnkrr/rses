package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/mattn/go-runewidth"

	"github.com/bnkrr/rses/internal/daemon"
	"github.com/bnkrr/rses/internal/session"
	"github.com/bnkrr/rses/internal/tablefmt"
)

type tickMsg time.Time

const (
	localGroupKey       = "local"
	watchTickInterval   = 250 * time.Millisecond
	localRefreshTicks   = 4
	remoteRefreshTicks  = 20
	defaultLogLineCount = 200
)

type watchItem struct {
	spec       session.Spec
	status     session.Status
	log        string
	logErr     string
	logLoading bool
}

type watchGroup struct {
	key      string
	host     string
	remote   bool
	inFlight bool
	pending  bool
	seq      int
	spinner  int
}

type model struct {
	projectDir        string
	items             []watchItem
	groups            map[string]*watchGroup
	selected          int
	width             int
	height            int
	logView           viewport.Model
	message           string
	includeConfigured bool
	runtime           session.RuntimeOptions
	ticks             int
}

func Run(projectDir string, all []session.Spec, selected []session.Spec, runtime session.RuntimeOptions) error {
	m := newModel(projectDir, selected, runtime)
	p := tea.NewProgram(m, tea.WithAltScreen())
	_, err := p.Run()
	return err
}

func newModel(projectDir string, selected []session.Spec, runtime session.RuntimeOptions) model {
	m := model{
		projectDir:        projectDir,
		items:             make([]watchItem, 0, len(selected)),
		groups:            map[string]*watchGroup{},
		logView:           viewport.New(80, 20),
		includeConfigured: projectDir != "",
		runtime:           runtime,
	}
	for _, spec := range selected {
		m.items = append(m.items, watchItem{
			spec: spec,
			status: session.Status{
				Name:    spec.Name,
				Group:   spec.Group,
				Tags:    spec.Tags,
				Summary: spec.Summary,
				State:   session.StateUnknown,
			},
		})
		m.ensureGroup(spec)
	}
	return m
}

func (m model) Init() tea.Cmd {
	return tea.Batch(tick(), func() tea.Msg { return initialRefreshMsg{} })
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.logView.Width = max(20, msg.Width)
		m.logView.Height = max(5, msg.Height-len(m.items)-6)
		m.showSelectedLog()
		return m, nil
	case initialRefreshMsg:
		return m.refreshActiveGroups(true)
	case tea.KeyMsg:
		switch msg.String() {
		case "q", "ctrl+c", "esc":
			return m, tea.Quit
		case "up", "k":
			if m.selected > 0 {
				m.selected--
			}
			m.showSelectedLog()
			return m.refreshActiveGroups(true)
		case "down", "j":
			if m.selected < len(m.items)-1 {
				m.selected++
			}
			m.showSelectedLog()
			return m.refreshActiveGroups(true)
		case "s":
			return m, m.startSelected()
		case "x":
			return m, m.stopSelected()
		case "r":
			return m.refreshAllGroups(true)
		}
	case tickMsg:
		m.ticks++
		m.advanceSpinners()
		cmds := []tea.Cmd{tick()}
		if m.ticks%localRefreshTicks == 0 {
			var cmd tea.Cmd
			m, cmd = m.refreshGroup(localGroupKey, false)
			cmds = append(cmds, cmd)
		}
		if m.selectedItem().spec.IsRemote() && m.ticks%remoteRefreshTicks == 0 {
			var cmd tea.Cmd
			m, cmd = m.refreshGroup(groupKey(m.selectedItem().spec), false)
			cmds = append(cmds, cmd)
		}
		return m, tea.Batch(cmds...)
	case snapshotMsg:
		return m.applySnapshot(msg)
	case operationStreamMsg:
		if msg.err != nil {
			m.message = msg.err.Error()
			return m, nil
		}
		if msg.done {
			return m, nil
		}
		m = m.applyOperationEvent(msg.event)
		if msg.event.Type == "done" {
			if msg.action == "start" || msg.action == "stop" {
				m.message = resultMessage(msg.event.Results)
				return m.refreshActiveGroups(true)
			}
			return m, nil
		}
		return m, nextOperationStreamMsg(msg.action, msg.ch)
	}
	var cmd tea.Cmd
	m.logView, cmd = m.logView.Update(msg)
	return m, cmd
}

func (m model) View() string {
	var b strings.Builder
	fmt.Fprintf(&b, "rses watch  q quit  j/k move  s start  x stop  r refresh\n\n")
	nameWidth := 24
	summaryWidth := max(12, m.width-2-1-nameWidth-1-10-1-8-1-8-1)
	fmt.Fprintf(&b, "%-2s %s %-10s %-8s %-8s %s\n", "", tablefmt.PadRight("NAME", nameWidth), "STATE", "PID", "UPTIME", "SUMMARY")
	for i, item := range m.items {
		st := item.status
		cursor := " "
		if i == m.selected {
			cursor = ">"
		}
		name := tablefmt.PadRight(tablefmt.Truncate(item.spec.Name, nameWidth), nameWidth)
		state := tablefmt.Truncate(m.stateText(item), 10)
		summary := tablefmt.Truncate(firstNonEmpty(statusSummary(st), item.spec.Summary), summaryWidth)
		fmt.Fprintf(&b, "%-2s %s %-10s %-8d %-8s %s\n", cursor, name, state, st.PID, uptime(st), summary)
	}
	if m.message != "" {
		fmt.Fprintf(&b, "\n%s\n", m.message)
	} else {
		b.WriteString("\n")
	}
	b.WriteString(strings.Repeat("-", max(20, m.width)))
	b.WriteString("\n")
	b.WriteString(m.logView.View())
	return b.String()
}

func (m model) startSelected() tea.Cmd {
	if len(m.items) == 0 {
		return nil
	}
	spec := m.items[m.selected].spec
	return startOperationStream(daemon.Request{Action: "start", ProjectDir: m.projectDir, Specs: []session.Spec{spec}, Runtime: m.runtime})
}

func (m model) stopSelected() tea.Cmd {
	if len(m.items) == 0 {
		return nil
	}
	spec := m.items[m.selected].spec
	return startOperationStream(daemon.Request{
		Action:            "stop",
		ProjectDir:        m.projectDir,
		Specs:             []session.Spec{spec},
		Names:             []string{spec.Name},
		IncludeConfigured: m.includeConfigured,
		Runtime:           m.runtime,
	})
}

func tick() tea.Cmd {
	return tea.Tick(watchTickInterval, func(t time.Time) tea.Msg { return tickMsg(t) })
}

type initialRefreshMsg struct{}

type snapshotMsg struct {
	key  string
	seq  int
	resp daemon.Response
	err  error
}

func (m *model) ensureGroup(spec session.Spec) *watchGroup {
	key := groupKey(spec)
	group := m.groups[key]
	if group == nil {
		group = &watchGroup{key: key}
		if spec.IsRemote() {
			group.remote = true
			group.host = spec.Host
		}
		m.groups[key] = group
	}
	return group
}

func groupKey(spec session.Spec) string {
	if spec.IsRemote() {
		return "remote:" + spec.Host
	}
	return localGroupKey
}

func (m model) selectedItem() watchItem {
	if len(m.items) == 0 || m.selected < 0 || m.selected >= len(m.items) {
		return watchItem{}
	}
	return m.items[m.selected]
}

func (m model) groupSpecs(key string) []session.Spec {
	specs := []session.Spec{}
	for _, item := range m.items {
		if groupKey(item.spec) == key {
			specs = append(specs, item.spec)
		}
	}
	return specs
}

func (m model) refreshActiveGroups(force bool) (model, tea.Cmd) {
	cmds := []tea.Cmd{}
	var cmd tea.Cmd
	m, cmd = m.refreshGroup(localGroupKey, force)
	cmds = append(cmds, cmd)
	selected := m.selectedItem()
	if selected.spec.IsRemote() {
		m, cmd = m.refreshGroup(groupKey(selected.spec), force)
		cmds = append(cmds, cmd)
	}
	return m, tea.Batch(cmds...)
}

func (m model) refreshAllGroups(force bool) (model, tea.Cmd) {
	cmds := []tea.Cmd{}
	keys := []string{localGroupKey}
	for _, item := range m.items {
		key := groupKey(item.spec)
		if key == localGroupKey {
			continue
		}
		if !containsString(keys, key) {
			keys = append(keys, key)
		}
	}
	for _, key := range keys {
		var cmd tea.Cmd
		m, cmd = m.refreshGroup(key, force)
		cmds = append(cmds, cmd)
	}
	return m, tea.Batch(cmds...)
}

func (m model) refreshGroup(key string, force bool) (model, tea.Cmd) {
	group := m.groups[key]
	if group == nil {
		return m, nil
	}
	specs := m.groupSpecs(key)
	if len(specs) == 0 {
		return m, nil
	}
	if group.inFlight {
		if force {
			group.pending = true
		}
		return m, nil
	}
	group.inFlight = true
	group.seq++
	group.spinner++

	selectedName := ""
	selected := m.selectedItem()
	if selected.spec.Name != "" && groupKey(selected.spec) == key {
		selectedName = selected.spec.Name
		m.markLogLoading(selectedName, true)
		m.showSelectedLog()
	}
	req := daemon.Request{
		Action:            "watch-snapshot",
		ProjectDir:        m.projectDir,
		Specs:             specs,
		Names:             namesFromSpecs(specs),
		SelectedName:      selectedName,
		IncludeConfigured: m.includeConfigured,
		Lines:             defaultLogLineCount,
		Runtime:           m.runtime,
	}
	if group.remote {
		req.Host = group.host
	}
	seq := group.seq
	return m, func() tea.Msg {
		resp, err := daemon.Call(req)
		return snapshotMsg{key: key, seq: seq, resp: resp, err: err}
	}
}

func (m model) applySnapshot(msg snapshotMsg) (model, tea.Cmd) {
	group := m.groups[msg.key]
	if group == nil || msg.seq != group.seq {
		return m, nil
	}
	group.inFlight = false
	if msg.err != nil {
		m.message = msg.err.Error()
		for i := range m.items {
			if groupKey(m.items[i].spec) == msg.key {
				m.items[i].status.State = session.StateUnknown
				m.items[i].status.LastError = msg.err.Error()
			}
		}
	} else {
		for _, st := range msg.resp.Statuses {
			m.upsertStatus(st)
		}
		if msg.resp.LogName != "" {
			m.updateLog(msg.resp.LogName, msg.resp.Log, msg.resp.LogError)
		}
	}
	for i := range m.items {
		if groupKey(m.items[i].spec) == msg.key {
			m.items[i].logLoading = false
		}
	}
	m.showSelectedLog()
	if group.pending {
		group.pending = false
		return m.refreshGroup(msg.key, true)
	}
	return m, nil
}

func (m *model) markLogLoading(name string, loading bool) {
	for i := range m.items {
		if m.items[i].spec.Name == name {
			m.items[i].logLoading = loading
			return
		}
	}
}

func (m *model) updateLog(name, log, logErr string) {
	for i := range m.items {
		if m.items[i].spec.Name == name {
			m.items[i].log = log
			m.items[i].logErr = logErr
			m.items[i].logLoading = false
			return
		}
	}
}

func (m *model) showSelectedLog() {
	if len(m.items) == 0 {
		m.logView.SetContent("")
		return
	}
	item := m.items[m.selected]
	content := item.log
	if content == "" && item.logErr != "" {
		content = item.logErr
	}
	if content == "" && item.logLoading {
		content = "updating...\n"
	}
	m.logView.SetContent(wrapLogContent(content, m.logView.Width))
	m.logView.GotoBottom()
}

func (m *model) advanceSpinners() {
	for _, group := range m.groups {
		if group.inFlight {
			group.spinner++
		}
	}
}

func (m model) stateText(item watchItem) string {
	state := string(item.status.State)
	if group := m.groups[groupKey(item.spec)]; group != nil && group.inFlight {
		return state + " " + spinnerFrame(group.spinner)
	}
	return state
}

func spinnerFrame(value int) string {
	frames := []string{"|", "/", "-", "\\"}
	return frames[value%len(frames)]
}

type operationStreamResult struct {
	event daemon.OperationEvent
	err   error
}

type operationStreamMsg struct {
	action string
	event  daemon.OperationEvent
	err    error
	done   bool
	ch     <-chan operationStreamResult
}

func startOperationStream(req daemon.Request) tea.Cmd {
	ch := make(chan operationStreamResult, 64)
	go func() {
		err := daemon.Stream(req, func(event daemon.OperationEvent) error {
			ch <- operationStreamResult{event: event}
			return nil
		})
		if err != nil {
			ch <- operationStreamResult{err: err}
		}
		close(ch)
	}()
	return nextOperationStreamMsg(req.Action, ch)
}

func nextOperationStreamMsg(action string, ch <-chan operationStreamResult) tea.Cmd {
	return func() tea.Msg {
		result, ok := <-ch
		if !ok {
			return operationStreamMsg{action: action, done: true}
		}
		return operationStreamMsg{action: action, event: result.event, err: result.err, ch: ch}
	}
}

func (m model) applyOperationEvent(event daemon.OperationEvent) model {
	switch event.Type {
	case "status", "success", "failed":
		if event.Status != nil {
			m.upsertStatus(*event.Status)
		}
	}
	switch event.Type {
	case "progress", "success", "skipped", "failed":
		if event.Message != "" {
			m.message = event.Message
		} else if event.Error != "" {
			m.message = event.Error
		}
	}
	if event.Type == "failed" && event.Name == "" && event.Error != "" {
		m.message = event.Error
	}
	return m
}

func (m *model) upsertStatus(status session.Status) {
	for i := range m.items {
		if m.items[i].spec.Name == status.Name {
			m.items[i].status = status
			return
		}
	}
}

func wrapLogContent(content string, width int) string {
	if width <= 0 || content == "" {
		return sanitizeLogContent(content)
	}

	content = sanitizeLogContent(content)
	lines := strings.Split(content, "\n")
	var b strings.Builder
	for i, line := range lines {
		if i == len(lines)-1 && line == "" {
			break
		}
		if i > 0 {
			b.WriteByte('\n')
		}
		b.WriteString(wrapLogLine(line, width))
	}
	if strings.HasSuffix(content, "\n") {
		b.WriteByte('\n')
	}
	return b.String()
}

func sanitizeLogContent(content string) string {
	content = strings.ReplaceAll(content, "\r\n", "\n")
	content = strings.ReplaceAll(content, "\r", "\n")
	content = stripANSISequences(content)
	return strings.Map(func(r rune) rune {
		if r == '\n' || r == '\t' {
			return r
		}
		if r < 0x20 || r == 0x7f {
			return -1
		}
		return r
	}, content)
}

func stripANSISequences(content string) string {
	var b strings.Builder
	b.Grow(len(content))
	for i := 0; i < len(content); {
		if content[i] != '\x1b' {
			b.WriteByte(content[i])
			i++
			continue
		}
		if i+1 >= len(content) {
			i++
			continue
		}
		switch content[i+1] {
		case '[':
			i += 2
			for i < len(content) {
				c := content[i]
				i++
				if c >= 0x40 && c <= 0x7e {
					break
				}
			}
		case ']':
			i += 2
			for i < len(content) {
				if content[i] == '\x07' {
					i++
					break
				}
				if content[i] == '\x1b' && i+1 < len(content) && content[i+1] == '\\' {
					i += 2
					break
				}
				i++
			}
		default:
			i++
		}
	}
	return b.String()
}

func wrapLogLine(line string, width int) string {
	if width <= 0 {
		return line
	}

	var b strings.Builder
	col := 0
	for _, r := range line {
		rw := runewidth.RuneWidth(r)
		if rw <= 0 {
			b.WriteRune(r)
			continue
		}
		if col > 0 && col+rw > width {
			b.WriteByte('\n')
			col = 0
		}
		b.WriteRune(r)
		col += rw
	}
	return b.String()
}

func uptime(st session.Status) string {
	if st.StartedAt.IsZero() || st.State != session.StateRunning {
		return "-"
	}
	return time.Since(st.StartedAt).Round(time.Second).String()
}

func resultMessage(results map[string]string) string {
	for name, result := range results {
		return name + ": " + result
	}
	return ""
}

func statusSummary(st session.Status) string {
	if st.State == session.StateUnknown && strings.TrimSpace(st.LastError) != "" {
		return strings.Join(strings.Fields(st.LastError), " ")
	}
	return st.Summary
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func namesFromSpecs(specs []session.Spec) []string {
	names := make([]string, 0, len(specs))
	for _, spec := range specs {
		names = append(names, spec.Name)
	}
	return names
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
