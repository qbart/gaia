package web

import (
	"context"
	"embed"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/SoftKiwiGames/zen/zen"
	"github.com/SoftKiwiGames/zen/zen/sqlite"
	"github.com/qbart/gaia/config"
	"github.com/qbart/gaia/pm"
	"github.com/qbart/gaia/web/core"
	"github.com/qbart/gaia/web/ui"
	"github.com/tmaxmax/go-sse"
)

//go:embed static
var static embed.FS

type Server struct {
	Envs    zen.Envs
	Store   *core.Store
	ScanDir string
	Worker  *Worker
}

func (s *Server) Run(ctx context.Context) {
	embeds, err := zen.NewEmbeds(static, "static", zen.ReactPreset)
	if err != nil {
		slog.Error("embedding failed", "err", err.Error())
		os.Exit(1)
	}

	if s.Store == nil {
		root := strings.TrimSpace(os.Getenv("GAIA_DATA"))
		if root == "" {
			slog.Error("GAIA_DATA env var is required")
			os.Exit(1)
		}
		if err := os.MkdirAll(root, 0o755); err != nil {
			slog.Error("creating GAIA_DATA dir", "err", err.Error(), "path", root)
			os.Exit(1)
		}
		dbPath := filepath.Join(root, "gaia.db")
		db := sqlite.NewSQLite(dbPath, core.Migrations())
		if err := db.Connect(ctx); err != nil {
			slog.Error("connecting sqlite", "err", err.Error(), "path", dbPath)
			os.Exit(1)
		}
		if err := db.MigrateWithGoose(ctx, core.MigrationsDir); err != nil {
			slog.Error("running migrations", "err", err.Error())
			os.Exit(1)
		}
		s.Store = core.NewStore(db)
	}

	if s.ScanDir == "" {
		cwd, err := os.Getwd()
		if err != nil {
			slog.Error("getting working directory", "err", err.Error())
			os.Exit(1)
		}
		s.ScanDir = cwd
	}
	added, err := s.Store.ScanProjects(s.ScanDir)
	if err != nil {
		slog.Error("scanning projects", "err", err.Error(), "dir", s.ScanDir)
		os.Exit(1)
	}
	for _, p := range added {
		slog.Info("registered project", "id", p.ID, "name", p.Name, "path", p.Path)
	}

	apiToken := strings.TrimSpace(os.Getenv("GAIA_TOKEN"))
	if s.Worker == nil {
		baseURL := strings.TrimSpace(os.Getenv("GAIA_URL"))
		if baseURL == "" {
			baseURL = "http://localhost:4000"
		}
		s.Worker = NewWorker(baseURL, apiToken, "", true, 10*time.Second, 15*time.Minute)
	}
	go func() {
		time.Sleep(5 * time.Second)
		s.Worker.Run(ctx)
	}()

	sseHandler := newSSEServer()
	go s.bridgeWorkerToSSE(ctx, sseHandler)

	all, err := s.Store.ListProjects()
	if err != nil {
		slog.Error("listing projects", "err", err.Error())
		os.Exit(1)
	}
	for _, p := range all {
		s.Worker.Input <- WorkerEvent{Kind: WorkerEventProjectAdded, Project: p}
	}

	srv := zen.NewHttpServer(&zen.Options{
		AllowedHosts: config.AllowedHosts(),
		CorsOrigins:  config.AllowedOrigins(),
		SSL:          config.SSL,
	})
	srv.Embeds("/static", embeds)

	// API group: token-protected, machine-readable. Mounted only when
	// GAIA_TOKEN is set so a misconfigured server doesn't expose an
	// open API by accident.
	if token := strings.TrimSpace(os.Getenv("GAIA_TOKEN")); token != "" {
		srv.Group("/api/projects/{projectID}", func(r *zen.Router) {
			r.Use(MiddlewareBearerToken(token))
			r.APIResource("/tasks", &taskAPIResource{store: s.Store})
			r.Post("/tasks/{taskID}/move", apiMoveTask(s.Store))
			r.Post("/tasks/{taskID}/comments", apiCommentTask(s.Store))
		})
	} else {
		slog.Warn("GAIA_TOKEN not set — /api endpoints disabled")
	}

	// Optional HTTP basic auth on the UI group. Only enabled when both
	// AUTH_USER and AUTH_PASS are set; otherwise the UI is open (handy
	// when running locally on a single-user machine).
	authUser := strings.TrimSpace(os.Getenv("AUTH_USER"))
	authPass := os.Getenv("AUTH_PASS")
	srv.Group("/", func(r *zen.Router) {
		if authUser != "" && authPass != "" {
			r.Use(zen.MiddlewareBasicAuth(authUser, authPass, "Gaia"))
		}
		r.Get("/", func(w http.ResponseWriter, r *http.Request) {
			projects, err := s.Store.ListProjects()
			if err != nil {
				zen.HttpInternalServerError(w, err.Error())
				return
			}
			if len(projects) == 0 {
				http.Redirect(w, r, "/projects/new", http.StatusSeeOther)
				return
			}
			http.Redirect(w, r, "/projects/"+formatProjectID(projects[0].ID), http.StatusSeeOther)
		})

		r.Get("/projects/new", func(w http.ResponseWriter, r *http.Request) {
			projects, err := s.Store.ListProjects()
			if err != nil {
				zen.HttpInternalServerError(w, err.Error())
				return
			}
			data := ui.ProjectNewPageData{Projects: toUIProjects(projects)}
			ui.Layout(ui.LayoutPage{}, ui.ProjectNewPage(data)).Render(r.Context(), w)
		})

		r.Post("/projects", func(w http.ResponseWriter, r *http.Request) {
			if err := r.ParseForm(); err != nil {
				zen.HttpBadRequest(w, err, "invalid form")
				return
			}
			url := strings.TrimSpace(r.FormValue("url"))
			project, err := s.cloneProject(r.Context(), url)
			if err != nil {
				projects, listErr := s.Store.ListProjects()
				if listErr != nil {
					zen.HttpInternalServerError(w, listErr.Error())
					return
				}
				data := ui.ProjectNewPageData{
					Projects: toUIProjects(projects),
					URL:      url,
					Error:    err.Error(),
				}
				w.WriteHeader(http.StatusUnprocessableEntity)
				ui.Layout(ui.LayoutPage{}, ui.ProjectNewPage(data)).Render(r.Context(), w)
				return
			}
			http.Redirect(w, r, "/projects/"+formatProjectID(project.ID), http.StatusSeeOther)
		})

		r.Get("/projects/{id}/events", func(w http.ResponseWriter, r *http.Request) {
			sseHandler.ServeHTTP(w, r)
		})

		r.Get("/projects/{id}", func(w http.ResponseWriter, r *http.Request) {
			project, ok := s.resolveProject(w, r, "id")
			if !ok {
				return
			}
			data, err := s.boardData(project)
			if err != nil {
				zen.HttpInternalServerError(w, err.Error())
				return
			}
			ui.Layout(ui.LayoutPage{}, ui.ProjectPage(data)).Render(r.Context(), w)
		})

		r.Get("/projects/{id}/tasks/new", func(w http.ResponseWriter, r *http.Request) {
			project, ok := s.resolveProject(w, r, "id")
			if !ok {
				return
			}
			status := pm.Status(r.URL.Query().Get("status"))
			if status == "" {
				status = pm.StatusTodo
			}
			data := ui.TaskNewPageData{
				ProjectID:   pm.ProjectID(formatProjectID(project.ID)),
				ProjectName: project.Name,
				Status:      status,
			}
			ui.Layout(ui.LayoutPage{}, ui.TaskNewPage(data)).Render(r.Context(), w)
		})

		r.Get("/projects/{id}/tasks/{taskID}/edit", func(w http.ResponseWriter, r *http.Request) {
			project, ok := s.resolveProject(w, r, "id")
			if !ok {
				return
			}
			taskID, ok := parseTaskIDParam(r, "taskID")
			if !ok {
				zen.HttpNotFound(w)
				return
			}
			task, err := s.Store.GetTask(project.ID, taskID)
			if err != nil {
				if errors.Is(err, core.ErrTaskNotFound) {
					zen.HttpNotFound(w)
					return
				}
				zen.HttpInternalServerError(w, err.Error())
				return
			}
			pmTask := toPMTask(task)
			if ui.IsReadOnlyStatus(pmTask.Status) {
				http.Redirect(w, r, "/projects/"+formatProjectID(project.ID), http.StatusSeeOther)
				return
			}
			data := ui.TaskEditPageData{
				ProjectID:   pm.ProjectID(formatProjectID(project.ID)),
				ProjectName: project.Name,
				TaskID:      pmTask.ID,
				Status:      pmTask.Status,
				Name:        pmTask.Name,
				Body:        pmTask.Body,
				Comments:    pmTask.Comments,
			}
			ui.Layout(ui.LayoutPage{}, ui.TaskEditPage(data)).Render(r.Context(), w)
		})

		r.Post("/projects/{id}/tasks/{taskID}/comments", func(w http.ResponseWriter, r *http.Request) {
			project, ok := s.resolveProject(w, r, "id")
			if !ok {
				return
			}
			taskID, ok := parseTaskIDParam(r, "taskID")
			if !ok {
				zen.HttpNotFound(w)
				return
			}
			if err := r.ParseForm(); err != nil {
				zen.HttpBadRequest(w, err, "invalid form")
				return
			}
			task, err := s.Store.GetTask(project.ID, taskID)
			if err != nil {
				if errors.Is(err, core.ErrTaskNotFound) {
					zen.HttpNotFound(w)
					return
				}
				zen.HttpInternalServerError(w, err.Error())
				return
			}
			pmTask := toPMTask(task)
			if ui.IsReadOnlyStatus(pmTask.Status) {
				zen.HttpForbidden(w)
				return
			}
			body := r.FormValue("body")
			if _, err := s.Store.AddComment(project.ID, taskID, body); err != nil {
				data := ui.TaskEditPageData{
					ProjectID:    pm.ProjectID(formatProjectID(project.ID)),
					ProjectName:  project.Name,
					TaskID:       pmTask.ID,
					Status:       pmTask.Status,
					Name:         pmTask.Name,
					Body:         pmTask.Body,
					Comments:     pmTask.Comments,
					CommentDraft: body,
					CommentError: err.Error(),
				}
				w.WriteHeader(http.StatusUnprocessableEntity)
				ui.Layout(ui.LayoutPage{}, ui.TaskEditPage(data)).Render(r.Context(), w)
				return
			}
			http.Redirect(w, r, "/projects/"+formatProjectID(project.ID)+"/tasks/"+formatTaskID(taskID)+"/edit", http.StatusSeeOther)
		})

		r.Post("/projects/{id}/tasks/{taskID}", func(w http.ResponseWriter, r *http.Request) {
			project, ok := s.resolveProject(w, r, "id")
			if !ok {
				return
			}
			taskID, ok := parseTaskIDParam(r, "taskID")
			if !ok {
				zen.HttpNotFound(w)
				return
			}
			if err := r.ParseForm(); err != nil {
				zen.HttpBadRequest(w, err, "invalid form")
				return
			}
			task, err := s.Store.GetTask(project.ID, taskID)
			if err != nil {
				if errors.Is(err, core.ErrTaskNotFound) {
					zen.HttpNotFound(w)
					return
				}
				zen.HttpInternalServerError(w, err.Error())
				return
			}
			if ui.IsReadOnlyStatus(pm.Status(task.Status)) {
				zen.HttpForbidden(w)
				return
			}
			name := r.FormValue("name")
			body := r.FormValue("body")
			// status field is informational only — only the move endpoint
			// can change a task's column. We always rewrite to the task's
			// current status to prevent the form from sneaking a status
			// change past the doing/review/rejected guards.
			status := task.Status

			if _, err := s.Store.UpdateTask(project.ID, taskID, name, body, status, task.Tags); err != nil {
				data := ui.TaskEditPageData{
					ProjectID:   pm.ProjectID(formatProjectID(project.ID)),
					ProjectName: project.Name,
					TaskID:      pm.TaskID(formatTaskID(taskID)),
					Status:      pm.Status(status),
					Name:        name,
					Body:        body,
					Comments:    task.Comments,
					Error:       err.Error(),
				}
				w.WriteHeader(http.StatusUnprocessableEntity)
				ui.Layout(ui.LayoutPage{}, ui.TaskEditPage(data)).Render(r.Context(), w)
				return
			}
			http.Redirect(w, r, "/projects/"+formatProjectID(project.ID), http.StatusSeeOther)
		})

		r.Post("/projects/{id}/tasks/{taskID}/delete", func(w http.ResponseWriter, r *http.Request) {
			project, ok := s.resolveProject(w, r, "id")
			if !ok {
				return
			}
			taskID, ok := parseTaskIDParam(r, "taskID")
			if !ok {
				zen.HttpNotFound(w)
				return
			}
			task, err := s.Store.GetTask(project.ID, taskID)
			if err != nil {
				if errors.Is(err, core.ErrTaskNotFound) {
					zen.HttpNotFound(w)
					return
				}
				zen.HttpInternalServerError(w, err.Error())
				return
			}
			if !ui.CanDeleteStatus(pm.Status(task.Status)) {
				zen.HttpForbidden(w)
				return
			}
			if err := s.Store.DeleteTask(project.ID, taskID); err != nil {
				if errors.Is(err, core.ErrTaskNotFound) {
					zen.HttpNotFound(w)
					return
				}
				zen.HttpInternalServerError(w, err.Error())
				return
			}
			http.Redirect(w, r, "/projects/"+formatProjectID(project.ID), http.StatusSeeOther)
		})

		r.Post("/projects/{id}/tasks/{taskID}/move", func(w http.ResponseWriter, r *http.Request) {
			project, ok := s.resolveProject(w, r, "id")
			if !ok {
				return
			}
			taskID, ok := parseTaskIDParam(r, "taskID")
			if !ok {
				zen.HttpNotFound(w)
				return
			}
			if err := r.ParseForm(); err != nil {
				zen.HttpBadRequest(w, err, "invalid form")
				return
			}
			target := core.Status(r.FormValue("status"))
			task, err := s.Store.GetTask(project.ID, taskID)
			if err != nil {
				if errors.Is(err, core.ErrTaskNotFound) {
					zen.HttpNotFound(w)
					return
				}
				zen.HttpInternalServerError(w, err.Error())
				return
			}
			// The doing column is owned by the agent: humans can never move
			// tasks into it or out of it from the web UI.
			if task.Status == core.StatusDoing || target == core.StatusDoing {
				zen.HttpForbidden(w)
				return
			}
			if _, err := s.Store.MoveTask(project.ID, taskID, target); err != nil {
				if errors.Is(err, core.ErrInvalidStatus) {
					zen.HttpBadRequest(w, err, "invalid status")
					return
				}
				if errors.Is(err, core.ErrTaskNotFound) {
					zen.HttpNotFound(w)
					return
				}
				zen.HttpInternalServerError(w, err.Error())
				return
			}
			http.Redirect(w, r, "/projects/"+formatProjectID(project.ID), http.StatusSeeOther)
		})

		r.Post("/projects/{id}/columns/{status}/order", func(w http.ResponseWriter, r *http.Request) {
			project, ok := s.resolveProject(w, r, "id")
			if !ok {
				return
			}
			status := core.Status(zen.Param(r, "status"))
			if status == core.StatusDoing {
				zen.HttpForbidden(w)
				return
			}
			if err := r.ParseForm(); err != nil {
				zen.HttpBadRequest(w, err, "invalid form")
				return
			}
			raw := r.FormValue("ids")
			ids, err := parseTaskIDs(raw)
			if err != nil {
				zen.HttpBadRequest(w, err, err.Error())
				return
			}
			if err := s.Store.ReorderColumn(project.ID, status, ids); err != nil {
				if errors.Is(err, core.ErrProjectNotFound) {
					zen.HttpNotFound(w)
					return
				}
				if errors.Is(err, core.ErrInvalidStatus) {
					zen.HttpBadRequest(w, err, "invalid status")
					return
				}
				zen.HttpBadRequest(w, err, err.Error())
				return
			}
			w.WriteHeader(http.StatusNoContent)
		})

		r.Post("/projects/{id}/tasks", func(w http.ResponseWriter, r *http.Request) {
			project, ok := s.resolveProject(w, r, "id")
			if !ok {
				return
			}
			if err := r.ParseForm(); err != nil {
				zen.HttpBadRequest(w, err, "invalid form")
				return
			}
			name := r.FormValue("name")
			body := r.FormValue("body")
			status := core.Status(r.FormValue("status"))
			if status == core.StatusDoing {
				zen.HttpForbidden(w)
				return
			}

			if _, err := s.Store.CreateTask(project.ID, status, name, body, nil); err != nil {
				data := ui.TaskNewPageData{
					ProjectID:   pm.ProjectID(formatProjectID(project.ID)),
					ProjectName: project.Name,
					Status:      pm.Status(status),
					Name:        name,
					Body:        body,
					Error:       err.Error(),
				}
				w.WriteHeader(http.StatusUnprocessableEntity)
				ui.Layout(ui.LayoutPage{}, ui.TaskNewPage(data)).Render(r.Context(), w)
				return
			}
			http.Redirect(w, r, "/projects/"+formatProjectID(project.ID), http.StatusSeeOther)
		})
	})

	s.Envs["ADDR"] = ":4000"
	srv.Run(ctx, s.Envs)
}

func (s *Server) boardData(project core.Project) (ui.ProjectPageData, error) {
	projects, err := s.Store.ListProjects()
	if err != nil {
		return ui.ProjectPageData{}, err
	}
	tasks, err := s.Store.ListTasksByProject(project.ID)
	if err != nil {
		return ui.ProjectPageData{}, err
	}
	pmTasks := make([]pm.Task, 0, len(tasks))
	for _, t := range tasks {
		pmTasks = append(pmTasks, toPMTask(t))
	}
	active := pm.ProjectID(formatProjectID(project.ID))
	return ui.ProjectPageData{
		Projects: toUIProjects(projects),
		Active:   active,
		Columns:  ui.BuildColumns(active, pmTasks),
	}, nil
}

func toUIProjects(in []core.Project) []ui.Project {
	out := make([]ui.Project, 0, len(in))
	for _, p := range in {
		out = append(out, ui.Project{
			ID:   pm.ProjectID(formatProjectID(p.ID)),
			Name: p.Name,
			Icon: iconFromName(p.Name),
		})
	}
	return out
}

func toPMTask(t core.Task) pm.Task {
	return pm.Task{
		ID:        pm.TaskID(formatTaskID(t.ID)),
		ProjectID: pm.ProjectID(formatProjectID(t.ProjectID)),
		Name:      t.Title,
		Body:      t.Description,
		Status:    pm.Status(t.Status),
		Tags:      t.Tags,
		Comments:  t.Comments,
	}
}

func iconFromName(name string) string {
	for _, r := range name {
		return strings.ToUpper(string(r))
	}
	return "?"
}

// newSSEServer builds an SSE server that lets a client subscribe to a single
// topic (the project ID) via the `id` path parameter. Clients only see logs
// for the project they are viewing.
func newSSEServer() *sse.Server {
	return &sse.Server{
		OnSession: func(w http.ResponseWriter, r *http.Request) ([]string, bool) {
			id := strings.TrimSpace(zen.Param(r, "id"))
			if id == "" {
				w.WriteHeader(http.StatusBadRequest)
				return nil, false
			}
			return []string{id}, true
		},
	}
}

// bridgeWorkerToSSE forwards every WorkerLog into the SSE server, publishing
// to a topic named after the project ID so subscribers receive only their
// project's stream.
func (s *Server) bridgeWorkerToSSE(ctx context.Context, h *sse.Server) {
	for {
		select {
		case <-ctx.Done():
			return
		case log := <-s.Worker.Output:
			msg := &sse.Message{Type: sse.Type(log.Stream)}
			msg.AppendData(log.Text)
			_ = h.Publish(msg, formatProjectID(log.ProjectID))
		}
	}
}

// cloneProject runs `git clone` into ScanDir and registers the resulting
// folder as a project. Returns the registered project so the caller can
// redirect to it.
func (s *Server) cloneProject(ctx context.Context, url string) (core.Project, error) {
	if url == "" {
		return core.Project{}, fmt.Errorf("git url is required")
	}
	if strings.HasPrefix(url, "-") {
		return core.Project{}, fmt.Errorf("invalid git url")
	}
	dirName := deriveCloneDir(url)
	if dirName == "" {
		return core.Project{}, fmt.Errorf("could not derive a directory name from url")
	}
	dest := filepath.Join(s.ScanDir, dirName)
	if _, err := os.Stat(dest); err == nil {
		return core.Project{}, fmt.Errorf("directory %s already exists", dirName)
	} else if !errors.Is(err, os.ErrNotExist) {
		return core.Project{}, fmt.Errorf("checking destination: %w", err)
	}

	cmd := exec.CommandContext(ctx, "git", "clone", "--", url, dirName)
	cmd.Dir = s.ScanDir
	out, err := cmd.CombinedOutput()
	if err != nil {
		msg := strings.TrimSpace(string(out))
		if msg == "" {
			msg = err.Error()
		}
		return core.Project{}, fmt.Errorf("git clone failed: %s", msg)
	}

	added, err := s.Store.ScanProjects(s.ScanDir)
	if err != nil {
		return core.Project{}, fmt.Errorf("scanning projects: %w", err)
	}
	for _, p := range added {
		if p.Path == dest {
			if s.Worker != nil {
				s.Worker.Input <- WorkerEvent{Kind: WorkerEventProjectAdded, Project: p}
			}
			return p, nil
		}
	}
	for _, p := range existingByPath(s.Store, dest) {
		return p, nil
	}
	return core.Project{}, fmt.Errorf("clone succeeded but project not found")
}

func existingByPath(store *core.Store, path string) []core.Project {
	all, err := store.ListProjects()
	if err != nil {
		return nil
	}
	out := make([]core.Project, 0, 1)
	for _, p := range all {
		if p.Path == path {
			out = append(out, p)
		}
	}
	return out
}

// deriveCloneDir picks the directory name `git clone` would default to:
// the segment after the final '/' or ':', with a trailing ".git" stripped.
func deriveCloneDir(url string) string {
	s := strings.TrimSpace(url)
	s = strings.TrimRight(s, "/")
	if i := strings.LastIndexAny(s, "/:"); i >= 0 {
		s = s[i+1:]
	}
	s = strings.TrimSuffix(s, ".git")
	return s
}

// parseTaskIDParam reads {name} from the request and parses it as a positive
// int64 task id. Returns ok=false when the segment isn't a valid id — the
// caller is expected to respond 404.
func parseTaskIDParam(r *http.Request, name string) (core.TaskID, bool) {
	v, err := strconv.ParseInt(zen.Param(r, name), 10, 64)
	if err != nil || v <= 0 {
		return 0, false
	}
	return core.TaskID(v), true
}

// formatTaskID renders an internal task id for URL paths, JSON bodies, and
// the pm.TaskID boundary type (which is a string).
func formatTaskID(id core.TaskID) string {
	return strconv.FormatInt(int64(id), 10)
}

// formatProjectID renders an internal project id for URL paths and the
// pm.ProjectID boundary type.
func formatProjectID(id core.ProjectID) string {
	return strconv.FormatInt(int64(id), 10)
}

// parseProjectIDParam reads {name} from the request and parses it as a
// positive int64 project id. Returns ok=false when the segment isn't a
// valid id — the caller is expected to respond 404.
func parseProjectIDParam(r *http.Request, name string) (core.ProjectID, bool) {
	v, err := strconv.ParseInt(zen.Param(r, name), 10, 64)
	if err != nil || v <= 0 {
		return 0, false
	}
	return core.ProjectID(v), true
}

// resolveProject reads {name} from the URL as an integer project id and
// loads the project. Writes 404 (and returns ok=false) on parse failure or
// missing project; writes 500 on other DB errors.
func (s *Server) resolveProject(w http.ResponseWriter, r *http.Request, name string) (core.Project, bool) {
	v, err := strconv.ParseInt(zen.Param(r, name), 10, 64)
	if err != nil || v <= 0 {
		zen.HttpNotFound(w)
		return core.Project{}, false
	}
	project, err := s.Store.GetProject(core.ProjectID(v))
	if err != nil {
		if errors.Is(err, core.ErrProjectNotFound) {
			zen.HttpNotFound(w)
		} else {
			zen.HttpInternalServerError(w, err.Error())
		}
		return core.Project{}, false
	}
	return project, true
}

// parseTaskIDs splits a comma-separated list of task ids. Empty input yields
// an empty slice. Returns an error if any entry is not a positive int64.
func parseTaskIDs(raw string) ([]core.TaskID, error) {
	parts := strings.Split(raw, ",")
	out := make([]core.TaskID, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		v, err := strconv.ParseInt(p, 10, 64)
		if err != nil || v <= 0 {
			return nil, fmt.Errorf("invalid task id %q", p)
		}
		out = append(out, core.TaskID(v))
	}
	return out, nil
}
