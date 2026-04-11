package pm

import (
	"context"
	"fmt"
	"strconv"

	"github.com/google/go-github/v84/github"
)

type GitHub struct {
	client *github.Client
	owner  string
	repo   string
}

func NewGitHub(token, owner, repo string) *GitHub {
	return &GitHub{
		client: github.NewClient(nil).WithAuthToken(token),
		owner:  owner,
		repo:   repo,
	}
}

var statusColors = map[Status]string{
	StatusDocs:       "d876e3",
	StatusBrainstorm: "d876e3",
	StatusTodo:       "0d9488",
	StatusInProgress: "f9a825",
	StatusInReview:   "b91c1c",
	StatusRejected:   "0d9488",
	StatusDone:       "0e8a16",
}

var statusDescriptions = map[Status]string{
	StatusDocs:       "Instructions for AI",
	StatusBrainstorm: "Instructions for AI to come up with new tasks when nothing to work on",
	StatusTodo:       "Tasks to be picked up by Claude",
	StatusInProgress: "Claude picked it up and is currently working on it",
	StatusInReview:   "Claude finished and marked the issue for review, will not be picked unless rejected",
	StatusRejected:   "Review is rejected, Claude will pick it up again and apply feedback from comments",
	StatusDone:       "Review is approved, task is closed",
}

func (g *GitHub) Init(ctx context.Context) error {
	existing, _, err := g.client.Issues.ListLabels(ctx, g.owner, g.repo, &github.ListOptions{PerPage: 100})
	if err != nil {
		return fmt.Errorf("listing labels: %w", err)
	}
	have := make(map[string]bool, len(existing))
	for _, l := range existing {
		have[l.GetName()] = true
	}
	for _, s := range Statuses {
		name := string(s)
		if have[name] {
			continue
		}
		color := statusColors[s]
		desc := statusDescriptions[s]
		_, _, err := g.client.Issues.CreateLabel(ctx, g.owner, g.repo, &github.Label{
			Name:        &name,
			Color:       &color,
			Description: &desc,
		})
		if err != nil {
			return fmt.Errorf("creating label %q: %w", name, err)
		}
	}
	return nil
}

func (g *GitHub) ListTasks(ctx context.Context, status Status) ([]*Task, error) {
	opts := &github.IssueListByRepoOptions{
		State: "open",
		ListOptions: github.ListOptions{PerPage: 100},
	}
	if status == StatusDone {
		opts.State = "closed"
	} else {
		opts.Labels = []string{string(status)}
	}

	issues, _, err := g.client.Issues.ListByRepo(ctx, g.owner, g.repo, opts)
	if err != nil {
		return nil, err
	}

	tasks := make([]*Task, 0, len(issues))
	for _, issue := range issues {
		num := issue.GetNumber()
		comments, _, err := g.client.Issues.ListComments(ctx, g.owner, g.repo, num, &github.IssueListCommentsOptions{
			ListOptions: github.ListOptions{PerPage: 100},
		})
		if err != nil {
			return nil, err
		}
		bodies := make([]string, 0, len(comments))
		for _, c := range comments {
			if body := c.GetBody(); body != "" {
				bodies = append(bodies, body)
			}
		}
		tasks = append(tasks, &Task{
			ID:       TaskID(fmt.Sprintf("%d", num)),
			Name:     fmt.Sprintf("#%d %s", num, issue.GetTitle()),
			Body:     issue.GetBody(),
			Status:   status,
			Comments: bodies,
		})
	}
	return tasks, nil
}

func (g *GitHub) CreateTask(ctx context.Context, task Task) (TaskID, error) {
	label := string(StatusTodo)
	issue, _, err := g.client.Issues.Create(ctx, g.owner, g.repo, &github.IssueRequest{
		Title:  &task.Name,
		Body:   &task.Body,
		Labels: &[]string{label},
	})
	if err != nil {
		return "", err
	}
	return TaskID(fmt.Sprintf("%d", issue.GetNumber())), nil
}

func (g *GitHub) MoveTaskTo(ctx context.Context, id TaskID, status Status) error {
	num, err := issueNumber(id)
	if err != nil {
		return err
	}

	// Build new label set: strip all known status labels, add the new one (if not done)
	issue, _, err := g.client.Issues.Get(ctx, g.owner, g.repo, num)
	if err != nil {
		return err
	}

	statusSet := make(map[string]bool, len(Statuses))
	for _, s := range Statuses {
		statusSet[string(s)] = true
	}

	newLabels := make([]string, 0)
	for _, l := range issue.Labels {
		if !statusSet[l.GetName()] {
			newLabels = append(newLabels, l.GetName())
		}
	}
	if status != StatusDone {
		newLabels = append(newLabels, string(status))
	}

	_, _, err = g.client.Issues.ReplaceLabelsForIssue(ctx, g.owner, g.repo, num, newLabels)
	if err != nil {
		return err
	}

	state := "open"
	if status == StatusDone {
		state = "closed"
	}
	_, _, err = g.client.Issues.Edit(ctx, g.owner, g.repo, num, &github.IssueRequest{State: &state})
	return err
}

func (g *GitHub) CommentTask(ctx context.Context, id TaskID, body string) error {
	num, err := issueNumber(id)
	if err != nil {
		return err
	}

	_, _, err = g.client.Issues.CreateComment(ctx, g.owner, g.repo, num, &github.IssueComment{Body: &body})
	return err
}

func issueNumber(id TaskID) (int, error) {
	num, err := strconv.Atoi(string(id))
	if err != nil {
		return 0, fmt.Errorf("invalid task id %q: must be a GitHub issue number", id)
	}
	return num, nil
}
