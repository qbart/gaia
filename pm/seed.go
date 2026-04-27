package pm

import "context"

func SeedDemo(ctx context.Context, projects ProjectRepository, tasks TaskRepository) error {
	seeds := []struct {
		Name  string
		Tasks []CreateTaskInput
	}{
		{
			Name: "Gaia",
			Tasks: []CreateTaskInput{
				{Name: "Architecture overview", Body: "High level system diagram", Status: StatusDocs, Tags: []string{"docs"}},
				{Name: "Onboarding guide", Body: "Steps for new contributors", Status: StatusDocs, Tags: []string{"docs", "intro"}},
				{Name: "Voice command mode", Body: "Investigate Whisper integration", Status: StatusDocs, Tags: []string{"brainstorm", "idea"}},
				{Name: "Telemetry dashboard", Body: "Cards / latency / errors", Status: StatusDocs, Tags: []string{"brainstorm", "idea", "ops"}},
				{Name: "Implement /server command", Body: "CLI subcommand starting web/server.go", Status: StatusTodo, Tags: []string{"cli"}},
				{Name: "Bootstrap layout", Body: "Switch to Bootstrap 3 base layout", Status: StatusTodo, Tags: []string{"ui"}},
				{Name: "Project page", Body: "Trello-like board with workspace switcher", Status: StatusInProgress, Tags: []string{"ui"}},
				{Name: "GitHub provider", Body: "Tasks read/write via GH API", Status: StatusDone, Tags: []string{"providers"}},
			},
		},
		{
			Name: "Zen",
			Tasks: []CreateTaskInput{
				{Name: "SQLite WAL tuning", Body: "Validate read/write pool sizes", Status: StatusTodo, Tags: []string{"db"}},
				{Name: "Session store benchmark", Body: "Memory vs Redis numbers", Status: StatusInProgress, Tags: []string{"perf"}},
				{Name: "Documented middleware list", Body: "Cover MiddlewaRequireAuth typo", Status: StatusDocs, Tags: []string{"docs"}},
			},
		},
		{
			Name: "Atlas",
			Tasks: []CreateTaskInput{
				{Name: "Tile cache", Body: "Investigate LRU sizes for map tiles", Status: StatusDocs, Tags: []string{"brainstorm", "idea"}},
				{Name: "Geocoding fallback", Body: "Vendor lookup when primary fails", Status: StatusInReview, Tags: []string{"infra"}},
			},
		},
	}

	for _, seed := range seeds {
		out, err := projects.Create(ctx, CreateProjectInput{Name: seed.Name})
		if err != nil {
			return err
		}
		for _, t := range seed.Tasks {
			t.ProjectID = out.Project.ID
			if _, err := tasks.Create(ctx, t); err != nil {
				return err
			}
		}
	}
	return nil
}
