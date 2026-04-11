package gaia

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/qbart/gaia/pm"
	"golang.org/x/sync/errgroup"
)

type Command struct {
	Kind   string
	Enable bool
	Tasks  int
}

type TaskCommand struct {
	Name     string
	Duration time.Duration
	Finished bool
}

type Dispatcher chan (Command)

type Agent struct {
	Errors          chan (error)
	Dispatcher      chan (Command)
	Tasks           chan (TaskCommand)
	Output          chan string
	Provider        pm.Provider
	Model           string
	GodMode         bool
	TasksDocs       *Tasks
	TasksBrainstorm *Tasks
	TasksTodo       *Tasks
	TasksDoing      *Tasks
	TasksRejected   *Tasks
	TasksReview     *Tasks
	firstRun        bool
	RateLimit       bool
	WaitDuration    time.Duration
}

func NewAgent(p pm.Provider, model string, god bool, waitDuration time.Duration) *Agent {
	return &Agent{
		firstRun:        true,
		Dispatcher:      make(Dispatcher),
		Errors:          make(chan error),
		Tasks:           make(chan TaskCommand),
		Output:          make(chan string, 256),
		Provider:        p,
		Model:           model,
		GodMode:         god,
		WaitDuration:    waitDuration,
		TasksDocs:       NewTasks(),
		TasksBrainstorm: NewTasks(),
		TasksTodo:       NewTasks(),
		TasksDoing:      NewTasks(),
		TasksRejected:   NewTasks(),
		TasksReview:     NewTasks(),
	}
}

func (a *Agent) Run(ctx context.Context) {
	for {
		a.Wait(ctx)
		a.ReadTasks(ctx)

		if a.WorkableTasks() > 0 {
			a.Do(ctx)
			if !a.RateLimit {
				a.Report(ctx)
				a.Sync(ctx)
			}
		} else if a.TasksBrainstorm.Len() > 0 {
			a.Brainstorm(ctx)
		}
	}
}

func (a *Agent) Wait(ctx context.Context) {
	// dont wait for the first time
	if a.firstRun {
		a.firstRun = false
		return
	}
	if a.WorkableTasks() > 0 {
		// we have job to do, no need to wait
		return
	}

	wait := a.WaitDuration
	if a.RateLimit {
		wait = 5 * time.Minute
	}

	a.Dispatcher <- Command{Kind: "wait", Enable: true}
	time.Sleep(wait)
	a.Dispatcher <- Command{Kind: "wait", Enable: false}

	a.RateLimit = false
}

func (a *Agent) ReadTasks(ctx context.Context) {
	a.TasksDocs.Reset()
	a.TasksBrainstorm.Reset()
	a.TasksTodo.Reset()
	a.TasksDoing.Reset()
	a.TasksRejected.Reset()

	a.Dispatcher <- Command{Kind: "read-docs", Enable: true}
	a.Dispatcher <- Command{Kind: "read-brainstorm", Enable: true}
	a.Dispatcher <- Command{Kind: "read-todo", Enable: true}
	a.Dispatcher <- Command{Kind: "read-doing", Enable: true}
	a.Dispatcher <- Command{Kind: "read-rejected", Enable: true}

	g := errgroup.Group{}
	g.Go(func() error {
		tasks, err := a.Provider.ListTasks(ctx, pm.StatusDocs)
		if err != nil {
			return err
		}
		a.TasksDocs.Append(tasks...)
		a.TasksDocs.Sort()
		return nil
	})
	g.Go(func() error {
		tasks, err := a.Provider.ListTasks(ctx, pm.StatusBrainstorm)
		if err != nil {
			return err
		}
		a.TasksBrainstorm.Append(tasks...)
		a.TasksBrainstorm.Sort()
		return nil
	})
	g.Go(func() error {
		tasks, err := a.Provider.ListTasks(ctx, pm.StatusTodo)
		if err != nil {
			return err
		}
		a.TasksTodo.Append(tasks...)
		a.TasksTodo.Sort()
		return nil
	})
	g.Go(func() error {
		tasks, err := a.Provider.ListTasks(ctx, pm.StatusInProgress)
		if err != nil {
			return err
		}
		a.TasksDoing.Append(tasks...)
		a.TasksDoing.Sort()
		return nil
	})
	g.Go(func() error {
		tasks, err := a.Provider.ListTasks(ctx, pm.StatusRejected)
		if err != nil {
			return err
		}
		a.TasksRejected.Append(tasks...)
		a.TasksRejected.Sort()
		return nil
	})
	err := g.Wait()
	if err != nil {
		a.Errors <- err
	}

	docs := a.TasksDocs.Len()
	brainstorm := a.TasksBrainstorm.Len()
	todo := a.TasksTodo.Len()
	doing := a.TasksDoing.Len()
	rejected := a.TasksRejected.Len()

	a.Dispatcher <- Command{Kind: "read-docs", Enable: false, Tasks: docs}
	a.Dispatcher <- Command{Kind: "read-brainstorm", Enable: false, Tasks: brainstorm}
	a.Dispatcher <- Command{Kind: "read-todo", Enable: false, Tasks: todo}
	a.Dispatcher <- Command{Kind: "read-doing", Enable: false, Tasks: doing}
	a.Dispatcher <- Command{Kind: "read-rejected", Enable: false, Tasks: rejected}
}

func (a *Agent) Do(ctx context.Context) {
	a.Dispatcher <- Command{Kind: "do", Enable: true}
	defer func() {
		a.Dispatcher <- Command{Kind: "do", Enable: false}
	}()

	task := a.TasksDoing.First()
	if task == nil {
		task = a.TasksRejected.First()
	}
	if task == nil {
		task = a.TasksTodo.First()
	}
	if task != nil {
		if err := a.Provider.MoveTaskTo(ctx, task.ID, pm.StatusInProgress); err != nil {
			a.Errors <- err
		}
		start := time.Now()
		a.Tasks <- TaskCommand{Name: task.Name, Duration: time.Duration(0)}
		for iter := 0; iter < 10; iter++ {
			var sb strings.Builder
			for _, doc := range a.TasksDocs.All() {
				sb.WriteString(doc.Body)
				sb.WriteString("\n\n")
			}
			sb.WriteString("Task to implement:\n")
			sb.WriteString(task.Name)
			sb.WriteString("\n\n")
			sb.WriteString(task.Body)
			if len(task.Comments) > 0 {
				sb.WriteString("\n\nComments:\n")
				for _, c := range task.Comments {
					sb.WriteString("- ")
					sb.WriteString(c)
					sb.WriteString("\n")
				}
			}
			sb.WriteString("\n\nWhen TASK is done, write a brief summary for future github issue comment of the changes made to .gaia/")
			sb.WriteString(string(task.ID))
			sb.WriteString(".md")

			args := []string{"-p",
				"--output-format", "stream-json",
				"--verbose",
				"--allowedTools", "Bash(git diff *),Bash(git log *),Bash(git status *)",
				"--permission-mode", "auto",
			}
			if a.GodMode {
				args = []string{"-p",
					"--output-format", "stream-json",
					"--verbose",
					"--dangerously-skip-permissions",
				}
			}

			if a.Model != "" {
				args = append(args, "--model", a.Model)
			}
			cmd := exec.CommandContext(ctx, "claude", args...)
			cmd.Stdin = strings.NewReader(sb.String())
			stdout, err := cmd.StdoutPipe()
			if err != nil {
				a.Errors <- err
			} else if err = cmd.Start(); err != nil {
				a.Errors <- err
			} else {
				scanner := bufio.NewScanner(stdout)
				scanner.Buffer(make([]byte, 1024*1024), 1024*1024)
				for scanner.Scan() {
					line := scanner.Text()
					a.detectRateLimit(line)
					select {
					case a.Output <- line:
					default:
					}
				}
				if err := cmd.Wait(); err != nil {
					a.Errors <- err
				}
			}
			if a.RateLimit {
				break
			}
			if _, err := os.Stat(".gaia/" + string(task.ID) + ".md"); err == nil {
				break
			}
		}
		if !a.RateLimit {
			a.TasksReview.Append(task)
		}
		now := time.Now()
		duration := now.Sub(start)
		a.Tasks <- TaskCommand{Name: task.Name, Duration: duration, Finished: true}
	}
}

func (a *Agent) Report(ctx context.Context) {
	a.Dispatcher <- Command{Kind: "report", Enable: true}
	defer func() {
		a.Dispatcher <- Command{Kind: "report", Enable: false}
	}()

	os.MkdirAll(".gaia", 0755)
	tasks := a.TasksReview.All()
	for _, task := range tasks {
		if err := a.Provider.MoveTaskTo(ctx, task.ID, pm.StatusInReview); err != nil {
			a.Errors <- err
		}
		if note, err := os.ReadFile(".gaia/" + string(task.ID) + ".md"); err == nil {
			if err := a.Provider.CommentTask(ctx, task.ID, string(note)); err != nil {
				a.Errors <- err
			}
		}
	}
	a.TasksReview.Reset()
}

func (a *Agent) Sync(ctx context.Context) {
	a.Dispatcher <- Command{Kind: "sync", Enable: true}
	defer func() {
		a.Dispatcher <- Command{Kind: "sync", Enable: false}
	}()

	entries, err := os.ReadDir(".gaia")
	if err != nil || len(entries) == 0 {
		return
	}

	var sb strings.Builder
	sb.WriteString("Generate a concise git commit message for the following changes. Output only the commit message, nothing else.\n\n")
	for _, entry := range entries {
		if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".md") {
			if data, err := os.ReadFile(".gaia/" + entry.Name()); err == nil {
				sb.Write(data)
				sb.WriteString("\n\n")
			}
		}
	}
	os.RemoveAll(".gaia")

	commitMsg := "chore: automated changes"
	commitCmd := exec.CommandContext(ctx, "claude", "-p", "--output-format", "text")
	commitCmd.Stdin = strings.NewReader(sb.String())
	if out, err := commitCmd.Output(); err != nil {
		a.Errors <- err
	} else if msg := strings.TrimSpace(string(out)); msg != "" {
		commitMsg = msg
	}

	if err := exec.CommandContext(ctx, "git", "add", "-A").Run(); err != nil {
		a.Errors <- err
		return
	}
	if err := exec.CommandContext(ctx, "git", "commit", "-m", commitMsg).Run(); err != nil {
		a.Errors <- err
		return
	}
	if err := exec.CommandContext(ctx, "git", "push", "origin", "HEAD").Run(); err != nil {
		a.Errors <- err
		return
	}
}

func (a *Agent) Brainstorm(ctx context.Context) {
	a.Dispatcher <- Command{Kind: "brainstorm", Enable: true}
	defer func() {
		a.Dispatcher <- Command{Kind: "brainstorm", Enable: false}
	}()

	var sb strings.Builder
	for _, doc := range a.TasksDocs.All() {
		sb.WriteString(doc.Body)
		sb.WriteString("\n\n")
	}
	for _, task := range a.TasksBrainstorm.All() {
		sb.WriteString(task.Name)
		sb.WriteString("\n\n")
		sb.WriteString(task.Body)
		sb.WriteString("\n\n")
	}
	sb.WriteString("Based on the above context, generate new actionable development tasks.\n\n")
	sb.WriteString("When done, create the file .gaia/brainstorm.json with the following exact structure — no markdown, no code fences, no explanation, raw JSON only:\n")
	sb.WriteString("[\n  {\"title\": \"<task title>\", \"body\": \"<task description>\"}\n]\n")
	sb.WriteString("Each object must have exactly two string fields: \"title\" (short task name) and \"body\" (detailed description). Write nothing else to the file.")

	args := []string{"-p",
		"--output-format", "stream-json",
		"--verbose",
		"--allowedTools", "Bash(git diff *),Bash(git log *),Bash(git status *)",
		"--permission-mode", "auto",
	}
	if a.GodMode {
		args = []string{"-p",
			"--output-format", "stream-json",
			"--verbose",
			"--dangerously-skip-permissions",
		}
	}
	if a.Model != "" {
		args = append(args, "--model", a.Model)
	}
	cmd := exec.CommandContext(ctx, "claude", args...)
	cmd.Stdin = strings.NewReader(sb.String())
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		a.Errors <- err
		return
	} else if err = cmd.Start(); err != nil {
		a.Errors <- err
		return
	} else {
		scanner := bufio.NewScanner(stdout)
		scanner.Buffer(make([]byte, 1024*1024), 1024*1024)
		for scanner.Scan() {
			line := scanner.Text()
			a.detectRateLimit(line)
			select {
			case a.Output <- line:
			default:
			}
		}
		if err := cmd.Wait(); err != nil {
			a.Errors <- err
		}
	}

	const brainstormFile = ".gaia/brainstorm.json"
	data, err := os.ReadFile(brainstormFile)
	if err != nil {
		a.Errors <- err
		return
	}
	defer os.Remove(brainstormFile)
	var ideas []struct {
		Title string `json:"title"`
		Body  string `json:"body"`
	}
	if err := json.Unmarshal(bytes.TrimSpace(data), &ideas); err != nil {
		a.Errors <- err
		return
	}
	for _, idea := range ideas {
		if _, err := a.Provider.CreateTask(ctx, pm.Task{Name: idea.Title, Body: idea.Body}); err != nil {
			a.Errors <- err
		}
	}
}

func (a *Agent) WorkableTasks() int {
	return a.TasksDoing.Len() + a.TasksRejected.Len() + a.TasksTodo.Len()
}

func (a *Agent) detectRateLimit(line string) {
	var ev struct {
		Type          string `json:"type"`
		RateLimitInfo *struct {
			Status string `json:"status"`
		} `json:"rate_limit_info"`
	}
	if err := json.Unmarshal([]byte(line), &ev); err != nil {
		return
	}
	if ev.Type == "rate_limit_event" && ev.RateLimitInfo != nil {
		a.RateLimit = true
		a.Errors <- fmt.Errorf("rate limit hit, waiting 5m before next attempt")
	}
}
