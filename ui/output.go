package ui

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

var (
	outputBg = lipgloss.Color("#0d1117")
	outputFg = lipgloss.Color("#e6edf3")
)

type AppendOutputMsg struct {
	Text string
}

type OutputModel struct {
	width  int
	height int
	lines  []string
}

func NewOutputModel() OutputModel {
	return OutputModel{}
}

func (m OutputModel) Update(msg tea.Msg) OutputModel {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
	case AppendOutputMsg:
		m.lines = append(m.lines, msg.Text)
	}
	return m
}

func (m OutputModel) View() string {
	if m.height <= 0 {
		return ""
	}

	style := lipgloss.NewStyle().Background(outputBg).Foreground(outputFg)

	visible := m.lines
	if len(visible) > m.height {
		visible = visible[len(visible)-m.height:]
	}

	width := m.width - 1
	if width < 0 {
		width = 0
	}

	rows := make([]string, m.height)
	for i := 0; i < m.height; i++ {
		line := ""
		if i < len(visible) {
			line = visible[i]
		}
		if len(line) > width {
			line = line[:width]
		}
		pad := width - len(line)
		if pad < 0 {
			pad = 0
		}
		rows[i] = style.Render(line+strings.Repeat(" ", pad)) + paintToEOL(outputBg)
	}
	return strings.Join(rows, "\n")
}
