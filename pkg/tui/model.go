package tui

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/glamour"
	"github.com/charmbracelet/lipgloss"
)

// ---------------------------------------------------------------------------
// Color palette
// ---------------------------------------------------------------------------

var (
	colorBrand     = lipgloss.Color("#A78BFA") // violet
	colorBrandDim  = lipgloss.Color("#7C3AED") // deeper violet
	colorBlue      = lipgloss.Color("#60A5FA") // soft blue
	colorText      = lipgloss.Color("#E5E7EB") // off-white
	colorDim       = lipgloss.Color("#6B7280") // warm gray
	colorMuted     = lipgloss.Color("#4B5563") // muted
	colorSeparator = lipgloss.Color("#374151") // charcoal
	colorSurface   = lipgloss.Color("#1F2937") // dark surface
	colorEmerald   = lipgloss.Color("#34D399") // green
	colorAmber     = lipgloss.Color("#FBBF24") // amber
	colorRed       = lipgloss.Color("#F87171") // soft red
	colorBadgeBg   = lipgloss.Color("#312E81") // indigo dark bg
	colorBadgeBg2  = lipgloss.Color("#1E3A5F") // blue dark bg
	colorBadgeBg3  = lipgloss.Color("#3B1F5E") // purple dark bg
	colorBadgeBg4  = lipgloss.Color("#1A3636") // teal dark bg
	colorWhite     = lipgloss.Color("#F9FAFB") // near-white
	colorInputBg   = lipgloss.Color("#111827") // very dark bg
)

// ---------------------------------------------------------------------------
// Spinner
// ---------------------------------------------------------------------------

// Saturated gradient animation for the thinking indicator.
var gradientPalette = []lipgloss.Color{
	"#A855F7", // purple
	"#8B5CF6", // violet
	"#6366F1", // indigo
	"#3B82F6", // blue
	"#0EA5E9", // sky
	"#06B6D4", // cyan
}

// Pulse sizes: bar width oscillates for a breathing effect.
var pulseSizes = []int{3, 4, 5, 7, 9, 11, 12, 12, 11, 9, 7, 5, 4, 3}

// Block density chars for soft-edge rendering.
var densityChars = []rune{'░', '▒', '▓', '█'}

const animTotalFrames = 56 // LCM-ish of palette and pulse lengths

type spinnerTickMsg time.Time

func tickSpinner() tea.Cmd {
	return tea.Tick(100*time.Millisecond, func(t time.Time) tea.Msg {
		return spinnerTickMsg(t)
	})
}

// ---------------------------------------------------------------------------
// Types
// ---------------------------------------------------------------------------

type Handler func(string) (string, error)

type PickerProvider func(field string) (PickerModal, error)

type StreamMsg string

type ReasoningMsg string

type ToolStreamMsg struct {
	ID             string
	Name           string
	ArgumentsDelta string
}

type ToolProgressMsg struct {
	Phase  string
	Tool   string
	ID     string
	Args   map[string]any
	Output string
	Error  string
}

type ApprovalRequestMsg struct {
	Tool   string
	Risk   string
	Reason string
	Args   map[string]any
	Reply  chan bool
}

type CommandDef struct {
	Name        string
	Description string
}

type Config struct {
	CWD             string
	Title           string
	SessionID       string
	Provider        string
	Model           string
	BaseURL         string
	Reasoning       string
	Sandbox         string
	Approval        string
	Mode            string
	CartCount       int
	MaxContext      int
	MaxOutput       int
	MaxSteps        int
	MaxMessages     int
	MaxInstructions int
	FastModel       string
	EditModel       string
	DeepModel       string
	Handler         Handler
	PickerProvider  PickerProvider
	Commands        []CommandDef
	InitialLines    []string
}

type modalKind int

const (
	modalNone modalKind = iota
	modalPicker
	modalSettings
	modalHelp
)

type Model struct {
	cwd             string
	title           string
	sessionID       string
	provider        string
	model           string
	baseURL         string
	reasoning       string
	sandbox         string
	approval        string
	mode            string
	cartCount       int
	maxContext      int
	maxOutput       int
	maxSteps        int
	maxMessages     int
	maxInstructions int
	fastModel       string
	editModel       string
	deepModel       string
	handler         Handler
	pickerProvider  PickerProvider
	input           textinput.Model
	lines           []string
	width           int
	height          int
	busy            bool
	err             error
	commands        []CommandDef
	filteredCmds    []CommandDef
	selectedCmdIdx  int
	streamBuf       string
	renderer        *glamour.TermRenderer
	approvalReq     *ApprovalRequestMsg
	spinnerFrame    int
	viewport        viewport.Model
	contextDebug    string
	debugExpanded   bool
	history         []string
	historyIdx      int
	tempInput       string
	reasoningText   string
	reasonExpanded  bool
	copyMode        bool

	// Modal state
	activeModal   modalKind
	pickerModal   *PickerModal
	helpModal     *HelpModal
	settingsModal *SettingsModal
}

type responseMsg struct {
	input    string
	output   string
	err      error
	agentRun bool
}

type pickerLoadedMsg struct {
	picker PickerModal
	err    error
}

type SessionLoadedMsg struct {
	SessionID string
	Lines     []string
}

func New(cfg Config) Model {
	input := textinput.New()
	input.Placeholder = "Ask anything or type / for commands..."
	input.Prompt = "  > "
	input.PromptStyle = lipgloss.NewStyle().Foreground(colorBrand).Bold(true)
	input.TextStyle = lipgloss.NewStyle().Foreground(colorText)
	input.Cursor.Style = lipgloss.NewStyle().Foreground(colorBrand)
	input.Cursor.Blink = false
	input.PlaceholderStyle = lipgloss.NewStyle().Foreground(colorMuted).Italic(true)
	input.Focus()
	input.CharLimit = 8000

	title := cfg.Title
	if strings.TrimSpace(title) == "" {
		title = "Klyra"
	}
	handler := cfg.Handler
	if handler == nil {
		handler = func(string) (string, error) { return "", nil }
	}

	renderer, _ := glamour.NewTermRenderer(
		glamour.WithStandardStyle("dark"),
		glamour.WithPreservedNewLines(),
		glamour.WithWordWrap(80),
	)

	m := Model{
		cwd:             cfg.CWD,
		title:           title,
		sessionID:       cfg.SessionID,
		provider:        cfg.Provider,
		model:           cfg.Model,
		baseURL:         cfg.BaseURL,
		reasoning:       cfg.Reasoning,
		sandbox:         cfg.Sandbox,
		approval:        cfg.Approval,
		mode:            cfg.Mode,
		cartCount:       cfg.CartCount,
		maxContext:      cfg.MaxContext,
		maxOutput:       cfg.MaxOutput,
		maxSteps:        cfg.MaxSteps,
		maxMessages:     cfg.MaxMessages,
		maxInstructions: cfg.MaxInstructions,
		fastModel:       cfg.FastModel,
		editModel:       cfg.EditModel,
		deepModel:       cfg.DeepModel,
		handler:         handler,
		pickerProvider:  cfg.PickerProvider,
		input:           input,
		commands:        cfg.Commands,
		filteredCmds:    nil,
		selectedCmdIdx:  0,
		renderer:        renderer,
		lines:           append([]string(nil), cfg.InitialLines...),
		viewport:        viewport.New(80, 20),
		history:         []string{},
		historyIdx:      0,
		reasoningText:   "",
		reasonExpanded:  false,
	}
	m.width = 80
	m.height = 24
	m.syncViewport(true)
	return m
}

func (m Model) Init() tea.Cmd {
	return nil
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.input.Width = max(20, msg.Width-6)
		m.renderer, _ = glamour.NewTermRenderer(
			glamour.WithStandardStyle("dark"),
			glamour.WithPreservedNewLines(),
			glamour.WithWordWrap(max(40, m.width-8)),
		)
		m.syncViewport(true)
		return m, nil
	case spinnerTickMsg:
		if m.busy {
			m.spinnerFrame = (m.spinnerFrame + 1) % animTotalFrames
			m.syncViewport(false)
			return m, tickSpinner()
		}
		return m, nil
	case tea.MouseMsg:
		switch msg.Type {
		case tea.MouseLeft:
			if m.handleViewportClick(msg.Y) {
				m.syncViewport(false)
				return m, tea.ClearScreen
			}
		case tea.MouseWheelUp:
			m.viewport.LineUp(3)
			return m, nil
		case tea.MouseWheelDown:
			m.viewport.LineDown(3)
			return m, nil
		}
	case tea.KeyMsg:
		// Approval prompt takes highest priority
		if m.approvalReq != nil {
			switch msg.String() {
			case "y", "Y", "enter":
				m.approvalReq.Reply <- true
				m.lines = append(m.lines, "system: approved "+m.approvalReq.Tool)
				m.approvalReq = nil
				return m, nil
			case "n", "N", "esc":
				m.approvalReq.Reply <- false
				m.lines = append(m.lines, "system: rejected "+m.approvalReq.Tool)
				m.approvalReq = nil
				return m, nil
			}
			return m, nil
		}

		// Modal routing
		if m.activeModal != modalNone {
			return m.updateModal(msg)
		}

		switch msg.String() {
		case "ctrl+c":
			return m, tea.Quit
		case "f2", "ctrl+s":
			m.openSettingsModal()
			return m, nil
		case "f3":
			m.debugExpanded = !m.debugExpanded
			m.syncViewport(m.debugExpanded)
			return m, nil
		case "f4":
			m.toggleLatestThoughts()
			m.syncViewport(true)
			return m, tea.ClearScreen
		case "f6":
			m.copyMode = !m.copyMode
			if m.copyMode {
				return m, tea.Batch(tea.DisableMouse, tea.ExitAltScreen)
			}
			return m, tea.Batch(tea.EnterAltScreen, tea.EnableMouseCellMotion)
		case "pgup":
			m.viewport.PageUp()
			return m, nil
		case "pgdn":
			m.viewport.PageDown()
			return m, nil
		case "right", "l":
			if m.toggleLatestThoughtsExpand(true) {
				m.syncViewport(true)
				return m, tea.ClearScreen
			}
		case "left", "h":
			if m.toggleLatestThoughtsExpand(false) {
				m.syncViewport(true)
				return m, tea.ClearScreen
			}
		case "shift+up":
			m.viewport.LineUp(1)
			return m, nil
		case "shift+down":
			m.viewport.LineDown(1)
			return m, nil
		case "up":
			if len(m.filteredCmds) > 0 {
				m.selectedCmdIdx--
				if m.selectedCmdIdx < 0 {
					m.selectedCmdIdx = len(m.filteredCmds) - 1
				}
				return m, nil
			}
			return m.historyPrevious()
		case "shift+tab":
			if len(m.filteredCmds) > 0 {
				m.selectedCmdIdx--
				if m.selectedCmdIdx < 0 {
					m.selectedCmdIdx = len(m.filteredCmds) - 1
				}
				return m, nil
			}
		case "down":
			if len(m.filteredCmds) > 0 {
				m.selectedCmdIdx++
				if m.selectedCmdIdx >= len(m.filteredCmds) {
					m.selectedCmdIdx = 0
				}
				return m, nil
			}
			return m.historyNext()
		case "tab":
			if len(m.filteredCmds) > 0 {
				m.selectedCmdIdx++
				if m.selectedCmdIdx >= len(m.filteredCmds) {
					m.selectedCmdIdx = 0
				}
				return m, nil
			}
		case "ctrl+up", "ctrl+p":
			return m.historyPrevious()
		case "ctrl+down", "ctrl+n":
			return m.historyNext()
		case "enter":
			if len(m.filteredCmds) > 0 {
				m.input.SetValue(m.filteredCmds[m.selectedCmdIdx].Name + " ")
				m.input.SetCursor(len(m.input.Value()))
				m.filteredCmds = nil
				return m, nil
			}

			value := strings.TrimSpace(m.input.Value())
			if value == "" {
				if m.toggleLatestThoughts() {
					m.syncViewport(false)
					return m, tea.ClearScreen
				}
				return m, nil
			}
			if len(m.history) == 0 || m.history[len(m.history)-1] != value {
				m.history = append(m.history, value)
			}
			m.historyIdx = len(m.history)
			m.tempInput = ""

			m.input.SetValue("")
			m.filteredCmds = nil
			if value == "/exit" || value == "/quit" {
				return m, tea.Quit
			}
			if handled, cmd := m.handleLocalCommand(value); handled {
				return m, cmd
			}
			if m.busy {
				m.lines = append(m.lines, "", "system: agent is still running; slash commands remain available")
				m.syncViewport(true)
				return m, nil
			}
			m.busy = true
			m.streamBuf = ""
			m.reasoningText = ""
			m.reasonExpanded = false
			m.spinnerFrame = 0
			if len(m.lines) > 0 {
				m.lines = append(m.lines, "")
			}
			m.lines = append(m.lines, "you: "+value)
			m.syncViewport(true)
			return m, tea.Batch(runHandler(m.handler, value, true), tickSpinner())
		}
	case StreamMsg:
		wasAtBottom := m.viewport.AtBottom()
		m.streamBuf += string(msg)
		m.syncViewport(wasAtBottom)
		return m, nil
	case ReasoningMsg:
		wasAtBottom := m.viewport.AtBottom()
		m.reasoningText += string(msg)
		m.syncViewport(wasAtBottom)
		return m, nil
	case ToolStreamMsg:
		wasAtBottom := m.viewport.AtBottom()
		m.appendToolStream(msg)
		m.syncViewport(wasAtBottom)
		return m, nil
	case ToolProgressMsg:
		wasAtBottom := m.viewport.AtBottom()
		m.appendToolProgress(msg)
		m.syncViewport(wasAtBottom)
		return m, nil
	case ApprovalRequestMsg:
		wasAtBottom := m.viewport.AtBottom()
		m.approvalReq = &msg
		m.syncViewport(wasAtBottom)
		return m, nil
	case pickerLoadedMsg:
		if msg.err != nil {
			m.lines = append(m.lines, "", "error: "+msg.err.Error())
			m.syncViewport(true)
			return m, nil
		}
		m.openPickerModal(msg.picker)
		return m, nil
	case SessionLoadedMsg:
		m.sessionID = msg.SessionID
		m.lines = append([]string(nil), msg.Lines...)
		m.contextDebug = ""
		m.streamBuf = ""
		m.reasoningText = ""
		m.reasonExpanded = false
		m.syncViewport(true)
		return m, nil
	case responseMsg:
		wasAtBottom := m.viewport.AtBottom()
		if msg.agentRun {
			m.busy = false
		}
		streamedText := strings.TrimSpace(m.streamBuf)
		if msg.agentRun {
			m.streamBuf = ""
		}
		m.err = msg.err
		if msg.err != nil {
			m.lines = append(m.lines, "")
			m.lines = append(m.lines, "error: "+msg.err.Error())
		}
		if msg.agentRun && streamedText != "" {
			m.lines = append(m.lines, "")
			m.appendThoughtsOutput(m.reasoningText, false)
			m.reasoningText = ""
			m.reasonExpanded = false
			m.appendAgentOutput(streamedText)
		}

		outText := strings.TrimSpace(msg.output)
		var debugText string
		if idx := strings.Index(outText, "## Context Debugger"); idx >= 0 {
			debugText = strings.TrimSpace(outText[idx:])
			outText = strings.TrimSpace(outText[:idx])
		}
		m.contextDebug = debugText

		if outText != "" {
			m.lines = append(m.lines, "")

			isAgentStream := strings.Contains(outText, "assistant: ") || strings.Contains(outText, "tool: ")
			if !isAgentStream {
				text := outText
				if m.renderer != nil && (strings.HasPrefix(text, "#") || strings.HasPrefix(text, "-") || strings.HasPrefix(text, "*") || strings.Contains(text, "`")) {
					if rendered, errRender := m.renderer.Render(text); errRender == nil {
						text = strings.TrimRight(rendered, " \n\r\t")
					}
				}
				for _, line := range strings.Split(text, "\n") {
					m.lines = append(m.lines, "md: "+line)
				}
			} else {
				var assistantBlock []string
				var mdBlock []string

				flushAssistant := func() {
					if len(assistantBlock) > 0 {
						m.appendThoughtsOutput(m.reasoningText, false)
						m.reasoningText = ""
						m.reasonExpanded = false
						m.appendAgentOutput(strings.Join(assistantBlock, "\n"))
						assistantBlock = nil
					}
				}

				flushMd := func() {
					if len(mdBlock) > 0 {
						text := strings.Join(mdBlock, "\n")
						if m.renderer != nil && (strings.HasPrefix(text, "#") || strings.HasPrefix(text, "-") || strings.HasPrefix(text, "*") || strings.Contains(text, "`")) {
							if rendered, errRender := m.renderer.Render(text); errRender == nil {
								text = strings.TrimRight(rendered, " \n\r\t")
							}
						}
						for _, line := range strings.Split(text, "\n") {
							m.lines = append(m.lines, "md: "+line)
						}
						mdBlock = nil
					}
				}

				inAssistant := false

				for _, line := range strings.Split(outText, "\n") {
					if strings.HasPrefix(line, "assistant: ") {
						flushMd()
						inAssistant = true
						assistantBlock = append(assistantBlock, strings.TrimPrefix(line, "assistant: "))
					} else if strings.HasPrefix(line, "tool: ") || strings.HasPrefix(line, "tool rejected:") || strings.HasPrefix(line, "tool error:") || strings.HasPrefix(line, "usage:") || strings.HasPrefix(line, "policy:") {
						flushAssistant()
						flushMd()
						inAssistant = false
						m.lines = append(m.lines, line)
					} else if strings.TrimSpace(line) == "" {
						if inAssistant {
							if len(assistantBlock) > 0 {
								assistantBlock = append(assistantBlock, "")
							} else {
								m.lines = append(m.lines, "")
							}
						} else {
							if len(mdBlock) > 0 {
								mdBlock = append(mdBlock, "")
							} else {
								m.lines = append(m.lines, "")
							}
						}
					} else {
						if inAssistant {
							assistantBlock = append(assistantBlock, line)
						} else {
							mdBlock = append(mdBlock, line)
						}
					}
				}
				flushAssistant()
				flushMd()
			}
		}
		m.syncViewport(wasAtBottom)
		return m, nil
	}

	var cmd tea.Cmd
	prevVal := m.input.Value()
	m.input, cmd = m.input.Update(msg)

	if m.input.Value() != prevVal {
		m.updateCompletions()
	}

	var vpCmd tea.Cmd
	m.viewport, vpCmd = m.viewport.Update(msg)
	m.syncViewport(false)

	return m, tea.Batch(cmd, vpCmd)
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

func (m Model) calculateBodyHeight() int {
	footerHeight := 2 // separator + footer text
	inputHeight := 2  // separator + input text
	autocompleteHeight := 0
	if len(m.filteredCmds) > 0 {
		maxItems := 5
		itemsCount := len(m.filteredCmds)
		if itemsCount > maxItems {
			itemsCount = maxItems
		}
		autocompleteHeight = itemsCount + 3
	}
	bodyHeight := m.height - footerHeight - inputHeight - autocompleteHeight
	if bodyHeight < 5 {
		bodyHeight = 5
	}
	return bodyHeight
}

func (m Model) buildFormattedLines() []string {
	headerLines := strings.Split(m.renderHeader(), "\n")

	var formattedLines []string
	formattedLines = append(formattedLines, headerLines...)
	formattedLines = append(formattedLines, "") // breathing room after header

	for _, line := range m.lines {
		if strings.HasPrefix(line, "you: ") {
			formattedLines = append(formattedLines, userMsgStyle.Render("  "+userPrefix+" "+line[5:]))
		} else if strings.HasPrefix(line, "agent: ") {
			text := line[7:]
			if m.renderer != nil {
				if rendered, err := m.renderer.Render(text); err == nil {
					text = strings.TrimRight(rendered, " \n\r\t")
				}
			}
			agentLines := strings.Split(text, "\n")
			for _, al := range agentLines {
				formattedLines = append(formattedLines, agentBarStyle.Render(agentBar)+" "+agentMsgStyle.Render(al)+"\x1b[0m")
			}
		} else if strings.HasPrefix(line, "thoughts:") {
			formattedLines = append(formattedLines, m.renderThoughtBlock(line)...)
		} else if strings.HasPrefix(line, "error: ") {
			formattedLines = append(formattedLines, errorMsgStyle.Render("  "+errorPrefix+" "+line[7:]))
		} else if strings.HasPrefix(line, "system: ") {
			formattedLines = append(formattedLines, systemMsgStyle.Render("  "+systemPrefix+" "+line[8:]))
		} else if strings.HasPrefix(line, "toolstream: ") {
			formattedLines = append(formattedLines, m.renderToolStreamLine(line[len("toolstream: "):])...)
		} else if strings.HasPrefix(line, "toolprogress:") {
			formattedLines = append(formattedLines, m.renderToolProgressLine(line)...)
		} else if strings.HasPrefix(line, "tool:") {
			formattedLines = append(formattedLines, m.renderToolLine(line)...)
		} else if strings.HasPrefix(line, "tool rejected: ") {
			formattedLines = append(formattedLines, lipgloss.NewStyle().Foreground(colorRed).Bold(true).Render("  tool rejected: "+line[15:]))
		} else if strings.HasPrefix(line, "tool error: ") {
			formattedLines = append(formattedLines, lipgloss.NewStyle().Foreground(colorRed).Bold(true).Render("  tool error: "+line[12:]))
		} else if strings.HasPrefix(line, "usage: ") {
			formattedLines = append(formattedLines, lipgloss.NewStyle().Foreground(colorDim).Render("  usage: "+line[7:]))
		} else if strings.HasPrefix(line, "policy: ") {
			formattedLines = append(formattedLines, lipgloss.NewStyle().Foreground(colorAmber).Render("  policy: "+line[8:]))
		} else if strings.HasPrefix(line, "md: ") {
			formattedLines = append(formattedLines, renderCommandOutputLine(line[4:]))
		} else if line == "" {
			formattedLines = append(formattedLines, "")
		} else {
			formattedLines = append(formattedLines, systemMsgStyle.Render("  "+systemPrefix+" "+line))
		}
	}

	// Streaming content with spinner
	if m.busy && m.streamBuf != "" {
		formattedLines = append(formattedLines, "")
		if strings.TrimSpace(m.reasoningText) != "" {
			formattedLines = append(formattedLines, m.renderLiveThoughtBlock()...)
			formattedLines = append(formattedLines, "")
		}

		var rendered string
		var err error
		if m.renderer != nil {
			rendered, err = m.renderer.Render(m.streamBuf)
		} else {
			err = fmt.Errorf("no renderer")
		}

		var agentLines []string
		if err == nil {
			agentLines = strings.Split(strings.TrimRight(rendered, "\n"), "\n")
		} else {
			agentLines = strings.Split(m.streamBuf, "\n")
		}

		for _, al := range agentLines {
			formattedLines = append(formattedLines, agentBarStyle.Render(agentBar)+" "+agentMsgStyle.Render(al)+"\x1b[0m")
		}
	} else if m.busy {
		formattedLines = append(formattedLines, "")
		if strings.TrimSpace(m.reasoningText) != "" {
			formattedLines = append(formattedLines, m.renderLiveThoughtBlock()...)
			formattedLines = append(formattedLines, "")
		}
		formattedLines = append(formattedLines, m.renderThinkingBar())
	}

	if m.contextDebug != "" {
		formattedLines = append(formattedLines, "")
		if m.debugExpanded {
			formattedLines = append(formattedLines, lipgloss.NewStyle().Foreground(colorBrandDim).Render("  [F3] Hide Context Debugger"))

			// Render the debugger text via Glamour
			text := m.contextDebug
			if m.renderer != nil {
				if rendered, errRender := m.renderer.Render(text); errRender == nil {
					text = strings.TrimRight(rendered, " \n\r\t")
				}
			}
			for _, line := range strings.Split(text, "\n") {
				formattedLines = append(formattedLines, "  "+line)
			}
		} else {
			formattedLines = append(formattedLines, lipgloss.NewStyle().Foreground(colorBrandDim).Render("  [F3] Show Context Debugger"))
		}
	}

	return formattedLines
}

func (m *Model) syncViewport(scrollToBottom bool) {
	m.viewport.Width = m.width
	m.viewport.Height = m.calculateBodyHeight()

	lines := m.buildFormattedLines()
	padding := m.viewport.Height - len(lines)
	if padding > 0 {
		paddedLines := make([]string, padding)
		for i := range paddedLines {
			paddedLines[i] = ""
		}
		lines = append(paddedLines, lines...)
	}

	m.viewport.SetContent(strings.Join(lines, "\n"))
	if scrollToBottom {
		m.viewport.GotoBottom()
	}
}

// ---------------------------------------------------------------------------
// View
// ---------------------------------------------------------------------------

func (m Model) View() string {
	// Autocomplete
	var autocomplete string
	if len(m.filteredCmds) > 0 {
		autocomplete, _ = m.renderAutocomplete()
	}

	// Footer
	footer := m.renderFooter()

	// Input with separator
	inputSep := lipgloss.NewStyle().Foreground(colorSeparator).Render(strings.Repeat("─", max(10, m.width)))
	inputView := inputSep + "\n" + m.input.View()

	// Body from viewport
	body := m.viewport.View()

	// Overlays: approval prompt
	if m.approvalReq != nil {
		body = centerOverlay(m.width, m.viewport.Height, m.renderApprovalModal())
	}

	// Modal overlays
	switch m.activeModal {
	case modalPicker:
		if m.pickerModal != nil {
			body = centerOverlay(m.width, m.viewport.Height, m.pickerModal.View(m.width))
		}
	case modalHelp:
		if m.helpModal != nil {
			body = centerOverlay(m.width, m.viewport.Height, m.helpModal.View(m.width, m.height))
		}
	case modalSettings:
		if m.settingsModal != nil {
			body = centerOverlay(m.width, m.viewport.Height, m.settingsModal.View(m.width, m.height))
		}
	}

	if autocomplete != "" {
		return lipgloss.JoinVertical(lipgloss.Left,
			body,
			autocomplete,
			inputView,
			footer,
		)
	}

	return lipgloss.JoinVertical(lipgloss.Left,
		body,
		inputView,
		footer,
	)
}

// ---------------------------------------------------------------------------
// Autocomplete
// ---------------------------------------------------------------------------

func (m Model) renderAutocomplete() (string, int) {
	var lines []string
	maxItems := 5
	startIdx := m.selectedCmdIdx - maxItems/2
	if startIdx < 0 {
		startIdx = 0
	}
	if startIdx+maxItems > len(m.filteredCmds) {
		startIdx = len(m.filteredCmds) - maxItems
		if startIdx < 0 {
			startIdx = 0
		}
	}

	for i := startIdx; i < startIdx+maxItems && i < len(m.filteredCmds); i++ {
		c := m.filteredCmds[i]
		if i == m.selectedCmdIdx {
			line := acSelectedStyle.Render(fmt.Sprintf(" %s %-18s %s ", acPointer, c.Name, c.Description))
			lines = append(lines, line)
		} else {
			line := acItemStyle.Render(fmt.Sprintf("   %-18s %s ", c.Name, c.Description))
			lines = append(lines, line)
		}
	}

	// Hint line
	hint := acHintStyle.Render("  up/down navigate  enter select  esc dismiss")
	lines = append(lines, hint)

	content := strings.Join(lines, "\n")

	box := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(colorMuted).
		Render(content)

	return box, lipgloss.Height(box)
}

// ---------------------------------------------------------------------------
// Approval modal
// ---------------------------------------------------------------------------

func (m Model) renderApprovalModal() string {
	req := m.approvalReq
	if req == nil {
		return ""
	}

	titleStyle := lipgloss.NewStyle().Foreground(colorAmber).Bold(true)
	labelStyle := lipgloss.NewStyle().Foreground(colorDim)
	valueStyle := lipgloss.NewStyle().Foreground(colorText).Bold(true)
	keyStyle := lipgloss.NewStyle().Foreground(colorEmerald).Bold(true)
	keyRejectStyle := lipgloss.NewStyle().Foreground(colorRed).Bold(true)

	lines := []string{
		titleStyle.Render("Approval required"),
		"",
		labelStyle.Render("tool:   ") + valueStyle.Render(req.Tool),
	}
	if req.Risk != "" {
		lines = append(lines, labelStyle.Render("risk:   ")+valueStyle.Render(req.Risk))
	}
	if req.Reason != "" {
		lines = append(lines, labelStyle.Render("reason: ")+valueStyle.Render(req.Reason))
	}
	lines = append(lines, "")
	lines = append(lines, keyStyle.Render("[Y] Approve")+"  "+keyRejectStyle.Render("[N] Reject"))

	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(colorAmber).
		Foreground(colorText).
		Padding(1, 2).
		Render(strings.Join(lines, "\n"))
}

// ---------------------------------------------------------------------------
// Modal management
// ---------------------------------------------------------------------------

func (m *Model) openSettingsModal() {
	sm := NewSettingsModal(
		valueOr(m.provider, "mock"),
		m.model,
		m.baseURL,
		m.reasoning,
		valueOr(m.approval, "auto"),
		valueOr(m.sandbox, "workspace-write"),
		valueOr(m.mode, "edit"),
		m.maxContext, m.maxOutput, m.maxSteps, m.maxMessages, m.maxInstructions,
		m.fastModel, m.editModel, m.deepModel,
	)
	m.settingsModal = &sm
	m.activeModal = modalSettings
}

func (m *Model) openPickerModal(picker PickerModal) {
	m.pickerModal = &picker
	m.activeModal = modalPicker
}

func (m *Model) openHelpModal() {
	hm := NewHelpModal(m.commands)
	m.helpModal = &hm
	m.activeModal = modalHelp
}

func (m *Model) closeModal() {
	m.activeModal = modalNone
	m.pickerModal = nil
	m.helpModal = nil
	m.settingsModal = nil
}

func (m Model) updateModal(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch m.activeModal {
	case modalPicker:
		return m.updatePickerModal(msg)
	case modalHelp:
		return m.updateHelpModal(msg)
	case modalSettings:
		return m.updateSettingsModal(msg)
	}
	return m, nil
}

// ---------------------------------------------------------------------------
// Picker modal update
// ---------------------------------------------------------------------------

func (m Model) updatePickerModal(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.pickerModal == nil {
		m.closeModal()
		return m, nil
	}
	switch msg.String() {
	case "esc", "ctrl+c":
		m.closeModal()
		return m, nil
	case "up", "k":
		m.pickerModal.MoveUp()
		return m, nil
	case "down", "j":
		m.pickerModal.MoveDown()
		return m, nil
	case "enter":
		value := m.pickerModal.SelectedValue()
		field := m.pickerModal.Field
		m.closeModal()

		// Apply optimistically
		switch field {
		case "approval":
			m.approval = value
		case "provider":
			m.provider = value
			m.model = ""
		case "sandbox":
			m.sandbox = value
		case "mode":
			m.mode = value
		case "reasoning":
			m.reasoning = value
		case "session":
			m.sessionID = value
		}

		// Send to handler
		cmdName := field
		if field == "checkpoint" && value == "restore" && m.pickerProvider != nil {
			return m, runPickerProvider(m.pickerProvider, "checkpoint_restore")
		}
		if field == "checkpoint_restore" {
			cmdText := "/checkpoint restore " + value
			m.lines = append(m.lines, "system: checkpoint restore → "+valueOr(value, "default"))
			m.syncViewport(true)
			return m, runHandler(m.handler, cmdText, false)
		}
		if field == "session" {
			cmdName = "session"
		}
		cmdText := "/" + cmdName + " " + value
		m.lines = append(m.lines, "system: "+field+" → "+valueOr(value, "default"))
		m.syncViewport(true)
		return m, runHandler(m.handler, cmdText, false)
	}
	return m, nil
}

// ---------------------------------------------------------------------------
// Help modal update
// ---------------------------------------------------------------------------

func (m Model) updateHelpModal(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.helpModal == nil {
		m.closeModal()
		return m, nil
	}
	switch msg.String() {
	case "esc", "ctrl+c", "q":
		m.closeModal()
		return m, nil
	case "up", "k":
		m.helpModal.ScrollUp()
		return m, nil
	case "down", "j":
		// Calculate total lines for scroll bounds
		total := 0
		for _, cat := range m.helpModal.Categories {
			total += 2 + len(cat.Commands) // header + blank + commands
		}
		total += 8 // keyboard shortcuts section + padding
		m.helpModal.ScrollDown(total)
		return m, nil
	}
	return m, nil
}

// ---------------------------------------------------------------------------
// Settings modal update
// ---------------------------------------------------------------------------

func (m Model) updateSettingsModal(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.settingsModal == nil {
		m.closeModal()
		return m, nil
	}
	sm := m.settingsModal

	// If editing a text field, handle specially
	if sm.Editing {
		switch msg.String() {
		case "esc":
			sm.Editing = false
			return m, nil
		case "enter":
			sm.CommitEdit()
			return m, nil
		case "backspace":
			sm.Backspace()
			return m, nil
		default:
			if len(msg.Runes) > 0 {
				sm.TypeChar(string(msg.Runes))
			}
			return m, nil
		}
	}

	switch msg.String() {
	case "esc":
		m.closeModal()
		return m, nil
	case "tab", "down", "j":
		sm.MoveDown()
		return m, nil
	case "shift+tab", "up", "k":
		sm.MoveUp()
		return m, nil
	case "left", "h":
		sm.CycleLeft()
		return m, nil
	case "right", "l":
		sm.CycleRight()
		return m, nil
	case "backspace":
		sm.Backspace()
		return m, nil
	case "enter", "ctrl+s":
		// Save all settings
		m.provider = sm.GetValue("provider")
		m.model = sm.GetValue("model")
		m.baseURL = sm.GetValue("endpoint")
		m.reasoning = sm.GetValue("reasoning")
		m.approval = sm.GetValue("approval")
		m.sandbox = sm.GetValue("sandbox")
		m.mode = sm.GetValue("mode")
		if parsed := parsePositiveInt(sm.GetValue("context")); parsed > 0 {
			m.maxContext = parsed
		}
		if parsed := parsePositiveInt(sm.GetValue("output")); parsed > 0 {
			m.maxOutput = parsed
		}
		if parsed := parsePositiveInt(sm.GetValue("steps")); parsed > 0 {
			m.maxSteps = parsed
		}
		if parsed := parsePositiveInt(sm.GetValue("messages")); parsed > 0 {
			m.maxMessages = parsed
		}
		if parsed := parsePositiveInt(sm.GetValue("instructions")); parsed > 0 {
			m.maxInstructions = parsed
		}
		m.fastModel = sm.GetValue("fast_model")
		m.editModel = sm.GetValue("edit_model")
		m.deepModel = sm.GetValue("deep_model")

		// Build /set command for handler
		parts := []string{"/set",
			"provider=" + m.provider,
			"model=" + m.model,
			"endpoint=" + m.baseURL,
		}
		if strings.TrimSpace(m.reasoning) != "" {
			parts = append(parts, "reasoning="+m.reasoning)
		}
		parts = append(parts,
			"approval="+valueOr(m.approval, "auto"),
			"sandbox="+valueOr(m.sandbox, "workspace-write"),
			"mode="+valueOr(m.mode, "edit"),
			fmt.Sprintf("context=%d", m.maxContext),
			fmt.Sprintf("output=%d", m.maxOutput),
		)
		cmdText := strings.Join(parts, " ")

		// Handle API keys — set env vars at runtime
		keysToSave := make(map[string]string)
		for _, envField := range []struct{ name, envVar string }{
			{"openai_key", "OPENAI_API_KEY"},
			{"anthropic_key", "ANTHROPIC_API_KEY"},
			{"gemini_key", "GEMINI_API_KEY"},
		} {
			if val := sm.GetValue(envField.name); val != "" {
				_ = setEnvIfChanged(envField.envVar, val)
				keysToSave[envField.envVar] = val
			}
		}
		if len(keysToSave) > 0 {
			_ = saveEnvFile(m.cwd, keysToSave)
		}

		m.closeModal()
		m.lines = append(m.lines, "system: settings saved")
		m.syncViewport(true)
		return m, runHandler(m.handler, cmdText, false)
	}
	if len(msg.Runes) > 0 {
		sm.TypeChar(string(msg.Runes))
	}
	return m, nil
}

// ---------------------------------------------------------------------------
// Header
// ---------------------------------------------------------------------------

func (m Model) renderHeader() string {
	width := max(50, m.width)

	// --- Logo (>< chevrons with gradient bar) ---
	chevronStyle := lipgloss.NewStyle().Foreground(colorWhite).Bold(true)

	colors := []string{"#A855F7", "#8B5CF6", "#6366F1", "#3B82F6", "#0EA5E9", "#06B6D4"}
	var gradientBar strings.Builder
	for _, hex := range colors {
		gradientBar.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color(hex)).Render("█"))
	}

	logoLines := []string{
		chevronStyle.Render("     ██▄                ▄██"),
		chevronStyle.Render("       ██▄            ▄██"),
		chevronStyle.Render("         ██▄        ▄██"),
		chevronStyle.Render("         ▄██        ██▄"),
		chevronStyle.Render("       ▄██            ██▄"),
		chevronStyle.Render("     ▄██     ") + gradientBar.String() + chevronStyle.Render("     ██▄"),
	}

	titleText := m.title
	if strings.TrimSpace(titleText) == "" {
		titleText = "Klyra"
	}
	title := lipgloss.NewStyle().Foreground(colorBrand).Bold(true).Render(titleText)
	subtitle := lipgloss.NewStyle().Foreground(colorDim).Render("  agentic coding workspace")

	// Status
	status := "ready"
	if m.busy {
		status = "thinking"
	}
	if m.err != nil {
		status = "error"
	}
	statusIcon := statusGlyph(status)
	statusBadge := pillBadge(statusIcon+" "+status, statusColor(status), "")

	// Provider & model
	providerBadge := pillBadge(valueOr(m.provider, "mock"), colorBadgeBg, colorBlue)
	modelBadge := pillBadge(valueOr(m.model, "routed"), colorBadgeBg3, colorBrand)
	reasoningBadge := pillBadge("reasoning "+valueOr(m.reasoning, "default"), colorBadgeBg2, colorDim)

	// Safety
	safetyText := valueOr(m.mode, "edit") + " / " + valueOr(m.sandbox, "workspace-write") + " / " + valueOr(m.approval, "auto")
	safetyBadge := pillBadge(safetyText, colorBadgeBg4, colorEmerald)

	// Budget info
	budgetParts := []string{
		lipgloss.NewStyle().Foreground(colorMuted).Render("ctx ") + lipgloss.NewStyle().Foreground(colorDim).Render(formatNumber(m.maxContext)),
		lipgloss.NewStyle().Foreground(colorMuted).Render("out ") + lipgloss.NewStyle().Foreground(colorDim).Render(formatNumber(m.maxOutput)),
		lipgloss.NewStyle().Foreground(colorMuted).Render("cart ") + lipgloss.NewStyle().Foreground(colorDim).Render(fmt.Sprintf("%d", m.cartCount)),
		lipgloss.NewStyle().Foreground(colorMuted).Render("session ") + lipgloss.NewStyle().Foreground(colorDim).Render(valueOr(m.sessionID, "ephemeral")),
	}
	if m.baseURL != "" {
		budgetParts = append(budgetParts, lipgloss.NewStyle().Foreground(colorMuted).Render("endpoint ")+lipgloss.NewStyle().Foreground(colorDim).Render(shorten(m.baseURL, 24)))
	}
	budgets := strings.Join(budgetParts, lipgloss.NewStyle().Foreground(colorSeparator).Render(" · "))

	// Separator bar
	barWidth := max(10, min(width-2, 90))
	bar := lipgloss.NewStyle().Foreground(colorSeparator).Render(strings.Repeat("─", barWidth))

	topLine := lipgloss.JoinHorizontal(lipgloss.Top, title, subtitle)
	badgeLine := lipgloss.JoinHorizontal(lipgloss.Top, statusBadge, " ", providerBadge, " ", modelBadge, " ", reasoningBadge)

	result := []string{""}
	result = append(result, logoLines...)
	result = append(result,
		"",
		"  "+topLine,
		"  "+badgeLine,
		"  "+budgets+"  "+safetyBadge,
		"  "+bar,
	)

	return strings.Join(result, "\n")
}

// ---------------------------------------------------------------------------
// Footer
// ---------------------------------------------------------------------------

func (m Model) renderFooter() string {
	cmdHintStyle := lipgloss.NewStyle().Foreground(colorMuted)
	cmdSlashStyle := lipgloss.NewStyle().Foreground(colorBrandDim)
	modelStyle := lipgloss.NewStyle().Foreground(colorDim)
	sepStyle := lipgloss.NewStyle().Foreground(colorSeparator)

	leftParts := []string{
		cmdSlashStyle.Render("/") + cmdHintStyle.Render("help"),
		cmdSlashStyle.Render("/") + cmdHintStyle.Render("status"),
		cmdSlashStyle.Render("/") + cmdHintStyle.Render("mode"),
		cmdSlashStyle.Render("/") + cmdHintStyle.Render("attach"),
	}
	leftFooter := " " + strings.Join(leftParts, "  ")

	copyHint := "F6 copy"
	if m.copyMode {
		copyHint = "F6 scroll"
	}
	settingsHint := lipgloss.NewStyle().Foreground(colorMuted).Render("F2 settings  " + copyHint)
	rightFooter := modelStyle.Render(valueOr(m.model, "routed")) + "  " + settingsHint + " "

	separator := sepStyle.Render(strings.Repeat("─", max(10, m.width)))

	return lipgloss.JoinVertical(lipgloss.Left, separator, leftFooter+strings.Repeat(" ", max(0, m.width-lipgloss.Width(leftFooter)-lipgloss.Width(rightFooter)))+rightFooter)
}

// ---------------------------------------------------------------------------
// Thinking animation
// ---------------------------------------------------------------------------

func (m Model) renderThinkingBar() string {
	barLen := pulseSizes[m.spinnerFrame%len(pulseSizes)]
	colorOffset := m.spinnerFrame * 2 // flow speed

	var bar strings.Builder
	for i := 0; i < barLen; i++ {
		// Pick gradient color (flows over time)
		cIdx := (i + colorOffset) % len(gradientPalette)
		col := gradientPalette[cIdx]

		// Pick block density char (fade at edges)
		var ch rune
		distFromEdge := i
		if barLen-1-i < distFromEdge {
			distFromEdge = barLen - 1 - i
		}
		if distFromEdge >= len(densityChars) {
			ch = densityChars[len(densityChars)-1] // full block
		} else {
			ch = densityChars[distFromEdge]
		}

		bar.WriteString(lipgloss.NewStyle().Foreground(col).Render(string(ch)))
	}

	label := lipgloss.NewStyle().Foreground(colorMuted).Italic(true).Render(" thinking...")
	return "  " + bar.String() + label
}

// ---------------------------------------------------------------------------
// Local command handling
// ---------------------------------------------------------------------------

func (m *Model) handleLocalCommand(value string) (bool, tea.Cmd) {
	if value == "/clear" {
		m.lines = nil
		return true, nil
	}

	// Open modals for commands without arguments
	args := strings.Fields(value)
	if len(args) == 1 {
		switch args[0] {
		case "/approval":
			m.openPickerModal(ApprovalPicker(valueOr(m.approval, "auto")))
			return true, nil
		case "/provider":
			m.openPickerModal(ProviderPicker(valueOr(m.provider, "mock")))
			return true, nil
		case "/sandbox":
			m.openPickerModal(SandboxPicker(valueOr(m.sandbox, "workspace-write")))
			return true, nil
		case "/mode":
			m.openPickerModal(ModePicker(valueOr(m.mode, "edit")))
			return true, nil
		case "/reasoning":
			m.openPickerModal(ReasoningPicker(m.reasoning))
			return true, nil
		case "/sessions":
			if m.pickerProvider != nil {
				return true, runPickerProvider(m.pickerProvider, "session")
			}
		case "/checkpoint":
			m.openPickerModal(CheckpointPicker())
			return true, nil
		case "/config":
			m.openPickerModal(ConfigPicker())
			return true, nil
		case "/instructions":
			m.openPickerModal(InstructionsPicker())
			return true, nil
		case "/diff":
			m.openPickerModal(DiffPicker())
			return true, nil
		case "/settings":
			m.openSettingsModal()
			return true, nil
		case "/help":
			m.openHelpModal()
			return true, nil
		}
	}

	if strings.HasPrefix(value, "/") {
		m.applyOptimisticCommand(value)
		if len(m.lines) > 0 {
			m.lines = append(m.lines, "")
		}
		m.lines = append(m.lines, "you: "+value)
		m.syncViewport(true)
		return true, runHandler(m.handler, value, false)
	}

	return false, nil
}

func (m *Model) applyOptimisticCommand(value string) {
	args := strings.Fields(value)
	if len(args) < 2 {
		return
	}
	switch args[0] {
	case "/set":
		for _, arg := range args[1:] {
			key, value, ok := strings.Cut(arg, "=")
			if !ok {
				continue
			}
			switch key {
			case "provider":
				m.provider = value
			case "model":
				m.model = value
			case "endpoint":
				m.baseURL = value
			case "reasoning":
				m.reasoning = value
			case "approval":
				m.approval = value
			case "sandbox":
				m.sandbox = value
			case "mode":
				m.mode = value
			case "context":
				if parsed := parsePositiveInt(value); parsed > 0 {
					m.maxContext = parsed
				}
			case "output":
				if parsed := parsePositiveInt(value); parsed > 0 {
					m.maxOutput = parsed
				}
			}
		}
	case "/provider":
		m.provider = args[1]
		m.model = ""
	case "/model":
		m.model = strings.Join(args[1:], " ")
	case "/endpoint":
		m.baseURL = strings.Join(args[1:], " ")
	case "/reasoning":
		m.reasoning = args[1]
	case "/approval":
		m.approval = args[1]
	case "/sandbox":
		m.sandbox = args[1]
	case "/mode":
		m.mode = args[1]
	case "/cart":
		if len(args) >= 3 && args[1] == "add" {
			m.cartCount += len(args) - 2
		}
	case "/limits":
		if len(args) < 3 {
			return
		}
		value := parsePositiveInt(args[2])
		if value <= 0 {
			return
		}
		switch args[1] {
		case "context", "ctx":
			m.maxContext = value
		case "output", "out":
			m.maxOutput = value
		}
	}
}

// ---------------------------------------------------------------------------
// Style helpers
// ---------------------------------------------------------------------------

func pillBadge(text string, bg, fg lipgloss.Color) string {
	style := lipgloss.NewStyle().
		Padding(0, 1)
	if bg != "" {
		style = style.Background(bg)
	}
	if fg != "" {
		style = style.Foreground(fg)
	} else {
		style = style.Foreground(colorWhite)
	}
	return style.Render(text)
}

func statusGlyph(status string) string {
	switch status {
	case "thinking":
		return "●"
	case "error":
		return "✖"
	default:
		return "✔"
	}
}

func statusColor(status string) lipgloss.Color {
	switch status {
	case "thinking":
		return colorAmber
	case "error":
		return colorRed
	default:
		return colorEmerald
	}
}

func formatNumber(value int) string {
	if value <= 0 {
		return "default"
	}
	if value >= 1000 {
		return fmt.Sprintf("%dk", value/1000)
	}
	return fmt.Sprintf("%d", value)
}

func parsePositiveInt(value string) int {
	parsed, err := strconv.Atoi(value)
	if err != nil || parsed <= 0 {
		return 0
	}
	return parsed
}

func shorten(value string, maxLen int) string {
	if len(value) <= maxLen {
		return value
	}
	if maxLen <= 1 {
		return value[:maxLen]
	}
	return value[:maxLen-1] + "..."
}

func runHandler(handler Handler, input string, agentRun bool) tea.Cmd {
	return func() tea.Msg {
		output, err := handler(input)
		return responseMsg{input: input, output: output, err: err, agentRun: agentRun}
	}
}

func runPickerProvider(provider PickerProvider, field string) tea.Cmd {
	return func() tea.Msg {
		picker, err := provider(field)
		return pickerLoadedMsg{picker: picker, err: err}
	}
}

func centerOverlay(width, height int, content string) string {
	if width <= 0 {
		width = 80
	}
	if height <= 0 {
		height = 20
	}
	return lipgloss.Place(width, height, lipgloss.Center, lipgloss.Center, content)
}

func (m *Model) appendAgentOutput(text string) {
	text = strings.TrimSpace(text)
	if text == "" {
		return
	}
	m.lines = append(m.lines, "agent: "+text)
}

func (m *Model) appendThoughtsOutput(text string, expanded bool) {
	if strings.TrimSpace(text) == "" {
		return
	}
	state := "0"
	if expanded {
		state = "1"
	}
	m.lines = append(m.lines, "thoughts:"+state+":"+text)
}

func (m *Model) appendToolStream(msg ToolStreamMsg) {
	if strings.TrimSpace(msg.Name) == "" && strings.TrimSpace(msg.ArgumentsDelta) == "" {
		return
	}
	if data, err := json.Marshal(msg); err == nil {
		m.lines = append(m.lines, "toolstream: "+string(data))
	}
}

func (m *Model) appendToolProgress(msg ToolProgressMsg) {
	if strings.TrimSpace(msg.Tool) == "" {
		return
	}
	if data, err := json.Marshal(msg); err == nil {
		m.lines = append(m.lines, "toolprogress:0:"+string(data))
	}
}

func (m *Model) toggleLatestThoughts() bool {
	if strings.TrimSpace(m.reasoningText) != "" {
		m.reasonExpanded = !m.reasonExpanded
		return true
	}
	for i := len(m.lines) - 1; i >= 0; i-- {
		if strings.HasPrefix(m.lines[i], "thoughts:0:") {
			m.lines[i] = "thoughts:1:" + strings.TrimPrefix(m.lines[i], "thoughts:0:")
			return true
		}
		if strings.HasPrefix(m.lines[i], "thoughts:1:") {
			m.lines[i] = "thoughts:0:" + strings.TrimPrefix(m.lines[i], "thoughts:1:")
			return true
		}
	}
	return false
}

func (m *Model) toggleLatestThoughtsExpand(expanded bool) bool {
	if strings.TrimSpace(m.reasoningText) != "" {
		m.reasonExpanded = expanded
		return true
	}
	for i := len(m.lines) - 1; i >= 0; i-- {
		if strings.HasPrefix(m.lines[i], "thoughts:0:") || strings.HasPrefix(m.lines[i], "thoughts:1:") {
			text := strings.TrimPrefix(strings.TrimPrefix(m.lines[i], "thoughts:0:"), "thoughts:1:")
			state := "0"
			if expanded {
				state = "1"
			}
			m.lines[i] = "thoughts:" + state + ":" + text
			return true
		}
	}
	return false
}

func (m *Model) toggleLatestToolDetails() bool {
	for i := len(m.lines) - 1; i >= 0; i-- {
		switch {
		case strings.HasPrefix(m.lines[i], "toolprogress:0:"):
			m.lines[i] = "toolprogress:1:" + strings.TrimPrefix(m.lines[i], "toolprogress:0:")
			return true
		case strings.HasPrefix(m.lines[i], "toolprogress:1:"):
			m.lines[i] = "toolprogress:0:" + strings.TrimPrefix(m.lines[i], "toolprogress:1:")
			return true
		case strings.HasPrefix(m.lines[i], "tool:0:"):
			m.lines[i] = "tool:1:" + strings.TrimPrefix(m.lines[i], "tool:0:")
			return true
		case strings.HasPrefix(m.lines[i], "tool:1:"):
			m.lines[i] = "tool:0:" + strings.TrimPrefix(m.lines[i], "tool:1:")
			return true
		case strings.HasPrefix(m.lines[i], "tool: "):
			m.lines[i] = "tool:1:" + strings.TrimPrefix(m.lines[i], "tool: ")
			return true
		}
	}
	return false
}

func (m *Model) handleViewportClick(y int) bool {
	if y < 0 || y >= m.viewport.Height {
		return false
	}
	lines := m.currentViewportLines()
	index := m.viewport.YOffset + y
	if index < 0 || index >= len(lines) {
		return false
	}
	plain := stripANSICodes(lines[index])
	if strings.Contains(plain, "Thinking") || strings.Contains(plain, "Thoughts") {
		return m.toggleLatestThoughts()
	}
	if strings.Contains(plain, "▸ ") || strings.Contains(plain, "▾ ") || strings.Contains(plain, "details") {
		return m.toggleLatestToolDetails()
	}
	return false
}

func (m Model) currentViewportLines() []string {
	lines := m.buildFormattedLines()
	padding := m.viewport.Height - len(lines)
	if padding > 0 {
		paddedLines := make([]string, padding)
		lines = append(paddedLines, lines...)
	}
	return lines
}

func stripANSICodes(value string) string {
	var out strings.Builder
	inEscape := false
	for _, r := range value {
		if inEscape {
			if r >= '@' && r <= '~' {
				inEscape = false
			}
			continue
		}
		if r == '\x1b' {
			inEscape = true
			continue
		}
		out.WriteRune(r)
	}
	return out.String()
}

func (m Model) renderLiveThoughtBlock() []string {
	return m.renderThoughts(m.reasoningText, m.reasonExpanded, true)
}

func (m Model) renderThoughtBlock(line string) []string {
	expanded := strings.HasPrefix(line, "thoughts:1:")
	text := strings.TrimPrefix(strings.TrimPrefix(line, "thoughts:0:"), "thoughts:1:")
	return m.renderThoughts(text, expanded, false)
}

func (m Model) renderThoughts(text string, expanded, live bool) []string {
	if strings.TrimSpace(text) == "" {
		return nil
	}
	icon := "▸"
	label := "Thoughts"
	if live {
		label = "Thinking"
	}
	if expanded {
		icon = "▾"
	}
	headerStyle := lipgloss.NewStyle().Foreground(colorDim).Background(colorInputBg).Padding(0, 1)
	bodyStyle := lipgloss.NewStyle().Foreground(colorDim).Background(colorInputBg).Padding(0, 1).Width(max(24, m.width-8))
	borderStyle := lipgloss.NewStyle().Foreground(colorMuted)

	header := headerStyle.Render(fmt.Sprintf("%s %s", icon, label))
	if !expanded {
		summary := compactThoughtSummary(text, max(20, m.width-22))
		return []string{"  " + borderStyle.Render("┌") + header + " " + lipgloss.NewStyle().Foreground(colorMuted).Render(summary)}
	}

	var lines []string
	lines = append(lines, "  "+borderStyle.Render("┌")+" "+header)
	rendered := text
	if m.renderer != nil {
		if out, err := m.renderer.Render(text); err == nil {
			rendered = strings.TrimRight(out, " \n\r\t")
		}
	}
	wrapped := bodyStyle.Render(rendered)
	for _, line := range strings.Split(strings.TrimRight(wrapped, "\n"), "\n") {
		lines = append(lines, "  "+borderStyle.Render("│")+" "+line)
	}
	lines = append(lines, "  "+borderStyle.Render("└")+" "+lipgloss.NewStyle().Foreground(colorMuted).Render("Enter/F4 toggles"))
	return lines
}

func compactThoughtSummary(text string, maxLen int) string {
	text = strings.Join(strings.Fields(text), " ")
	if len(text) <= maxLen {
		return text
	}
	if maxLen <= 3 {
		return text[:maxLen]
	}
	return text[:maxLen-3] + "..."
}

type toolDisplay struct {
	Tool   string `json:"tool"`
	Output string `json:"output"`
	Error  string `json:"error"`
}

func (m Model) renderToolStreamLine(raw string) []string {
	var msg ToolStreamMsg
	if err := json.Unmarshal([]byte(raw), &msg); err != nil {
		return []string{lipgloss.NewStyle().Foreground(colorEmerald).Render("  ◇ tool call " + raw)}
	}
	name := strings.TrimSpace(msg.Name)
	if name == "" {
		name = "tool"
	}
	delta := strings.TrimSpace(msg.ArgumentsDelta)
	headerStyle := lipgloss.NewStyle().Foreground(colorEmerald).Bold(true)
	labelStyle := lipgloss.NewStyle().Foreground(colorMuted)
	bodyStyle := lipgloss.NewStyle().Foreground(colorDim).Width(max(24, m.width-12))
	lines := []string{"  " + headerStyle.Render("◇ "+name) + " " + labelStyle.Render("model is preparing tool call")}
	if delta != "" {
		for _, line := range strings.Split(bodyStyle.Render(delta), "\n") {
			lines = append(lines, "  "+lipgloss.NewStyle().Foreground(colorMuted).Render("│")+" "+line)
		}
	}
	return lines
}

func (m Model) renderToolProgressLine(raw string) []string {
	expanded, raw := parseCollapsiblePayload(raw, "toolprogress")
	var msg ToolProgressMsg
	if err := json.Unmarshal([]byte(raw), &msg); err != nil {
		return []string{lipgloss.NewStyle().Foreground(colorEmerald).Render("  ◆ tool " + raw)}
	}
	phase := strings.TrimSpace(msg.Phase)
	if phase == "" {
		phase = "running"
	}
	name := strings.TrimSpace(msg.Tool)
	if name == "" {
		name = "tool"
	}
	headerStyle := lipgloss.NewStyle().Foreground(colorEmerald).Bold(true)
	if phase == "error" || phase == "rejected" {
		headerStyle = lipgloss.NewStyle().Foreground(colorRed).Bold(true)
	}
	labelStyle := lipgloss.NewStyle().Foreground(colorMuted)
	bodyStyle := lipgloss.NewStyle().Foreground(colorDim).Width(max(24, m.width-12))
	borderStyle := lipgloss.NewStyle().Foreground(colorMuted)
	errorStyle := lipgloss.NewStyle().Foreground(colorRed)

	summary := toolProgressSummary(msg)
	icon := "▸"
	if expanded {
		icon = "▾"
	}
	lines := []string{"  " + headerStyle.Render(icon+" "+name) + " " + labelStyle.Render(summary)}
	if !expanded {
		return lines
	}
	if len(msg.Args) > 0 && (phase == "queued" || phase == "running") {
		if data, err := json.Marshal(msg.Args); err == nil {
			for _, line := range strings.Split(bodyStyle.Render(string(data)), "\n") {
				lines = append(lines, "  "+borderStyle.Render("│")+" "+line)
			}
		}
	}
	output := strings.TrimRight(msg.Output, "\n")
	if output != "" {
		for _, line := range strings.Split(bodyStyle.Render(output), "\n") {
			lines = append(lines, "  "+borderStyle.Render("│")+" "+line)
		}
	}
	if strings.TrimSpace(msg.Error) != "" {
		for _, line := range strings.Split(bodyStyle.Render(msg.Error), "\n") {
			lines = append(lines, "  "+borderStyle.Render("│")+" "+errorStyle.Render(line))
		}
	}
	return lines
}

func toolProgressSummary(msg ToolProgressMsg) string {
	phase := strings.TrimSpace(msg.Phase)
	if phase == "" {
		phase = "running"
	}
	switch phase {
	case "queued":
		return "planned tool call, details collapsed"
	case "running":
		return "running tool, details collapsed"
	case "done":
		if strings.TrimSpace(msg.Output) == "" {
			return "finished with empty result"
		}
		return fmt.Sprintf("finished, %s", outputSummary(msg.Output, 70))
	case "error":
		return fmt.Sprintf("failed, %s", outputSummary(msg.Error, 70))
	case "rejected":
		return fmt.Sprintf("rejected, %s", outputSummary(msg.Error, 70))
	default:
		return phase + ", details collapsed"
	}
}

func toolPhaseLabel(phase string) string {
	switch phase {
	case "queued":
		return "queued"
	case "running":
		return "running"
	case "done":
		return "done"
	case "error":
		return "error"
	case "rejected":
		return "rejected"
	default:
		return phase
	}
}

func (m Model) renderToolLine(raw string) []string {
	expanded, raw := parseCollapsiblePayload(raw, "tool")
	raw = strings.TrimSpace(raw)
	display := toolDisplay{Tool: "tool", Output: raw}
	if strings.HasPrefix(raw, "{") {
		var parsed toolDisplay
		if err := json.Unmarshal([]byte(raw), &parsed); err == nil {
			display = parsed
		}
	}
	if strings.TrimSpace(display.Tool) == "" {
		display.Tool = "tool"
	}

	headerStyle := lipgloss.NewStyle().Foreground(colorEmerald).Bold(true)
	labelStyle := lipgloss.NewStyle().Foreground(colorMuted)
	bodyStyle := lipgloss.NewStyle().Foreground(colorDim).Width(max(24, m.width-10))
	borderStyle := lipgloss.NewStyle().Foreground(colorMuted)
	errorStyle := lipgloss.NewStyle().Foreground(colorRed)

	icon := "▸"
	if expanded {
		icon = "▾"
	}
	summary := toolDisplaySummary(display)
	lines := []string{
		"  " + headerStyle.Render(icon+" "+display.Tool) + " " + labelStyle.Render(summary),
	}
	if !expanded {
		return lines
	}
	output := strings.TrimRight(display.Output, "\n")
	if output != "" {
		for _, line := range strings.Split(bodyStyle.Render(output), "\n") {
			lines = append(lines, "  "+borderStyle.Render("│")+" "+line)
		}
	}
	if strings.TrimSpace(display.Error) != "" {
		for _, line := range strings.Split(bodyStyle.Render(display.Error), "\n") {
			lines = append(lines, "  "+borderStyle.Render("│")+" "+errorStyle.Render(line))
		}
	}
	if output == "" && strings.TrimSpace(display.Error) == "" {
		lines = append(lines, "  "+borderStyle.Render("│")+" "+labelStyle.Render("empty result"))
	}
	return lines
}

func parseCollapsiblePayload(raw, prefix string) (bool, string) {
	zero := prefix + ":0:"
	one := prefix + ":1:"
	spaced := prefix + ": "
	switch {
	case strings.HasPrefix(raw, zero):
		return false, strings.TrimPrefix(raw, zero)
	case strings.HasPrefix(raw, one):
		return true, strings.TrimPrefix(raw, one)
	case strings.HasPrefix(raw, spaced):
		return false, strings.TrimPrefix(raw, spaced)
	}
	return false, raw
}

func toolDisplaySummary(display toolDisplay) string {
	if strings.TrimSpace(display.Error) != "" {
		return "failed, " + outputSummary(display.Error, 70)
	}
	if strings.TrimSpace(display.Output) == "" {
		return "finished with empty result"
	}
	return fmt.Sprintf("finished, %s", outputSummary(display.Output, 70))
}

func outputSummary(text string, maxLen int) string {
	text = strings.Join(strings.Fields(text), " ")
	if text == "" {
		return "no details"
	}
	if len(text) <= maxLen {
		return text
	}
	if maxLen <= 3 {
		return text[:maxLen]
	}
	return text[:maxLen-3] + "..."
}

func renderCommandOutputLine(line string) string {
	if strings.TrimSpace(stripANSICodes(line)) == "" {
		return ""
	}
	plain := strings.TrimSpace(stripANSICodes(line))
	lower := strings.ToLower(plain)
	accent := lipgloss.NewStyle().Foreground(colorSeparator).Render("│")
	style := lipgloss.NewStyle().Foreground(colorDim)

	switch {
	case strings.HasPrefix(lower, "setting saved:"):
		accent = lipgloss.NewStyle().Foreground(colorEmerald).Render("│")
		style = lipgloss.NewStyle().Foreground(colorEmerald)
	case strings.HasPrefix(lower, "usage:") || strings.Contains(lower, " usage"):
		accent = lipgloss.NewStyle().Foreground(colorBlue).Render("│")
		style = lipgloss.NewStyle().Foreground(colorBlue)
	case strings.Contains(lower, "requires") || strings.Contains(lower, "warning"):
		accent = lipgloss.NewStyle().Foreground(colorAmber).Render("│")
		style = lipgloss.NewStyle().Foreground(colorAmber)
	case strings.HasPrefix(plain, "✓") || strings.HasPrefix(lower, "saved"):
		accent = lipgloss.NewStyle().Foreground(colorEmerald).Render("│")
		style = lipgloss.NewStyle().Foreground(colorEmerald)
	case strings.HasPrefix(plain, "✗") || strings.HasPrefix(lower, "error"):
		accent = lipgloss.NewStyle().Foreground(colorRed).Render("│")
		style = lipgloss.NewStyle().Foreground(colorRed)
	case strings.HasPrefix(plain, "- ") || strings.HasPrefix(plain, "• "):
		accent = lipgloss.NewStyle().Foreground(colorBrandDim).Render("│")
	}

	if strings.Contains(line, "\x1b[") {
		return "  " + accent + " " + line
	}
	return "  " + accent + " " + style.Render(line)
}

func (m Model) historyPrevious() (tea.Model, tea.Cmd) {
	if len(m.history) == 0 {
		return m, nil
	}
	if m.historyIdx == len(m.history) {
		m.tempInput = m.input.Value()
	}
	m.historyIdx--
	if m.historyIdx < 0 {
		m.historyIdx = 0
	}
	m.input.SetValue(m.history[m.historyIdx])
	m.input.SetCursor(len(m.input.Value()))
	return m, nil
}

func (m Model) historyNext() (tea.Model, tea.Cmd) {
	if len(m.history) == 0 {
		return m, nil
	}
	m.historyIdx++
	if m.historyIdx > len(m.history) {
		m.historyIdx = len(m.history)
	}
	if m.historyIdx == len(m.history) {
		m.input.SetValue(m.tempInput)
	} else {
		m.input.SetValue(m.history[m.historyIdx])
	}
	m.input.SetCursor(len(m.input.Value()))
	return m, nil
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

func min(left, right int) int {
	if left < right {
		return left
	}
	return right
}

// ---------------------------------------------------------------------------
// Message formatting constants
// ---------------------------------------------------------------------------

const (
	userPrefix   = ">"
	agentBar     = "|"
	errorPrefix  = "x"
	systemPrefix = "-"
	acPointer    = ">"
)

// ---------------------------------------------------------------------------
// Styles
// ---------------------------------------------------------------------------

var (
	userMsgStyle = lipgloss.NewStyle().
			Foreground(colorBlue)

	agentMsgStyle = lipgloss.NewStyle().
			Foreground(colorText)

	agentBarStyle = lipgloss.NewStyle().
			Foreground(colorBrand)

	errorMsgStyle = lipgloss.NewStyle().
			Foreground(colorRed).
			Bold(true)

	systemMsgStyle = lipgloss.NewStyle().
			Foreground(colorMuted).
			Italic(true)

	acItemStyle = lipgloss.NewStyle().
			Foreground(colorDim)

	acSelectedStyle = lipgloss.NewStyle().
			Background(colorBadgeBg).
			Foreground(colorBrand).
			Bold(true)

	acHintStyle = lipgloss.NewStyle().
			Foreground(colorMuted).
			Italic(true)
)

// setEnvIfChanged sets an environment variable only when the new value differs.
func setEnvIfChanged(envVar, value string) error {
	if os.Getenv(envVar) == value {
		return nil
	}
	return os.Setenv(envVar, value)
}

func saveEnvFile(dir string, keys map[string]string) error {
	if dir == "" {
		dir = "."
	}
	path := filepath.Join(dir, ".env")

	envMap := make(map[string]string)
	var lines []string

	if data, err := os.ReadFile(path); err == nil {
		for _, line := range strings.Split(string(data), "\n") {
			trimmed := strings.TrimSpace(line)
			if trimmed == "" || strings.HasPrefix(trimmed, "#") {
				lines = append(lines, line)
				continue
			}
			rawLine := line
			trimmed = strings.TrimPrefix(trimmed, "export ")
			key, _, ok := strings.Cut(trimmed, "=")
			if !ok {
				lines = append(lines, line)
				continue
			}
			key = strings.TrimSpace(key)
			envMap[key] = rawLine
			lines = append(lines, key)
		}
	}

	for k, v := range keys {
		quotedVal := fmt.Sprintf("%s=\"%s\"", k, v)
		envMap[k] = quotedVal

		found := false
		for _, line := range lines {
			if line == k {
				found = true
				break
			}
		}
		if !found {
			lines = append(lines, k)
		}
	}

	var outLines []string
	for _, line := range lines {
		if val, exists := envMap[line]; exists {
			outLines = append(outLines, val)
		} else {
			outLines = append(outLines, line)
		}
	}

	return os.WriteFile(path, []byte(strings.Join(outLines, "\n")), 0o600)
}
