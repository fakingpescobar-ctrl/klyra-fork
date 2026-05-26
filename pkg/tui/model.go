package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type Handler func(string) (string, error)

type CommandDef struct {
	Name        string
	Description string
}

type Config struct {
	Title     string
	SessionID string
	Provider  string
	Model     string
	Handler   Handler
	Commands  []CommandDef
}

type Model struct {
	title          string
	sessionID      string
	provider       string
	model          string
	handler        Handler
	input          textinput.Model
	lines          []string
	width          int
	height         int
	busy           bool
	err            error
	commands       []CommandDef
	filteredCmds   []CommandDef
	selectedCmdIdx int
}

type responseMsg struct {
	input  string
	output string
	err    error
}

func New(cfg Config) Model {
	input := textinput.New()
	input.Placeholder = ""
	input.Prompt = "> "
	input.PromptStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("33"))
	input.TextStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("252"))
	input.Cursor.Style = lipgloss.NewStyle().Foreground(lipgloss.Color("252"))
	input.Focus()
	input.CharLimit = 8000

	title := cfg.Title
	if strings.TrimSpace(title) == "" {
		title = "Antigravity CLI"
	}
	handler := cfg.Handler
	if handler == nil {
		handler = func(string) (string, error) { return "", nil }
	}

	return Model{
		title:          title,
		sessionID:      cfg.SessionID,
		provider:       cfg.Provider,
		model:          cfg.Model,
		handler:        handler,
		input:          input,
		commands:       cfg.Commands,
		lines:          []string{}, // Start empty, header is drawn above
	}
}

func (m Model) Init() tea.Cmd {
	return textinput.Blink
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.input.Width = max(20, msg.Width-2)
		return m, nil
	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c":
			return m, tea.Quit
		case "up", "shift+tab":
			if len(m.filteredCmds) > 0 {
				m.selectedCmdIdx--
				if m.selectedCmdIdx < 0 {
					m.selectedCmdIdx = len(m.filteredCmds) - 1
				}
				return m, nil
			}
		case "down", "tab":
			if len(m.filteredCmds) > 0 {
				m.selectedCmdIdx++
				if m.selectedCmdIdx >= len(m.filteredCmds) {
					m.selectedCmdIdx = 0
				}
				return m, nil
			}
		case "enter":
			if len(m.filteredCmds) > 0 {
				m.input.SetValue(m.filteredCmds[m.selectedCmdIdx].Name + " ")
				m.input.SetCursor(len(m.input.Value()))
				m.filteredCmds = nil
				return m, nil
			}

			value := strings.TrimSpace(m.input.Value())
			if value == "" || m.busy {
				return m, nil
			}
			m.input.SetValue("")
			m.filteredCmds = nil
			if value == "/exit" || value == "/quit" {
				return m, tea.Quit
			}
			if handled, cmd := m.handleLocalCommand(value); handled {
				return m, cmd
			}
			m.busy = true
			m.lines = append(m.lines, "you: "+value)
			return m, runHandler(m.handler, value)
		}
	case responseMsg:
		m.busy = false
		m.err = msg.err
		if msg.err != nil {
			m.lines = append(m.lines, "error: "+msg.err.Error())
		}
		if strings.TrimSpace(msg.output) != "" {
			m.lines = append(m.lines, "agent: "+strings.TrimSpace(msg.output))
		}
		return m, nil
	}

	var cmd tea.Cmd
	prevVal := m.input.Value()
	m.input, cmd = m.input.Update(msg)
	
	if m.input.Value() != prevVal {
		m.updateCompletions()
	}

	return m, cmd
}

func (m *Model) updateCompletions() {
	val := m.input.Value()
	m.filteredCmds = nil
	m.selectedCmdIdx = 0
	if strings.HasPrefix(val, "/") {
		for _, c := range m.commands {
			if strings.HasPrefix(c.Name, val) {
				m.filteredCmds = append(m.filteredCmds, c)
			}
		}
	}
}

func (m Model) View() string {
	// 1. Header (Logo + Info)
	logoLines := []string{
		lipgloss.NewStyle().Foreground(lipgloss.Color("214")).Render("  ▄   "),
		lipgloss.NewStyle().Foreground(lipgloss.Color("208")).Render(" ▄█▄  "),
		lipgloss.NewStyle().Foreground(lipgloss.Color("118")).Render(" █ █  "),
		lipgloss.NewStyle().Foreground(lipgloss.Color("43")).Render(" █ █  "),
		lipgloss.NewStyle().Foreground(lipgloss.Color("33")).Render("█████ "),
		lipgloss.NewStyle().Foreground(lipgloss.Color("27")).Render("█   █ "),
		lipgloss.NewStyle().Foreground(lipgloss.Color("21")).Render("█   █ "),
	}
	infoLines := []string{
		"",
		lipgloss.NewStyle().Foreground(lipgloss.Color("33")).Bold(true).Render(m.title + " 1.0.2"),
		lipgloss.NewStyle().Foreground(lipgloss.Color("244")).Render(valueOr(m.provider, "default")),
		lipgloss.NewStyle().Foreground(lipgloss.Color("244")).Render(valueOr(m.model, "routed")),
		lipgloss.NewStyle().Foreground(lipgloss.Color("244")).Render("Session: " + valueOr(m.sessionID, "ephemeral")),
	}

	var headerLines []string
	for i := 0; i < len(logoLines); i++ {
		info := ""
		if i < len(infoLines) {
			info = infoLines[i]
		}
		headerLines = append(headerLines, logoLines[i]+"  "+info)
	}
	headerLines = append(headerLines, "")

	// 2. Chat history
	var formattedLines []string
	formattedLines = append(formattedLines, headerLines...)

	for _, line := range m.lines {
		if strings.HasPrefix(line, "you: ") {
			formattedLines = append(formattedLines, userMsgStyle.Render("> "+line[5:]))
		} else if strings.HasPrefix(line, "agent: ") {
			agentLines := strings.Split(line[7:], "\n")
			for _, al := range agentLines {
				formattedLines = append(formattedLines, agentMsgStyle.Render(al))
			}
		} else if strings.HasPrefix(line, "error: ") {
			formattedLines = append(formattedLines, errorMsgStyle.Render(line))
		} else {
			formattedLines = append(formattedLines, systemMsgStyle.Render(line))
		}
	}

	// 3. Autocomplete
	var autocomplete string
	autocompleteHeight := 0
	if len(m.filteredCmds) > 0 {
		var lines []string
		for i, c := range m.filteredCmds {
			if i >= 5 {
				break
			}
			style := autocompleteItemStyle
			if i == m.selectedCmdIdx {
				style = autocompleteSelectedStyle
			}
			lines = append(lines, style.Render(fmt.Sprintf("%-20s %s", c.Name, c.Description)))
		}
		autocomplete = strings.Join(lines, "\n")
		autocompleteHeight = len(lines)
	}

	// 4. Footer
	leftFooter := "? for shortcuts"
	rightFooter := valueOr(m.model, "routed")
	spaces := m.width - lipgloss.Width(leftFooter) - lipgloss.Width(rightFooter)
	if spaces < 0 {
		spaces = 0
	}
	footerLine := lipgloss.NewStyle().Foreground(lipgloss.Color("244")).Render(leftFooter + strings.Repeat(" ", spaces) + rightFooter)
	separator := lipgloss.NewStyle().Foreground(lipgloss.Color("238")).Render(strings.Repeat("─", m.width))
	
	footer := lipgloss.JoinVertical(lipgloss.Left, separator, footerLine)

	// Calculate layout
	footerHeight := 2
	inputHeight := 1
	bodyHeight := m.height - footerHeight - inputHeight
	if autocompleteHeight > 0 {
		bodyHeight -= autocompleteHeight
	}

	if bodyHeight < 5 {
		bodyHeight = 5
	}

	bodyLines := visibleTail(formattedLines, bodyHeight)

	// Push content to the bottom
	padding := bodyHeight - len(bodyLines)
	if padding > 0 {
		for i := 0; i < padding; i++ {
			bodyLines = append([]string{""}, bodyLines...)
		}
	}

	body := strings.Join(bodyLines, "\n")

	if autocomplete != "" {
		return lipgloss.JoinVertical(lipgloss.Left,
			body,
			autocomplete,
			m.input.View(),
			footer,
		)
	}

	return lipgloss.JoinVertical(lipgloss.Left,
		body,
		m.input.View(),
		footer,
	)
}

func (m *Model) handleLocalCommand(value string) (bool, tea.Cmd) {
	if value == "/clear" {
		m.lines = nil
		return true, nil
	}
	
	if strings.HasPrefix(value, "/") {
		m.busy = true
		m.lines = append(m.lines, "you: "+value)
		return true, runHandler(m.handler, value)
	}
	
	return false, nil
}

func runHandler(handler Handler, input string) tea.Cmd {
	return func() tea.Msg {
		output, err := handler(input)
		return responseMsg{input: input, output: output, err: err}
	}
}

func visibleTail(lines []string, limit int) []string {
	if len(lines) <= limit {
		return lines
	}
	return lines[len(lines)-limit:]
}

func valueOr(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}

func max(left, right int) int {
	if left > right {
		return left
	}
	return right
}

var (
	userMsgStyle = lipgloss.NewStyle().
		Foreground(lipgloss.Color("33"))

	agentMsgStyle = lipgloss.NewStyle().
		Foreground(lipgloss.Color("252"))

	errorMsgStyle = lipgloss.NewStyle().
		Foreground(lipgloss.Color("196")).
		Bold(true)

	systemMsgStyle = lipgloss.NewStyle().
		Foreground(lipgloss.Color("240")).
		Italic(true)

	autocompleteItemStyle = lipgloss.NewStyle().
		Foreground(lipgloss.Color("244"))

	autocompleteSelectedStyle = lipgloss.NewStyle().
		Foreground(lipgloss.Color("63")).
		Bold(true)
)
