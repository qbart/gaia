package ui

import "github.com/qbart/gaia/pm"

type TaskNewPageData struct {
	ProjectID   pm.ProjectID
	ProjectName string
	Status      pm.Status
	Name        string
	Body        string
	Error       string
}
