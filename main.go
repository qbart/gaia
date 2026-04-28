package main

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/SoftKiwiGames/zen/zen"
	"github.com/joho/godotenv"
	"github.com/qbart/gaia/gaia"
	"github.com/qbart/gaia/pm"
	"github.com/qbart/gaia/ui"
	"github.com/qbart/gaia/web"
	"github.com/qbart/tui/tui"
	"github.com/urfave/cli/v3"

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
	app := &cli.Command{
		Name:  "gaia",
		Usage: "AI-powered task agent",
		Commands: []*cli.Command{
			{
				Name:  "server",
				Usage: "Run the web server",
				Flags: []cli.Flag{
					&cli.StringFlag{
						Name:  "env-file",
						Usage: "Path to .env file to load environment variables from",
					},
				},
				Action: func(ctx context.Context, cmd *cli.Command) error {
					envs := zen.LoadEnvs()
					srv := &web.Server{
						Envs: envs,
					}
					srv.Run(ctx)
					return nil
				},
			},
		},
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:  "env-file",
				Usage: "Path to .env file to load environment variables from",
			},
			&cli.StringFlag{
				Name:  "provider",
				Usage: "Task provider: gaia, github or trello",
				Value: "gaia",
			},
			&cli.StringFlag{
				Name:  "project",
				Usage: "Gaia: project slug. GitHub: owner/repo. Trello: board ID",
			},
			&cli.StringFlag{
				Name:  "model",
				Usage: "Claude model name to use",
			},
			&cli.BoolFlag{
				Name:  "god",
				Usage: "Use dangerous permission mode (bypassPermissions)",
			},
			&cli.DurationFlag{
				Name:  "wait",
				Usage: "Default wait duration between loops",
				Value: 30 * time.Second,
			},
			&cli.DurationFlag{
				Name:  "hook-timeout",
				Usage: "Maximum execution time for hooks",
				Value: 10 * time.Minute,
			},
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			if envFile := cmd.String("env-file"); envFile != "" {
				if err := godotenv.Load(envFile); err != nil {
					return fmt.Errorf("loading env file: %w", err)
				}
			}
			var provider pm.Provider
			switch cmd.String("provider") {
			case "gaia":
				project := cmd.String("project")
				if project == "" {
					return fmt.Errorf("--project must be set to a gaia project slug")
				}
				baseURL := strings.TrimSpace(os.Getenv("GAIA_URL"))
				if baseURL == "" {
					baseURL = "http://localhost:4000"
				}
				token := os.Getenv("GAIA_TOKEN")
				if token == "" {
					return fmt.Errorf("GAIA_TOKEN must be set for gaia provider")
				}
				provider = pm.NewGaia(baseURL, token, project)
			case "github":
				parts := strings.SplitN(cmd.String("project"), "/", 2)
				if len(parts) != 2 {
					return fmt.Errorf("--project must be in owner/repo format for github provider")
				}
				provider = pm.NewGitHub(os.Getenv("PAT"), parts[0], parts[1])
			case "trello":
				boardID := cmd.String("project")
				if boardID == "" {
					return fmt.Errorf("--project must be set to a Trello board ID")
				}
				provider = pm.NewTrello(os.Getenv("TRELLO_KEY"), os.Getenv("TRELLO_TOKEN"), boardID)
			default:
				return fmt.Errorf("unknown provider %q, must be gaia, github or trello", cmd.String("provider"))
			}
			if err := provider.Init(ctx); err != nil {
				return fmt.Errorf("initializing provider: %w", err)
			}
			agent := gaia.NewAgent(provider, cmd.String("model"), cmd.Bool("god"), cmd.Duration("wait"), cmd.Duration("hook-timeout"))

			spec := tui.NewPipelineSpec("gaia", []tui.StepSpec{
				{ID: "loop-start", JobName: "Loop start", Status: tui.StatusBlue},
				{ID: "wait", JobName: "Wait for tasks", DependsOn: []tui.StepID{"loop-start"}},
				{ID: "read-docs", JobName: "Read docs", DependsOn: []tui.StepID{"wait"}},
				{ID: "read-brainstorm", JobName: "Get Brainstorm tasks", DependsOn: []tui.StepID{"wait"}},
				{ID: "read-todo", JobName: "Get TODOs", DependsOn: []tui.StepID{"wait"}},
				{ID: "read-doing", JobName: "Get In Progress tasks", DependsOn: []tui.StepID{"wait"}},
				{ID: "read-rejected", JobName: "Get Rejected tasks", DependsOn: []tui.StepID{"wait"}},
				{ID: "each-task", JobName: "For each task", DependsOn: []tui.StepID{"read-doing", "read-rejected", "read-todo", "read-docs", "read-brainstorm"}, Status: tui.StatusBlue},
				{ID: "do", JobName: "Build", DependsOn: []tui.StepID{"each-task"}},
				{ID: "brainstorm", JobName: "Brainstorm", DependsOn: []tui.StepID{"each-task"}},
				{ID: "report", JobName: "Report", DependsOn: []tui.StepID{"do"}},
				{ID: "sync", JobName: "Sync", DependsOn: []tui.StepID{"report"}},
				{ID: "loop-end", JobName: "Loop end", DependsOn: []tui.StepID{"sync", "brainstorm"}, Status: tui.StatusBlue},
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
						if task.Finished {
							p.Send(ui.StatusBarSetLeftMsg{Text: ""})
						} else {
							p.Send(ui.StatusBarSetLeftMsg{Text: task.Name})
						}
					case err := <-agent.Errors:
						p.Send(ui.StatusBarSetLeftMsg{Text: err.Error()})
					case line := <-agent.Output:
						p.Send(ui.AppendOutputMsg{Text: line})
					case <-ticker.C:
						now := time.Now()
						duration := now.Sub(start).Round(time.Second)
						p.Send(ui.StatusBarSetRightMsg{Text: duration.String()})
					}
				}
			}()

			if _, err := p.Run(); err != nil {
				return err
			}
			return nil
		},
	}

	if err := app.Run(context.Background(), os.Args); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}
