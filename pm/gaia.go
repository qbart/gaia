package pm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Gaia is a pm.Provider that talks to a running gaia web server over HTTP.
// It's the default provider for the CLI: the agent runs as a client of its
// own UI process. Authentication uses a bearer token (GAIA_TOKEN).
type Gaia struct {
	baseURL string
	token   string
	project string
	client  *http.Client
}

func NewGaia(baseURL, token, project string) *Gaia {
	return &Gaia{
		baseURL: strings.TrimRight(baseURL, "/"),
		token:   token,
		project: project,
		client:  &http.Client{Timeout: 30 * time.Second},
	}
}

// GaiaTask is the JSON shape returned by the API. The struct lives on the
// pm side because pm is the canonical client; the web server embeds these
// types for its responses.
type GaiaTask struct {
	ID       string   `json:"id"`
	Name     string   `json:"name"`
	Body     string   `json:"body"`
	Status   string   `json:"status"`
	Tags     []string `json:"tags"`
	Comments []string `json:"comments"`
}

type GaiaCreateTaskRequest struct {
	Name   string `json:"name" validate:"required"`
	Body   string `json:"body"`
	Status string `json:"status"`
}

type GaiaMoveRequest struct {
	Status string `json:"status" validate:"required"`
}

type GaiaCommentRequest struct {
	Body string `json:"body" validate:"required"`
}

func (g *Gaia) Init(ctx context.Context) error {
	if g.token == "" {
		return fmt.Errorf("GAIA_TOKEN not set")
	}
	if g.project == "" {
		return fmt.Errorf("--project not set")
	}
	// Probe the project to surface auth/404 errors at startup.
	if _, err := g.ListTasks(ctx, StatusTodo); err != nil {
		return fmt.Errorf("gaia init: %w", err)
	}
	return nil
}

func (g *Gaia) ListTasks(ctx context.Context, status Status) ([]*Task, error) {
	path := g.tasksPath() + "?status=" + url.QueryEscape(string(status))
	var resp []GaiaTask
	if err := g.do(ctx, http.MethodGet, path, nil, &resp); err != nil {
		return nil, err
	}
	out := make([]*Task, 0, len(resp))
	for _, t := range resp {
		out = append(out, &Task{
			ID:       TaskID(t.ID),
			Name:     t.Name,
			Body:     t.Body,
			Status:   Status(t.Status),
			Tags:     t.Tags,
			Comments: t.Comments,
		})
	}
	return out, nil
}

func (g *Gaia) CreateTask(ctx context.Context, task Task) (TaskID, error) {
	body := GaiaCreateTaskRequest{
		Name:   task.Name,
		Body:   task.Body,
		Status: string(task.Status),
	}
	if body.Status == "" {
		body.Status = string(StatusTodo)
	}
	var resp GaiaTask
	if err := g.do(ctx, http.MethodPost, g.tasksPath(), body, &resp); err != nil {
		return "", err
	}
	return TaskID(resp.ID), nil
}

func (g *Gaia) MoveTaskTo(ctx context.Context, id TaskID, status Status) error {
	body := GaiaMoveRequest{Status: string(status)}
	return g.do(ctx, http.MethodPost, g.taskPath(id)+"/move", body, nil)
}

func (g *Gaia) CommentTask(ctx context.Context, id TaskID, body string) error {
	payload := GaiaCommentRequest{Body: body}
	return g.do(ctx, http.MethodPost, g.taskPath(id)+"/comments", payload, nil)
}

func (g *Gaia) tasksPath() string {
	return "/api/projects/" + url.PathEscape(g.project) + "/tasks"
}

func (g *Gaia) taskPath(id TaskID) string {
	return g.tasksPath() + "/" + url.PathEscape(string(id))
}

func (g *Gaia) do(ctx context.Context, method, path string, body any, out any) error {
	var reader io.Reader
	if body != nil {
		buf, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("encoding request: %w", err)
		}
		reader = bytes.NewReader(buf)
	}
	req, err := http.NewRequestWithContext(ctx, method, g.baseURL+path, reader)
	if err != nil {
		return fmt.Errorf("building request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+g.token)
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := g.client.Do(req)
	if err != nil {
		return fmt.Errorf("calling %s %s: %w", method, path, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("gaia api %s %s: %s: %s", method, path, resp.Status, strings.TrimSpace(string(body)))
	}
	if out != nil && resp.StatusCode != http.StatusNoContent {
		if err := json.NewDecoder(resp.Body).Decode(out); err != nil && err != io.EOF {
			return fmt.Errorf("decoding response: %w", err)
		}
	}
	return nil
}
