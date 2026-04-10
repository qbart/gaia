package ui

import (
	"fmt"
	"strconv"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

var (
	statusBarBg = lipgloss.Color("#2563eb")
	statusBarFg = lipgloss.Color("#ffffff")
)

type StatusBarSetLeftMsg struct {
	Text string
}

type StatusBarSetRightMsg struct {
	Text string
}

type StatusBar struct {
	width int
	left  string
	right string
}

func NewStatusBar() StatusBar {
	return StatusBar{}
}

func (s StatusBar) Update(msg tea.Msg) StatusBar {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		s.width = msg.Width
	case StatusBarSetLeftMsg:
		s.left = msg.Text
	case StatusBarSetRightMsg:
		s.right = msg.Text
	}
	return s
}

func (s StatusBar) View() string {
	style := lipgloss.NewStyle().Background(statusBarBg).Foreground(statusBarFg)

	// Use width-1 like the pipeline does, then paint EOL to fill the last column.
	width := s.width - 1
	if width < 0 {
		width = 0
	}

	left := " " + s.left
	right := s.right + " "

	gap := width - lipgloss.Width(left) - lipgloss.Width(right)
	if gap < 0 {
		gap = 0
	}

	line := left + strings.Repeat(" ", gap) + right
	return style.Render(line) + paintToEOL(statusBarBg)
}

// paintToEOL sets the background and erases to end of line,
// matching how the pipeline fills the terminal's last column.
func paintToEOL(bg lipgloss.Color) string {
	s := strings.TrimSpace(string(bg))
	var seq string
	if strings.HasPrefix(s, "#") && len(s) == 7 {
		r, errR := strconv.ParseInt(s[1:3], 16, 64)
		g, errG := strconv.ParseInt(s[3:5], 16, 64)
		b, errB := strconv.ParseInt(s[5:7], 16, 64)
		if errR == nil && errG == nil && errB == nil {
			seq = fmt.Sprintf("\x1b[48;2;%d;%d;%dm", r, g, b)
		}
	}
	if seq == "" {
		if n, err := strconv.Atoi(s); err == nil {
			seq = fmt.Sprintf("\x1b[48;5;%dm", n)
		}
	}
	return seq + "\x1b[K\x1b[0m"
}
