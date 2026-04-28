package ui

import "github.com/qbart/gaia/pm"

type ProjectCard struct {
	ID        pm.TaskID
	ProjectID pm.ProjectID
	Name      string
	Body      string
	ReadOnly  bool
}

type ProjectColumn struct {
	ProjectID pm.ProjectID
	Status    pm.Status
	Title     string
	Cards     []ProjectCard
}

type Project struct {
	ID   pm.ProjectID
	Name string
	Icon string
}

type ProjectPageData struct {
	Projects []Project
	Active   pm.ProjectID
	Columns  []ProjectColumn
}

var statusTitles = map[pm.Status]string{
	pm.StatusDocs:       "Docs",
	pm.StatusTodo:       "Todo",
	pm.StatusInProgress: "In Progress",
	pm.StatusInReview:   "In Review",
	pm.StatusRejected:   "Rejected",
	pm.StatusDone:       "Done",
}

var boardStatuses = []pm.Status{
	pm.StatusDocs,
	pm.StatusTodo,
	pm.StatusInProgress,
	pm.StatusInReview,
	pm.StatusRejected,
	pm.StatusDone,
}

// IsReadOnlyStatus reports whether the AI agent owns this column. Cards in
// these columns are not user-editable from the UI.
func IsReadOnlyStatus(s pm.Status) bool {
	return s == pm.StatusInProgress
}

// IsReorderableStatus reports whether users can drag-reorder cards within
// the column. The doing column is owned by the agent and not user-orderable.
func IsReorderableStatus(s pm.Status) bool {
	return s != pm.StatusInProgress
}

// CanDeleteStatus reports whether a user can delete a card in this status.
func CanDeleteStatus(s pm.Status) bool {
	return s != pm.StatusInProgress
}

// TaskEditMode picks which UI form to render on the task edit page.
type TaskEditMode string

const (
	TaskEditModeForm     TaskEditMode = "form"
	TaskEditModeReview   TaskEditMode = "review"
	TaskEditModeRejected TaskEditMode = "rejected"
)

func ModeFor(s pm.Status) TaskEditMode {
	switch s {
	case pm.StatusInReview:
		return TaskEditModeReview
	case pm.StatusRejected:
		return TaskEditModeRejected
	default:
		return TaskEditModeForm
	}
}

func columnStatusClass(s pm.Status) string {
	if s == pm.StatusInProgress {
		return "gaia-column-doing"
	}
	return ""
}

func allowsAddCard(s pm.Status) bool {
	return s != pm.StatusInProgress && s != pm.StatusInReview && s != pm.StatusRejected
}

func StatusTitle(s pm.Status) string {
	if t, ok := statusTitles[s]; ok {
		return t
	}
	return string(s)
}

func BuildColumns(projectID pm.ProjectID, tasks []pm.Task) []ProjectColumn {
	bucket := make(map[pm.Status][]ProjectCard, len(boardStatuses))
	for _, t := range tasks {
		status := t.Status
		if status == pm.StatusBrainstorm {
			status = pm.StatusDocs
		}
		bucket[status] = append(bucket[status], ProjectCard{
			ID:        t.ID,
			ProjectID: t.ProjectID,
			Name:      t.Name,
			Body:      t.Body,
			ReadOnly:  IsReadOnlyStatus(status),
		})
	}
	cols := make([]ProjectColumn, 0, len(boardStatuses))
	for _, s := range boardStatuses {
		cols = append(cols, ProjectColumn{
			ProjectID: projectID,
			Status:    s,
			Title:     StatusTitle(s),
			Cards:     bucket[s],
		})
	}
	return cols
}
