package ui

import (
	tea "github.com/charmbracelet/bubbletea"
	"github.com/qbart/tui/tui"
)

type AppModel struct {
	pipeline  tui.PipelineModel
	output    OutputModel
	statusBar StatusBar
	height    int
}

func NewAppModel(pipeline tui.PipelineModel) AppModel {
	return AppModel{
		pipeline:  pipeline,
		output:    NewOutputModel(),
		statusBar: NewStatusBar(),
	}
}

func (m AppModel) Init() tea.Cmd {
	return m.pipeline.Init()
}

func (m AppModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.height = msg.Height
		m.statusBar = m.statusBar.Update(msg)

		available := msg.Height - 1 // reserve 1 for statusbar
		pipelineHeight := available / 2
		outputHeight := available - pipelineHeight

		updated, cmd := m.pipeline.Update(tea.WindowSizeMsg{Width: msg.Width, Height: pipelineHeight})
		m.pipeline = updated.(tui.PipelineModel)
		m.output = m.output.Update(tea.WindowSizeMsg{Width: msg.Width, Height: outputHeight})
		return m, cmd

	case StatusBarSetLeftMsg:
		m.statusBar = m.statusBar.Update(msg)
		return m, nil

	case StatusBarSetRightMsg:
		m.statusBar = m.statusBar.Update(msg)
		return m, nil

	case AppendOutputMsg:
		m.output = m.output.Update(msg)
		return m, nil
	}

	updated, cmd := m.pipeline.Update(msg)
	m.pipeline = updated.(tui.PipelineModel)
	return m, cmd
}

func (m AppModel) View() string {
	if m.height <= 0 {
		return "Loading..."
	}
	return m.pipeline.View() + "\n" + m.output.View() + "\n" + m.statusBar.View()
}
