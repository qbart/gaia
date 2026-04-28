package web

import (
	"crypto/subtle"
	"errors"
	"net/http"
	"strings"

	"github.com/SoftKiwiGames/zen/zen"
	"github.com/qbart/gaia/pm"
	"github.com/qbart/gaia/web/core"
)

// MiddlewareBearerToken authenticates requests with a static bearer token,
// using a constant-time compare so request timing can't leak the token.
// Missing or wrong tokens get a 401.
func MiddlewareBearerToken(token string) zen.Middleware {
	expected := []byte(token)
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			h := r.Header.Get("Authorization")
			if !strings.HasPrefix(h, "Bearer ") {
				zen.HttpUnauthorized(w)
				return
			}
			given := []byte(strings.TrimPrefix(h, "Bearer "))
			if subtle.ConstantTimeCompare(given, expected) != 1 {
				zen.HttpUnauthorized(w)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// taskAPIResource serves the per-project task collection. We embed the
// no-op zen.APIResource and only override Index and Create — pm.Provider
// doesn't need Show/Update/Destroy, and adding them would collide on the
// `id` route param under a nested project route.
type taskAPIResource struct {
	zen.APIResource
	store *core.Store
}

func (res *taskAPIResource) Index(w http.ResponseWriter, r *http.Request) {
	projectID := core.ProjectID(zen.Param(r, "projectID"))
	statusFilter := r.URL.Query().Get("status")

	tasks, err := res.store.ListTasksByProject(projectID)
	if err != nil {
		if errors.Is(err, core.ErrProjectNotFound) {
			zen.HttpNotFound(w)
			return
		}
		zen.HttpInternalServerError(w, err.Error())
		return
	}
	out := make([]pm.GaiaTask, 0, len(tasks))
	for _, t := range tasks {
		if statusFilter != "" && string(t.Status) != statusFilter {
			continue
		}
		out = append(out, toGaiaTask(t))
	}
	zen.HttpOk(w, out)
}

func (res *taskAPIResource) Create(w http.ResponseWriter, r *http.Request) {
	projectID := core.ProjectID(zen.Param(r, "projectID"))
	req, err := zen.ParseAndValidateJSON[pm.GaiaCreateTaskRequest](r.Body)
	if err != nil {
		zen.HttpBadRequest(w, err, err.Error())
		return
	}
	status := core.Status(req.Status)
	if status == "" {
		status = core.StatusTodo
	}
	t, err := res.store.CreateTask(projectID, status, req.Name, req.Body, nil)
	if err != nil {
		switch {
		case errors.Is(err, core.ErrProjectNotFound):
			zen.HttpNotFound(w)
		case errors.Is(err, core.ErrInvalidStatus),
			errors.Is(err, core.ErrInvalidTaskTitle):
			zen.HttpBadRequest(w, err, err.Error())
		default:
			zen.HttpInternalServerError(w, err.Error())
		}
		return
	}
	zen.HttpCreated(w, toGaiaTask(t))
}

// apiMoveTask handles `POST /api/projects/{projectID}/tasks/{taskID}/move`.
// Unlike the UI handler this does not refuse moves to/from doing — that's
// the AI agent's lane and the agent calls this endpoint to claim work.
func apiMoveTask(store *core.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		projectID := core.ProjectID(zen.Param(r, "projectID"))
		taskID := core.TaskID(zen.Param(r, "taskID"))
		req, err := zen.ParseAndValidateJSON[pm.GaiaMoveRequest](r.Body)
		if err != nil {
			zen.HttpBadRequest(w, err, err.Error())
			return
		}
		if _, err := store.MoveTask(projectID, taskID, core.Status(req.Status)); err != nil {
			switch {
			case errors.Is(err, core.ErrProjectNotFound),
				errors.Is(err, core.ErrTaskNotFound):
				zen.HttpNotFound(w)
			case errors.Is(err, core.ErrInvalidStatus):
				zen.HttpBadRequest(w, err, err.Error())
			default:
				zen.HttpInternalServerError(w, err.Error())
			}
			return
		}
		zen.HttpOkDefault(w)
	}
}

// apiCommentTask handles `POST /api/projects/{projectID}/tasks/{taskID}/comments`.
func apiCommentTask(store *core.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		projectID := core.ProjectID(zen.Param(r, "projectID"))
		taskID := core.TaskID(zen.Param(r, "taskID"))
		req, err := zen.ParseAndValidateJSON[pm.GaiaCommentRequest](r.Body)
		if err != nil {
			zen.HttpBadRequest(w, err, err.Error())
			return
		}
		if _, err := store.AddComment(projectID, taskID, req.Body); err != nil {
			switch {
			case errors.Is(err, core.ErrProjectNotFound),
				errors.Is(err, core.ErrTaskNotFound):
				zen.HttpNotFound(w)
			case errors.Is(err, core.ErrEmptyComment):
				zen.HttpBadRequest(w, err, err.Error())
			default:
				zen.HttpInternalServerError(w, err.Error())
			}
			return
		}
		zen.HttpCreatedDefault(w)
	}
}

func toGaiaTask(t core.Task) pm.GaiaTask {
	tags := t.Tags
	if tags == nil {
		tags = []string{}
	}
	comments := t.Comments
	if comments == nil {
		comments = []string{}
	}
	return pm.GaiaTask{
		ID:       string(t.ID),
		Name:     t.Title,
		Body:     t.Description,
		Status:   string(t.Status),
		Tags:     tags,
		Comments: comments,
	}
}

