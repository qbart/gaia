package core_test

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/SoftKiwiGames/zen/zen/sqlite"
	"github.com/qbart/gaia/web/core"
)

func newStoreAt(t *testing.T, path string) *core.Store {
	t.Helper()
	db := sqlite.NewSQLite(path, core.Migrations())
	ctx := context.Background()
	if err := db.Connect(ctx); err != nil {
		t.Fatalf("connect: %v", err)
	}
	if err := db.MigrateWithGoose(ctx, core.MigrationsDir); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	t.Cleanup(func() { db.Close(ctx) })
	return core.NewStore(db)
}

func newTempStore(t *testing.T) *core.Store {
	t.Helper()
	return newStoreAt(t, filepath.Join(t.TempDir(), "test.db"))
}

func mustCreateProject(t *testing.T, s *core.Store, name string) core.Project {
	t.Helper()
	p, err := s.CreateProject(name)
	if err != nil {
		t.Fatalf("CreateProject(%q): %v", name, err)
	}
	return p
}

func TestCreateProject_PersistsAndAssignsSlug(t *testing.T) {
	s := newTempStore(t)
	p, err := s.CreateProject("gaia")
	if err != nil {
		t.Fatalf("CreateProject: %v", err)
	}
	if p.ID <= 0 {
		t.Fatalf("id = %d, want > 0", p.ID)
	}
	if p.Slug != "gaia" || p.Name != "gaia" {
		t.Fatalf("project = %+v", p)
	}
	got, err := s.GetProject(p.ID)
	if err != nil {
		t.Fatalf("GetProject: %v", err)
	}
	if got.Slug != "gaia" || got.Name != "gaia" {
		t.Fatalf("reloaded = %+v", got)
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
		t.Fatalf("duplicate ids: %d", a.ID)
	}
	if b.Slug != "gaia-2" {
		t.Fatalf("second slug = %q, want gaia-2", b.Slug)
	}
}

func TestListProjects_SortedBySlug(t *testing.T) {
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
	want := []string{"atlas", "gaia", "zen"}
	for i, p := range list {
		if p.Slug != want[i] {
			t.Fatalf("list[%d].Slug = %q, want %q", i, p.Slug, want[i])
		}
	}
}

func TestGetProject_NotFound(t *testing.T) {
	s := newTempStore(t)
	_, err := s.GetProject(99999)
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

func TestCreateTask_AssignsAutoincrementIDs(t *testing.T) {
	s := newTempStore(t)
	p := mustCreateProject(t, s, "gaia")

	task, err := s.CreateTask(p.ID, core.StatusTodo, "implement core", "Body text", []string{"core"})
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	if task.ID <= 0 {
		t.Fatalf("ID = %d, want > 0", task.ID)
	}
	if task.Title != "implement core" || task.Description != "Body text" {
		t.Fatalf("task fields wrong: %+v", task)
	}
	if task.Status != core.StatusTodo {
		t.Fatalf("status = %q", task.Status)
	}

	reload, err := s.GetTask(p.ID, task.ID)
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	if reload.ID != task.ID {
		t.Fatalf("reload id = %d, want %d", reload.ID, task.ID)
	}
	if reload.Title != "implement core" || reload.Description != "Body text" {
		t.Fatalf("reload fields wrong: %+v", reload)
	}
	if len(reload.Tags) != 1 || reload.Tags[0] != "core" {
		t.Fatalf("reload tags = %v", reload.Tags)
	}

	next, err := s.CreateTask(p.ID, core.StatusTodo, "Second", "", nil)
	if err != nil {
		t.Fatalf("CreateTask 2: %v", err)
	}
	if next.ID <= task.ID {
		t.Fatalf("ID2 = %d, want > %d", next.ID, task.ID)
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
	_, err := s.CreateTask(99999, core.StatusTodo, "x", "", nil)
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
			t.Fatalf("GetTask %d: %v", want.ID, err)
		}
		if got.Title != want.Title || got.Status != want.Status {
			t.Fatalf("got = %+v, want title=%q status=%q", got, want.Title, want.Status)
		}
	}
}

func TestGetTask_NotFound(t *testing.T) {
	s := newTempStore(t)
	p := mustCreateProject(t, s, "gaia")
	_, err := s.GetTask(p.ID, 999)
	if !errors.Is(err, core.ErrTaskNotFound) {
		t.Fatalf("err = %v", err)
	}
}

// A task from project B must not be readable as if it belonged to project A.
// The SQL scope check enforces this.
func TestGetTask_ScopedToProject(t *testing.T) {
	s := newTempStore(t)
	a := mustCreateProject(t, s, "alpha")
	b := mustCreateProject(t, s, "beta")
	task, _ := s.CreateTask(a.ID, core.StatusTodo, "T", "", nil)

	if _, err := s.GetTask(b.ID, task.ID); !errors.Is(err, core.ErrTaskNotFound) {
		t.Fatalf("cross-project GetTask err = %v, want ErrTaskNotFound", err)
	}
}

func TestListTasksByProject_AcrossStatuses(t *testing.T) {
	s := newTempStore(t)
	p := mustCreateProject(t, s, "gaia")
	tTodo, _ := s.CreateTask(p.ID, core.StatusTodo, "A", "", nil)
	tDone, _ := s.CreateTask(p.ID, core.StatusDone, "B", "", nil)
	tDocs, _ := s.CreateTask(p.ID, core.StatusDocs, "C", "", nil)
	tArch, _ := s.CreateTask(p.ID, core.StatusArchive, "D", "", nil)

	tasks, err := s.ListTasksByProject(p.ID)
	if err != nil {
		t.Fatalf("ListTasksByProject: %v", err)
	}
	if len(tasks) != 4 {
		t.Fatalf("len = %d, want 4 (%v)", len(tasks), tasks)
	}
	// canonical Statuses order: docs, brainstorm, todo, doing, review, rejected, done, archive
	want := []core.TaskID{tDocs.ID, tTodo.ID, tDone.ID, tArch.ID}
	for i, id := range want {
		if tasks[i].ID != id {
			t.Fatalf("tasks[%d].ID = %d, want %d", i, tasks[i].ID, id)
		}
	}
}

func TestListTasksByProject_ScopedToProject(t *testing.T) {
	s := newTempStore(t)
	a := mustCreateProject(t, s, "alpha")
	b := mustCreateProject(t, s, "beta")
	_, _ = s.CreateTask(a.ID, core.StatusTodo, "in-a", "", nil)
	_, _ = s.CreateTask(b.ID, core.StatusTodo, "in-b", "", nil)

	tasks, err := s.ListTasksByProject(a.ID)
	if err != nil {
		t.Fatalf("ListTasksByProject: %v", err)
	}
	if len(tasks) != 1 || tasks[0].Title != "in-a" {
		t.Fatalf("alpha tasks = %+v", tasks)
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

func TestUpdateTask_StatusChangePersists(t *testing.T) {
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

	reload, err := s.GetTask(p.ID, t1.ID)
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	if reload.Status != core.StatusDoing {
		t.Fatalf("reloaded status = %q", reload.Status)
	}
}

func TestMoveTask_BringsCommentsAlong(t *testing.T) {
	s := newTempStore(t)
	p := mustCreateProject(t, s, "gaia")
	t1, _ := s.CreateTask(p.ID, core.StatusTodo, "A", "", nil)
	if _, err := s.AddComment(p.ID, t1.ID, "first"); err != nil {
		t.Fatalf("AddComment: %v", err)
	}

	moved, err := s.MoveTask(p.ID, t1.ID, core.StatusDoing)
	if err != nil {
		t.Fatalf("MoveTask: %v", err)
	}
	if moved.Status != core.StatusDoing {
		t.Fatalf("status = %q", moved.Status)
	}
	if len(moved.Comments) != 1 || moved.Comments[0] != "first" {
		t.Fatalf("moved comments = %v", moved.Comments)
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
	_, err := s.MoveTask(p.ID, 42, core.StatusDoing)
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
	_, err := s.AddComment(p.ID, 999, "hello")
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
			t.Fatalf("duplicate id %d at index %d (all=%v)", ids[i], i, ids)
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
		t.Fatalf("stored task count = %d, want %d", len(tasks), N)
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
	if p.Slug != "my-project" {
		t.Fatalf("slug = %q, want my-project", p.Slug)
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
			t.Fatalf("%d position = %d, want %d", tid, got.Position, i+1)
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
	if err := s.ReorderColumn(p.ID, core.StatusTodo, []core.TaskID{a.ID, 999}); err == nil {
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

func TestDeleteTask_RemovesFromStore(t *testing.T) {
	s := newTempStore(t)
	p := mustCreateProject(t, s, "gaia")
	t1, _ := s.CreateTask(p.ID, core.StatusTodo, "a", "", nil)
	if _, err := s.AddComment(p.ID, t1.ID, "c"); err != nil {
		t.Fatalf("AddComment: %v", err)
	}

	if err := s.DeleteTask(p.ID, t1.ID); err != nil {
		t.Fatalf("DeleteTask: %v", err)
	}

	if _, err := s.GetTask(p.ID, t1.ID); !errors.Is(err, core.ErrTaskNotFound) {
		t.Fatalf("GetTask after delete: %v", err)
	}

	t2, _ := s.CreateTask(p.ID, core.StatusTodo, "b", "", nil)
	got, err := s.GetTask(p.ID, t2.ID)
	if err != nil {
		t.Fatalf("GetTask new task: %v", err)
	}
	if len(got.Comments) != 0 {
		t.Fatalf("new task picked up stale comments: %v", got.Comments)
	}
}

func TestDeleteTask_NotFound(t *testing.T) {
	s := newTempStore(t)
	p := mustCreateProject(t, s, "gaia")
	if err := s.DeleteTask(p.ID, 999); !errors.Is(err, core.ErrTaskNotFound) {
		t.Fatalf("err = %v", err)
	}
}

