package tui

import (
	"fmt"
	"os"
	"strings"
	"syscall"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/glamour"
	"github.com/charmbracelet/lipgloss"
	"github.com/jaredgizersky/stash/internal/claude"
	"github.com/jaredgizersky/stash/internal/codex"
	"github.com/jaredgizersky/stash/internal/config"
	"github.com/jaredgizersky/stash/internal/store"
	"github.com/muesli/termenv"
)

type view int

const (
	viewStash view = iota
	viewHistory
	viewActive
	viewPreview
)

var tabOrder = []view{viewStash, viewHistory, viewActive}

type Model struct {
	sessions   []claude.Session
	active     []claude.ActiveSession
	stashIndex *store.StashIndex

	// Filtered lists per tab
	stashed        []claude.Session
	filtered       []claude.Session
	filteredActive []claude.ActiveSession

	cursor            int
	offset            int
	width             int
	height            int
	currentView       view
	filterInput       textinput.Model
	filtering         bool
	filterText        string
	showAll           bool
	namedOnly         bool
	cwd               string
	preview           viewport.Model
	previewSession    *claude.Session
	previewReturnView view
	resumeID          string
	resumeCwd         string
	resumeSource      string
}

func New(sessions []claude.Session, active []claude.ActiveSession, stashIdx *store.StashIndex, cwd string, showAll bool) Model {
	ti := textinput.New()
	ti.Placeholder = "filter..."
	ti.CharLimit = 100

	claude.ApplyStashNames(sessions, stashIdx.NameMap(), stashIdx.SourceMap())
	claude.LinkTranscripts(active, sessions)

	m := Model{
		sessions:    sessions,
		active:      active,
		stashIndex:  stashIdx,
		showAll:     showAll,
		cwd:         cwd,
		currentView: viewStash,
		filterInput: ti,
	}
	m.refilter()
	return m
}

// --- Filtering ---

func (m *Model) refilter() {
	query := strings.ToLower(m.filterText)

	// Stash tab
	m.stashed = nil
	for _, s := range m.sessions {
		if !s.Stashed {
			continue
		}
		if !m.matchSession(s, query) {
			continue
		}
		m.stashed = append(m.stashed, s)
	}

	// History tab
	m.filtered = nil
	for _, s := range m.sessions {
		if !m.showAll && s.ProjectPath != m.cwd {
			continue
		}
		if m.namedOnly && !s.HasName() {
			continue
		}
		if !m.matchSession(s, query) {
			continue
		}
		m.filtered = append(m.filtered, s)
	}

	// Active tab
	m.filteredActive = nil
	for _, a := range m.active {
		if !m.showAll && a.Cwd != m.cwd {
			continue
		}
		if m.namedOnly {
			hasName := a.Name != "" || (a.Linked != nil && a.Linked.HasName())
			if !hasName {
				continue
			}
		}
		if query != "" {
			haystack := strings.ToLower(a.DisplayName() + " " + a.ShortCwd())
			if !strings.Contains(haystack, query) {
				continue
			}
		}
		m.filteredActive = append(m.filteredActive, a)
	}

	// Clamp cursor
	if m.cursor >= m.listLen() {
		m.cursor = max(0, m.listLen()-1)
	}
}

func (m *Model) matchSession(s claude.Session, query string) bool {
	if query == "" {
		return true
	}
	haystack := strings.ToLower(s.Title() + " " + s.FirstPrompt + " " + s.GitBranch + " " + s.ShortProject())
	return strings.Contains(haystack, query)
}

func (m Model) listLen() int {
	switch m.currentView {
	case viewStash:
		return len(m.stashed)
	case viewHistory:
		return len(m.filtered)
	case viewActive:
		return len(m.filteredActive)
	default:
		return 0
	}
}

// --- Update ---

func (m Model) Init() tea.Cmd {
	return tea.WindowSize()
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil

	case tea.KeyMsg:
		if msg.String() == "ctrl+c" {
			return m, tea.Quit
		}
		if m.currentView == viewPreview {
			return m.updatePreview(msg)
		}
		if m.filtering {
			return m.updateFilter(msg)
		}
		return m.updateMain(msg)
	}

	return m, nil
}

func (m Model) updateMain(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "q", "esc":
		return m, tea.Quit

	// Navigation
	case "j", "down":
		if m.cursor < m.listLen()-1 {
			m.cursor++
			m.ensureVisible()
		}
	case "k", "up":
		if m.cursor > 0 {
			m.cursor--
			m.ensureVisible()
		}
	case "g":
		m.cursor = 0
		m.offset = 0
	case "G":
		m.cursor = max(0, m.listLen()-1)
		m.ensureVisible()

	// Tab switching
	case "left":
		m.tabPrev()
	case "right":
		m.tabNext()
	case "s":
		m.switchTo(viewStash)
	case "h":
		if m.currentView != viewHistory {
			m.switchTo(viewHistory)
		}
	case "a":
		if m.currentView != viewActive {
			m.switchTo(viewActive)
		}

	// Toggles
	case "tab":
		m.showAll = !m.showAll
		m.refilter()
	case "n":
		m.namedOnly = !m.namedOnly
		m.cursor = 0
		m.offset = 0
		m.refilter()

	// Search
	case "/":
		m.filtering = true
		m.filterInput.Focus()
		return m, textinput.Blink

	// Actions
	case "enter":
		return m.actionPreview()
	case "r":
		return m.actionResume()
	case "d":
		if m.currentView == viewStash {
			m.actionUnstash()
		}
	case "S":
		if m.currentView == viewHistory || m.currentView == viewActive {
			m.actionAddToStash()
		}
	}
	return m, nil
}

func (m Model) updateFilter(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "enter", "esc", "escape":
		m.filtering = false
		m.filterInput.Blur()
		if msg.String() != "enter" {
			m.filterText = ""
			m.filterInput.SetValue("")
		} else {
			m.filterText = m.filterInput.Value()
		}
		m.refilter()
		return m, nil
	}

	var cmd tea.Cmd
	m.filterInput, cmd = m.filterInput.Update(msg)
	m.filterText = m.filterInput.Value()
	m.refilter()
	return m, cmd
}

func (m Model) updatePreview(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "q", "escape", "esc":
		m.currentView = m.previewReturnView
		return m, nil
	case "r", "enter":
		if m.previewSession != nil {
			m.resumeID = m.previewSession.SessionID
			m.resumeCwd = m.previewSession.ProjectPath
			m.resumeSource = m.previewSession.Source
			return m, tea.Quit
		}
	}

	var cmd tea.Cmd
	m.preview, cmd = m.preview.Update(msg)
	return m, cmd
}

// --- Actions ---

func (m Model) actionPreview() (tea.Model, tea.Cmd) {
	switch m.currentView {
	case viewStash:
		if len(m.stashed) > 0 {
			s := m.stashed[m.cursor]
			return m.openPreviewForSession(&s)
		}
	case viewHistory:
		if len(m.filtered) > 0 {
			s := m.filtered[m.cursor]
			return m.openPreviewForSession(&s)
		}
	case viewActive:
		if len(m.filteredActive) > 0 {
			a := m.filteredActive[m.cursor]
			if a.Linked != nil {
				return m.openPreviewForSession(a.Linked)
			}
		}
	}
	return m, nil
}

func (m Model) actionResume() (tea.Model, tea.Cmd) {
	switch m.currentView {
	case viewStash:
		if len(m.stashed) > 0 {
			s := m.stashed[m.cursor]
			m.resumeID = s.SessionID
			m.resumeCwd = s.ProjectPath
			m.resumeSource = s.Source
			return m, tea.Quit
		}
	case viewHistory:
		if len(m.filtered) > 0 {
			s := m.filtered[m.cursor]
			m.resumeID = s.SessionID
			m.resumeCwd = s.ProjectPath
			m.resumeSource = s.Source
			return m, tea.Quit
		}
	case viewActive:
		if len(m.filteredActive) > 0 {
			a := m.filteredActive[m.cursor]
			m.resumeID = a.SessionID
			m.resumeCwd = a.Cwd
			return m, tea.Quit
		}
	}
	return m, nil
}

func (m *Model) actionUnstash() {
	if len(m.stashed) == 0 {
		return
	}
	s := m.stashed[m.cursor]
	m.stashIndex.Remove(s.SessionID)
	_ = store.Save(m.stashIndex)
	for i := range m.sessions {
		if m.sessions[i].SessionID == s.SessionID {
			m.sessions[i].Stashed = false
		}
	}
	m.refilter()
}

func (m *Model) actionAddToStash() {
	var sessionID, name, projectPath, gitBranch, source string

	switch m.currentView {
	case viewHistory:
		if len(m.filtered) == 0 {
			return
		}
		s := m.filtered[m.cursor]
		sessionID = s.SessionID
		name = s.Title()
		projectPath = s.ProjectPath
		gitBranch = s.GitBranch
		source = s.Source
	case viewActive:
		if len(m.filteredActive) == 0 {
			return
		}
		a := m.filteredActive[m.cursor]
		sessionID = a.SessionID
		name = a.DisplayName()
		projectPath = a.Cwd
		if a.Linked != nil {
			gitBranch = a.Linked.GitBranch
		}
	default:
		return
	}

	m.stashIndex.Add(store.StashEntry{
		SessionID:   sessionID,
		Name:        name,
		ProjectPath: projectPath,
		GitBranch:   gitBranch,
		Source:      source,
	})
	_ = store.Save(m.stashIndex)

	for i := range m.sessions {
		if m.sessions[i].SessionID == sessionID {
			m.sessions[i].Stashed = true
		}
	}
	m.refilter()
}

// --- Tab helpers ---

func (m *Model) tabNext() {
	for i, v := range tabOrder {
		if v == m.currentView {
			m.switchTo(tabOrder[(i+1)%len(tabOrder)])
			return
		}
	}
}

func (m *Model) tabPrev() {
	for i, v := range tabOrder {
		if v == m.currentView {
			m.switchTo(tabOrder[(i+len(tabOrder)-1)%len(tabOrder)])
			return
		}
	}
}

func (m *Model) switchTo(v view) {
	m.currentView = v
	m.cursor = 0
	m.offset = 0
	m.refilter()
}

func (m *Model) openPreviewForSession(s *claude.Session) (tea.Model, tea.Cmd) {
	var entries []claude.TranscriptEntry
	var err error
	if s.Source == "codex" {
		entries, err = codex.ReadTranscript(s.FullPath)
	} else {
		entries, err = claude.ReadTranscript(s.FullPath)
	}

	var content string
	if err != nil || len(entries) == 0 {
		content = "(no transcript available)"
	} else {
		content = renderTranscript(entries, m.width-4, s.Source)
	}

	vp := viewport.New(m.width, m.height-4)
	vp.SetContent(content)
	m.preview = vp
	m.previewSession = s
	m.previewReturnView = m.currentView
	m.currentView = viewPreview
	return *m, nil
}

func (m *Model) ensureVisible() {
	lh := m.listHeight()
	if m.cursor < m.offset {
		m.offset = m.cursor
	}
	if m.cursor >= m.offset+lh {
		m.offset = m.cursor - lh + 1
	}
}

func (m Model) listHeight() int {
	overhead := 5
	if m.filtering {
		overhead++
	}
	h := m.height - overhead
	if h < 1 {
		return 1
	}
	return h
}

func (m Model) ResumeInfo() (sessionID, cwd, source string) {
	return m.resumeID, m.resumeCwd, m.resumeSource
}

// --- View ---

var (
	tabActiveStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("230")).
			Background(lipgloss.Color("99")).
			Padding(0, 1)

	tabInactiveStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("241")).
				Padding(0, 1)

	headerStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("241")).
			PaddingLeft(1)

	selectedStyle = lipgloss.NewStyle().
			Background(lipgloss.Color("236")).
			Foreground(lipgloss.Color("230")).
			Bold(true)

	normalStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("252"))

	dimStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("241"))

	stashNameStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("212")).
			Bold(true)

	aliveStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("114"))

	deadStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("203"))

	footerStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("241")).
			PaddingLeft(1)

	previewUserStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("75")).
				Bold(true)

	previewAssistantStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("114"))

	previewToolStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("241"))

	previewTitleStyle = lipgloss.NewStyle().
				Bold(true).
				Foreground(lipgloss.Color("99")).
				PaddingLeft(1).
				PaddingBottom(1)
)

func (m Model) View() string {
	if m.width == 0 {
		return ""
	}
	if m.currentView == viewPreview {
		return m.viewPreview()
	}
	if m.currentView == viewActive {
		return m.viewActiveList()
	}
	return m.viewSessionList()
}

// --- Tab bar ---

func (m Model) renderTabs() string {
	tab := func(label string, count int, isActive bool) string {
		text := fmt.Sprintf("%s (%d)", label, count)
		if isActive {
			return tabActiveStyle.Render(text)
		}
		return tabInactiveStyle.Render(text)
	}

	stash := tab("Stash", len(m.stashed), m.currentView == viewStash)
	history := tab("History", len(m.filtered), m.currentView == viewHistory)
	active := tab("Active", len(m.filteredActive), m.currentView == viewActive)

	scope := "cwd"
	if m.showAll {
		scope = "all"
	}
	if m.namedOnly {
		scope += " · named"
	}
	scopeLabel := dimStyle.Render(fmt.Sprintf(" [%s]", scope))

	return " " + stash + " " + history + " " + active + scopeLabel
}

// --- Session list (shared by Stash & History) ---

func (m Model) viewSessionList() string {
	var b strings.Builder

	b.WriteString(m.renderTabs())
	b.WriteString("\n")

	// Stash tab always shows project column
	showProject := m.showAll || m.currentView == viewStash
	b.WriteString(m.renderSessionHeader(showProject))
	b.WriteString("\n")
	b.WriteString(headerStyle.Render(strings.Repeat("─", min(m.width-2, 140))))
	b.WriteString("\n")

	var items []claude.Session
	if m.currentView == viewStash {
		items = m.stashed
	} else {
		items = m.filtered
	}

	listH := m.listHeight()
	if len(items) == 0 && m.currentView == viewStash {
		b.WriteString(dimStyle.Render("  No stashed sessions. Type \"stash <name>\" in a Claude session to save one."))
		b.WriteString("\n")
		for i := 1; i < listH; i++ {
			b.WriteString("\n")
		}
	} else {
		end := min(m.offset+listH, len(items))
		for i := m.offset; i < end; i++ {
			b.WriteString(m.renderSessionRow(items[i], i == m.cursor, showProject))
			b.WriteString("\n")
		}
		for i := end - m.offset; i < listH; i++ {
			b.WriteString("\n")
		}
	}

	if m.filtering {
		b.WriteString(" " + m.filterInput.View())
		b.WriteString("\n")
	}

	b.WriteString(footerStyle.Render(m.buildFooter()))

	return b.String()
}

func (m Model) calcSessionWidths(showProject bool) colWidths {
	c := colWidths{date: 12, msgs: 5, source: 6}

	if showProject {
		fixed := c.date + c.msgs + c.source + 11
		remaining := m.width - fixed
		c.project = max(remaining*30/100, 12)
		c.branch = max(remaining*25/100, 10)
		c.title = max(remaining-c.project-c.branch, 16)
	} else {
		fixed := c.date + c.msgs + c.source + 9
		remaining := m.width - fixed
		c.branch = max(min(remaining*30/100, 30), 10)
		c.title = max(remaining-c.branch, 16)
	}
	return c
}

type colWidths struct {
	date, title, project, msgs, branch, source int
}

func (m Model) renderSessionHeader(showProject bool) string {
	c := m.calcSessionWidths(showProject)
	if showProject {
		return headerStyle.Render(fmt.Sprintf(" %-*s  %-*s  %-*s  %-*s  %*s  %-*s",
			c.date, "DATE", c.source, "SRC", c.title, "TITLE", c.project, "PROJECT", c.msgs, "MSGS", c.branch, "BRANCH"))
	}
	return headerStyle.Render(fmt.Sprintf(" %-*s  %-*s  %-*s  %*s  %-*s",
		c.date, "DATE", c.source, "SRC", c.title, "TITLE", c.msgs, "MSGS", c.branch, "BRANCH"))
}

func (m Model) renderSessionRow(s claude.Session, selected, showProject bool) string {
	c := m.calcSessionWidths(showProject)

	date := relativeDate(s.Modified)

	src := s.Source
	if src == "" {
		src = "claude"
	}

	title := s.Title()
	if s.HasName() {
		title = "* " + title
	}
	if len(title) > c.title {
		title = title[:c.title-1] + "…"
	}

	branch := s.GitBranch
	if len(branch) > c.branch {
		branch = branch[:c.branch-1] + "…"
	}

	msgs := fmt.Sprintf("%d", s.MsgCount)

	var row string
	if showProject {
		proj := s.ShortProject()
		if len(proj) > c.project {
			proj = "…" + proj[len(proj)-c.project+1:]
		}
		row = fmt.Sprintf(" %-*s  %-*s  %-*s  %-*s  %*s  %-*s",
			c.date, date, c.source, src, c.title, title, c.project, proj, c.msgs, msgs, c.branch, branch)
	} else {
		row = fmt.Sprintf(" %-*s  %-*s  %-*s  %*s  %-*s",
			c.date, date, c.source, src, c.title, title, c.msgs, msgs, c.branch, branch)
	}

	if len(row) > m.width {
		row = row[:m.width]
	}

	if selected {
		return selectedStyle.Render(row)
	}
	if s.HasName() {
		return stashNameStyle.Render(row)
	}
	return normalStyle.Render(row)
}

// --- Active sessions view ---

func (m Model) viewActiveList() string {
	var b strings.Builder

	b.WriteString(m.renderTabs())
	b.WriteString("\n")
	b.WriteString(m.renderActiveHeader())
	b.WriteString("\n")
	b.WriteString(headerStyle.Render(strings.Repeat("─", min(m.width-2, 140))))
	b.WriteString("\n")

	listH := m.listHeight()
	end := min(m.offset+listH, len(m.filteredActive))
	for i := m.offset; i < end; i++ {
		b.WriteString(m.renderActiveRow(m.filteredActive[i], i == m.cursor))
		b.WriteString("\n")
	}
	for i := end - m.offset; i < listH; i++ {
		b.WriteString("\n")
	}

	if m.filtering {
		b.WriteString(" " + m.filterInput.View())
		b.WriteString("\n")
	}

	b.WriteString(footerStyle.Render(m.buildFooter()))

	return b.String()
}

type activeColWidths struct {
	status, pid, uptime, name, project int
}

func (m Model) calcActiveWidths() activeColWidths {
	c := activeColWidths{status: 3, pid: 7, uptime: 10}
	fixed := c.status + c.pid + c.uptime + 7
	remaining := m.width - fixed
	if m.showAll {
		c.project = max(remaining*35/100, 14)
		c.name = max(remaining-c.project, 16)
	} else {
		c.name = remaining
	}
	return c
}

func (m Model) renderActiveHeader() string {
	c := m.calcActiveWidths()
	if m.showAll {
		return headerStyle.Render(fmt.Sprintf("    %-*s  %-*s  %-*s  %-*s",
			c.pid, "PID", c.uptime, "UPTIME", c.name, "NAME", c.project, "PROJECT"))
	}
	return headerStyle.Render(fmt.Sprintf("    %-*s  %-*s  %-*s",
		c.pid, "PID", c.uptime, "UPTIME", c.name, "NAME"))
}

func (m Model) renderActiveRow(a claude.ActiveSession, selected bool) string {
	c := m.calcActiveWidths()

	var dot string
	if a.Alive {
		dot = aliveStyle.Render("●")
	} else {
		dot = deadStyle.Render("○")
	}

	hasName := a.Name != "" || (a.Linked != nil && a.Linked.HasName())
	name := a.DisplayName()
	if hasName {
		name = "* " + name
	}
	if len(name) > c.name {
		name = name[:c.name-1] + "…"
	}

	var row string
	if m.showAll {
		proj := a.ShortCwd()
		if len(proj) > c.project {
			proj = "…" + proj[len(proj)-c.project+1:]
		}
		row = fmt.Sprintf("%-*d  %-*s  %-*s  %-*s",
			c.pid, a.PID, c.uptime, relativeDate(a.Started), c.name, name, c.project, proj)
	} else {
		row = fmt.Sprintf("%-*d  %-*s  %-*s",
			c.pid, a.PID, c.uptime, relativeDate(a.Started), c.name, name)
	}

	if len(row) > m.width-4 {
		row = row[:m.width-4]
	}

	if selected {
		return " " + dot + " " + selectedStyle.Render(row)
	}
	if hasName {
		return " " + dot + " " + stashNameStyle.Render(row)
	}
	return " " + dot + " " + normalStyle.Render(row)
}

// --- Preview ---

func (m Model) viewPreview() string {
	var b strings.Builder

	s := m.previewSession
	b.WriteString(previewTitleStyle.Render(fmt.Sprintf("Preview: %s", s.Title())))
	b.WriteString("\n")
	b.WriteString(dimStyle.Render(fmt.Sprintf(" %s • %s • %d messages",
		s.Modified.Format("2006-01-02 15:04"), s.GitBranch, s.MsgCount)))
	b.WriteString("\n")
	b.WriteString(dimStyle.Render(strings.Repeat("─", min(m.width-2, 140))))
	b.WriteString("\n")
	b.WriteString(m.preview.View())
	b.WriteString("\n")
	b.WriteString(footerStyle.Render(" ↑↓ scroll • enter/r resume • esc/q back"))

	return b.String()
}

func renderTranscript(entries []claude.TranscriptEntry, width int, source string) string {
	renderer, _ := glamour.NewTermRenderer(
		glamour.WithAutoStyle(),
		glamour.WithWordWrap(width-4),
	)

	var b strings.Builder
	for i, e := range entries {
		prevTool := i > 0 && entries[i-1].Tool
		prevAssistant := i > 0 && entries[i-1].Role == "assistant" && !entries[i-1].Tool
		if i > 0 && !(e.Tool && prevTool) && !(e.Tool && prevAssistant) {
			b.WriteString("\n")
		}
		text := e.Text
		if len(text) > 2000 {
			text = text[:2000] + "\n... (truncated)"
		}
		if e.Tool {
			for _, line := range strings.Split(text, "\n") {
				b.WriteString(previewToolStyle.Render("  " + line))
				b.WriteString("\n")
			}
			continue
		}
		switch e.Role {
		case "user":
			b.WriteString(previewUserStyle.Render("You:"))
			b.WriteString("\n")
			b.WriteString(text)
		case "assistant":
			label := "Claude:"
			if source == "codex" {
				label = "Codex:"
			}
			b.WriteString(previewAssistantStyle.Render(label))
			b.WriteString("\n")
			if renderer != nil {
				rendered, err := renderer.Render(text)
				if err == nil {
					text = strings.TrimSpace(rendered)
				}
			}
			b.WriteString(text)
		}
		b.WriteString("\n")
	}
	return b.String()
}

// --- Footer ---

func (m Model) buildFooter() string {
	parts := []string{"↑↓/jk navigate", "enter preview", "r resume"}
	if m.currentView == viewStash {
		parts = append(parts, "d unstash")
	}
	if m.currentView == viewHistory || m.currentView == viewActive {
		parts = append(parts, "S stash")
	}
	parts = append(parts, "/ filter", "tab scope", "n named", "←→ tabs", "q quit")
	return " " + strings.Join(parts, " • ")
}

// --- Helpers ---

func relativeDate(t time.Time) string {
	now := time.Now()
	diff := now.Sub(t)
	switch {
	case diff < time.Minute:
		return "just now"
	case diff < time.Hour:
		return fmt.Sprintf("%dm ago", int(diff.Minutes()))
	case diff < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(diff.Hours()))
	case diff < 7*24*time.Hour:
		return fmt.Sprintf("%dd ago", int(diff.Hours()/24))
	default:
		return t.Format("Jan 02")
	}
}

// --- Public API ---

func Run(sessions []claude.Session, active []claude.ActiveSession, stashIdx *store.StashIndex, cwd string, showAll bool) (string, string, string, error) {
	m := New(sessions, active, stashIdx, cwd, showAll)
	options := []tea.ProgramOption{}
	if os.Getenv("STASH_FORCE_COLOR") != "" {
		lipgloss.SetColorProfile(termenv.TrueColor)
	}
	if os.Getenv("STASH_FORCE_STDIN") != "" {
		options = append(options, tea.WithInput(os.Stdin))
	}
	if os.Getenv("STASH_NO_ALT_SCREEN") == "" {
		options = append(options, tea.WithAltScreen())
	}
	p := tea.NewProgram(m, options...)
	result, err := p.Run()
	if err != nil {
		return "", "", "", err
	}
	final := result.(Model)
	sid, cwdPath, source := final.ResumeInfo()
	return sid, cwdPath, source, nil
}

func Resume(sessionID, cwd, source string) error {
	if cwd != "" {
		if err := os.Chdir(cwd); err != nil {
			return fmt.Errorf("cd to %s: %w", cwd, err)
		}
	}

	if source == "codex" {
		bin, err := findBinary("codex")
		if err != nil {
			return err
		}
		args := []string{"codex", "resume"}
		if shouldDangerouslySkipPermissions() {
			args = append(args, "--yolo")
		}
		args = append(args, sessionID)
		return syscall.Exec(bin, args, os.Environ())
	}

	bin, err := findBinary("claude")
	if err != nil {
		return err
	}
	args := []string{"claude", "--resume", sessionID}
	if shouldDangerouslySkipPermissions() {
		args = append(args, "--dangerously-skip-permissions")
	}
	return syscall.Exec(bin, args, os.Environ())
}

func shouldDangerouslySkipPermissions() bool {
	enabled, err := config.DangerouslySkipPermissions()
	if err != nil {
		fmt.Fprintf(os.Stderr, "stash: failed to load config: %v\n", err)
		return false
	}
	return enabled
}

func findBinary(name string) (string, error) {
	pathEnv := os.Getenv("PATH")
	for _, dir := range strings.Split(pathEnv, ":") {
		p := dir + "/" + name
		if _, err := os.Stat(p); err == nil {
			return p, nil
		}
	}
	return "", fmt.Errorf("%s binary not found in PATH", name)
}
