package core

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
)

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

// Statuses lists every status folder that exists on disk. StatusArchive is
// hidden from the UI but stored alongside the others.
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

type ProjectID string
type TaskID string

type Project struct {
	ID   ProjectID
	Name string
	Path string
}

type Manifest struct {
	Name string `json:"name"`
	Path string `json:"path,omitempty"`
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

// Store is a file-system backed project + task repository rooted at Root.
//
// Concurrency model:
//   - Mutations on a project (create/update/move task, add comment, sequence
//     bump) acquire BOTH a per-project sync.Mutex and an exclusive flock on
//     <project>/.lock. The mutex serializes goroutines inside this process,
//     while flock extends serialization across processes that share GAIA_DATA.
//   - Project creation goes through a root-wide sync.Mutex and an exclusive
//     flock on <root>/.lock so two callers picking the same slug cannot race.
//   - Reads (GetTask, ListTasksByProject, ListProjects) are lock-free; every
//     write goes through atomicWrite/os.Rename so readers always see a
//     complete file.
type Store struct {
	Root string

	rootMu sync.Mutex

	mu    sync.Mutex
	locks map[ProjectID]*sync.Mutex
}

func NewStore(root string) *Store {
	return &Store{
		Root:  root,
		locks: make(map[ProjectID]*sync.Mutex),
	}
}

func (s *Store) projectMutex(id ProjectID) *sync.Mutex {
	s.mu.Lock()
	defer s.mu.Unlock()
	if l, ok := s.locks[id]; ok {
		return l
	}
	l := &sync.Mutex{}
	s.locks[id] = l
	return l
}

// withProjectLock serializes mutations to a single project, both inside this
// process (via projectMutex) and across processes (via flock on <project>/.lock).
// The project must already exist on disk.
func (s *Store) withProjectLock(id ProjectID, fn func() error) error {
	pl := s.projectMutex(id)
	pl.Lock()
	defer pl.Unlock()

	lockPath := filepath.Join(s.projectDir(id), ".lock")
	f, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return fmt.Errorf("opening project lock: %w", err)
	}
	defer f.Close()
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX); err != nil {
		return fmt.Errorf("acquiring project lock: %w", err)
	}
	defer syscall.Flock(int(f.Fd()), syscall.LOCK_UN)

	return fn()
}

// withRootLock serializes operations that modify the data root itself
// (currently project creation) across processes and goroutines.
func (s *Store) withRootLock(fn func() error) error {
	s.rootMu.Lock()
	defer s.rootMu.Unlock()

	if err := os.MkdirAll(s.Root, 0o755); err != nil {
		return fmt.Errorf("creating data root: %w", err)
	}
	lockPath := filepath.Join(s.Root, ".lock")
	f, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return fmt.Errorf("opening root lock: %w", err)
	}
	defer f.Close()
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX); err != nil {
		return fmt.Errorf("acquiring root lock: %w", err)
	}
	defer syscall.Flock(int(f.Fd()), syscall.LOCK_UN)

	return fn()
}

func (s *Store) projectDir(id ProjectID) string {
	return filepath.Join(s.Root, string(id))
}

func (s *Store) manifestPath(id ProjectID) string {
	return filepath.Join(s.projectDir(id), "manifest.json")
}

func (s *Store) sequencePath(id ProjectID) string {
	return filepath.Join(s.projectDir(id), "sequence")
}

func (s *Store) statusDir(id ProjectID, status Status) string {
	return filepath.Join(s.projectDir(id), "tasks", string(status))
}

func (s *Store) taskPath(id ProjectID, status Status, taskID TaskID) string {
	return filepath.Join(s.statusDir(id, status), string(taskID)+".json")
}

func (s *Store) commentsPath(id ProjectID, status Status, taskID TaskID) string {
	return filepath.Join(s.statusDir(id, status), string(taskID)+".comments.json")
}

func (s *Store) CreateProject(name string) (Project, error) {
	if err := validateProjectName(name); err != nil {
		return Project{}, err
	}
	base := nameToSlug(name)

	var result Project
	err := s.withRootLock(func() error {
		id, err := s.uniqueProjectID(base)
		if err != nil {
			return err
		}
		if err := s.writeProjectLayout(id, Manifest{Name: name}); err != nil {
			return err
		}
		result = Project{ID: id, Name: name}
		return nil
	})
	if err != nil {
		return Project{}, err
	}
	return result, nil
}

// uniqueProjectID returns base, or base-N if base is already taken on disk.
// Caller must hold the root lock.
func (s *Store) uniqueProjectID(base string) (ProjectID, error) {
	id := ProjectID(base)
	for i := 2; ; i++ {
		if _, err := os.Stat(s.manifestPath(id)); errors.Is(err, os.ErrNotExist) {
			return id, nil
		} else if err != nil {
			return "", fmt.Errorf("checking project dir: %w", err)
		}
		id = ProjectID(base + "-" + strconv.Itoa(i))
	}
}

// writeProjectLayout creates the project directory tree (status folders,
// manifest, sequence). Caller must hold the root lock.
func (s *Store) writeProjectLayout(id ProjectID, m Manifest) error {
	dir := s.projectDir(id)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("creating project dir: %w", err)
	}
	for _, st := range Statuses {
		if err := os.MkdirAll(s.statusDir(id, st), 0o755); err != nil {
			return fmt.Errorf("creating status dir %s: %w", st, err)
		}
	}
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return fmt.Errorf("encoding manifest: %w", err)
	}
	if err := atomicWrite(s.manifestPath(id), data); err != nil {
		return fmt.Errorf("writing manifest: %w", err)
	}
	if err := atomicWrite(s.sequencePath(id), []byte("0")); err != nil {
		return fmt.Errorf("writing sequence: %w", err)
	}
	return nil
}

// ScanProjects discovers folders in scanDir that contain a .git subdirectory
// and registers each as a project (manifest with name=folder name and
// path=absolute path). Skips folders whose absolute path is already
// registered, so it is safe to call on every server boot.
func (s *Store) ScanProjects(scanDir string) ([]Project, error) {
	absScan, err := filepath.Abs(scanDir)
	if err != nil {
		return nil, fmt.Errorf("resolving scan dir: %w", err)
	}
	entries, err := os.ReadDir(absScan)
	if err != nil {
		return nil, fmt.Errorf("reading scan dir: %w", err)
	}
	existing, err := s.ListProjects()
	if err != nil {
		return nil, err
	}
	known := make(map[string]struct{}, len(existing))
	for _, p := range existing {
		if p.Path != "" {
			known[p.Path] = struct{}{}
		}
	}

	var added []Project
	err = s.withRootLock(func() error {
		for _, e := range entries {
			if !e.IsDir() {
				continue
			}
			name := e.Name()
			if strings.HasPrefix(name, ".") {
				continue
			}
			path := filepath.Join(absScan, name)
			gitInfo, err := os.Stat(filepath.Join(path, ".git"))
			if err != nil || !gitInfo.IsDir() {
				continue
			}
			if _, ok := known[path]; ok {
				continue
			}
			base := scanNameToSlug(name)
			if base == "" {
				continue
			}
			id, err := s.uniqueProjectID(base)
			if err != nil {
				return err
			}
			if err := s.writeProjectLayout(id, Manifest{Name: name, Path: path}); err != nil {
				return err
			}
			known[path] = struct{}{}
			added = append(added, Project{ID: id, Name: name, Path: path})
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return added, nil
}

func (s *Store) ListProjects() ([]Project, error) {
	entries, err := os.ReadDir(s.Root)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("reading data root: %w", err)
	}
	out := make([]Project, 0, len(entries))
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		id := ProjectID(e.Name())
		m, err := s.readManifest(id)
		if err != nil {
			if errors.Is(err, ErrProjectNotFound) {
				continue
			}
			return nil, err
		}
		out = append(out, Project{ID: id, Name: m.Name, Path: m.Path})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

func (s *Store) GetProject(id ProjectID) (Project, error) {
	m, err := s.readManifest(id)
	if err != nil {
		return Project{}, err
	}
	return Project{ID: id, Name: m.Name, Path: m.Path}, nil
}

func (s *Store) readManifest(id ProjectID) (Manifest, error) {
	data, err := os.ReadFile(s.manifestPath(id))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return Manifest{}, ErrProjectNotFound
		}
		return Manifest{}, fmt.Errorf("reading manifest: %w", err)
	}
	var m Manifest
	if err := json.Unmarshal(data, &m); err != nil {
		return Manifest{}, fmt.Errorf("parsing manifest: %w", err)
	}
	return m, nil
}

// NextSequence returns the next per-project task identifier and persists the
// updated counter atomically. The first call after CreateProject returns 1.
//
// Holds the project lock for the read-modify-write of the sequence file, so
// concurrent callers (in this process or another) always observe a strictly
// increasing sequence with no duplicates.
func (s *Store) NextSequence(id ProjectID) (uint64, error) {
	if _, err := os.Stat(s.manifestPath(id)); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return 0, ErrProjectNotFound
		}
		return 0, fmt.Errorf("checking project: %w", err)
	}
	var n uint64
	err := s.withProjectLock(id, func() error {
		v, err := s.bumpSequence(id)
		if err != nil {
			return err
		}
		n = v
		return nil
	})
	return n, err
}

// bumpSequence reads, increments, and rewrites the sequence file. The caller
// must hold the project lock.
func (s *Store) bumpSequence(id ProjectID) (uint64, error) {
	cur := uint64(0)
	data, err := os.ReadFile(s.sequencePath(id))
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return 0, fmt.Errorf("reading sequence: %w", err)
	}
	if len(data) > 0 {
		text := strings.TrimSpace(string(data))
		if text != "" {
			v, err := strconv.ParseUint(text, 10, 64)
			if err != nil {
				return 0, fmt.Errorf("parsing sequence %q: %w", text, err)
			}
			cur = v
		}
	}
	next := cur + 1
	if err := atomicWrite(s.sequencePath(id), []byte(strconv.FormatUint(next, 10))); err != nil {
		return 0, fmt.Errorf("writing sequence: %w", err)
	}
	return next, nil
}

func (s *Store) CreateTask(projectID ProjectID, status Status, title, description string, tags []string) (Task, error) {
	title = strings.TrimSpace(title)
	if title == "" {
		return Task{}, ErrInvalidTaskTitle
	}
	if !isKnownStatus(status) {
		return Task{}, ErrInvalidStatus
	}
	if _, err := s.readManifest(projectID); err != nil {
		return Task{}, err
	}

	var t Task
	err := s.withProjectLock(projectID, func() error {
		n, err := s.bumpSequence(projectID)
		if err != nil {
			return err
		}
		pos, err := s.maxPosition(projectID, status)
		if err != nil {
			return err
		}
		t = Task{
			ID:          TaskID(strconv.FormatUint(n, 10)),
			Title:       title,
			Description: strings.TrimSpace(description),
			Tags:        tags,
			Position:    pos + 1,
			ProjectID:   projectID,
			Status:      status,
		}
		return s.writeTask(projectID, status, t)
	})
	if err != nil {
		return Task{}, err
	}
	return t, nil
}

// maxPosition returns the highest Position currently used in a status folder
// or 0 if the folder is empty. Caller must hold the project lock.
func (s *Store) maxPosition(projectID ProjectID, status Status) (int, error) {
	tasks, err := s.listInStatus(projectID, status)
	if err != nil {
		return 0, err
	}
	max := 0
	for _, t := range tasks {
		if t.Position > max {
			max = t.Position
		}
	}
	return max, nil
}

func (s *Store) writeTask(projectID ProjectID, status Status, t Task) error {
	if err := os.MkdirAll(s.statusDir(projectID, status), 0o755); err != nil {
		return fmt.Errorf("creating status dir: %w", err)
	}
	data, err := json.MarshalIndent(t, "", "  ")
	if err != nil {
		return fmt.Errorf("encoding task: %w", err)
	}
	return atomicWrite(s.taskPath(projectID, status, t.ID), data)
}

func (s *Store) GetTask(projectID ProjectID, id TaskID) (Task, error) {
	if _, err := s.readManifest(projectID); err != nil {
		return Task{}, err
	}
	return s.findTask(projectID, id)
}

// findTask locates a task across status folders without consulting the
// manifest or holding any lock. Used both by GetTask and by mutators that
// already hold the project lock.
func (s *Store) findTask(projectID ProjectID, id TaskID) (Task, error) {
	for _, st := range Statuses {
		path := s.taskPath(projectID, st, id)
		_, err := os.Stat(path)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return Task{}, fmt.Errorf("stat task: %w", err)
		}
		t, err := readTaskFile(path)
		if err != nil {
			return Task{}, err
		}
		t.ProjectID = projectID
		t.Status = st
		comments, err := s.readComments(projectID, st, id)
		if err != nil {
			return Task{}, err
		}
		t.Comments = comments
		return t, nil
	}
	return Task{}, ErrTaskNotFound
}

// ListTasksByProject returns every task across every status. Tasks are
// grouped by the canonical Statuses order and, within each status, sorted by
// Position (then by id as a tiebreaker).
func (s *Store) ListTasksByProject(projectID ProjectID) ([]Task, error) {
	if _, err := s.readManifest(projectID); err != nil {
		return nil, err
	}
	out := make([]Task, 0)
	for _, st := range Statuses {
		ts, err := s.listInStatus(projectID, st)
		if err != nil {
			return nil, err
		}
		out = append(out, ts...)
	}
	return out, nil
}

func (s *Store) listInStatus(projectID ProjectID, status Status) ([]Task, error) {
	dir := s.statusDir(projectID, status)
	entries, err := os.ReadDir(dir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("reading status dir %s: %w", status, err)
	}
	out := make([]Task, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if !strings.HasSuffix(name, ".json") || strings.HasSuffix(name, ".comments.json") {
			continue
		}
		t, err := readTaskFile(filepath.Join(dir, name))
		if err != nil {
			return nil, err
		}
		t.ProjectID = projectID
		t.Status = status
		comments, err := s.readComments(projectID, status, t.ID)
		if err != nil {
			return nil, err
		}
		t.Comments = comments
		out = append(out, t)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Position != out[j].Position {
			return out[i].Position < out[j].Position
		}
		return idLess(out[i].ID, out[j].ID)
	})
	return out, nil
}

func readTaskFile(path string) (Task, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Task{}, fmt.Errorf("reading task %s: %w", path, err)
	}
	var t Task
	if err := json.Unmarshal(data, &t); err != nil {
		return Task{}, fmt.Errorf("parsing task %s: %w", path, err)
	}
	return t, nil
}

func (s *Store) UpdateTask(projectID ProjectID, id TaskID, title, description string, status Status, tags []string) (Task, error) {
	title = strings.TrimSpace(title)
	if title == "" {
		return Task{}, ErrInvalidTaskTitle
	}
	if !isKnownStatus(status) {
		return Task{}, ErrInvalidStatus
	}
	if _, err := s.readManifest(projectID); err != nil {
		return Task{}, err
	}

	var result Task
	err := s.withProjectLock(projectID, func() error {
		cur, err := s.findTask(projectID, id)
		if err != nil {
			return err
		}
		if status != cur.Status {
			moved, err := s.relocateTask(projectID, id, cur, status)
			if err != nil {
				return err
			}
			cur = moved
		}
		cur.Title = title
		cur.Description = strings.TrimSpace(description)
		cur.Tags = tags
		if err := s.writeTask(projectID, cur.Status, cur); err != nil {
			return err
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
	if _, err := s.readManifest(projectID); err != nil {
		return Task{}, err
	}
	var result Task
	err := s.withProjectLock(projectID, func() error {
		cur, err := s.findTask(projectID, id)
		if err != nil {
			return err
		}
		moved, err := s.relocateTask(projectID, id, cur, status)
		if err != nil {
			return err
		}
		result = moved
		return nil
	})
	return result, err
}

// relocateTask moves a task between status folders, assigning it a fresh
// position at the bottom of the destination column. The caller must hold the
// project lock and must have already verified the task currently lives at
// cur.Status. A no-op when source and target match.
func (s *Store) relocateTask(projectID ProjectID, id TaskID, cur Task, status Status) (Task, error) {
	if cur.Status == status {
		return cur, nil
	}
	fromStatus := cur.Status
	pos, err := s.maxPosition(projectID, status)
	if err != nil {
		return Task{}, err
	}
	if err := os.MkdirAll(s.statusDir(projectID, status), 0o755); err != nil {
		return Task{}, fmt.Errorf("creating destination status dir: %w", err)
	}
	cur.Status = status
	cur.Position = pos + 1
	if err := s.writeTask(projectID, status, cur); err != nil {
		return Task{}, err
	}
	if err := os.Remove(s.taskPath(projectID, fromStatus, id)); err != nil {
		return Task{}, fmt.Errorf("removing old task file: %w", err)
	}
	oldComments := s.commentsPath(projectID, fromStatus, id)
	if _, err := os.Stat(oldComments); err == nil {
		if err := os.Rename(oldComments, s.commentsPath(projectID, status, id)); err != nil {
			return Task{}, fmt.Errorf("moving comments file: %w", err)
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return Task{}, fmt.Errorf("checking comments file: %w", err)
	}
	return cur, nil
}

func (s *Store) AddComment(projectID ProjectID, id TaskID, body string) (Task, error) {
	body = strings.TrimSpace(body)
	if body == "" {
		return Task{}, ErrEmptyComment
	}
	if _, err := s.readManifest(projectID); err != nil {
		return Task{}, err
	}
	var result Task
	err := s.withProjectLock(projectID, func() error {
		t, err := s.findTask(projectID, id)
		if err != nil {
			return err
		}
		comments := append([]string(nil), t.Comments...)
		comments = append(comments, body)
		if err := s.writeComments(projectID, t.Status, id, comments); err != nil {
			return err
		}
		t.Comments = comments
		result = t
		return nil
	})
	return result, err
}

// DeleteTask removes a task and its comments file. Errors with ErrTaskNotFound
// if the task is not present.
func (s *Store) DeleteTask(projectID ProjectID, id TaskID) error {
	if _, err := s.readManifest(projectID); err != nil {
		return err
	}
	return s.withProjectLock(projectID, func() error {
		cur, err := s.findTask(projectID, id)
		if err != nil {
			return err
		}
		if err := os.Remove(s.taskPath(projectID, cur.Status, id)); err != nil {
			return fmt.Errorf("removing task file: %w", err)
		}
		commentsPath := s.commentsPath(projectID, cur.Status, id)
		if err := os.Remove(commentsPath); err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("removing comments file: %w", err)
		}
		return nil
	})
}

// ReorderColumn rewrites the Position field of every task in a status folder
// according to the order of ids. The provided slice must list exactly the
// tasks currently in the column; any mismatch returns an error so a stale
// client cannot overwrite a concurrent move.
func (s *Store) ReorderColumn(projectID ProjectID, status Status, ids []TaskID) error {
	if !isKnownStatus(status) {
		return ErrInvalidStatus
	}
	if _, err := s.readManifest(projectID); err != nil {
		return err
	}
	return s.withProjectLock(projectID, func() error {
		existing, err := s.listInStatus(projectID, status)
		if err != nil {
			return err
		}
		if len(existing) != len(ids) {
			return fmt.Errorf("reorder: column has %d tasks, got %d ids", len(existing), len(ids))
		}
		index := make(map[TaskID]Task, len(existing))
		for _, t := range existing {
			index[t.ID] = t
		}
		for _, id := range ids {
			if _, ok := index[id]; !ok {
				return fmt.Errorf("reorder: task %s not in status %s", id, status)
			}
		}
		seen := make(map[TaskID]struct{}, len(ids))
		for _, id := range ids {
			if _, dup := seen[id]; dup {
				return fmt.Errorf("reorder: duplicate task %s", id)
			}
			seen[id] = struct{}{}
		}
		for i, id := range ids {
			t := index[id]
			t.Position = i + 1
			if err := s.writeTask(projectID, status, t); err != nil {
				return err
			}
		}
		return nil
	})
}

func (s *Store) readComments(projectID ProjectID, status Status, id TaskID) ([]string, error) {
	data, err := os.ReadFile(s.commentsPath(projectID, status, id))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("reading comments: %w", err)
	}
	var out []string
	if err := json.Unmarshal(data, &out); err != nil {
		return nil, fmt.Errorf("parsing comments: %w", err)
	}
	return out, nil
}

func (s *Store) writeComments(projectID ProjectID, status Status, id TaskID, comments []string) error {
	if comments == nil {
		comments = []string{}
	}
	data, err := json.MarshalIndent(comments, "", "  ")
	if err != nil {
		return fmt.Errorf("encoding comments: %w", err)
	}
	return atomicWrite(s.commentsPath(projectID, status, id), data)
}

func isKnownStatus(s Status) bool {
	for _, k := range Statuses {
		if s == k {
			return true
		}
	}
	return false
}

func atomicWrite(path string, data []byte) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("creating dir: %w", err)
	}
	tmp, err := os.CreateTemp(dir, ".tmp-*")
	if err != nil {
		return fmt.Errorf("creating temp file: %w", err)
	}
	tmpName := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return fmt.Errorf("writing temp file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return fmt.Errorf("closing temp file: %w", err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		os.Remove(tmpName)
		return fmt.Errorf("renaming temp file: %w", err)
	}
	return nil
}

func idLess(a, b TaskID) bool {
	ai, aerr := strconv.ParseUint(string(a), 10, 64)
	bi, berr := strconv.ParseUint(string(b), 10, 64)
	if aerr == nil && berr == nil {
		return ai < bi
	}
	return string(a) < string(b)
}

// validateProjectName enforces the input rules for project names:
// lowercase ASCII letters, digits, '-', '_', and single spaces between
// words. Leading/trailing whitespace and consecutive spaces are rejected.
// Length must be 1..50 runes.
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

// nameToSlug converts a validated project name into a directory-safe slug.
func nameToSlug(name string) string {
	return strings.ReplaceAll(name, " ", "-")
}

// scanNameToSlug converts an arbitrary folder name into a project ID slug:
// lowercases, keeps [a-z0-9-_], collapses runs of other chars into a single
// '-', and trims leading/trailing dashes.
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
