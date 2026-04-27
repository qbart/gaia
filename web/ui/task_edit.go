package ui

import "github.com/qbart/gaia/pm"

type TaskEditPageData struct {
	Board        ProjectPageData
	TaskID       pm.TaskID
	Status       pm.Status
	Name         string
	Body         string
	Comments     []string
	CommentDraft string
	CommentError string
	Error        string
}
