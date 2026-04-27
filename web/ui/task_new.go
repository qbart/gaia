package ui

import "github.com/qbart/gaia/pm"

type TaskNewPageData struct {
	Board  ProjectPageData
	Status pm.Status
	Name   string
	Body   string
	Error  string
}
