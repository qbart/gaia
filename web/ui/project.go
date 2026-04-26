package ui

import "github.com/qbart/gaia/pm"

type ProjectCard struct {
	ID    pm.TaskID
	Name  string
	Body  string
	Tags  []string
}

type ProjectColumn struct {
	Status pm.Status
	Title  string
	Cards  []ProjectCard
}

type Project struct {
	ID    pm.ProjectID
	Name  string
	Icon  string
}

type ProjectPageData struct {
	Projects []Project
	Active   pm.ProjectID
	Columns  []ProjectColumn
}

func FakeProjectPage() ProjectPageData {
	projects := []Project{
		{ID: "gaia", Name: "Gaia", Icon: "G"},
		{ID: "zen", Name: "Zen", Icon: "Z"},
		{ID: "kiwi", Name: "Kiwi", Icon: "K"},
		{ID: "atlas", Name: "Atlas", Icon: "A"},
	}

	columns := []ProjectColumn{
		{
			Status: pm.StatusDocs,
			Title:  "Docs",
			Cards: []ProjectCard{
				{ID: "d-1", Name: "Architecture overview", Body: "High level system diagram", Tags: []string{"docs"}},
				{ID: "d-2", Name: "Onboarding guide", Body: "Steps for new contributors", Tags: []string{"docs", "intro"}},
			},
		},
		{
			Status: pm.StatusBrainstorm,
			Title:  "Brainstorm",
			Cards: []ProjectCard{
				{ID: "b-1", Name: "Voice command mode", Body: "Investigate Whisper integration", Tags: []string{"idea"}},
				{ID: "b-2", Name: "Telemetry dashboard", Body: "Cards / latency / errors", Tags: []string{"idea", "ops"}},
			},
		},
		{
			Status: pm.StatusTodo,
			Title:  "Todo",
			Cards: []ProjectCard{
				{ID: "t-1", Name: "Implement /server command", Body: "CLI subcommand starting web/server.go", Tags: []string{"cli"}},
				{ID: "t-2", Name: "Bootstrap layout", Body: "Switch to Bootstrap 3 base layout", Tags: []string{"ui"}},
				{ID: "t-3", Name: "Project page", Body: "Trello-like board with workspace switcher", Tags: []string{"ui"}},
			},
		},
		{
			Status: pm.StatusInProgress,
			Title:  "In Progress",
			Cards: []ProjectCard{
				{ID: "p-1", Name: "Postgres wiring", Body: "Connect via zen/pg", Tags: []string{"db"}},
			},
		},
		{
			Status: pm.StatusInReview,
			Title:  "In Review",
			Cards: []ProjectCard{
				{ID: "r-1", Name: "Trello rate limit fix", Body: "Backoff on 429", Tags: []string{"bugfix"}},
			},
		},
		{
			Status: pm.StatusRejected,
			Title:  "Rejected",
			Cards: []ProjectCard{
				{ID: "x-1", Name: "Rewrite in Rust", Body: "Out of scope", Tags: []string{"meta"}},
			},
		},
		{
			Status: pm.StatusDone,
			Title:  "Done",
			Cards: []ProjectCard{
				{ID: "y-1", Name: "Initial Gaia agent loop", Body: "Pipeline TUI shipped", Tags: []string{"core"}},
				{ID: "y-2", Name: "GitHub provider", Body: "Tasks read/write via GH API", Tags: []string{"providers"}},
			},
		},
	}

	return ProjectPageData{
		Projects: projects,
		Active:   "gaia",
		Columns:  columns,
	}
}
