package web

import (
	"context"
	"embed"
	"errors"
	"log/slog"
	"net/http"
	"os"

	"github.com/SoftKiwiGames/zen/zen"
	"github.com/qbart/gaia/config"
	"github.com/qbart/gaia/pm"
	"github.com/qbart/gaia/web/ui"
)

var errProjectNotFound = errors.New("project not found")

//go:embed static
var static embed.FS

type Server struct {
	Envs     zen.Envs
	Projects pm.ProjectRepository
	Tasks    pm.TaskRepository
}

func (s *Server) Run(ctx context.Context) {
	embeds, err := zen.NewEmbeds(static, "static", zen.ReactPreset)
	if err != nil {
		slog.Error("embedding failed", "err", err.Error())
		os.Exit(1)
	}

	seed := false
	if s.Projects == nil {
		s.Projects = pm.NewInMemoryProjectRepository()
		seed = true
	}
	if s.Tasks == nil {
		s.Tasks = pm.NewInMemoryTaskRepository()
	}
	if seed {
		if err := pm.SeedDemo(ctx, s.Projects, s.Tasks); err != nil {
			slog.Error("seeding demo data failed", "err", err.Error())
		}
	}

	srv := zen.NewHttpServer(&zen.Options{
		AllowedHosts: config.AllowedHosts(),
		CorsOrigins:  config.AllowedOrigins(),
		SSL:          config.SSL,
	})
	srv.Embeds("/static", embeds)
	srv.Group("/", func(r *zen.Router) {
		r.Get("/", func(w http.ResponseWriter, r *http.Request) {
			list, err := s.Projects.List(r.Context(), pm.ListProjectsInput{})
			if err != nil {
				zen.HttpInternalServerError(w, err.Error())
				return
			}
			if len(list.Projects) == 0 {
				http.Redirect(w, r, "/projects/new", http.StatusSeeOther)
				return
			}
			http.Redirect(w, r, "/projects/"+string(list.Projects[0].ID), http.StatusSeeOther)
		})

		r.Get("/projects/new", func(w http.ResponseWriter, r *http.Request) {
			list, err := s.Projects.List(r.Context(), pm.ListProjectsInput{})
			if err != nil {
				zen.HttpInternalServerError(w, err.Error())
				return
			}
			data := ui.ProjectNewPageData{Projects: toUIProjects(list.Projects)}
			ui.Layout(ui.LayoutPage{}, ui.ProjectNewPage(data)).Render(r.Context(), w)
		})

		r.Post("/projects", func(w http.ResponseWriter, r *http.Request) {
			if err := r.ParseForm(); err != nil {
				zen.HttpBadRequest(w, err, "invalid form")
				return
			}
			name := r.FormValue("name")
			out, err := s.Projects.Create(r.Context(), pm.CreateProjectInput{Name: name})
			if err != nil {
				list, listErr := s.Projects.List(r.Context(), pm.ListProjectsInput{})
				if listErr != nil {
					zen.HttpInternalServerError(w, listErr.Error())
					return
				}
				data := ui.ProjectNewPageData{
					Projects: toUIProjects(list.Projects),
					Name:     name,
					Error:    err.Error(),
				}
				w.WriteHeader(http.StatusUnprocessableEntity)
				ui.Layout(ui.LayoutPage{}, ui.ProjectNewPage(data)).Render(r.Context(), w)
				return
			}
			http.Redirect(w, r, "/projects/"+string(out.Project.ID), http.StatusSeeOther)
		})

		r.Get("/projects/{id}", func(w http.ResponseWriter, r *http.Request) {
			id := pm.ProjectID(zen.Param(r, "id"))
			data, err := s.boardData(r.Context(), id)
			if err != nil {
				if err == errProjectNotFound {
					zen.HttpNotFound(w)
					return
				}
				zen.HttpInternalServerError(w, err.Error())
				return
			}
			ui.Layout(ui.LayoutPage{}, ui.ProjectPage(data)).Render(r.Context(), w)
		})

		r.Get("/projects/{id}/tasks/new", func(w http.ResponseWriter, r *http.Request) {
			id := pm.ProjectID(zen.Param(r, "id"))
			board, err := s.boardData(r.Context(), id)
			if err != nil {
				if err == errProjectNotFound {
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
			data := ui.TaskNewPageData{Board: board, Status: status}
			ui.Layout(ui.LayoutPage{}, ui.TaskNewPage(data)).Render(r.Context(), w)
		})

		r.Get("/projects/{id}/tasks/{taskID}/edit", func(w http.ResponseWriter, r *http.Request) {
			id := pm.ProjectID(zen.Param(r, "id"))
			taskID := pm.TaskID(zen.Param(r, "taskID"))
			board, err := s.boardData(r.Context(), id)
			if err != nil {
				if err == errProjectNotFound {
					zen.HttpNotFound(w)
					return
				}
				zen.HttpInternalServerError(w, err.Error())
				return
			}
			get, err := s.Tasks.Get(r.Context(), pm.GetTaskInput{ID: taskID})
			if err != nil {
				zen.HttpInternalServerError(w, err.Error())
				return
			}
			if !get.Found || get.Task.ProjectID != id {
				zen.HttpNotFound(w)
				return
			}
			if ui.IsReadOnlyStatus(get.Task.Status) {
				http.Redirect(w, r, "/projects/"+string(id), http.StatusSeeOther)
				return
			}
			data := ui.TaskEditPageData{
				Board:    board,
				TaskID:   get.Task.ID,
				Status:   get.Task.Status,
				Name:     get.Task.Name,
				Body:     get.Task.Body,
				Comments: get.Task.Comments,
			}
			ui.Layout(ui.LayoutPage{}, ui.TaskEditPage(data)).Render(r.Context(), w)
		})

		r.Post("/projects/{id}/tasks/{taskID}/comments", func(w http.ResponseWriter, r *http.Request) {
			id := pm.ProjectID(zen.Param(r, "id"))
			taskID := pm.TaskID(zen.Param(r, "taskID"))
			if err := r.ParseForm(); err != nil {
				zen.HttpBadRequest(w, err, "invalid form")
				return
			}
			get, err := s.Tasks.Get(r.Context(), pm.GetTaskInput{ID: taskID})
			if err != nil {
				zen.HttpInternalServerError(w, err.Error())
				return
			}
			if !get.Found || get.Task.ProjectID != id {
				zen.HttpNotFound(w)
				return
			}
			if ui.IsReadOnlyStatus(get.Task.Status) {
				zen.HttpForbidden(w)
				return
			}
			body := r.FormValue("body")
			_, err = s.Tasks.AddComment(r.Context(), pm.AddCommentInput{TaskID: taskID, Body: body})
			if err != nil {
				board, berr := s.boardData(r.Context(), id)
				if berr != nil {
					if berr == errProjectNotFound {
						zen.HttpNotFound(w)
						return
					}
					zen.HttpInternalServerError(w, berr.Error())
					return
				}
				data := ui.TaskEditPageData{
					Board:        board,
					TaskID:       taskID,
					Status:       get.Task.Status,
					Name:         get.Task.Name,
					Body:         get.Task.Body,
					Comments:     get.Task.Comments,
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
			id := pm.ProjectID(zen.Param(r, "id"))
			taskID := pm.TaskID(zen.Param(r, "taskID"))
			if err := r.ParseForm(); err != nil {
				zen.HttpBadRequest(w, err, "invalid form")
				return
			}
			get, err := s.Tasks.Get(r.Context(), pm.GetTaskInput{ID: taskID})
			if err != nil {
				zen.HttpInternalServerError(w, err.Error())
				return
			}
			if !get.Found || get.Task.ProjectID != id {
				zen.HttpNotFound(w)
				return
			}
			if ui.IsReadOnlyStatus(get.Task.Status) {
				zen.HttpForbidden(w)
				return
			}
			name := r.FormValue("name")
			body := r.FormValue("body")
			status := pm.Status(r.FormValue("status"))

			_, err = s.Tasks.Update(r.Context(), pm.UpdateTaskInput{
				ID:     taskID,
				Name:   name,
				Body:   body,
				Status: status,
				Tags:   get.Task.Tags,
			})
			if err != nil {
				board, berr := s.boardData(r.Context(), id)
				if berr != nil {
					if berr == errProjectNotFound {
						zen.HttpNotFound(w)
						return
					}
					zen.HttpInternalServerError(w, berr.Error())
					return
				}
				data := ui.TaskEditPageData{
					Board:    board,
					TaskID:   taskID,
					Status:   status,
					Name:     name,
					Body:     body,
					Comments: get.Task.Comments,
					Error:    err.Error(),
				}
				w.WriteHeader(http.StatusUnprocessableEntity)
				ui.Layout(ui.LayoutPage{}, ui.TaskEditPage(data)).Render(r.Context(), w)
				return
			}
			http.Redirect(w, r, "/projects/"+string(id), http.StatusSeeOther)
		})

		r.Post("/projects/{id}/tasks", func(w http.ResponseWriter, r *http.Request) {
			id := pm.ProjectID(zen.Param(r, "id"))
			if err := r.ParseForm(); err != nil {
				zen.HttpBadRequest(w, err, "invalid form")
				return
			}
			name := r.FormValue("name")
			body := r.FormValue("body")
			status := pm.Status(r.FormValue("status"))

			_, err := s.Tasks.Create(r.Context(), pm.CreateTaskInput{
				ProjectID: id,
				Name:      name,
				Body:      body,
				Status:    status,
			})
			if err != nil {
				board, berr := s.boardData(r.Context(), id)
				if berr != nil {
					if berr == errProjectNotFound {
						zen.HttpNotFound(w)
						return
					}
					zen.HttpInternalServerError(w, berr.Error())
					return
				}
				data := ui.TaskNewPageData{
					Board:  board,
					Status: status,
					Name:   name,
					Body:   body,
					Error:  err.Error(),
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

func (s *Server) boardData(ctx context.Context, id pm.ProjectID) (ui.ProjectPageData, error) {
	get, err := s.Projects.Get(ctx, pm.GetProjectInput{ID: id})
	if err != nil {
		return ui.ProjectPageData{}, err
	}
	if !get.Found {
		return ui.ProjectPageData{}, errProjectNotFound
	}
	list, err := s.Projects.List(ctx, pm.ListProjectsInput{})
	if err != nil {
		return ui.ProjectPageData{}, err
	}
	tasks, err := s.Tasks.ListByProject(ctx, pm.ListTasksByProjectInput{ProjectID: id})
	if err != nil {
		return ui.ProjectPageData{}, err
	}
	return ui.ProjectPageData{
		Projects: toUIProjects(list.Projects),
		Active:   id,
		Columns:  ui.BuildColumns(id, tasks.Tasks),
	}, nil
}

func toUIProjects(in []pm.Project) []ui.Project {
	out := make([]ui.Project, 0, len(in))
	for _, p := range in {
		out = append(out, ui.Project{ID: p.ID, Name: p.Name, Icon: p.Icon})
	}
	return out
}
