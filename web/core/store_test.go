package core_test

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"

	"github.com/qbart/gaia/web/core"
)

func newTempStore(t *testing.T) *core.Store {
	t.Helper()
	return core.NewStore(t.TempDir())
}

func mustCreateProject(t *testing.T, s *core.Store, name string) core.Project {
	t.Helper()
	p, err := s.CreateProject(name)
	if err != nil {
		t.Fatalf("CreateProject(%q): %v", name, err)
	}
	return p
}

func TestNextSequence_StartsAtOne(t *testing.T) {
	s := newTempStore(t)
	p := mustCreateProject(t, s, "gaia")

	n, err := s.NextSequence(p.ID)
	if err != nil {
		t.Fatalf("NextSequence: %v", err)
	}
	if n != 1 {
		t.Fatalf("first sequence = %d, want 1", n)
	}
}

func TestNextSequence_Increments(t *testing.T) {
	s := newTempStore(t)
	p := mustCreateProject(t, s, "gaia")

	for want := uint64(1); want <= 5; want++ {
		got, err := s.NextSequence(p.ID)
		if err != nil {
			t.Fatalf("NextSequence: %v", err)
		}
		if got != want {
			t.Fatalf("got %d, want %d", got, want)
		}
	}
}

func TestNextSequence_PersistsAcrossInstances(t *testing.T) {
	root := t.TempDir()
	s1 := core.NewStore(root)
	p := mustCreateProject(t, s1, "gaia")
	for i := 0; i < 3; i++ {
		if _, err := s1.NextSequence(p.ID); err != nil {
			t.Fatalf("NextSequence: %v", err)
		}
	}

	s2 := core.NewStore(root)
	got, err := s2.NextSequence(p.ID)
	if err != nil {
		t.Fatalf("NextSequence on fresh store: %v", err)
	}
	if got != 4 {
		t.Fatalf("got %d, want 4", got)
	}
}

func TestNextSequence_Concurrent_Unique(t *testing.T) {
	s := newTempStore(t)
	p := mustCreateProject(t, s, "gaia")

	const N = 50
	var wg sync.WaitGroup
	results := make([]uint64, N)
	wg.Add(N)
	for i := 0; i < N; i++ {
		go func(i int) {
			defer wg.Done()
			n, err := s.NextSequence(p.ID)
			if err != nil {
				t.Errorf("NextSequence: %v", err)
				return
			}
			results[i] = n
		}(i)
	}
	wg.Wait()

	sort.Slice(results, func(i, j int) bool { return results[i] < results[j] })
	for i, v := range results {
		if v != uint64(i+1) {
			t.Fatalf("results[%d] = %d, want %d (got %v)", i, v, i+1, results)
		}
	}
}

func TestCreateProject_WritesManifest(t *testing.T) {
	s := newTempStore(t)
	p, err := s.CreateProject("gaia")
	if err != nil {
		t.Fatalf("CreateProject: %v", err)
	}
	if p.ID == "" {
		t.Fatalf("expected non-empty id")
	}
	if p.Name != "gaia" {
		t.Fatalf("name = %q, want Gaia", p.Name)
	}

	manifestPath := filepath.Join(s.Root, string(p.ID), "manifest.json")
	data, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatalf("read manifest: %v", err)
	}
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("unmarshal manifest: %v", err)
	}
	if m["name"] != "gaia" {
		t.Fatalf("manifest name = %v, want Gaia", m["name"])
	}

	seqPath := filepath.Join(s.Root, string(p.ID), "sequence")
	if data, err := os.ReadFile(seqPath); err != nil {
		t.Fatalf("read sequence: %v", err)
	} else if string(data) != "0" {
		t.Fatalf("initial sequence = %q, want 0", string(data))
	}

	for _, st := range core.Statuses {
		dir := filepath.Join(s.Root, string(p.ID), "tasks", string(st))
		info, err := os.Stat(dir)
		if err != nil {
			t.Fatalf("stat status dir %s: %v", st, err)
		}
		if !info.IsDir() {
			t.Fatalf("%s is not a dir", dir)
		}
	}
}

func TestCreateProject_EmptyName(t *testing.T) {
	s := newTempStore(t)
	_, err := s.CreateProject("   ")
	if !errors.Is(err, core.ErrInvalidProjectName) {
		t.Fatalf("err = %v, want ErrInvalidProjectName", err)
	}
}

func TestCreateProject_DedupesSlug(t *testing.T) {
	s := newTempStore(t)
	a, err := s.CreateProject("gaia")
	if err != nil {
		t.Fatalf("CreateProject a: %v", err)
	}
	b, err := s.CreateProject("gaia")
	if err != nil {
		t.Fatalf("CreateProject b: %v", err)
	}
	if a.ID == b.ID {
		t.Fatalf("duplicate ids: %s", a.ID)
	}
}

func TestListProjects_SortedByID(t *testing.T) {
	s := newTempStore(t)
	mustCreateProject(t, s, "zen")
	mustCreateProject(t, s, "atlas")
	mustCreateProject(t, s, "gaia")

	list, err := s.ListProjects()
	if err != nil {
		t.Fatalf("ListProjects: %v", err)
	}
	if len(list) != 3 {
		t.Fatalf("len = %d, want 3", len(list))
	}
	want := []core.ProjectID{"atlas", "gaia", "zen"}
	for i, p := range list {
		if p.ID != want[i] {
			t.Fatalf("list[%d].ID = %q, want %q", i, p.ID, want[i])
		}
	}
}

func TestListProjects_IgnoresUnrelatedDirs(t *testing.T) {
	s := newTempStore(t)
	mustCreateProject(t, s, "gaia")
	if err := os.MkdirAll(filepath.Join(s.Root, "garbage"), 0o755); err != nil {
		t.Fatalf("mkdir garbage: %v", err)
	}
	list, err := s.ListProjects()
	if err != nil {
		t.Fatalf("ListProjects: %v", err)
	}
	if len(list) != 1 || list[0].ID != "gaia" {
		t.Fatalf("list = %+v", list)
	}
}

func TestGetProject_NotFound(t *testing.T) {
	s := newTempStore(t)
	_, err := s.GetProject("missing")
	if !errors.Is(err, core.ErrProjectNotFound) {
		t.Fatalf("err = %v, want ErrProjectNotFound", err)
	}
}

func TestGetProject_Found(t *testing.T) {
	s := newTempStore(t)
	p := mustCreateProject(t, s, "gaia")
	got, err := s.GetProject(p.ID)
	if err != nil {
		t.Fatalf("GetProject: %v", err)
	}
	if got.Name != "gaia" {
		t.Fatalf("name = %q", got.Name)
	}
}

func TestCreateTask_WritesFileAndAssignsID(t *testing.T) {
	s := newTempStore(t)
	p := mustCreateProject(t, s, "gaia")

	task, err := s.CreateTask(p.ID, core.StatusTodo, "implement core", "Body text", []string{"core"})
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	if task.ID != "1" {
		t.Fatalf("ID = %q, want 1", task.ID)
	}
	if task.Title != "implement core" || task.Description != "Body text" {
		t.Fatalf("task fields wrong: %+v", task)
	}
	if task.Status != core.StatusTodo {
		t.Fatalf("status = %q", task.Status)
	}

	path := filepath.Join(s.Root, string(p.ID), "tasks", string(core.StatusTodo), "1.json")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read task file: %v", err)
	}
	var onDisk struct {
		ID          string   `json:"id"`
		Title       string   `json:"title"`
		Description string   `json:"description"`
		Tags        []string `json:"tags"`
	}
	if err := json.Unmarshal(data, &onDisk); err != nil {
		t.Fatalf("unmarshal task: %v", err)
	}
	if onDisk.Title != "implement core" || onDisk.Description != "Body text" {
		t.Fatalf("on-disk fields wrong: %+v", onDisk)
	}
	if onDisk.ID != "1" {
		t.Fatalf("on-disk id = %q", onDisk.ID)
	}
	if len(onDisk.Tags) != 1 || onDisk.Tags[0] != "core" {
		t.Fatalf("on-disk tags = %v", onDisk.Tags)
	}

	next, err := s.CreateTask(p.ID, core.StatusTodo, "Second", "", nil)
	if err != nil {
		t.Fatalf("CreateTask 2: %v", err)
	}
	if next.ID != "2" {
		t.Fatalf("ID2 = %q, want 2", next.ID)
	}
}

func TestCreateTask_EmptyTitle(t *testing.T) {
	s := newTempStore(t)
	p := mustCreateProject(t, s, "gaia")
	_, err := s.CreateTask(p.ID, core.StatusTodo, "  ", "", nil)
	if !errors.Is(err, core.ErrInvalidTaskTitle) {
		t.Fatalf("err = %v", err)
	}
}

func TestCreateTask_InvalidStatus(t *testing.T) {
	s := newTempStore(t)
	p := mustCreateProject(t, s, "gaia")
	_, err := s.CreateTask(p.ID, core.Status("bogus"), "x", "", nil)
	if !errors.Is(err, core.ErrInvalidStatus) {
		t.Fatalf("err = %v", err)
	}
}

func TestCreateTask_ProjectMissing(t *testing.T) {
	s := newTempStore(t)
	_, err := s.CreateTask("missing", core.StatusTodo, "x", "", nil)
	if !errors.Is(err, core.ErrProjectNotFound) {
		t.Fatalf("err = %v", err)
	}
}

func TestGetTask_FindsAcrossStatuses(t *testing.T) {
	s := newTempStore(t)
	p := mustCreateProject(t, s, "gaia")
	a, _ := s.CreateTask(p.ID, core.StatusTodo, "A", "", nil)
	b, _ := s.CreateTask(p.ID, core.StatusDone, "B", "", nil)
	c, _ := s.CreateTask(p.ID, core.StatusArchive, "C", "", nil)

	for _, want := range []core.Task{a, b, c} {
		got, err := s.GetTask(p.ID, want.ID)
		if err != nil {
			t.Fatalf("GetTask %s: %v", want.ID, err)
		}
		if got.Title != want.Title || got.Status != want.Status {
			t.Fatalf("got = %+v, want title=%q status=%q", got, want.Title, want.Status)
		}
	}
}

func TestGetTask_NotFound(t *testing.T) {
	s := newTempStore(t)
	p := mustCreateProject(t, s, "gaia")
	_, err := s.GetTask(p.ID, "999")
	if !errors.Is(err, core.ErrTaskNotFound) {
		t.Fatalf("err = %v", err)
	}
}

func TestListTasksByProject_AcrossStatuses(t *testing.T) {
	s := newTempStore(t)
	p := mustCreateProject(t, s, "gaia")
	_, _ = s.CreateTask(p.ID, core.StatusTodo, "A", "", nil)    // id 1, StatusTodo
	_, _ = s.CreateTask(p.ID, core.StatusDone, "B", "", nil)    // id 2, StatusDone
	_, _ = s.CreateTask(p.ID, core.StatusDocs, "C", "", nil)    // id 3, StatusDocs
	_, _ = s.CreateTask(p.ID, core.StatusArchive, "D", "", nil) // id 4, StatusArchive

	tasks, err := s.ListTasksByProject(p.ID)
	if err != nil {
		t.Fatalf("ListTasksByProject: %v", err)
	}
	if len(tasks) != 4 {
		t.Fatalf("len = %d, want 4 (%v)", len(tasks), tasks)
	}
	// canonical Statuses order: docs, brainstorm, todo, doing, review, rejected, done, archive
	for i, want := range []core.TaskID{"3", "1", "2", "4"} {
		if tasks[i].ID != want {
			t.Fatalf("tasks[%d].ID = %q, want %q", i, tasks[i].ID, want)
		}
	}
}

func TestUpdateTask_PersistsEdits(t *testing.T) {
	s := newTempStore(t)
	p := mustCreateProject(t, s, "gaia")
	t1, _ := s.CreateTask(p.ID, core.StatusTodo, "A", "old", []string{"x"})

	got, err := s.UpdateTask(p.ID, t1.ID, "A2", "new", core.StatusTodo, []string{"y"})
	if err != nil {
		t.Fatalf("UpdateTask: %v", err)
	}
	if got.Title != "A2" || got.Description != "new" {
		t.Fatalf("returned: %+v", got)
	}

	reload, err := s.GetTask(p.ID, t1.ID)
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	if reload.Title != "A2" || reload.Description != "new" || len(reload.Tags) != 1 || reload.Tags[0] != "y" {
		t.Fatalf("reloaded: %+v", reload)
	}
}

func TestUpdateTask_StatusChangeMovesFile(t *testing.T) {
	s := newTempStore(t)
	p := mustCreateProject(t, s, "gaia")
	t1, _ := s.CreateTask(p.ID, core.StatusTodo, "A", "", nil)

	got, err := s.UpdateTask(p.ID, t1.ID, "A2", "", core.StatusDoing, nil)
	if err != nil {
		t.Fatalf("UpdateTask: %v", err)
	}
	if got.Status != core.StatusDoing {
		t.Fatalf("status = %q", got.Status)
	}

	oldPath := filepath.Join(s.Root, string(p.ID), "tasks", string(core.StatusTodo), "1.json")
	if _, err := os.Stat(oldPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("old file still present: err=%v", err)
	}
	newPath := filepath.Join(s.Root, string(p.ID), "tasks", string(core.StatusDoing), "1.json")
	if _, err := os.Stat(newPath); err != nil {
		t.Fatalf("new file missing: %v", err)
	}
}

func TestMoveTask_BringsCommentsAlong(t *testing.T) {
	s := newTempStore(t)
	p := mustCreateProject(t, s, "gaia")
	t1, _ := s.CreateTask(p.ID, core.StatusTodo, "A", "", nil)
	if _, err := s.AddComment(p.ID, t1.ID, "first"); err != nil {
		t.Fatalf("AddComment: %v", err)
	}

	if _, err := s.MoveTask(p.ID, t1.ID, core.StatusDoing); err != nil {
		t.Fatalf("MoveTask: %v", err)
	}

	oldComments := filepath.Join(s.Root, string(p.ID), "tasks", string(core.StatusTodo), "1.comments.json")
	if _, err := os.Stat(oldComments); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("old comments still present: err=%v", err)
	}
	newComments := filepath.Join(s.Root, string(p.ID), "tasks", string(core.StatusDoing), "1.comments.json")
	if _, err := os.Stat(newComments); err != nil {
		t.Fatalf("new comments missing: %v", err)
	}

	got, err := s.GetTask(p.ID, t1.ID)
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	if len(got.Comments) != 1 || got.Comments[0] != "first" {
		t.Fatalf("comments = %v", got.Comments)
	}
}

func TestMoveTask_SameStatusNoOp(t *testing.T) {
	s := newTempStore(t)
	p := mustCreateProject(t, s, "gaia")
	t1, _ := s.CreateTask(p.ID, core.StatusTodo, "A", "", nil)

	got, err := s.MoveTask(p.ID, t1.ID, core.StatusTodo)
	if err != nil {
		t.Fatalf("MoveTask: %v", err)
	}
	if got.Status != core.StatusTodo {
		t.Fatalf("status = %q", got.Status)
	}
}

func TestMoveTask_InvalidStatus(t *testing.T) {
	s := newTempStore(t)
	p := mustCreateProject(t, s, "gaia")
	t1, _ := s.CreateTask(p.ID, core.StatusTodo, "A", "", nil)
	_, err := s.MoveTask(p.ID, t1.ID, core.Status("bogus"))
	if !errors.Is(err, core.ErrInvalidStatus) {
		t.Fatalf("err = %v", err)
	}
}

func TestMoveTask_NotFound(t *testing.T) {
	s := newTempStore(t)
	p := mustCreateProject(t, s, "gaia")
	_, err := s.MoveTask(p.ID, "42", core.StatusDoing)
	if !errors.Is(err, core.ErrTaskNotFound) {
		t.Fatalf("err = %v", err)
	}
}

func TestAddComment_AppendsAndPersists(t *testing.T) {
	s := newTempStore(t)
	p := mustCreateProject(t, s, "gaia")
	t1, _ := s.CreateTask(p.ID, core.StatusTodo, "A", "", nil)

	if _, err := s.AddComment(p.ID, t1.ID, "first"); err != nil {
		t.Fatalf("AddComment 1: %v", err)
	}
	if _, err := s.AddComment(p.ID, t1.ID, "second"); err != nil {
		t.Fatalf("AddComment 2: %v", err)
	}

	got, err := s.GetTask(p.ID, t1.ID)
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	if len(got.Comments) != 2 || got.Comments[0] != "first" || got.Comments[1] != "second" {
		t.Fatalf("comments = %v", got.Comments)
	}

	cmtPath := filepath.Join(s.Root, string(p.ID), "tasks", string(core.StatusTodo), "1.comments.json")
	data, err := os.ReadFile(cmtPath)
	if err != nil {
		t.Fatalf("read comments file: %v", err)
	}
	var onDisk []string
	if err := json.Unmarshal(data, &onDisk); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(onDisk) != 2 {
		t.Fatalf("on-disk len = %d", len(onDisk))
	}
}

func TestAddComment_Empty(t *testing.T) {
	s := newTempStore(t)
	p := mustCreateProject(t, s, "gaia")
	t1, _ := s.CreateTask(p.ID, core.StatusTodo, "A", "", nil)
	_, err := s.AddComment(p.ID, t1.ID, "  \n ")
	if !errors.Is(err, core.ErrEmptyComment) {
		t.Fatalf("err = %v", err)
	}
}

func TestAddComment_TaskNotFound(t *testing.T) {
	s := newTempStore(t)
	p := mustCreateProject(t, s, "gaia")
	_, err := s.AddComment(p.ID, "999", "hello")
	if !errors.Is(err, core.ErrTaskNotFound) {
		t.Fatalf("err = %v", err)
	}
}

func TestCreateTask_Concurrent_UniqueIDs(t *testing.T) {
	s := newTempStore(t)
	p := mustCreateProject(t, s, "gaia")

	const N = 50
	var wg sync.WaitGroup
	ids := make([]core.TaskID, N)
	errs := make([]error, N)
	wg.Add(N)
	for i := 0; i < N; i++ {
		go func(i int) {
			defer wg.Done()
			task, err := s.CreateTask(p.ID, core.StatusTodo, "T", "", nil)
			ids[i] = task.ID
			errs[i] = err
		}(i)
	}
	wg.Wait()

	seen := make(map[core.TaskID]struct{}, N)
	for i, err := range errs {
		if err != nil {
			t.Fatalf("CreateTask[%d]: %v", i, err)
		}
		if _, dup := seen[ids[i]]; dup {
			t.Fatalf("duplicate id %q at index %d (all=%v)", ids[i], i, ids)
		}
		seen[ids[i]] = struct{}{}
	}
	if len(seen) != N {
		t.Fatalf("got %d unique ids, want %d", len(seen), N)
	}

	tasks, err := s.ListTasksByProject(p.ID)
	if err != nil {
		t.Fatalf("ListTasksByProject: %v", err)
	}
	if len(tasks) != N {
		t.Fatalf("on-disk task count = %d, want %d", len(tasks), N)
	}
}

func TestAddComment_Concurrent_NoLostUpdates(t *testing.T) {
	s := newTempStore(t)
	p := mustCreateProject(t, s, "gaia")
	t1, err := s.CreateTask(p.ID, core.StatusTodo, "A", "", nil)
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}

	const N = 30
	var wg sync.WaitGroup
	wg.Add(N)
	for i := 0; i < N; i++ {
		go func(i int) {
			defer wg.Done()
			if _, err := s.AddComment(p.ID, t1.ID, "c"); err != nil {
				t.Errorf("AddComment[%d]: %v", i, err)
			}
		}(i)
	}
	wg.Wait()

	got, err := s.GetTask(p.ID, t1.ID)
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	if len(got.Comments) != N {
		t.Fatalf("comments = %d, want %d", len(got.Comments), N)
	}
}

func TestProjectLockFile_Created(t *testing.T) {
	s := newTempStore(t)
	p := mustCreateProject(t, s, "gaia")
	if _, err := s.CreateTask(p.ID, core.StatusTodo, "A", "", nil); err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	lockPath := filepath.Join(s.Root, string(p.ID), ".lock")
	if _, err := os.Stat(lockPath); err != nil {
		t.Fatalf("expected project .lock at %s: %v", lockPath, err)
	}
}

func TestValidateProjectName(t *testing.T) {
	s := newTempStore(t)
	good := []string{
		"gaia",
		"gaia 2",
		"my project",
		"a_b-c",
		"abc 123",
		"x",
	}
	bad := []string{
		"",
		"   ",
		"Gaia",
		"GAIA",
		"hello world!",
		"hello  world",
		" leading",
		"trailing ",
		"emoji ✨",
		strings.Repeat("a", 51),
	}
	for _, name := range good {
		if _, err := s.CreateProject(name); err != nil {
			t.Errorf("CreateProject(%q) rejected unexpectedly: %v", name, err)
		}
	}
	for _, name := range bad {
		if _, err := s.CreateProject(name); !errors.Is(err, core.ErrInvalidProjectName) {
			t.Errorf("CreateProject(%q) = %v, want ErrInvalidProjectName", name, err)
		}
	}
}

func TestCreateProject_SlugReplacesSpaces(t *testing.T) {
	s := newTempStore(t)
	p, err := s.CreateProject("my project")
	if err != nil {
		t.Fatalf("CreateProject: %v", err)
	}
	if p.ID != "my-project" {
		t.Fatalf("id = %q, want my-project", p.ID)
	}
	if p.Name != "my project" {
		t.Fatalf("name = %q", p.Name)
	}
}

func TestCreateTask_AssignsPositionAtEnd(t *testing.T) {
	s := newTempStore(t)
	p := mustCreateProject(t, s, "gaia")

	a, _ := s.CreateTask(p.ID, core.StatusTodo, "A", "", nil)
	b, _ := s.CreateTask(p.ID, core.StatusTodo, "B", "", nil)
	c, _ := s.CreateTask(p.ID, core.StatusDone, "C", "", nil)

	if a.Position != 1 || b.Position != 2 {
		t.Fatalf("todo positions = %d,%d", a.Position, b.Position)
	}
	if c.Position != 1 {
		t.Fatalf("done position = %d, want 1", c.Position)
	}
}

func TestListInStatus_SortedByPosition(t *testing.T) {
	s := newTempStore(t)
	p := mustCreateProject(t, s, "gaia")
	first, _ := s.CreateTask(p.ID, core.StatusTodo, "first", "", nil)
	second, _ := s.CreateTask(p.ID, core.StatusTodo, "second", "", nil)
	third, _ := s.CreateTask(p.ID, core.StatusTodo, "third", "", nil)

	if err := s.ReorderColumn(p.ID, core.StatusTodo, []core.TaskID{third.ID, first.ID, second.ID}); err != nil {
		t.Fatalf("ReorderColumn: %v", err)
	}

	tasks, err := s.ListTasksByProject(p.ID)
	if err != nil {
		t.Fatalf("ListTasksByProject: %v", err)
	}
	gotOrder := []core.TaskID{tasks[0].ID, tasks[1].ID, tasks[2].ID}
	wantOrder := []core.TaskID{third.ID, first.ID, second.ID}
	for i := range gotOrder {
		if gotOrder[i] != wantOrder[i] {
			t.Fatalf("order = %v, want %v", gotOrder, wantOrder)
		}
	}
	for i, tid := range wantOrder {
		got, _ := s.GetTask(p.ID, tid)
		if got.Position != i+1 {
			t.Fatalf("%s position = %d, want %d", tid, got.Position, i+1)
		}
	}
}

func TestReorderColumn_RejectsMismatchedSet(t *testing.T) {
	s := newTempStore(t)
	p := mustCreateProject(t, s, "gaia")
	a, _ := s.CreateTask(p.ID, core.StatusTodo, "a", "", nil)
	b, _ := s.CreateTask(p.ID, core.StatusTodo, "b", "", nil)

	if err := s.ReorderColumn(p.ID, core.StatusTodo, []core.TaskID{a.ID}); err == nil {
		t.Fatalf("expected error on mismatched len")
	}
	if err := s.ReorderColumn(p.ID, core.StatusTodo, []core.TaskID{a.ID, "999"}); err == nil {
		t.Fatalf("expected error on unknown id")
	}
	if err := s.ReorderColumn(p.ID, core.StatusTodo, []core.TaskID{a.ID, a.ID}); err == nil {
		t.Fatalf("expected error on duplicate id")
	}
	_ = b
}

func TestRelocateTask_AssignsPositionAtEndOfTarget(t *testing.T) {
	s := newTempStore(t)
	p := mustCreateProject(t, s, "gaia")
	_, _ = s.CreateTask(p.ID, core.StatusDone, "x", "", nil)
	_, _ = s.CreateTask(p.ID, core.StatusDone, "y", "", nil)
	src, _ := s.CreateTask(p.ID, core.StatusTodo, "src", "", nil)

	moved, err := s.MoveTask(p.ID, src.ID, core.StatusDone)
	if err != nil {
		t.Fatalf("MoveTask: %v", err)
	}
	if moved.Position != 3 {
		t.Fatalf("moved position = %d, want 3", moved.Position)
	}
}

func TestDeleteTask_RemovesFiles(t *testing.T) {
	s := newTempStore(t)
	p := mustCreateProject(t, s, "gaia")
	t1, _ := s.CreateTask(p.ID, core.StatusTodo, "a", "", nil)
	if _, err := s.AddComment(p.ID, t1.ID, "c"); err != nil {
		t.Fatalf("AddComment: %v", err)
	}

	if err := s.DeleteTask(p.ID, t1.ID); err != nil {
		t.Fatalf("DeleteTask: %v", err)
	}

	if _, err := os.Stat(filepath.Join(s.Root, string(p.ID), "tasks", string(core.StatusTodo), "1.json")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("task file still present: %v", err)
	}
	if _, err := os.Stat(filepath.Join(s.Root, string(p.ID), "tasks", string(core.StatusTodo), "1.comments.json")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("comments file still present: %v", err)
	}
	if _, err := s.GetTask(p.ID, t1.ID); !errors.Is(err, core.ErrTaskNotFound) {
		t.Fatalf("GetTask after delete: %v", err)
	}
}

func TestDeleteTask_NotFound(t *testing.T) {
	s := newTempStore(t)
	p := mustCreateProject(t, s, "gaia")
	if err := s.DeleteTask(p.ID, "999"); !errors.Is(err, core.ErrTaskNotFound) {
		t.Fatalf("err = %v", err)
	}
}
