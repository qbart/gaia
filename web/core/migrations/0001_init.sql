-- +goose Up
-- +goose StatementBegin
CREATE TABLE projects (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    slug       TEXT NOT NULL,
    name       TEXT NOT NULL,
    path       TEXT NOT NULL DEFAULT '',
    created_at TEXT NOT NULL
);
-- +goose StatementEnd

-- +goose StatementBegin
CREATE UNIQUE INDEX idx_projects_slug ON projects(slug);
-- +goose StatementEnd

-- +goose StatementBegin
CREATE UNIQUE INDEX idx_projects_path ON projects(path) WHERE path != '';
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TABLE tasks (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    project_id  INTEGER NOT NULL REFERENCES projects(id),
    title       TEXT NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    status      TEXT NOT NULL,
    position    INTEGER NOT NULL,
    tags        TEXT NOT NULL DEFAULT '[]',
    created_at  TEXT NOT NULL
);
-- +goose StatementEnd

-- +goose StatementBegin
CREATE INDEX idx_tasks_project_status_position
    ON tasks(project_id, status, position);
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TABLE task_comments (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    task_id    INTEGER NOT NULL REFERENCES tasks(id),
    body       TEXT NOT NULL,
    created_at TEXT NOT NULL
);
-- +goose StatementEnd

-- +goose StatementBegin
CREATE INDEX idx_task_comments_task ON task_comments(task_id, id);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP INDEX IF EXISTS idx_task_comments_task;
-- +goose StatementEnd
-- +goose StatementBegin
DROP TABLE IF EXISTS task_comments;
-- +goose StatementEnd
-- +goose StatementBegin
DROP INDEX IF EXISTS idx_tasks_project_status_position;
-- +goose StatementEnd
-- +goose StatementBegin
DROP TABLE IF EXISTS tasks;
-- +goose StatementEnd
-- +goose StatementBegin
DROP INDEX IF EXISTS idx_projects_path;
-- +goose StatementEnd
-- +goose StatementBegin
DROP INDEX IF EXISTS idx_projects_slug;
-- +goose StatementEnd
-- +goose StatementBegin
DROP TABLE IF EXISTS projects;
-- +goose StatementEnd
