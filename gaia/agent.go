package gaia

import (
	"context"
	"time"

	"github.com/qbart/gaia/pm"
)

type Command struct {
	Kind   string
	Enable bool
}

type Dispatcher chan (Command)

type Agent struct {
	Dispatcher chan (Command)
	Provider   pm.Provider
}

func NewAgent(p pm.Provider) *Agent {
	return &Agent{
		Dispatcher: make(Dispatcher),
		Provider:   p,
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
	a.Dispatcher <- Command{Kind: "wait", Enable: true}

	time.Sleep(1 * time.Second)

	a.Dispatcher <- Command{Kind: "wait", Enable: false}
}

func (a *Agent) ReadTasks(ctx context.Context) {
	a.Dispatcher <- Command{Kind: "read-docs", Enable: true}
	a.Dispatcher <- Command{Kind: "read-todo", Enable: true}
	a.Dispatcher <- Command{Kind: "read-doing", Enable: true}
	a.Dispatcher <- Command{Kind: "read-rejected", Enable: true}

	time.Sleep(1 * time.Second)

	a.Dispatcher <- Command{Kind: "read-docs", Enable: false}
	a.Dispatcher <- Command{Kind: "read-todo", Enable: false}
	a.Dispatcher <- Command{Kind: "read-doing", Enable: false}
	a.Dispatcher <- Command{Kind: "read-rejected", Enable: false}
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
