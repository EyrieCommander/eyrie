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
	tabActivity = 1
	tabLogs     = 2

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

type chatEntry struct {
	Role    string // "user", "assistant", "error"
	Content string
	Time    time.Time
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

type streamDoneMsg struct{}
type streamErrorMsg struct{ err error }

type chatReplyMsg struct {
	reply *adapter.ChatMessage
	err   error
}

type sessionsLoadedMsg struct {
	sessions []adapter.Session
}

type sessionMessagesMsg struct {
	messages []adapter.ChatMessage
	err      error
}

type sessionDeletedMsg struct {
	key string
	err error
}

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

	// Activity tab — session-based conversations with chat input
	sessions         []adapter.Session
	activeSessionIdx int
	activeSessionKey string
	sessionMessages  []adapter.ChatMessage
	activityScroll   int
	activityCursor   int
	expandedActivity int // -1 = none expanded
	chatInput        string
	chatSending      bool
	chatEditing      bool // true when input is focused
	chatLocalMsgs    []chatEntry
	creatingSession  bool   // true when typing a new session name
	newSessionName   string

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
		cfg:              cfg,
		agents:           fetchAgents(cfg),
		expandedActivity: -1,
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

	case streamDoneMsg:
		return m, nil

	case streamErrorMsg:
		m.streamErr = msg.err
		return m, nil

	case chatReplyMsg:
		m.chatSending = false
		if msg.err != nil {
			m.chatLocalMsgs = append(m.chatLocalMsgs, chatEntry{
				Role: "error", Content: msg.err.Error(), Time: time.Now(),
			})
		} else if msg.reply != nil {
			m.chatLocalMsgs = append(m.chatLocalMsgs, chatEntry{
				Role: msg.reply.Role, Content: msg.reply.Content, Time: msg.reply.Timestamp,
			})
		}
		total := len(m.sessionMessages) + len(m.chatLocalMsgs)
		h := m.detailContentHeight() - 3
		if total > h {
			m.activityScroll = total - h
		}
		return m, nil

	case sessionsLoadedMsg:
		m.sessions = msg.sessions
		if len(msg.sessions) > 0 && m.activeSessionKey == "" {
			m.activeSessionIdx = 0
			m.activeSessionKey = msg.sessions[0].Key
			return m, m.loadSessionMessages()
		}
		found := false
		for i, s := range msg.sessions {
			if s.Key == m.activeSessionKey {
				m.activeSessionIdx = i
				found = true
				break
			}
		}
		if !found && len(msg.sessions) > 0 {
			m.activeSessionIdx = 0
			m.activeSessionKey = msg.sessions[0].Key
			return m, m.loadSessionMessages()
		}
		return m, m.loadSessionMessages()

	case sessionMessagesMsg:
		if msg.err != nil {
			m.streamErr = msg.err
			return m, nil
		}
		m.sessionMessages = msg.messages
		m.chatLocalMsgs = nil
		h := m.detailContentHeight() - 3
		if len(m.sessionMessages) > h {
			m.activityScroll = len(m.sessionMessages) - h
		} else {
			m.activityScroll = 0
		}
		m.activityCursor = 0
		return m, nil

	case sessionDeletedMsg:
		if msg.err != nil {
			m.streamErr = msg.err
			return m, nil
		}
		remaining := make([]adapter.Session, 0, len(m.sessions))
		for _, s := range m.sessions {
			if s.Key != msg.key {
				remaining = append(remaining, s)
			}
		}
		m.sessions = remaining
		if len(remaining) > 0 {
			if m.activeSessionIdx >= len(remaining) {
				m.activeSessionIdx = len(remaining) - 1
			}
			m.activeSessionKey = remaining[m.activeSessionIdx].Key
		} else {
			fw := m.currentAgentFramework()
			defaultKey := "agent:main:main"
			if fw == "zeroclaw" {
				defaultKey = "main"
			}
			m.activeSessionKey = defaultKey
			m.activeSessionIdx = 0
		}
		m.sessionMessages = nil
		m.chatLocalMsgs = nil
		return m, m.loadSessionMessages()
	}

	return m, nil
}

func (m *Model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.activeTab == tabActivity && m.chatEditing {
		return m.handleChatInput(msg)
	}
	if m.activeTab == tabActivity && m.creatingSession {
		return m.handleNewSessionInput(msg)
	}

	key := msg.String()

	switch key {
	case "q", "ctrl+c":
		m.cancelStream()
		return m, tea.Quit

	case "1":
		return m, m.switchTab(tabStatus)
	case "2":
		return m, m.switchTab(tabActivity)
	case "3":
		return m, m.switchTab(tabLogs)

	case "left", "h":
		prev := m.activeTab - 1
		if prev < tabStatus {
			prev = tabLogs
		}
		return m, m.switchTab(prev)
	case "right", "l":
		next := m.activeTab + 1
		if next > tabLogs {
			next = tabStatus
		}
		return m, m.switchTab(next)

	case "up", "k":
		return m, m.handleUp()
	case "down", "j":
		return m, m.handleDown()

	case "enter":
		if m.activeTab == tabActivity {
			total := len(m.sessionMessages) + len(m.chatLocalMsgs)
			absIdx := m.activityScroll + m.activityCursor
			if absIdx < total {
				if m.expandedActivity == absIdx {
					m.expandedActivity = -1
				} else {
					m.expandedActivity = absIdx
				}
			}
		}

	case "i":
		activeReadOnly := m.activeSessionIdx < len(m.sessions) && m.sessions[m.activeSessionIdx].ReadOnly
		if m.activeTab == tabActivity && !m.chatSending && !activeReadOnly {
			m.chatEditing = true
		}

	case "]", "tab":
		if m.activeTab == tabActivity && len(m.sessions) > 1 {
			m.activeSessionIdx = (m.activeSessionIdx + 1) % len(m.sessions)
			m.activeSessionKey = m.sessions[m.activeSessionIdx].Key
			m.sessionMessages = nil
			m.chatLocalMsgs = nil
			m.activityScroll = 0
			m.activityCursor = 0
			m.expandedActivity = -1
			return m, m.loadSessionMessages()
		}

	case "[", "shift+tab":
		if m.activeTab == tabActivity && len(m.sessions) > 1 {
			m.activeSessionIdx--
			if m.activeSessionIdx < 0 {
				m.activeSessionIdx = len(m.sessions) - 1
			}
			m.activeSessionKey = m.sessions[m.activeSessionIdx].Key
			m.sessionMessages = nil
			m.chatLocalMsgs = nil
			m.activityScroll = 0
			m.activityCursor = 0
			m.expandedActivity = -1
			return m, m.loadSessionMessages()
		}

	case "n":
		if m.activeTab == tabActivity && m.currentAgentFramework() != "zeroclaw" {
			m.creatingSession = true
			m.newSessionName = ""
		}

	case "d":
		if m.activeTab == tabActivity && m.currentAgentFramework() != "zeroclaw" && len(m.sessions) > 0 {
			cur := m.sessions[m.activeSessionIdx]
			fw := m.currentAgentFramework()
			defaultKey := "agent:main:main"
			if fw == "zeroclaw" {
				defaultKey = "main"
			}
			if cur.Key != defaultKey {
				return m, m.deleteCurrentSession()
			}
		}

	case "esc":
		if m.activeTab == tabActivity && m.expandedActivity >= 0 {
			m.expandedActivity = -1
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

func (m *Model) handleNewSessionInput(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	key := msg.String()
	switch key {
	case "esc":
		m.creatingSession = false
		m.newSessionName = ""
		return m, nil
	case "ctrl+c":
		m.cancelStream()
		return m, tea.Quit
	case "enter":
		name := strings.TrimSpace(m.newSessionName)
		if name == "" {
			m.creatingSession = false
			return m, nil
		}
		name = strings.ToLower(strings.ReplaceAll(name, " ", "-"))
		sessionKey := "agent:main:" + name
		m.sessions = append(m.sessions, adapter.Session{Key: sessionKey, Title: name})
		m.activeSessionIdx = len(m.sessions) - 1
		m.activeSessionKey = sessionKey
		m.sessionMessages = nil
		m.chatLocalMsgs = nil
		m.activityScroll = 0
		m.activityCursor = 0
		m.creatingSession = false
		m.newSessionName = ""
		return m, m.loadSessionMessages()
	case "backspace":
		if len(m.newSessionName) > 0 {
			m.newSessionName = m.newSessionName[:len(m.newSessionName)-1]
		}
		return m, nil
	default:
		if len(key) == 1 || key == " " {
			m.newSessionName += key
		}
		return m, nil
	}
}

func (m *Model) handleChatInput(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	key := msg.String()

	switch key {
	case "esc":
		m.chatEditing = false
		return m, nil
	case "ctrl+c":
		m.cancelStream()
		return m, tea.Quit
	case "enter":
		text := strings.TrimSpace(m.chatInput)
		if text == "" || m.chatSending {
			return m, nil
		}
		m.chatInput = ""
		m.chatSending = true
		m.chatEditing = false
		m.chatLocalMsgs = append(m.chatLocalMsgs, chatEntry{
			Role: "user", Content: text, Time: time.Now(),
		})
		total := len(m.sessionMessages) + len(m.chatLocalMsgs)
		h := m.detailContentHeight() - 3
		if total > h {
			m.activityScroll = total - h
		}
		return m, m.sendChatMessage(text)
	case "backspace":
		if len(m.chatInput) > 0 {
			m.chatInput = m.chatInput[:len(m.chatInput)-1]
		}
		return m, nil
	default:
		if len(key) == 1 || key == " " {
			m.chatInput += key
		}
		return m, nil
	}
}

func (m *Model) handleUp() tea.Cmd {
	switch m.activeTab {
	case tabLogs:
		if m.logScroll > 0 {
			m.logScroll--
		}
		return nil
	case tabActivity:
		if m.activityCursor > 0 {
			m.activityCursor--
		} else if m.activityScroll > 0 {
			m.activityScroll--
		}
		return nil
	}
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
		total := len(m.sessionMessages) + len(m.chatLocalMsgs)
		h := m.detailContentHeight() - 3
		absIdx := m.activityScroll + m.activityCursor
		if absIdx < total-1 {
			if m.activityCursor < h-1 {
				m.activityCursor++
			} else {
				m.activityScroll++
			}
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
	m.chatEditing = false
	m.creatingSession = false

	switch tab {
	case tabLogs:
		m.logEntries = nil
		m.logScroll = 0
		return m.startLogStream()
	case tabActivity:
		m.activityScroll = 0
		m.activityCursor = 0
		m.expandedActivity = -1
		m.chatLocalMsgs = nil
		m.chatInput = ""
		m.chatSending = false
		m.sessionMessages = nil
		return m.loadSessions()
	}

	return nil
}

func (m *Model) onAgentChanged() tea.Cmd {
	m.cancelStream()
	m.streamErr = nil

	m.chatInput = ""
	m.chatSending = false
	m.chatEditing = false
	m.chatLocalMsgs = nil
	m.creatingSession = false
	m.sessions = nil
	m.sessionMessages = nil
	m.activeSessionKey = ""
	m.activeSessionIdx = 0

	switch m.activeTab {
	case tabLogs:
		m.logEntries = nil
		m.logScroll = 0
		return m.startLogStream()
	case tabActivity:
		m.activityScroll = 0
		m.activityCursor = 0
		m.expandedActivity = -1
		return m.loadSessions()
	}

	return nil
}

func (m *Model) cancelStream() {
	if m.streamCancel != nil {
		m.streamCancel()
		m.streamCancel = nil
	}
}

func (m *Model) loadSessions() tea.Cmd {
	agent := m.currentAgent()
	if agent == nil {
		return nil
	}
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		sessions, err := agent.Sessions(ctx)
		if err != nil || sessions == nil {
			fw := ""
			if agent != nil {
				fw = agent.Framework()
			}
			defaultKey := "agent:main:main"
			if fw == "zeroclaw" {
				defaultKey = "main"
			}
			return sessionsLoadedMsg{sessions: []adapter.Session{
				{Key: defaultKey, Title: "main"},
			}}
		}
		return sessionsLoadedMsg{sessions: sessions}
	}
}

func (m *Model) loadSessionMessages() tea.Cmd {
	agent := m.currentAgent()
	if agent == nil {
		return nil
	}
	key := m.activeSessionKey
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		msgs, err := agent.ChatHistory(ctx, key, 100)
		return sessionMessagesMsg{messages: msgs, err: err}
	}
}

func (m *Model) deleteCurrentSession() tea.Cmd {
	agent := m.currentAgent()
	if agent == nil {
		return nil
	}
	key := m.activeSessionKey
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		err := agent.ResetSession(ctx, key)
		return sessionDeletedMsg{key: key, err: err}
	}
}

func (m *Model) sendChatMessage(text string) tea.Cmd {
	agent := m.currentAgent()
	if agent == nil {
		return func() tea.Msg {
			return chatReplyMsg{err: fmt.Errorf("agent not available")}
		}
	}
	sessionKey := m.activeSessionKey
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
		defer cancel()
		reply, err := agent.SendMessage(ctx, text, sessionKey)
		return chatReplyMsg{reply: reply, err: err}
	}
}

func (m *Model) currentAgent() adapter.Agent {
	if m.cursor >= len(m.agents) || !m.agents[m.cursor].Alive {
		return nil
	}
	return discovery.NewAgent(m.agents[m.cursor].Discovered)
}

func (m *Model) currentAgentFramework() string {
	if m.cursor >= len(m.agents) {
		return ""
	}
	return m.agents[m.cursor].Discovered.Framework
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
		if m.chatEditing {
			lines = []string{
				helpKey("enter") + " send  " + helpKey("esc") + " cancel",
			}
		} else if m.creatingSession {
			lines = []string{
				helpKey("enter") + " create  " + helpKey("esc") + " cancel",
			}
		} else {
			sessionHelp := helpKey("[/]") + " session  "
			if m.currentAgentFramework() != "zeroclaw" {
				sessionHelp += helpKey("n") + " new  " + helpKey("d") + " delete  "
			}
			lines = []string{
				helpKey("\u2191/\u2193") + " scroll  " + helpKey("enter") + " expand  " + helpKey("i") + " chat  " + tabHelp,
				sessionHelp + helpKey("r") + " refresh  " + helpKey("q") + " quit",
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

	tabs := []string{"1:Status", "2:Activity", "3:Logs"}
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
	case tabActivity:
		b.WriteString(m.renderActivityContent())
	case tabLogs:
		b.WriteString(m.renderLogsContent())
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

	var b strings.Builder

	// Session bar
	if len(m.sessions) > 0 {
		var sessionParts []string
		for i, s := range m.sessions {
			name := sessionShortName(s.Key)
			if s.ReadOnly {
				name = s.Title
			}
			if i == m.activeSessionIdx {
				if s.ReadOnly {
					sessionParts = append(sessionParts, dimStyle.Render("["+name+"]"))
				} else {
					sessionParts = append(sessionParts, activeTabStyle.Render(name))
				}
			} else {
				if s.ReadOnly {
					sessionParts = append(sessionParts, dimStyle.Render(name))
				} else {
					sessionParts = append(sessionParts, inactiveTabStyle.Render(name))
				}
			}
		}
		if m.creatingSession {
			cursor := lipgloss.NewStyle().Foreground(lipgloss.Color("42")).Render("█")
			sessionParts = append(sessionParts, lipgloss.NewStyle().Foreground(lipgloss.Color("42")).Render("+ "+m.newSessionName+cursor))
		} else if m.currentAgentFramework() != "zeroclaw" {
			sessionParts = append(sessionParts, dimStyle.Render("[+new]"))
		} else if len(m.sessions) <= 1 {
			sessionParts = append(sessionParts, dimStyle.Render("(single session)"))
		}
		b.WriteString(strings.Join(sessionParts, "  "))
		b.WriteString("\n\n")
	}

	isActiveReadOnly := m.activeSessionIdx < len(m.sessions) && m.sessions[m.activeSessionIdx].ReadOnly
	h := m.detailContentHeight() - 5 // reserve for session bar + input bar

	// Combine session messages + local pending messages
	type msgItem struct {
		ts      time.Time
		role    string
		content string
		isLocal bool
	}

	var items []msgItem
	for _, msg := range m.sessionMessages {
		items = append(items, msgItem{ts: msg.Timestamp, role: msg.Role, content: msg.Content})
	}
	if len(m.chatLocalMsgs) > 0 && len(m.sessionMessages) > 0 {
		items = append(items, msgItem{role: "separator", content: "new messages"})
	}
	for _, msg := range m.chatLocalMsgs {
		items = append(items, msgItem{ts: msg.Time, role: msg.Role, content: msg.Content, isLocal: true})
	}

	if len(items) == 0 && !m.chatSending {
		if isActiveReadOnly {
			b.WriteString(dimStyle.Render("No messages in this archived session."))
		} else {
			b.WriteString(dimStyle.Render("No messages yet. Press i to chat."))
		}
		b.WriteString("\n")
	} else {
		start := m.activityScroll
		end := start + h
		if end > len(items) {
			end = len(items)
		}
		if start > len(items) {
			start = len(items)
		}

		for vi, idx := range makeRange(start, end) {
			item := items[idx]
			isCursor := (vi == m.activityCursor)
			prefix := "  "
			if isCursor {
				prefix = "\u25b8 "
			}

			if item.role == "separator" {
				sep := renderSeparatorLine(item.content, m.width/2)
				b.WriteString(sep + "\n")
				continue
			}

			ts := item.ts.Format("15:04:05")
			var line string
			switch item.role {
			case "user":
				role := userRoleStyle.Render("user:")
				line = fmt.Sprintf("%s%s %s %s", prefix, dimStyle.Render(ts), role, item.content)
			case "assistant":
				role := assistantRoleStyle.Render("assistant:")
				content := item.content
				maxW := m.width/2 - 20
				if maxW > 10 && len(content) > maxW {
					content = content[:maxW] + "..."
				}
				line = fmt.Sprintf("%s%s %s %s", prefix, dimStyle.Render(ts), role, content)
			case "error":
				errLabel := lipgloss.NewStyle().Foreground(lipgloss.Color("196")).Bold(true).Render("error:")
				line = fmt.Sprintf("%s%s %s %s", prefix, dimStyle.Render(ts), errLabel, item.content)
			}

			if isCursor {
				line = selectedStyle.Render(line)
			}
			b.WriteString(line + "\n")

			if m.expandedActivity == idx && len(item.content) > 80 {
				b.WriteString(renderExpandedContent(item.content, m.width/2-4))
			}
		}

		if m.chatSending {
			thinking := lipgloss.NewStyle().Foreground(lipgloss.Color("141")).Render("  assistant: thinking...")
			b.WriteString(thinking + "\n")
		}
	}

	// Pad
	lines := strings.Count(b.String(), "\n")
	for lines < h+2 {
		b.WriteString("\n")
		lines++
	}

	// Input bar
	prompt := lipgloss.NewStyle().Foreground(lipgloss.Color("42")).Bold(true).Render("> ")
	b.WriteString("─────────────────────────────\n")
	if isActiveReadOnly {
		b.WriteString(dimStyle.Render("  archived session (read-only)"))
	} else if m.chatEditing {
		cursor := lipgloss.NewStyle().Foreground(lipgloss.Color("42")).Render("█")
		b.WriteString(prompt + m.chatInput + cursor)
	} else if m.chatSending {
		b.WriteString(dimStyle.Render("  waiting for response..."))
	} else {
		b.WriteString(prompt + dimStyle.Render("press i to type"))
	}

	return b.String()
}

func sessionShortName(key string) string {
	parts := strings.Split(key, ":")
	if len(parts) > 0 {
		return parts[len(parts)-1]
	}
	return key
}

func renderSeparatorLine(label string, width int) string {
	if width < 10 {
		width = 40
	}
	padLen := (width - len(label) - 2) / 2
	if padLen < 3 {
		padLen = 3
	}
	line := strings.Repeat("\u2500", padLen)
	return dimStyle.Render(fmt.Sprintf("%s %s %s", line, label, line))
}

func renderExpandedContent(content string, width int) string {
	if width < 20 {
		width = 60
	}
	var b strings.Builder
	b.WriteString(dimStyle.Render("    \u2502 "))
	b.WriteString("\n")
	for _, line := range wrapText(content, width) {
		b.WriteString(dimStyle.Render("    \u2502 "))
		b.WriteString(line)
		b.WriteString("\n")
	}
	b.WriteString(dimStyle.Render("    \u2502 "))
	b.WriteString("\n")
	return b.String()
}

func wrapText(s string, width int) []string {
	if width <= 0 {
		return []string{s}
	}
	var lines []string
	for len(s) > width {
		brk := strings.LastIndex(s[:width], " ")
		if brk <= 0 {
			brk = width
		}
		lines = append(lines, s[:brk])
		s = strings.TrimLeft(s[brk:], " ")
	}
	if len(s) > 0 {
		lines = append(lines, s)
	}
	return lines
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

func makeRange(start, end int) []int {
	r := make([]int, 0, end-start)
	for i := start; i < end; i++ {
		r = append(r, i)
	}
	return r
}

func appendCapped[T any](s []T, item T, maxSize int) []T {
	s = append(s, item)
	if len(s) > maxSize {
		s = s[len(s)-maxSize:]
	}
	return s
}
