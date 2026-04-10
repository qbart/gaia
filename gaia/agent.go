package gaia

import (
	"context"
	"time"

	"github.com/qbart/gaia/pm"
	"golang.org/x/sync/errgroup"
)

type Command struct {
	Kind   string
	Enable bool
	Tasks  int
}

type Dispatcher chan (Command)

type Agent struct {
	Errors        chan (error)
	Dispatcher    chan (Command)
	Provider      pm.Provider
	TasksDocs     *Tasks
	TasksTodo     *Tasks
	TasksDoing    *Tasks
	TasksRejected *Tasks
	firstRun      bool
}

func NewAgent(p pm.Provider) *Agent {
	return &Agent{
		Dispatcher:    make(Dispatcher),
		Errors:        make(chan error),
		Provider:      p,
		TasksDocs:     NewTasks(),
		TasksTodo:     NewTasks(),
		TasksDoing:    NewTasks(),
		TasksRejected: NewTasks(),
	}
}

func (a *Agent) Run(ctx context.Context) {
	for {
		a.Wait(ctx)
		a.ReadTasks(ctx)
		a.Do(ctx)
		a.Report(ctx)
		a.Sync(ctx)
	}
}

func (a *Agent) Wait(ctx context.Context) {
	if a.firstRun {
		a.firstRun = false
		return
	}
	a.Dispatcher <- Command{Kind: "wait", Enable: true}
	time.Sleep(5 * time.Second)
	a.Dispatcher <- Command{Kind: "wait", Enable: false}
}

func (a *Agent) ReadTasks(ctx context.Context) {
	a.Dispatcher <- Command{Kind: "read-docs", Enable: true}
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
		return nil
	})
	g.Go(func() error {
		tasks, err := a.Provider.ListTasks(ctx, pm.StatusTodo)
		if err != nil {
			return err
		}
		a.TasksTodo.Append(tasks...)
		return nil
	})
	g.Go(func() error {
		tasks, err := a.Provider.ListTasks(ctx, pm.StatusInProgress)
		if err != nil {
			return err
		}
		a.TasksDoing.Append(tasks...)
		return nil
	})
	g.Go(func() error {
		tasks, err := a.Provider.ListTasks(ctx, pm.StatusRejected)
		if err != nil {
			return err
		}
		a.TasksRejected.Append(tasks...)
		return nil
	})
	err := g.Wait()
	if err != nil {
		a.Errors <- err
	}

	a.Dispatcher <- Command{Kind: "read-docs", Enable: false, Tasks: a.TasksDocs.Len()}
	a.Dispatcher <- Command{Kind: "read-todo", Enable: false, Tasks: a.TasksTodo.Len()}
	a.Dispatcher <- Command{Kind: "read-doing", Enable: false, Tasks: a.TasksDoing.Len()}
	a.Dispatcher <- Command{Kind: "read-rejected", Enable: false, Tasks: a.TasksRejected.Len()}
}

func (a *Agent) Do(ctx context.Context) {
	a.Dispatcher <- Command{Kind: "do", Enable: true}

	time.Sleep(1 * time.Second)

	a.Dispatcher <- Command{Kind: "do", Enable: false}
}

func (a *Agent) Report(ctx context.Context) {
	a.Dispatcher <- Command{Kind: "report", Enable: true}

	time.Sleep(1 * time.Second)

	a.Dispatcher <- Command{Kind: "report", Enable: false}
}

func (a *Agent) Sync(ctx context.Context) {
	a.Dispatcher <- Command{Kind: "sync", Enable: true}

	time.Sleep(1 * time.Second)

	a.Dispatcher <- Command{Kind: "sync", Enable: false}
}
