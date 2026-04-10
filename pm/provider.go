package pm

import "context"

type TaskID string
type ProjectID string
type Status string

type Task struct {
	ID       TaskID
	Name     string
	Body     string
	Status   Status
	Comments []string
}

const (
	StatusDocs       Status = "docs"
	StatusBacklog    Status = "backlog"
	StatusTodo       Status = "todo"
	StatusInProgress Status = "doing"
	StatusInReview   Status = "review"
	StatusRejected   Status = "rejected"
	StatusDone       Status = "done"
)

var Statuses = []Status{
	StatusDocs,
	StatusBacklog,
	StatusTodo,
	StatusInProgress,
	StatusInReview,
	StatusRejected,
	StatusDone,
}

type Provider interface {
	ListTasks(ctx context.Context, status Status) ([]*Task, error)
	MoveTaskTo(ctx context.Context, id TaskID, status Status) error
	CommentTask(ctx context.Context, id TaskID, body string) error
}
