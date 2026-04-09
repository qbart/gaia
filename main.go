package main

import (
	"fmt"
	"os"
	"time"

	"github.com/qbart/gaia/config"
	"github.com/qbart/tui/tui"

	tea "github.com/charmbracelet/bubbletea"
)

func main() {
	fmt.Println(config.Name)
	spec := tui.NewPipelineSpec("gaia", []tui.StepSpec{
		{ID: "loop-start", JobName: "Loop start", Status: tui.StatusBlue},
		{ID: "wait", JobName: "Wait for tasks", DependsOn: []tui.StepID{"loop-start"}},
		{ID: "read-docs", JobName: "Read docs", DependsOn: []tui.StepID{"wait"}},
		{ID: "read-todo", JobName: "Get TODOs", DependsOn: []tui.StepID{"wait"}},
		{ID: "read-doing", JobName: "Get In Progress tasks", DependsOn: []tui.StepID{"wait"}},
		{ID: "read-rejected", JobName: "Get Rejected tasks", DependsOn: []tui.StepID{"wait"}},
		{ID: "plan", JobName: "Plan", DependsOn: []tui.StepID{"read-doing", "read-rejected", "read-todo", "read-docs"}},
		{ID: "do", JobName: "Build", DependsOn: []tui.StepID{"plan"}},
		{ID: "report", JobName: "Report", DependsOn: []tui.StepID{"do"}},
		{ID: "sync", JobName: "Sync", DependsOn: []tui.StepID{"report"}},
		{ID: "loop-end", JobName: "Loop end", DependsOn: []tui.StepID{"sync"}, Status: tui.StatusBlue},
	})

	pipelineModel := tui.NewPipelineModel(spec)
	p := tea.NewProgram(pipelineModel, tea.WithAltScreen())

	go func() {
		time.Sleep(1 * time.Second)
		p.Send(tui.SetStepStatusMsg{StepID: "perf-a-setup", Status: tui.StatusGray})
		time.Sleep(150 * time.Millisecond)
		p.Send(tui.SetStepSpinnerMsg{StepID: "perf-a-setup", Spinner: true})
		p.Send(tui.SetStepStatusMsg{StepID: "perf-a-setup", Status: tui.StatusYellow})
		time.Sleep(2 * time.Second)
		p.Send(tui.SetStepSpinnerMsg{StepID: "perf-a-setup", Spinner: false})
		p.Send(tui.SetStepStatusMsg{StepID: "perf-a-setup", Status: tui.StatusGreen})
		p.Send(tui.SetStepSelectedMsg{StepID: "perf-b-stress"})
		time.Sleep(500 * time.Millisecond)
		p.Send(tui.SetStepSelectedMsg{StepID: ""})
	}()

	if _, err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "error running program: %v\n", err)
		os.Exit(1)
	}
}
