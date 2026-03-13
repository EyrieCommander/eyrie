package tui

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/natalie/eyrie/internal/adapter"
	"github.com/natalie/eyrie/internal/config"
	"github.com/natalie/eyrie/internal/discovery"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

const (
	tabStatus   = 0
	tabLogs     = 1
	tabActivity = 2
	tabHistory  = 3

	maxBufferSize = 500
)

// ---------------------------------------------------------------------------
// AgentView caches fetched health/status for rendering without network calls.
// ---------------------------------------------------------------------------

type AgentView struct {
	Discovered adapter.DiscoveredAgent
	Alive      bool
	Health     *adapter.HealthStatus
	Status     *adapter.AgentStatus
}

// ---------------------------------------------------------------------------
// Bubble Tea messages
//
// Stream messages carry the channel + context so Update can schedule the next
// read as a chained tea.Cmd. This is the standard Bubble Tea pattern for
// consuming a Go channel as a continuous stream.
// ---------------------------------------------------------------------------

type tickMsg time.Time

type logStreamMsg struct {
	entry adapter.LogEntry
	ch    <-chan adapter.LogEntry
	ctx   context.Context
}

type activityStreamMsg struct {
	event adapter.ActivityEvent
	ch    <-chan adapter.ActivityEvent
	ctx   context.Context
}

type sessionsMsg []adapter.Session
type chatHistoryMsg []adapter.ChatMessage
type streamDoneMsg struct{}
type streamErrorMsg struct{ err error }

// ---------------------------------------------------------------------------
// Model
// ---------------------------------------------------------------------------

type Model struct {
	cfg    config.Config
	agents []AgentView
	cursor int
	width  int
	height int

	activeTab int

	// Logs tab
	logEntries []adapter.LogEntry
	logScroll  int

	// Activity tab
	activityEvents []adapter.ActivityEvent
	activityScroll int

	// History tab
	sessions      []adapter.Session
	chatMessages  []adapter.ChatMessage
	sessionCursor int
	viewingChat   bool

	// Stream error (e.g. auth failure)
	streamErr error

	// Cancels the current stream goroutine when switching tabs/agents
	streamCancel context.CancelFunc
}

// ---------------------------------------------------------------------------
// Public entry point
// ---------------------------------------------------------------------------

func Run(cfg config.Config) error {
	m := initialModel(cfg)
	p := tea.NewProgram(m, tea.WithAltScreen())
	_, err := p.Run()
	return err
}

func initialModel(cfg config.Config) Model {
	return Model{
		cfg:    cfg,
		agents: fetchAgents(cfg),
	}
}

func fetchAgents(cfg config.Config) []AgentView {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	result := discovery.Run(ctx, cfg)
	views := make([]AgentView, 0, len(result.Agents))

	for _, ar := range result.Agents {
		v := AgentView{Discovered: ar.Agent, Alive: ar.Alive}
		if ar.Alive {
			agent := discovery.NewAgent(ar.Agent)
			if health, err := agent.Health(ctx); err == nil {
				v.Health = health
			}
			if status, err := agent.Status(ctx); err == nil {
				v.Status = status
			}
		}
		views = append(views, v)
	}

	return views
}

// ---------------------------------------------------------------------------
// Init / Update
// ---------------------------------------------------------------------------

func (m Model) Init() tea.Cmd {
	return tickCmd()
}

func tickCmd() tea.Cmd {
	return tea.Tick(3*time.Second, func(t time.Time) tea.Msg {
		return tickMsg(t)
	})
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		return m.handleKey(msg)

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil

	case tickMsg:
		m.agents = fetchAgents(m.cfg)
		return m, tickCmd()

	case logStreamMsg:
		m.logEntries = appendCapped(m.logEntries, msg.entry, maxBufferSize)
		m.logScroll = max(0, len(m.logEntries)-m.detailContentHeight())
		return m, waitForLogEntry(msg.ctx, msg.ch)

	case activityStreamMsg:
		m.activityEvents = appendCapped(m.activityEvents, msg.event, maxBufferSize)
		m.activityScroll = max(0, len(m.activityEvents)-m.detailContentHeight())
		return m, waitForActivityEvent(msg.ctx, msg.ch)

	case sessionsMsg:
		m.sessions = []adapter.Session(msg)
		m.sessionCursor = 0
		m.viewingChat = false
		return m, nil

	case chatHistoryMsg:
		m.chatMessages = []adapter.ChatMessage(msg)
		m.viewingChat = true
		return m, nil

	case streamDoneMsg:
		return m, nil

	case streamErrorMsg:
		m.streamErr = msg.err
		return m, nil
	}

	return m, nil
}

func (m *Model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	key := msg.String()

	switch key {
	case "q", "ctrl+c":
		m.cancelStream()
		return m, tea.Quit

	case "1":
		return m, m.switchTab(tabStatus)
	case "2":
		return m, m.switchTab(tabLogs)
	case "3":
		return m, m.switchTab(tabActivity)
	case "4":
		return m, m.switchTab(tabHistory)

	case "left", "h":
		prev := m.activeTab - 1
		if prev < tabStatus {
			prev = tabHistory
		}
		return m, m.switchTab(prev)
	case "right", "l":
		next := m.activeTab + 1
		if next > tabHistory {
			next = tabStatus
		}
		return m, m.switchTab(next)

	case "up", "k":
		return m, m.handleUp()
	case "down", "j":
		return m, m.handleDown()

	case "enter":
		if m.activeTab == tabHistory && !m.viewingChat && len(m.sessions) > 0 {
			return m, m.fetchChatHistory()
		}

	case "esc":
		if m.activeTab == tabHistory && m.viewingChat {
			m.viewingChat = false
			m.chatMessages = nil
		}

	case "r":
		m.agents = fetchAgents(m.cfg)

	case "s":
		if m.cursor < len(m.agents) && m.agents[m.cursor].Alive {
			m.doLifecycle("stop")
		}

	case "R":
		if m.cursor < len(m.agents) && m.agents[m.cursor].Alive {
			m.doLifecycle("restart")
		}
	}

	return m, nil
}

func (m *Model) handleUp() tea.Cmd {
	switch m.activeTab {
	case tabLogs:
		if m.logScroll > 0 {
			m.logScroll--
		}
		return nil
	case tabActivity:
		if m.activityScroll > 0 {
			m.activityScroll--
		}
		return nil
	case tabHistory:
		if !m.viewingChat && m.sessionCursor > 0 {
			m.sessionCursor--
		}
		return nil
	}
	// Status tab: navigate agent list
	if m.cursor > 0 {
		m.cursor--
		return m.onAgentChanged()
	}
	return nil
}

func (m *Model) handleDown() tea.Cmd {
	switch m.activeTab {
	case tabLogs:
		h := m.detailContentHeight()
		if m.logScroll < len(m.logEntries)-h {
			m.logScroll++
		}
		return nil
	case tabActivity:
		h := m.detailContentHeight()
		if m.activityScroll < len(m.activityEvents)-h {
			m.activityScroll++
		}
		return nil
	case tabHistory:
		if !m.viewingChat && m.sessionCursor < len(m.sessions)-1 {
			m.sessionCursor++
		}
		return nil
	}
	if m.cursor < len(m.agents)-1 {
		m.cursor++
		return m.onAgentChanged()
	}
	return nil
}

// ---------------------------------------------------------------------------
// Tab switching & stream lifecycle
// ---------------------------------------------------------------------------

func (m *Model) switchTab(tab int) tea.Cmd {
	if m.activeTab == tab {
		return nil
	}
	m.cancelStream()
	m.activeTab = tab
	m.streamErr = nil

	switch tab {
	case tabLogs:
		m.logEntries = nil
		m.logScroll = 0
		return m.startLogStream()
	case tabActivity:
		m.activityEvents = nil
		m.activityScroll = 0
		return m.startActivityStream()
	case tabHistory:
		m.sessions = nil
		m.chatMessages = nil
		m.viewingChat = false
		m.sessionCursor = 0
		return m.fetchSessions()
	}

	return nil
}

func (m *Model) onAgentChanged() tea.Cmd {
	m.cancelStream()
	m.streamErr = nil

	switch m.activeTab {
	case tabLogs:
		m.logEntries = nil
		m.logScroll = 0
		return m.startLogStream()
	case tabActivity:
		m.activityEvents = nil
		m.activityScroll = 0
		return m.startActivityStream()
	case tabHistory:
		m.sessions = nil
		m.chatMessages = nil
		m.viewingChat = false
		return m.fetchSessions()
	}

	return nil
}

func (m *Model) cancelStream() {
	if m.streamCancel != nil {
		m.streamCancel()
		m.streamCancel = nil
	}
}

func (m *Model) currentAgent() adapter.Agent {
	if m.cursor >= len(m.agents) || !m.agents[m.cursor].Alive {
		return nil
	}
	return discovery.NewAgent(m.agents[m.cursor].Discovered)
}

// startLogStream opens TailLogs and returns a Cmd that reads the first entry.
// Each logStreamMsg carries the channel so Update can chain the next read.
func (m *Model) startLogStream() tea.Cmd {
	agent := m.currentAgent()
	if agent == nil {
		return nil
	}

	ctx, cancel := context.WithCancel(context.Background())
	m.streamCancel = cancel

	return func() tea.Msg {
		ch, err := agent.TailLogs(ctx)
		if err != nil {
			return streamErrorMsg{err: err}
		}
		select {
		case <-ctx.Done():
			return streamDoneMsg{}
		case entry, ok := <-ch:
			if !ok {
				return streamDoneMsg{}
			}
			return logStreamMsg{entry: entry, ch: ch, ctx: ctx}
		}
	}
}

func (m *Model) startActivityStream() tea.Cmd {
	agent := m.currentAgent()
	if agent == nil {
		return nil
	}

	ctx, cancel := context.WithCancel(context.Background())
	m.streamCancel = cancel

	return func() tea.Msg {
		ch, err := agent.TailActivity(ctx)
		if err != nil {
			return streamErrorMsg{err: err}
		}
		select {
		case <-ctx.Done():
			return streamDoneMsg{}
		case event, ok := <-ch:
			if !ok {
				return streamDoneMsg{}
			}
			return activityStreamMsg{event: event, ch: ch, ctx: ctx}
		}
	}
}

// waitForLogEntry returns a Cmd that reads the next entry from the channel.
func waitForLogEntry(ctx context.Context, ch <-chan adapter.LogEntry) tea.Cmd {
	return func() tea.Msg {
		select {
		case <-ctx.Done():
			return streamDoneMsg{}
		case entry, ok := <-ch:
			if !ok {
				return streamDoneMsg{}
			}
			return logStreamMsg{entry: entry, ch: ch, ctx: ctx}
		}
	}
}

func waitForActivityEvent(ctx context.Context, ch <-chan adapter.ActivityEvent) tea.Cmd {
	return func() tea.Msg {
		select {
		case <-ctx.Done():
			return streamDoneMsg{}
		case event, ok := <-ch:
			if !ok {
				return streamDoneMsg{}
			}
			return activityStreamMsg{event: event, ch: ch, ctx: ctx}
		}
	}
}

func (m *Model) fetchSessions() tea.Cmd {
	agent := m.currentAgent()
	if agent == nil {
		return nil
	}

	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		sessions, err := agent.Sessions(ctx)
		if err != nil || sessions == nil {
			return sessionsMsg(nil)
		}
		return sessionsMsg(sessions)
	}
}

func (m *Model) fetchChatHistory() tea.Cmd {
	agent := m.currentAgent()
	if agent == nil || m.sessionCursor >= len(m.sessions) {
		return nil
	}

	sessionKey := m.sessions[m.sessionCursor].Key
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		messages, err := agent.ChatHistory(ctx, sessionKey, 50)
		if err != nil {
			return chatHistoryMsg(nil)
		}
		return chatHistoryMsg(messages)
	}
}

func (m *Model) detailContentHeight() int {
	h := m.height - 8
	if h < 5 {
		h = 5
	}
	return h
}

func (m *Model) doLifecycle(action string) {
	if m.cursor >= len(m.agents) {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	agent := discovery.NewAgent(m.agents[m.cursor].Discovered)
	switch action {
	case "stop":
		_ = agent.Stop(ctx)
	case "restart":
		_ = agent.Restart(ctx)
	}

	time.Sleep(500 * time.Millisecond)
	m.agents = fetchAgents(m.cfg)
}

// ---------------------------------------------------------------------------
// View
// ---------------------------------------------------------------------------

func (m Model) View() string {
	if m.width == 0 {
		return "Loading..."
	}

	if len(m.agents) == 0 {
		return noAgentsView()
	}

	listWidth := 40
	detailWidth := m.width - listWidth - 4

	if detailWidth < 40 {
		return m.verticalView()
	}

	return lipgloss.JoinHorizontal(
		lipgloss.Top,
		m.renderAgentList(listWidth),
		"  ",
		m.renderDetailPane(detailWidth),
	)
}

func (m Model) verticalView() string {
	var b strings.Builder

	b.WriteString(headerStyle.Render("Eyrie - Claw Agent Manager"))
	b.WriteString("\n\n")

	for i, av := range m.agents {
		cur := " "
		if m.cursor == i {
			cur = "\u25b6"
		}

		status := statusText(av.Alive)
		mem := "-"
		if av.Health != nil {
			mem = formatMemory(av.Health.RAM)
		}
		prov := "-"
		if av.Status != nil && av.Status.Provider != "" {
			prov = av.Status.Provider
		}

		line := fmt.Sprintf("%s %-15s  %s  %8s  %s", cur, av.Discovered.Name, status, mem, prov)

		if m.cursor == i {
			b.WriteString(selectedStyle.Render(line))
		} else {
			b.WriteString(line)
		}
		b.WriteString("\n")
	}

	b.WriteString("\n")
	b.WriteString(m.helpBar())

	return b.String()
}

func (m Model) renderAgentList(width int) string {
	var b strings.Builder

	b.WriteString(titleStyle.Width(width).Render("AGENTS"))
	b.WriteString("\n\n")

	for i, av := range m.agents {
		cur := " "
		if m.cursor == i {
			cur = "\u25b6"
		}

		dot := statusDot(av.Alive)
		line := fmt.Sprintf("%s %s %-12s", cur, dot, av.Discovered.Name)

		if m.cursor == i {
			b.WriteString(selectedStyle.Width(width - 2).Render(line))
		} else {
			b.WriteString(line)
		}
		b.WriteString("\n")
	}

	b.WriteString("\n")
	b.WriteString(m.helpBar())

	return boxStyle.Width(width).Render(b.String())
}

// helpBar returns context-sensitive help text based on the active tab.
func (m Model) helpBar() string {
	var lines []string

	tabHelp := helpKey("\u2190/\u2192") + " switch tab"

	switch m.activeTab {
	case tabStatus:
		lines = []string{
			helpKey("\u2191/\u2193") + " select agent  " + tabHelp,
			helpKey("s") + " stop  " + helpKey("R") + " restart  " + helpKey("r") + " refresh  " + helpKey("q") + " quit",
		}
	case tabLogs:
		lines = []string{
			helpKey("\u2191/\u2193") + " scroll  " + tabHelp,
			helpKey("r") + " refresh  " + helpKey("q") + " quit",
		}
	case tabActivity:
		lines = []string{
			helpKey("\u2191/\u2193") + " scroll  " + tabHelp,
			helpKey("r") + " refresh  " + helpKey("q") + " quit",
		}
	case tabHistory:
		if m.viewingChat {
			lines = []string{
				helpKey("Esc") + " back to sessions  " + tabHelp,
				helpKey("q") + " quit",
			}
		} else {
			lines = []string{
				helpKey("\u2191/\u2193") + " select session  " + helpKey("Enter") + " view messages",
				tabHelp + "  " + helpKey("q") + " quit",
			}
		}
	}

	return helpStyle.Render(strings.Join(lines, "\n"))
}

func helpKey(key string) string {
	return helpKeyStyle.Render(key)
}

// ---------------------------------------------------------------------------
// Detail pane with tabs
// ---------------------------------------------------------------------------

func (m Model) renderDetailPane(width int) string {
	if m.cursor >= len(m.agents) {
		return boxStyle.Width(width).Render("No agent selected")
	}

	var b strings.Builder

	tabs := []string{"1:Status", "2:Logs", "3:Activity", "4:History"}
	var tabParts []string
	for i, label := range tabs {
		if i == m.activeTab {
			tabParts = append(tabParts, activeTabStyle.Render(label))
		} else {
			tabParts = append(tabParts, inactiveTabStyle.Render(label))
		}
	}
	b.WriteString(strings.Join(tabParts, "  "))
	b.WriteString("\n\n")

	switch m.activeTab {
	case tabStatus:
		b.WriteString(m.renderStatusContent())
	case tabLogs:
		b.WriteString(m.renderLogsContent())
	case tabActivity:
		b.WriteString(m.renderActivityContent())
	case tabHistory:
		b.WriteString(m.renderHistoryContent())
	}

	return boxStyle.Width(width).Render(b.String())
}

func (m Model) renderStatusContent() string {
	av := m.agents[m.cursor]
	var b strings.Builder

	row := func(label, value string) {
		b.WriteString(labelStyle.Render(label))
		b.WriteString(value)
		b.WriteString("\n")
	}

	row("Status:    ", statusText(av.Alive))
	row("Framework: ", av.Discovered.Framework)
	row("Port:      ", fmt.Sprintf("%d", av.Discovered.Port))

	if av.Discovered.ConfigPath != "" {
		row("Config:    ", av.Discovered.ConfigPath)
	}

	if av.Health != nil {
		h := av.Health
		if h.PID > 0 {
			row("PID:       ", fmt.Sprintf("%d", h.PID))
		}
		row("Memory:    ", formatMemory(h.RAM))
		row("Uptime:    ", formatDuration(h.Uptime))

		if len(h.Components) > 0 {
			b.WriteString("\n")
			b.WriteString(labelStyle.Render("Components:"))
			b.WriteString("\n")
			for name, c := range h.Components {
				dot := componentDot(c.Status)
				line := fmt.Sprintf("  %s %s", dot, name)
				if c.RestartCount > 0 {
					line += fmt.Sprintf(" (restarts: %d)", c.RestartCount)
				}
				b.WriteString(line + "\n")
			}
		}
	}

	if av.Status != nil {
		st := av.Status
		b.WriteString("\n")
		if st.Provider != "" {
			row("Provider:  ", st.Provider)
		}
		if st.Model != "" {
			row("Model:     ", st.Model)
		}
		if len(st.Channels) > 0 {
			row("Channels:  ", strings.Join(st.Channels, ", "))
		}
		if st.Skills > 0 {
			row("Skills:    ", fmt.Sprintf("%d", st.Skills))
		}
		if st.Errors24h > 0 {
			row("Errors24h: ", fmt.Sprintf("%d", st.Errors24h))
		}
	}

	return b.String()
}

func (m Model) renderLogsContent() string {
	if m.cursor < len(m.agents) && !m.agents[m.cursor].Alive {
		return dimStyle.Render("Agent is not running.")
	}

	if m.streamErr != nil {
		return m.renderStreamError()
	}

	if len(m.logEntries) == 0 {
		return dimStyle.Render("Waiting for log entries...")
	}

	var b strings.Builder
	h := m.detailContentHeight()
	start := m.logScroll
	end := start + h
	if end > len(m.logEntries) {
		end = len(m.logEntries)
	}

	for _, entry := range m.logEntries[start:end] {
		ts := entry.Timestamp.Format("15:04:05")
		level := entry.Level
		if level == "" {
			level = "info"
		}
		levelStr := logLevelStyle(level).Render(fmt.Sprintf("[%-5s]", level))
		b.WriteString(fmt.Sprintf("%s %s %s\n", dimStyle.Render(ts), levelStr, entry.Message))
	}

	if len(m.logEntries) > h {
		b.WriteString(dimStyle.Render(fmt.Sprintf("\n-- %d/%d (j/k to scroll) --", end, len(m.logEntries))))
	}

	return b.String()
}

func (m Model) renderActivityContent() string {
	if m.cursor < len(m.agents) && !m.agents[m.cursor].Alive {
		return dimStyle.Render("Agent is not running.")
	}

	if m.streamErr != nil {
		return m.renderStreamError()
	}

	if len(m.activityEvents) == 0 {
		return dimStyle.Render("Waiting for activity events...")
	}

	var b strings.Builder
	h := m.detailContentHeight()
	start := m.activityScroll
	end := start + h
	if end > len(m.activityEvents) {
		end = len(m.activityEvents)
	}

	for _, ev := range m.activityEvents[start:end] {
		ts := ev.Timestamp.Format("15:04:05")
		typeStr := activityTypeStyle(ev.Type).Render(fmt.Sprintf("[%-14s]", ev.Type))
		b.WriteString(fmt.Sprintf("%s %s %s\n", dimStyle.Render(ts), typeStr, ev.Summary))
	}

	if len(m.activityEvents) > h {
		b.WriteString(dimStyle.Render(fmt.Sprintf("\n-- %d/%d (j/k to scroll) --", end, len(m.activityEvents))))
	}

	return b.String()
}

func (m Model) renderStreamError() string {
	var b strings.Builder
	errMsg := m.streamErr.Error()

	b.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color("196")).Bold(true).Render("Stream error"))
	b.WriteString("\n\n")

	if strings.Contains(errMsg, "401") || strings.Contains(strings.ToLower(errMsg), "unauthorized") {
		b.WriteString(dimStyle.Render("The agent gateway requires authentication."))
		b.WriteString("\n\n")
		if m.cursor < len(m.agents) {
			name := m.agents[m.cursor].Discovered.Name
			b.WriteString(fmt.Sprintf("Run:  eyrie pair %s <pairing-code>\n\n", name))
		}
		b.WriteString(dimStyle.Render("The pairing code is shown in the agent's console on startup."))
	} else {
		b.WriteString(dimStyle.Render(errMsg))
	}

	return b.String()
}

func (m Model) renderHistoryContent() string {
	if m.cursor < len(m.agents) && !m.agents[m.cursor].Alive {
		return dimStyle.Render("Agent is not running.")
	}

	if m.cursor < len(m.agents) && m.agents[m.cursor].Discovered.Framework == "zeroclaw" {
		return dimStyle.Render("Conversation history is not supported by ZeroClaw.")
	}

	if m.viewingChat {
		return m.renderChatThread()
	}

	if m.sessions == nil {
		return dimStyle.Render("Loading sessions...")
	}

	if len(m.sessions) == 0 {
		return dimStyle.Render("No sessions found.")
	}

	var b strings.Builder
	b.WriteString(labelStyle.Render("Sessions"))
	b.WriteString(dimStyle.Render("  (Enter to view, Esc to go back)"))
	b.WriteString("\n\n")

	for i, s := range m.sessions {
		cur := " "
		if m.sessionCursor == i {
			cur = "\u25b6"
		}

		title := s.Title
		if title == "" {
			title = s.Key
		}
		if len(title) > 40 {
			title = title[:40] + "..."
		}

		age := ""
		if s.LastMsg != nil {
			age = " (" + timeAgo(*s.LastMsg) + ")"
		}

		channel := ""
		if s.Channel != "" {
			channel = " [" + s.Channel + "]"
		}

		line := fmt.Sprintf("%s %s%s%s", cur, title, channel, age)
		if m.sessionCursor == i {
			b.WriteString(selectedStyle.Render(line))
		} else {
			b.WriteString(line)
		}
		b.WriteString("\n")
	}

	return b.String()
}

func (m Model) renderChatThread() string {
	if len(m.chatMessages) == 0 {
		return dimStyle.Render("No messages in this session.")
	}

	var b strings.Builder
	b.WriteString(labelStyle.Render("Chat History"))
	b.WriteString(dimStyle.Render("  (Esc to go back)"))
	b.WriteString("\n\n")

	for _, msg := range m.chatMessages {
		ts := ""
		if !msg.Timestamp.IsZero() {
			ts = msg.Timestamp.Format("15:04") + " "
		}

		var roleStyle lipgloss.Style
		switch msg.Role {
		case "user":
			roleStyle = userRoleStyle
		case "assistant":
			roleStyle = assistantRoleStyle
		default:
			roleStyle = dimStyle
		}

		content := msg.Content
		if len(content) > 200 {
			content = content[:200] + "..."
		}

		b.WriteString(fmt.Sprintf("%s%s %s\n", dimStyle.Render(ts), roleStyle.Render(msg.Role+":"), content))
	}

	return b.String()
}

func noAgentsView() string {
	return boxStyle.Render(
		lipgloss.JoinVertical(
			lipgloss.Left,
			titleStyle.Render("Eyrie - No Agents Found"),
			"",
			"No Claw agents are currently running.",
			"",
			"Make sure ZeroClaw or OpenClaw is running,",
			"then press 'r' to refresh.",
			"",
			helpStyle.Render("r refresh  q quit"),
		),
	)
}

// ---------------------------------------------------------------------------
// Styles
// ---------------------------------------------------------------------------

var (
	headerStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("86"))

	titleStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("99")).
			Background(lipgloss.Color("235")).
			Padding(0, 1)

	boxStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("240")).
			Padding(1, 2)

	selectedStyle = lipgloss.NewStyle().
			Background(lipgloss.Color("237")).
			Foreground(lipgloss.Color("229"))

	labelStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("240")).
			Bold(true)

	helpStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("240"))

	dimStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("242"))

	runningStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("42"))
	stoppedStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("240"))
	errorStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("196"))

	activeTabStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("99")).
			Background(lipgloss.Color("235")).
			Padding(0, 1)

	inactiveTabStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("242")).
				Padding(0, 1)

	helpKeyStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("252"))

	userRoleStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("86"))

	assistantRoleStyle = lipgloss.NewStyle().
				Bold(true).
				Foreground(lipgloss.Color("99"))
)

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func statusText(alive bool) string {
	if alive {
		return runningStyle.Render("running")
	}
	return stoppedStyle.Render("stopped")
}

func statusDot(alive bool) string {
	if alive {
		return runningStyle.Render("\u25cf")
	}
	return stoppedStyle.Render("\u25cf")
}

func componentDot(status string) string {
	switch status {
	case "ok", "running", "healthy":
		return runningStyle.Render("\u25cf")
	case "error", "failed":
		return errorStyle.Render("\u25cf")
	default:
		return stoppedStyle.Render("\u25cf")
	}
}

func logLevelStyle(level string) lipgloss.Style {
	switch level {
	case "error", "fatal":
		return lipgloss.NewStyle().Foreground(lipgloss.Color("196"))
	case "warn", "warning":
		return lipgloss.NewStyle().Foreground(lipgloss.Color("226"))
	case "debug", "trace":
		return lipgloss.NewStyle().Foreground(lipgloss.Color("242"))
	default:
		return lipgloss.NewStyle().Foreground(lipgloss.Color("42"))
	}
}

func activityTypeStyle(typ string) lipgloss.Style {
	switch typ {
	case "agent_start":
		return lipgloss.NewStyle().Foreground(lipgloss.Color("86"))
	case "agent_end":
		return lipgloss.NewStyle().Foreground(lipgloss.Color("99"))
	case "tool_call", "tool_call_start":
		return lipgloss.NewStyle().Foreground(lipgloss.Color("214"))
	case "llm_request":
		return lipgloss.NewStyle().Foreground(lipgloss.Color("117"))
	case "error":
		return lipgloss.NewStyle().Foreground(lipgloss.Color("196"))
	case "chat":
		return lipgloss.NewStyle().Foreground(lipgloss.Color("42"))
	default:
		return lipgloss.NewStyle().Foreground(lipgloss.Color("242"))
	}
}

func formatMemory(bytes uint64) string {
	if bytes == 0 {
		return "-"
	}

	const (
		kb = 1024
		mb = 1024 * kb
		gb = 1024 * mb
	)

	switch {
	case bytes >= gb:
		return fmt.Sprintf("%.1f GB", float64(bytes)/float64(gb))
	case bytes >= mb:
		return fmt.Sprintf("%.1f MB", float64(bytes)/float64(mb))
	case bytes >= kb:
		return fmt.Sprintf("%.1f KB", float64(bytes)/float64(kb))
	default:
		return fmt.Sprintf("%d B", bytes)
	}
}

func formatDuration(d time.Duration) string {
	if d == 0 {
		return "-"
	}

	days := int(d.Hours()) / 24
	hours := int(d.Hours()) % 24
	mins := int(d.Minutes()) % 60

	if days > 0 {
		return fmt.Sprintf("%dd %dh", days, hours)
	}
	if hours > 0 {
		return fmt.Sprintf("%dh %dm", hours, mins)
	}
	return fmt.Sprintf("%dm", mins)
}

func timeAgo(t time.Time) string {
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	default:
		return fmt.Sprintf("%dd ago", int(d.Hours()/24))
	}
}

func appendCapped[T any](s []T, item T, maxSize int) []T {
	s = append(s, item)
	if len(s) > maxSize {
		s = s[len(s)-maxSize:]
	}
	return s
}
