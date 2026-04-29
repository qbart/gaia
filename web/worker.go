package web

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/qbart/gaia/gaia"
	"github.com/qbart/gaia/pm"
	"github.com/qbart/gaia/web/core"
)

// WorkerEventKind is the type of input event a Worker accepts.
type WorkerEventKind string

const (
	WorkerEventProjectAdded   WorkerEventKind = "project-added"
	WorkerEventProjectRemoved WorkerEventKind = "project-removed"
)

// WorkerEvent is the input message accepted on Worker.Input.
type WorkerEvent struct {
	Kind    WorkerEventKind
	Project core.Project
}

// WorkerLog is the output message produced by Worker.Output, tagged with the
// project it originated from so subscribers can filter.
type WorkerLog struct {
	ProjectID core.ProjectID
	Stream    string
	Text      string
}

// Worker supervises one gaia.Agent per registered project.
//
//   - Send WorkerEventProjectAdded to spawn an agent (idempotent).
//   - Send WorkerEventProjectRemoved to stop one (idempotent).
//   - Read WorkerLog values from Output to multiplex agent logs by project.
type Worker struct {
	Input  chan WorkerEvent
	Output chan WorkerLog

	BaseURL      string
	Token        string
	Model        string
	God          bool
	WaitDuration time.Duration
	HookTimeout  time.Duration

	mu     sync.Mutex
	agents map[core.ProjectID]*agentEntry
}

type agentEntry struct {
	agent  *gaia.Agent
	cancel context.CancelFunc
}

func NewWorker(baseURL, token, model string, god bool, wait, hookTimeout time.Duration) *Worker {
	return &Worker{
		Input:        make(chan WorkerEvent, 32),
		Output:       make(chan WorkerLog, 1024),
		BaseURL:      baseURL,
		Token:        token,
		Model:        model,
		God:          god,
		WaitDuration: wait,
		HookTimeout:  hookTimeout,
		agents:       make(map[core.ProjectID]*agentEntry),
	}
}

// Run blocks until ctx is cancelled, processing input events and supervising
// agents.
func (w *Worker) Run(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			w.shutdown()
			return
		case ev := <-w.Input:
			switch ev.Kind {
			case WorkerEventProjectAdded:
				w.startAgent(ctx, ev.Project)
			case WorkerEventProjectRemoved:
				w.stopAgent(ev.Project.ID)
			}
		}
	}
}

func (w *Worker) startAgent(ctx context.Context, p core.Project) {
	w.mu.Lock()
	if _, ok := w.agents[p.ID]; ok {
		w.mu.Unlock()
		return
	}
	if p.Path == "" {
		w.mu.Unlock()
		slog.Warn("worker: project has no path, skipping", "id", p.ID)
		return
	}
	if w.Token == "" {
		w.mu.Unlock()
		slog.Warn("worker: GAIA_TOKEN not set, skipping agent", "id", p.ID)
		return
	}

	provider := pm.NewGaia(w.BaseURL, w.Token, string(p.ID))
	if err := provider.Init(ctx); err != nil {
		w.mu.Unlock()
		slog.Error("worker: provider init failed", "id", p.ID, "err", err.Error())
		return
	}
	agent := gaia.NewAgent(provider, w.Model, w.God, w.WaitDuration, w.HookTimeout)
	agent.Dir = p.Path

	actx, cancel := context.WithCancel(ctx)
	entry := &agentEntry{agent: agent, cancel: cancel}
	w.agents[p.ID] = entry
	w.mu.Unlock()

	slog.Info("worker: starting agent", "id", p.ID, "dir", p.Path)
	go agent.Run(actx)
	go w.consume(actx, p.ID, agent)
}

func (w *Worker) stopAgent(id core.ProjectID) {
	w.mu.Lock()
	entry, ok := w.agents[id]
	if ok {
		delete(w.agents, id)
	}
	w.mu.Unlock()
	if !ok {
		return
	}
	slog.Info("worker: stopping agent", "id", id)
	entry.cancel()
}

func (w *Worker) shutdown() {
	w.mu.Lock()
	defer w.mu.Unlock()
	for id, entry := range w.agents {
		entry.cancel()
		delete(w.agents, id)
	}
}

// consume forwards every log signal from a single agent into Output, tagged
// with the project ID. Drops messages on a full channel rather than blocking
// the agent.
func (w *Worker) consume(ctx context.Context, id core.ProjectID, a *gaia.Agent) {
	send := func(stream, text string) {
		select {
		case w.Output <- WorkerLog{ProjectID: id, Stream: stream, Text: text}:
		case <-ctx.Done():
		default:
		}
	}
	for {
		select {
		case <-ctx.Done():
			return
		case event := <-a.Dispatcher:
			state := "off"
			if event.Enable {
				state = "on"
			}
			send("step", event.Kind+" "+state)
		case task := <-a.Tasks:
			if task.Finished {
				send("task", "finished: "+task.Name)
			} else {
				send("task", "running: "+task.Name)
			}
		case err := <-a.Errors:
			send("error", err.Error())
		case line := <-a.Output:
			send("output", line)
		}
	}
}
