package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/qbart/gaia/config"
	"github.com/qbart/gaia/gaia"
	"github.com/qbart/gaia/pm"
	"github.com/qbart/gaia/ui"
	"github.com/qbart/tui/tui"

	tea "github.com/charmbracelet/bubbletea"
)

func Event(p *tea.Program, kind string, spinner bool) {
	p.Send(tui.SetStepSpinnerMsg{StepID: tui.StepID(kind), Spinner: spinner})
	if spinner {
		p.Send(tui.SetStepStatusMsg{StepID: tui.StepID(kind), Status: tui.StatusYellow})
	} else {
		p.Send(tui.SetStepStatusMsg{StepID: tui.StepID(kind), Status: tui.StatusBlack})
	}
}

func main() {
	ctx := context.Background()

	gh := pm.NewGitHub(os.Getenv("PAT"), "qbart", "gaia")
	agent := gaia.NewAgent(gh)

	fmt.Println(config.Name)
	fmt.Println("Hello, World!")
	spec := tui.NewPipelineSpec("gaia", []tui.StepSpec{
		{ID: "loop-start", JobName: "Loop start", Status: tui.StatusBlue},
		{ID: "wait", JobName: "Wait for tasks", DependsOn: []tui.StepID{"loop-start"}},
		{ID: "read-docs", JobName: "Read docs", DependsOn: []tui.StepID{"wait"}},
		{ID: "read-todo", JobName: "Get TODOs", DependsOn: []tui.StepID{"wait"}},
		{ID: "read-doing", JobName: "Get In Progress tasks", DependsOn: []tui.StepID{"wait"}},
		{ID: "read-rejected", JobName: "Get Rejected tasks", DependsOn: []tui.StepID{"wait"}},
		{ID: "each-task", JobName: "For each task", DependsOn: []tui.StepID{"read-doing", "read-rejected", "read-todo", "read-docs"}, Status: tui.StatusBlue},
		{ID: "do", JobName: "Build", DependsOn: []tui.StepID{"each-task"}},
		{ID: "report", JobName: "Report", DependsOn: []tui.StepID{"do"}},
		{ID: "sync", JobName: "Sync", DependsOn: []tui.StepID{"report"}},
		{ID: "loop-end", JobName: "Loop end", DependsOn: []tui.StepID{"sync"}, Status: tui.StatusBlue},
	})

	pipelineModel := tui.NewPipelineModel(spec)
	p := tea.NewProgram(ui.NewAppModel(pipelineModel), tea.WithAltScreen())

	ticker := time.NewTicker(1 * time.Second)
	start := time.Now()

	go agent.Run(ctx)

	go func() {
		for {
			select {
			case event := <-agent.Dispatcher:
				Event(p, event.Kind, event.Enable)
			case task := <-agent.Tasks:
				p.Send(ui.StatusBarSetLeftMsg{Text: task.Name})
			case err := <-agent.Errors:
				p.Send(ui.StatusBarSetLeftMsg{Text: err.Error()})
			case line := <-agent.Output:
				p.Send(ui.AppendOutputMsg{Text: line})
			case <-ticker.C:
				now := time.Now()
				duration := now.Sub(start).Round(time.Second)
				p.Send(ui.StatusBarSetRightMsg{Text: fmt.Sprintf("%s", duration.String())})
			}
		}
	}()

	if _, err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "error running program: %v\n", err)
		os.Exit(1)
	}
}
