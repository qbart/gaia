package core

import (
	"context"
	"database/sql"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"sort"
	"strings"
	"time"

	"github.com/SoftKiwiGames/zen/zen/sqlite"
)

//go:embed migrations
var migrationsFS embed.FS

// Migrations exposes the embedded migration files so callers (typically the
// server bootstrap) can hand them to zen/sqlite.
func Migrations() fs.FS {
	return migrationsFS
}

// MigrationsDir is the path inside Migrations() where the .sql files live.
const MigrationsDir = "migrations"

var (
	ErrInvalidProjectName = errors.New("project name must not be empty")
	ErrInvalidTaskTitle   = errors.New("task title must not be empty")
	ErrInvalidStatus      = errors.New("invalid task status")
	ErrProjectNotFound    = errors.New("project not found")
	ErrTaskNotFound       = errors.New("task not found")
	ErrEmptyComment       = errors.New("comment must not be empty")
)

type Status string

const (
	StatusDocs       Status = "docs"
	StatusBrainstorm Status = "brainstorm"
	StatusTodo       Status = "todo"
	StatusDoing      Status = "doing"
	StatusReview     Status = "review"
	StatusRejected   Status = "rejected"
	StatusDone       Status = "done"
	StatusArchive    Status = "archive"
)

// Statuses lists every status in display order. ListTasksByProject groups
// tasks by this order.
var Statuses = []Status{
	StatusDocs,
	StatusBrainstorm,
	StatusTodo,
	StatusDoing,
	StatusReview,
	StatusRejected,
	StatusDone,
	StatusArchive,
}

// statusRank maps a status to its index in Statuses so we can ORDER BY it.
var statusRank = func() map[Status]int {
	m := make(map[Status]int, len(Statuses))
	for i, s := range Statuses {
		m[s] = i
	}
	return m
}()

// ProjectID is the autoincrement primary key on projects. URLs and the
// pm.ProjectID boundary type both render it as a decimal string. The Slug
// field on Project is stored alongside (unique, derived from the name)
// but is not used for routing.
type ProjectID int64

// TaskID is the SQLite autoincrement primary key on tasks. Globally unique
// across projects.
type TaskID int64

type Project struct {
	ID   ProjectID
	Slug string
	Name string
	Path string
}

type Task struct {
	ID          TaskID    `json:"id"`
	Title       string    `json:"title"`
	Description string    `json:"description"`
	Tags        []string  `json:"tags,omitempty"`
	Position    int       `json:"position"`
	ProjectID   ProjectID `json:"-"`
	Status      Status    `json:"-"`
	Comments    []string  `json:"-"`
}

// Store persists projects, tasks, and comments in SQLite. Writes go through
// the single-connection write pool zen/sqlite sets up, so concurrent writes
// from this process are already serialized; we use transactions for
// multi-statement mutations to get atomicity.
type Store struct {
	DB *sqlite.SQLite
}

func NewStore(db *sqlite.SQLite) *Store {
	return &Store{DB: db}
}

// ──────────────────────────────────────────────────────────────────────────
// Projects
// ──────────────────────────────────────────────────────────────────────────

func (s *Store) CreateProject(name string) (Project, error) {
	if err := validateProjectName(name); err != nil {
		return Project{}, err
	}
	base := nameToSlug(name)

	ctx := context.Background()
	var result Project
	err := s.DB.Transaction(ctx, func(tx *sql.Tx) error {
		q := s.DB.WithTx(tx)
		slug, err := uniqueProjectSlug(ctx, q, base)
		if err != nil {
			return err
		}
		var id int64
		err = q.QueryRow(ctx,
			`INSERT INTO projects (slug, name, path, created_at) VALUES (?, ?, '', ?) RETURNING id`,
			slug, name, sqlite.Timestamp(time.Now()),
		).Scan(&id)
		if err != nil {
			return fmt.Errorf("insert project: %w", err)
		}
		result = Project{ID: ProjectID(id), Slug: slug, Name: name}
		return nil
	})
	if err != nil {
		return Project{}, err
	}
	return result, nil
}

// uniqueProjectSlug returns base, or base-N (N≥2) if base is already taken.
// Runs inside the caller's transaction so the chosen slug can't race with
// another writer.
func uniqueProjectSlug(ctx context.Context, q *sqlite.SQLiteQuery, base string) (string, error) {
	candidate := base
	for i := 2; ; i++ {
		var exists int
		err := q.QueryRow(ctx,
			`SELECT 1 FROM projects WHERE slug = ? LIMIT 1`,
			candidate,
		).Scan(&exists)
		if sqlite.NoRows(err) {
			return candidate, nil
		}
		if err != nil {
			return "", fmt.Errorf("check project slug: %w", err)
		}
		candidate = fmt.Sprintf("%s-%d", base, i)
	}
}

// ScanProjects walks scanDir and registers every folder containing a .git
// subdirectory as a project (if not already registered by path). Returns
// the freshly-added projects.
func (s *Store) ScanProjects(scanDir string) ([]Project, error) {
	absScan, err := absPath(scanDir)
	if err != nil {
		return nil, fmt.Errorf("resolving scan dir: %w", err)
	}
	entries, err := readScanDir(absScan)
	if err != nil {
		return nil, err
	}
	known, err := s.knownPaths()
	if err != nil {
		return nil, err
	}

	ctx := context.Background()
	var added []Project
	err = s.DB.Transaction(ctx, func(tx *sql.Tx) error {
		q := s.DB.WithTx(tx)
		for _, e := range entries {
			path := e.Path
			if _, ok := known[path]; ok {
				continue
			}
			base := scanNameToSlug(e.Name)
			if base == "" {
				continue
			}
			slug, err := uniqueProjectSlug(ctx, q, base)
			if err != nil {
				return err
			}
			var id int64
			err = q.QueryRow(ctx,
				`INSERT INTO projects (slug, name, path, created_at) VALUES (?, ?, ?, ?) RETURNING id`,
				slug, e.Name, path, sqlite.Timestamp(time.Now()),
			).Scan(&id)
			if err != nil {
				return fmt.Errorf("insert scanned project: %w", err)
			}
			known[path] = struct{}{}
			added = append(added, Project{ID: ProjectID(id), Slug: slug, Name: e.Name, Path: path})
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return added, nil
}

func (s *Store) knownPaths() (map[string]struct{}, error) {
	rows, err := s.DB.Query(context.Background(),
		`SELECT path FROM projects WHERE path != ''`)
	if err != nil {
		return nil, fmt.Errorf("listing project paths: %w", err)
	}
	defer rows.Close()
	out := make(map[string]struct{})
	for rows.Next() {
		var p string
		if err := rows.Scan(&p); err != nil {
			return nil, fmt.Errorf("scanning project path: %w", err)
		}
		out[p] = struct{}{}
	}
	return out, rows.Err()
}

// ListProjects returns every project sorted alphabetically by slug so the
// UI sidebar has a stable, human-readable order.
func (s *Store) ListProjects() ([]Project, error) {
	rows, err := s.DB.Query(context.Background(),
		`SELECT id, slug, name, path FROM projects ORDER BY slug ASC`)
	if err != nil {
		return nil, fmt.Errorf("listing projects: %w", err)
	}
	defer rows.Close()
	out := make([]Project, 0)
	for rows.Next() {
		var (
			id              int64
			slug, name, path string
		)
		if err := rows.Scan(&id, &slug, &name, &path); err != nil {
			return nil, fmt.Errorf("scanning project: %w", err)
		}
		out = append(out, Project{ID: ProjectID(id), Slug: slug, Name: name, Path: path})
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

// GetProject loads a project by its internal integer id.
func (s *Store) GetProject(id ProjectID) (Project, error) {
	var (
		slug, name, path string
	)
	err := s.DB.QueryRow(context.Background(),
		`SELECT slug, name, path FROM projects WHERE id = ?`, int64(id),
	).Scan(&slug, &name, &path)
	if sqlite.NoRows(err) {
		return Project{}, ErrProjectNotFound
	}
	if err != nil {
		return Project{}, fmt.Errorf("loading project: %w", err)
	}
	return Project{ID: id, Slug: slug, Name: name, Path: path}, nil
}

// ──────────────────────────────────────────────────────────────────────────
// Tasks
// ──────────────────────────────────────────────────────────────────────────

func (s *Store) CreateTask(projectID ProjectID, status Status, title, description string, tags []string) (Task, error) {
	title = strings.TrimSpace(title)
	if title == "" {
		return Task{}, ErrInvalidTaskTitle
	}
	if !isKnownStatus(status) {
		return Task{}, ErrInvalidStatus
	}

	ctx := context.Background()
	tagsJSON, err := encodeTags(tags)
	if err != nil {
		return Task{}, err
	}

	var t Task
	err = s.DB.Transaction(ctx, func(tx *sql.Tx) error {
		q := s.DB.WithTx(tx)
		if err := assertProjectExistsTx(ctx, q, projectID); err != nil {
			return err
		}
		pos, err := maxPositionTx(ctx, q, projectID, status)
		if err != nil {
			return err
		}
		t = Task{
			Title:       title,
			Description: strings.TrimSpace(description),
			Tags:        tags,
			Position:    pos + 1,
			ProjectID:   projectID,
			Status:      status,
		}
		var newID int64
		err = q.QueryRow(ctx,
			`INSERT INTO tasks (project_id, title, description, status, position, tags, created_at)
			 VALUES (?, ?, ?, ?, ?, ?, ?)
			 RETURNING id`,
			int64(projectID), t.Title, t.Description,
			string(status), t.Position, tagsJSON, sqlite.Timestamp(time.Now()),
		).Scan(&newID)
		if err != nil {
			return fmt.Errorf("insert task: %w", err)
		}
		t.ID = TaskID(newID)
		return nil
	})
	if err != nil {
		return Task{}, err
	}
	return t, nil
}

func assertProjectExistsTx(ctx context.Context, q *sqlite.SQLiteQuery, id ProjectID) error {
	var exists int
	err := q.QueryRow(ctx,
		`SELECT 1 FROM projects WHERE id = ? LIMIT 1`, int64(id),
	).Scan(&exists)
	if sqlite.NoRows(err) {
		return ErrProjectNotFound
	}
	if err != nil {
		return fmt.Errorf("check project: %w", err)
	}
	return nil
}

func maxPositionTx(ctx context.Context, q *sqlite.SQLiteQuery, projectID ProjectID, status Status) (int, error) {
	var pos sql.NullInt64
	err := q.QueryRow(ctx,
		`SELECT MAX(position) FROM tasks WHERE project_id = ? AND status = ?`,
		int64(projectID), string(status),
	).Scan(&pos)
	if err != nil && !sqlite.NoRows(err) {
		return 0, fmt.Errorf("max position: %w", err)
	}
	if !pos.Valid {
		return 0, nil
	}
	return int(pos.Int64), nil
}

// GetTask returns the task scoped to projectID. A task that exists but
// belongs to a different project returns ErrTaskNotFound.
func (s *Store) GetTask(projectID ProjectID, id TaskID) (Task, error) {
	if _, err := s.GetProject(projectID); err != nil {
		return Task{}, err
	}
	t, err := s.readTask(context.Background(), projectID, id)
	if err != nil {
		return Task{}, err
	}
	comments, err := s.readComments(context.Background(), id)
	if err != nil {
		return Task{}, err
	}
	t.Comments = comments
	return t, nil
}

// readTask loads a single task row scoped to (projectID, id). The project
// scope is enforced in SQL so a forged URL cannot read another project's
// task.
func (s *Store) readTask(ctx context.Context, projectID ProjectID, id TaskID) (Task, error) {
	var (
		title, description, status, tagsJSON string
		position                              int
	)
	err := s.DB.QueryRow(ctx,
		`SELECT title, description, status, position, tags
		   FROM tasks WHERE id = ? AND project_id = ?`,
		int64(id), int64(projectID),
	).Scan(&title, &description, &status, &position, &tagsJSON)
	if sqlite.NoRows(err) {
		return Task{}, ErrTaskNotFound
	}
	if err != nil {
		return Task{}, fmt.Errorf("loading task: %w", err)
	}
	tags, err := decodeTags(tagsJSON)
	if err != nil {
		return Task{}, err
	}
	return Task{
		ID:          id,
		Title:       title,
		Description: description,
		Tags:        tags,
		Position:    position,
		ProjectID:   projectID,
		Status:      Status(status),
	}, nil
}

func readTaskTx(ctx context.Context, q *sqlite.SQLiteQuery, projectID ProjectID, id TaskID) (Task, error) {
	var (
		title, description, status, tagsJSON string
		position                              int
	)
	err := q.QueryRow(ctx,
		`SELECT title, description, status, position, tags
		   FROM tasks WHERE id = ? AND project_id = ?`,
		int64(id), int64(projectID),
	).Scan(&title, &description, &status, &position, &tagsJSON)
	if sqlite.NoRows(err) {
		return Task{}, ErrTaskNotFound
	}
	if err != nil {
		return Task{}, fmt.Errorf("loading task: %w", err)
	}
	tags, err := decodeTags(tagsJSON)
	if err != nil {
		return Task{}, err
	}
	return Task{
		ID:          id,
		Title:       title,
		Description: description,
		Tags:        tags,
		Position:    position,
		ProjectID:   projectID,
		Status:      Status(status),
	}, nil
}

// ListTasksByProject returns every task across every status, grouped by the
// canonical Statuses order and, within each status, sorted by Position then
// id.
func (s *Store) ListTasksByProject(projectID ProjectID) ([]Task, error) {
	if _, err := s.GetProject(projectID); err != nil {
		return nil, err
	}
	ctx := context.Background()
	rows, err := s.DB.Query(ctx,
		`SELECT id, title, description, status, position, tags
		   FROM tasks WHERE project_id = ?`,
		int64(projectID),
	)
	if err != nil {
		return nil, fmt.Errorf("listing tasks: %w", err)
	}
	defer rows.Close()

	tasks := make([]Task, 0)
	for rows.Next() {
		var (
			id                                   int64
			title, description, status, tagsJSON string
			position                             int
		)
		if err := rows.Scan(&id, &title, &description, &status, &position, &tagsJSON); err != nil {
			return nil, fmt.Errorf("scanning task: %w", err)
		}
		tags, err := decodeTags(tagsJSON)
		if err != nil {
			return nil, err
		}
		tasks = append(tasks, Task{
			ID:          TaskID(id),
			Title:       title,
			Description: description,
			Tags:        tags,
			Position:    position,
			ProjectID:   projectID,
			Status:      Status(status),
		})
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	sort.SliceStable(tasks, func(i, j int) bool {
		ri := statusRank[tasks[i].Status]
		rj := statusRank[tasks[j].Status]
		if ri != rj {
			return ri < rj
		}
		if tasks[i].Position != tasks[j].Position {
			return tasks[i].Position < tasks[j].Position
		}
		return tasks[i].ID < tasks[j].ID
	})

	for i := range tasks {
		comments, err := s.readComments(ctx, tasks[i].ID)
		if err != nil {
			return nil, err
		}
		tasks[i].Comments = comments
	}
	return tasks, nil
}

func (s *Store) UpdateTask(projectID ProjectID, id TaskID, title, description string, status Status, tags []string) (Task, error) {
	title = strings.TrimSpace(title)
	if title == "" {
		return Task{}, ErrInvalidTaskTitle
	}
	if !isKnownStatus(status) {
		return Task{}, ErrInvalidStatus
	}
	tagsJSON, err := encodeTags(tags)
	if err != nil {
		return Task{}, err
	}

	ctx := context.Background()
	var result Task
	err = s.DB.Transaction(ctx, func(tx *sql.Tx) error {
		q := s.DB.WithTx(tx)
		if err := assertProjectExistsTx(ctx, q, projectID); err != nil {
			return err
		}
		cur, err := readTaskTx(ctx, q, projectID, id)
		if err != nil {
			return err
		}
		if status != cur.Status {
			pos, err := maxPositionTx(ctx, q, projectID, status)
			if err != nil {
				return err
			}
			cur.Status = status
			cur.Position = pos + 1
		}
		cur.Title = title
		cur.Description = strings.TrimSpace(description)
		cur.Tags = tags
		_, err = q.Exec(ctx,
			`UPDATE tasks
			    SET title = ?, description = ?, status = ?, position = ?, tags = ?
			  WHERE id = ? AND project_id = ?`,
			cur.Title, cur.Description, string(cur.Status), cur.Position, tagsJSON,
			int64(id), int64(projectID),
		)
		if err != nil {
			return fmt.Errorf("update task: %w", err)
		}
		result = cur
		return nil
	})
	return result, err
}

func (s *Store) MoveTask(projectID ProjectID, id TaskID, status Status) (Task, error) {
	if !isKnownStatus(status) {
		return Task{}, ErrInvalidStatus
	}
	ctx := context.Background()
	var result Task
	err := s.DB.Transaction(ctx, func(tx *sql.Tx) error {
		q := s.DB.WithTx(tx)
		if err := assertProjectExistsTx(ctx, q, projectID); err != nil {
			return err
		}
		cur, err := readTaskTx(ctx, q, projectID, id)
		if err != nil {
			return err
		}
		if cur.Status == status {
			result = cur
			return nil
		}
		pos, err := maxPositionTx(ctx, q, projectID, status)
		if err != nil {
			return err
		}
		cur.Status = status
		cur.Position = pos + 1
		_, err = q.Exec(ctx,
			`UPDATE tasks SET status = ?, position = ? WHERE id = ? AND project_id = ?`,
			string(cur.Status), cur.Position, int64(id), int64(projectID),
		)
		if err != nil {
			return fmt.Errorf("move task: %w", err)
		}
		result = cur
		return nil
	})
	if err != nil {
		return Task{}, err
	}
	// Comments are keyed by task_id so they ride along automatically. Reload
	// them so callers see a complete Task.
	comments, err := s.readComments(ctx, id)
	if err != nil {
		return Task{}, err
	}
	result.Comments = comments
	return result, nil
}

func (s *Store) DeleteTask(projectID ProjectID, id TaskID) error {
	ctx := context.Background()
	return s.DB.Transaction(ctx, func(tx *sql.Tx) error {
		q := s.DB.WithTx(tx)
		if err := assertProjectExistsTx(ctx, q, projectID); err != nil {
			return err
		}
		if _, err := readTaskTx(ctx, q, projectID, id); err != nil {
			return err
		}
		if _, err := q.Exec(ctx,
			`DELETE FROM task_comments WHERE task_id = ?`, int64(id),
		); err != nil {
			return fmt.Errorf("delete comments: %w", err)
		}
		if _, err := q.Exec(ctx,
			`DELETE FROM tasks WHERE id = ? AND project_id = ?`,
			int64(id), int64(projectID),
		); err != nil {
			return fmt.Errorf("delete task: %w", err)
		}
		return nil
	})
}

// ReorderColumn rewrites Position for every task in a status column to match
// the order of ids. The slice must list exactly the tasks currently in the
// column; any mismatch returns an error so a stale client cannot overwrite
// a concurrent move.
func (s *Store) ReorderColumn(projectID ProjectID, status Status, ids []TaskID) error {
	if !isKnownStatus(status) {
		return ErrInvalidStatus
	}
	ctx := context.Background()
	return s.DB.Transaction(ctx, func(tx *sql.Tx) error {
		q := s.DB.WithTx(tx)
		if err := assertProjectExistsTx(ctx, q, projectID); err != nil {
			return err
		}
		rows, err := q.Query(ctx,
			`SELECT id FROM tasks WHERE project_id = ? AND status = ?`,
			int64(projectID), string(status),
		)
		if err != nil {
			return fmt.Errorf("listing column: %w", err)
		}
		existing := make(map[TaskID]struct{})
		for rows.Next() {
			var id int64
			if err := rows.Scan(&id); err != nil {
				rows.Close()
				return fmt.Errorf("scan column: %w", err)
			}
			existing[TaskID(id)] = struct{}{}
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return err
		}

		if len(existing) != len(ids) {
			return fmt.Errorf("reorder: column has %d tasks, got %d ids", len(existing), len(ids))
		}
		seen := make(map[TaskID]struct{}, len(ids))
		for _, id := range ids {
			if _, ok := existing[id]; !ok {
				return fmt.Errorf("reorder: task %d not in status %s", id, status)
			}
			if _, dup := seen[id]; dup {
				return fmt.Errorf("reorder: duplicate task %d", id)
			}
			seen[id] = struct{}{}
		}
		for i, id := range ids {
			if _, err := q.Exec(ctx,
				`UPDATE tasks SET position = ? WHERE id = ? AND project_id = ?`,
				i+1, int64(id), int64(projectID),
			); err != nil {
				return fmt.Errorf("reorder update: %w", err)
			}
		}
		return nil
	})
}

// ──────────────────────────────────────────────────────────────────────────
// Comments
// ──────────────────────────────────────────────────────────────────────────

func (s *Store) AddComment(projectID ProjectID, id TaskID, body string) (Task, error) {
	body = strings.TrimSpace(body)
	if body == "" {
		return Task{}, ErrEmptyComment
	}
	ctx := context.Background()
	var result Task
	err := s.DB.Transaction(ctx, func(tx *sql.Tx) error {
		q := s.DB.WithTx(tx)
		if err := assertProjectExistsTx(ctx, q, projectID); err != nil {
			return err
		}
		t, err := readTaskTx(ctx, q, projectID, id)
		if err != nil {
			return err
		}
		if _, err := q.Exec(ctx,
			`INSERT INTO task_comments (task_id, body, created_at) VALUES (?, ?, ?)`,
			int64(id), body, sqlite.Timestamp(time.Now()),
		); err != nil {
			return fmt.Errorf("insert comment: %w", err)
		}
		result = t
		return nil
	})
	if err != nil {
		return Task{}, err
	}
	comments, err := s.readComments(ctx, id)
	if err != nil {
		return Task{}, err
	}
	result.Comments = comments
	return result, nil
}

func (s *Store) readComments(ctx context.Context, id TaskID) ([]string, error) {
	rows, err := s.DB.Query(ctx,
		`SELECT body FROM task_comments WHERE task_id = ? ORDER BY id ASC`,
		int64(id),
	)
	if err != nil {
		return nil, fmt.Errorf("loading comments: %w", err)
	}
	defer rows.Close()
	out := make([]string, 0)
	for rows.Next() {
		var body string
		if err := rows.Scan(&body); err != nil {
			return nil, fmt.Errorf("scan comment: %w", err)
		}
		out = append(out, body)
	}
	return out, rows.Err()
}

// ──────────────────────────────────────────────────────────────────────────
// helpers
// ──────────────────────────────────────────────────────────────────────────

func encodeTags(tags []string) (string, error) {
	if tags == nil {
		return "[]", nil
	}
	b, err := json.Marshal(tags)
	if err != nil {
		return "", fmt.Errorf("encoding tags: %w", err)
	}
	return string(b), nil
}

func decodeTags(s string) ([]string, error) {
	if s == "" {
		return nil, nil
	}
	var tags []string
	if err := json.Unmarshal([]byte(s), &tags); err != nil {
		return nil, fmt.Errorf("decoding tags: %w", err)
	}
	if len(tags) == 0 {
		return nil, nil
	}
	return tags, nil
}

func isKnownStatus(s Status) bool {
	_, ok := statusRank[s]
	return ok
}

// validateProjectName enforces input rules: lowercase ASCII letters, digits,
// '-', '_', and single spaces between words. Length 1..50 runes.
func validateProjectName(name string) error {
	if name == "" {
		return ErrInvalidProjectName
	}
	if len([]rune(name)) > 50 {
		return ErrInvalidProjectName
	}
	prevSpace := false
	for i, r := range name {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '-', r == '_':
			prevSpace = false
		case r == ' ':
			if i == 0 || prevSpace {
				return ErrInvalidProjectName
			}
			prevSpace = true
		default:
			return ErrInvalidProjectName
		}
	}
	if prevSpace {
		return ErrInvalidProjectName
	}
	return nil
}

func nameToSlug(name string) string {
	return strings.ReplaceAll(name, " ", "-")
}

// scanNameToSlug converts an arbitrary folder name into a project ID slug.
func scanNameToSlug(name string) string {
	var b strings.Builder
	dash := false
	for _, r := range strings.ToLower(name) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '_':
			b.WriteRune(r)
			dash = false
		case r == '-':
			if b.Len() > 0 && !dash {
				b.WriteRune('-')
				dash = true
			}
		default:
			if b.Len() > 0 && !dash {
				b.WriteRune('-')
				dash = true
			}
		}
	}
	return strings.TrimRight(b.String(), "-")
}
