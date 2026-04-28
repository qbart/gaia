package web

import (
	"context"
	"embed"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"strings"

	"github.com/SoftKiwiGames/zen/zen"
	"github.com/qbart/gaia/config"
	"github.com/qbart/gaia/pm"
	"github.com/qbart/gaia/web/core"
	"github.com/qbart/gaia/web/ui"
)

//go:embed static
var static embed.FS

type Server struct {
	Envs  zen.Envs
	Store *core.Store
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
		s.Store = core.NewStore(root)
	}

	srv := zen.NewHttpServer(&zen.Options{
		AllowedHosts: config.AllowedHosts(),
		CorsOrigins:  config.AllowedOrigins(),
		SSL:          config.SSL,
	})
	srv.Embeds("/static", embeds)
	srv.Group("/", func(r *zen.Router) {
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
			http.Redirect(w, r, "/projects/"+string(projects[0].ID), http.StatusSeeOther)
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
			name := r.FormValue("name")
			project, err := s.Store.CreateProject(name)
			if err != nil {
				projects, listErr := s.Store.ListProjects()
				if listErr != nil {
					zen.HttpInternalServerError(w, listErr.Error())
					return
				}
				data := ui.ProjectNewPageData{
					Projects: toUIProjects(projects),
					Name:     name,
					Error:    err.Error(),
				}
				w.WriteHeader(http.StatusUnprocessableEntity)
				ui.Layout(ui.LayoutPage{}, ui.ProjectNewPage(data)).Render(r.Context(), w)
				return
			}
			http.Redirect(w, r, "/projects/"+string(project.ID), http.StatusSeeOther)
		})

		r.Get("/projects/{id}", func(w http.ResponseWriter, r *http.Request) {
			id := core.ProjectID(zen.Param(r, "id"))
			data, err := s.boardData(id)
			if err != nil {
				if errors.Is(err, core.ErrProjectNotFound) {
					zen.HttpNotFound(w)
					return
				}
				zen.HttpInternalServerError(w, err.Error())
				return
			}
			ui.Layout(ui.LayoutPage{}, ui.ProjectPage(data)).Render(r.Context(), w)
		})

		r.Get("/projects/{id}/tasks/new", func(w http.ResponseWriter, r *http.Request) {
			id := core.ProjectID(zen.Param(r, "id"))
			project, err := s.Store.GetProject(id)
			if err != nil {
				if errors.Is(err, core.ErrProjectNotFound) {
					zen.HttpNotFound(w)
					return
				}
				zen.HttpInternalServerError(w, err.Error())
				return
			}
			status := pm.Status(r.URL.Query().Get("status"))
			if status == "" {
				status = pm.StatusTodo
			}
			data := ui.TaskNewPageData{
				ProjectID:   pm.ProjectID(project.ID),
				ProjectName: project.Name,
				Status:      status,
			}
			ui.Layout(ui.LayoutPage{}, ui.TaskNewPage(data)).Render(r.Context(), w)
		})

		r.Get("/projects/{id}/tasks/{taskID}/edit", func(w http.ResponseWriter, r *http.Request) {
			id := core.ProjectID(zen.Param(r, "id"))
			taskID := core.TaskID(zen.Param(r, "taskID"))
			project, err := s.Store.GetProject(id)
			if err != nil {
				if errors.Is(err, core.ErrProjectNotFound) {
					zen.HttpNotFound(w)
					return
				}
				zen.HttpInternalServerError(w, err.Error())
				return
			}
			task, err := s.Store.GetTask(id, taskID)
			if err != nil {
				if errors.Is(err, core.ErrTaskNotFound) || errors.Is(err, core.ErrProjectNotFound) {
					zen.HttpNotFound(w)
					return
				}
				zen.HttpInternalServerError(w, err.Error())
				return
			}
			pmTask := toPMTask(task)
			if ui.IsReadOnlyStatus(pmTask.Status) {
				http.Redirect(w, r, "/projects/"+string(id), http.StatusSeeOther)
				return
			}
			data := ui.TaskEditPageData{
				ProjectID:   pm.ProjectID(project.ID),
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
			id := core.ProjectID(zen.Param(r, "id"))
			taskID := core.TaskID(zen.Param(r, "taskID"))
			if err := r.ParseForm(); err != nil {
				zen.HttpBadRequest(w, err, "invalid form")
				return
			}
			task, err := s.Store.GetTask(id, taskID)
			if err != nil {
				if errors.Is(err, core.ErrTaskNotFound) || errors.Is(err, core.ErrProjectNotFound) {
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
			if _, err := s.Store.AddComment(id, taskID, body); err != nil {
				project, berr := s.Store.GetProject(id)
				if berr != nil {
					if errors.Is(berr, core.ErrProjectNotFound) {
						zen.HttpNotFound(w)
						return
					}
					zen.HttpInternalServerError(w, berr.Error())
					return
				}
				data := ui.TaskEditPageData{
					ProjectID:    pm.ProjectID(project.ID),
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
			http.Redirect(w, r, "/projects/"+string(id)+"/tasks/"+string(taskID)+"/edit", http.StatusSeeOther)
		})

		r.Post("/projects/{id}/tasks/{taskID}", func(w http.ResponseWriter, r *http.Request) {
			id := core.ProjectID(zen.Param(r, "id"))
			taskID := core.TaskID(zen.Param(r, "taskID"))
			if err := r.ParseForm(); err != nil {
				zen.HttpBadRequest(w, err, "invalid form")
				return
			}
			task, err := s.Store.GetTask(id, taskID)
			if err != nil {
				if errors.Is(err, core.ErrTaskNotFound) || errors.Is(err, core.ErrProjectNotFound) {
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

			if _, err := s.Store.UpdateTask(id, taskID, name, body, status, task.Tags); err != nil {
				project, berr := s.Store.GetProject(id)
				if berr != nil {
					if errors.Is(berr, core.ErrProjectNotFound) {
						zen.HttpNotFound(w)
						return
					}
					zen.HttpInternalServerError(w, berr.Error())
					return
				}
				data := ui.TaskEditPageData{
					ProjectID:   pm.ProjectID(project.ID),
					ProjectName: project.Name,
					TaskID:      pm.TaskID(taskID),
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
			http.Redirect(w, r, "/projects/"+string(id), http.StatusSeeOther)
		})

		r.Post("/projects/{id}/tasks/{taskID}/delete", func(w http.ResponseWriter, r *http.Request) {
			id := core.ProjectID(zen.Param(r, "id"))
			taskID := core.TaskID(zen.Param(r, "taskID"))
			task, err := s.Store.GetTask(id, taskID)
			if err != nil {
				if errors.Is(err, core.ErrTaskNotFound) || errors.Is(err, core.ErrProjectNotFound) {
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
			if err := s.Store.DeleteTask(id, taskID); err != nil {
				if errors.Is(err, core.ErrTaskNotFound) {
					zen.HttpNotFound(w)
					return
				}
				zen.HttpInternalServerError(w, err.Error())
				return
			}
			http.Redirect(w, r, "/projects/"+string(id), http.StatusSeeOther)
		})

		r.Post("/projects/{id}/tasks/{taskID}/move", func(w http.ResponseWriter, r *http.Request) {
			id := core.ProjectID(zen.Param(r, "id"))
			taskID := core.TaskID(zen.Param(r, "taskID"))
			if err := r.ParseForm(); err != nil {
				zen.HttpBadRequest(w, err, "invalid form")
				return
			}
			target := core.Status(r.FormValue("status"))
			task, err := s.Store.GetTask(id, taskID)
			if err != nil {
				if errors.Is(err, core.ErrTaskNotFound) || errors.Is(err, core.ErrProjectNotFound) {
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
			if _, err := s.Store.MoveTask(id, taskID, target); err != nil {
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
			http.Redirect(w, r, "/projects/"+string(id), http.StatusSeeOther)
		})

		r.Post("/projects/{id}/columns/{status}/order", func(w http.ResponseWriter, r *http.Request) {
			id := core.ProjectID(zen.Param(r, "id"))
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
			ids := parseTaskIDs(raw)
			if err := s.Store.ReorderColumn(id, status, ids); err != nil {
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
			id := core.ProjectID(zen.Param(r, "id"))
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

			if _, err := s.Store.CreateTask(id, status, name, body, nil); err != nil {
				if errors.Is(err, core.ErrProjectNotFound) {
					zen.HttpNotFound(w)
					return
				}
				project, berr := s.Store.GetProject(id)
				if berr != nil {
					if errors.Is(berr, core.ErrProjectNotFound) {
						zen.HttpNotFound(w)
						return
					}
					zen.HttpInternalServerError(w, berr.Error())
					return
				}
				data := ui.TaskNewPageData{
					ProjectID:   pm.ProjectID(project.ID),
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
			http.Redirect(w, r, "/projects/"+string(id), http.StatusSeeOther)
		})
	})

	s.Envs["ADDR"] = ":4000"
	srv.Run(ctx, s.Envs)
}

func (s *Server) boardData(id core.ProjectID) (ui.ProjectPageData, error) {
	if _, err := s.Store.GetProject(id); err != nil {
		return ui.ProjectPageData{}, err
	}
	projects, err := s.Store.ListProjects()
	if err != nil {
		return ui.ProjectPageData{}, err
	}
	tasks, err := s.Store.ListTasksByProject(id)
	if err != nil {
		return ui.ProjectPageData{}, err
	}
	pmTasks := make([]pm.Task, 0, len(tasks))
	for _, t := range tasks {
		pmTasks = append(pmTasks, toPMTask(t))
	}
	return ui.ProjectPageData{
		Projects: toUIProjects(projects),
		Active:   pm.ProjectID(id),
		Columns:  ui.BuildColumns(pm.ProjectID(id), pmTasks),
	}, nil
}

func toUIProjects(in []core.Project) []ui.Project {
	out := make([]ui.Project, 0, len(in))
	for _, p := range in {
		out = append(out, ui.Project{
			ID:   pm.ProjectID(p.ID),
			Name: p.Name,
			Icon: iconFromName(p.Name),
		})
	}
	return out
}

func toPMTask(t core.Task) pm.Task {
	return pm.Task{
		ID:        pm.TaskID(t.ID),
		ProjectID: pm.ProjectID(t.ProjectID),
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

// parseTaskIDs splits a comma-separated list of task ids, trimming whitespace
// and dropping empty entries. Empty input yields an empty slice (not nil).
func parseTaskIDs(raw string) []core.TaskID {
	parts := strings.Split(raw, ",")
	out := make([]core.TaskID, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		out = append(out, core.TaskID(p))
	}
	return out
}
