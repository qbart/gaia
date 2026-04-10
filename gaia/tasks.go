package gaia

import (
	"sync"

	"github.com/qbart/gaia/pm"
)

type Tasks struct {
	Data []*pm.Task
	mux  sync.RWMutex
}

func NewTasks() *Tasks {
	return &Tasks{
		Data: make([]*pm.Task, 0),
	}
}

func (t *Tasks) Append(tasks ...*pm.Task) {
	t.mux.Lock()
	for _, task := range tasks {
		t.Data = append(t.Data, task)
	}
	t.mux.Unlock()
}

func (t *Tasks) Len() int {
	t.mux.RLock()
	defer t.mux.RUnlock()

	return len(t.Data)
}
