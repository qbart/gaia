package pm

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
)

type Project struct {
	ID   ProjectID
	Name string
	Icon string
}

type ProjectRepository interface {
	Create(ctx context.Context, in CreateProjectInput) (CreateProjectOutput, error)
	List(ctx context.Context, in ListProjectsInput) (ListProjectsOutput, error)
	Get(ctx context.Context, in GetProjectInput) (GetProjectOutput, error)
}

type CreateProjectInput struct {
	Name string
}

type CreateProjectOutput struct {
	Project Project
}

type ListProjectsInput struct{}

type ListProjectsOutput struct {
	Projects []Project
}

type GetProjectInput struct {
	ID ProjectID
}

type GetProjectOutput struct {
	Project Project
	Found   bool
}

var (
	ErrInvalidProjectName = errors.New("project name must not be empty")
	ErrInvalidTaskName    = errors.New("task name must not be empty")
	ErrInvalidStatus      = errors.New("invalid task status")
	ErrTaskNotFound       = errors.New("task not found")
	ErrEmptyComment       = errors.New("comment must not be empty")
)

type TaskRepository interface {
	Create(ctx context.Context, in CreateTaskInput) (CreateTaskOutput, error)
	ListByProject(ctx context.Context, in ListTasksByProjectInput) (ListTasksByProjectOutput, error)
	Get(ctx context.Context, in GetTaskInput) (GetTaskOutput, error)
	Update(ctx context.Context, in UpdateTaskInput) (UpdateTaskOutput, error)
	Move(ctx context.Context, in MoveTaskInput) (MoveTaskOutput, error)
	AddComment(ctx context.Context, in AddCommentInput) (AddCommentOutput, error)
}

type AddCommentInput struct {
	TaskID TaskID
	Body   string
}

type AddCommentOutput struct {
	Task Task
}

type GetTaskInput struct {
	ID TaskID
}

type GetTaskOutput struct {
	Task  Task
	Found bool
}

type UpdateTaskInput struct {
	ID     TaskID
	Name   string
	Body   string
	Status Status
	Tags   []string
}

type UpdateTaskOutput struct {
	Task Task
}

type CreateTaskInput struct {
	ProjectID ProjectID
	Name      string
	Body      string
	Status    Status
	Tags      []string
}

type CreateTaskOutput struct {
	Task Task
}

type ListTasksByProjectInput struct {
	ProjectID ProjectID
}

type ListTasksByProjectOutput struct {
	Tasks []Task
}

type MoveTaskInput struct {
	ID     TaskID
	Status Status
}

type MoveTaskOutput struct {
	Task Task
}

type InMemoryTaskRepository struct {
	mu     sync.RWMutex
	seq    atomic.Uint64
	order  []TaskID
	tasks  map[TaskID]Task
}

func NewInMemoryTaskRepository() *InMemoryTaskRepository {
	return &InMemoryTaskRepository{
		tasks: make(map[TaskID]Task),
	}
}

func (r *InMemoryTaskRepository) Create(ctx context.Context, in CreateTaskInput) (CreateTaskOutput, error) {
	name := strings.TrimSpace(in.Name)
	if name == "" {
		return CreateTaskOutput{}, ErrInvalidTaskName
	}
	if !isKnownStatus(in.Status) {
		return CreateTaskOutput{}, ErrInvalidStatus
	}

	id := TaskID(fmt.Sprintf("t-%d", r.seq.Add(1)))
	t := Task{
		ID:        id,
		ProjectID: in.ProjectID,
		Name:      name,
		Body:      strings.TrimSpace(in.Body),
		Status:    in.Status,
		Tags:      in.Tags,
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	r.tasks[id] = t
	r.order = append(r.order, id)

	return CreateTaskOutput{Task: t}, nil
}

func (r *InMemoryTaskRepository) ListByProject(ctx context.Context, in ListTasksByProjectInput) (ListTasksByProjectOutput, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	out := ListTasksByProjectOutput{Tasks: make([]Task, 0)}
	for _, id := range r.order {
		t := r.tasks[id]
		if t.ProjectID == in.ProjectID {
			out.Tasks = append(out.Tasks, t)
		}
	}
	return out, nil
}

func (r *InMemoryTaskRepository) Get(ctx context.Context, in GetTaskInput) (GetTaskOutput, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	t, ok := r.tasks[in.ID]
	return GetTaskOutput{Task: t, Found: ok}, nil
}

func (r *InMemoryTaskRepository) Update(ctx context.Context, in UpdateTaskInput) (UpdateTaskOutput, error) {
	name := strings.TrimSpace(in.Name)
	if name == "" {
		return UpdateTaskOutput{}, ErrInvalidTaskName
	}
	if !isKnownStatus(in.Status) {
		return UpdateTaskOutput{}, ErrInvalidStatus
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	t, ok := r.tasks[in.ID]
	if !ok {
		return UpdateTaskOutput{}, ErrTaskNotFound
	}
	t.Name = name
	t.Body = strings.TrimSpace(in.Body)
	t.Status = in.Status
	t.Tags = in.Tags
	r.tasks[in.ID] = t
	return UpdateTaskOutput{Task: t}, nil
}

func (r *InMemoryTaskRepository) AddComment(ctx context.Context, in AddCommentInput) (AddCommentOutput, error) {
	body := strings.TrimSpace(in.Body)
	if body == "" {
		return AddCommentOutput{}, ErrEmptyComment
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	t, ok := r.tasks[in.TaskID]
	if !ok {
		return AddCommentOutput{}, ErrTaskNotFound
	}
	t.Comments = append(t.Comments, body)
	r.tasks[in.TaskID] = t
	return AddCommentOutput{Task: t}, nil
}

func (r *InMemoryTaskRepository) Move(ctx context.Context, in MoveTaskInput) (MoveTaskOutput, error) {
	if !isKnownStatus(in.Status) {
		return MoveTaskOutput{}, ErrInvalidStatus
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	t, ok := r.tasks[in.ID]
	if !ok {
		return MoveTaskOutput{}, ErrTaskNotFound
	}
	t.Status = in.Status
	r.tasks[in.ID] = t
	return MoveTaskOutput{Task: t}, nil
}

func isKnownStatus(s Status) bool {
	for _, known := range Statuses {
		if s == known {
			return true
		}
	}
	return false
}

type InMemoryProjectRepository struct {
	mu       sync.RWMutex
	order    []ProjectID
	projects map[ProjectID]Project
}

func NewInMemoryProjectRepository() *InMemoryProjectRepository {
	return &InMemoryProjectRepository{
		projects: make(map[ProjectID]Project),
	}
}

func (r *InMemoryProjectRepository) Create(ctx context.Context, in CreateProjectInput) (CreateProjectOutput, error) {
	name := strings.TrimSpace(in.Name)
	if name == "" {
		return CreateProjectOutput{}, ErrInvalidProjectName
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	id := ProjectID(slugify(name))
	if _, exists := r.projects[id]; exists {
		id = ProjectID(string(id) + "-" + randomSuffix(len(r.projects)+1))
	}

	p := Project{
		ID:   id,
		Name: name,
		Icon: iconFromName(name),
	}
	r.projects[id] = p
	r.order = append(r.order, id)

	return CreateProjectOutput{Project: p}, nil
}

func (r *InMemoryProjectRepository) List(ctx context.Context, in ListProjectsInput) (ListProjectsOutput, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	out := ListProjectsOutput{Projects: make([]Project, 0, len(r.order))}
	for _, id := range r.order {
		out.Projects = append(out.Projects, r.projects[id])
	}
	return out, nil
}

func (r *InMemoryProjectRepository) Get(ctx context.Context, in GetProjectInput) (GetProjectOutput, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	p, ok := r.projects[in.ID]
	return GetProjectOutput{Project: p, Found: ok}, nil
}

func slugify(name string) string {
	var b strings.Builder
	b.Grow(len(name))
	prevDash := false
	for _, r := range strings.ToLower(name) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
			prevDash = false
		case r == ' ' || r == '-' || r == '_':
			if !prevDash && b.Len() > 0 {
				b.WriteByte('-')
				prevDash = true
			}
		}
	}
	out := strings.TrimRight(b.String(), "-")
	if out == "" {
		out = "project"
	}
	return out
}

func iconFromName(name string) string {
	for _, r := range name {
		return strings.ToUpper(string(r))
	}
	return "?"
}

func randomSuffix(n int) string {
	const alphabet = "abcdefghijklmnopqrstuvwxyz0123456789"
	if n <= 0 {
		n = 1
	}
	var b strings.Builder
	for n > 0 {
		b.WriteByte(alphabet[n%len(alphabet)])
		n /= len(alphabet)
	}
	return b.String()
}
