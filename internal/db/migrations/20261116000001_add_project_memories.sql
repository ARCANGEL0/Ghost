-- +goose Up
CREATE TABLE project_memories (
    project_path TEXT NOT NULL,
    key TEXT NOT NULL,
    value TEXT NOT NULL,
    category TEXT NOT NULL DEFAULT '',
    source_session TEXT NOT NULL DEFAULT '',
    confidence REAL NOT NULL DEFAULT 1,
    use_count INTEGER NOT NULL DEFAULT 0,
    created_at INTEGER NOT NULL,
    updated_at INTEGER NOT NULL,
    PRIMARY KEY (project_path, key)
);

CREATE INDEX idx_project_memories_category ON project_memories (project_path, category);
CREATE INDEX idx_project_memories_updated ON project_memories (project_path, updated_at DESC);

-- +goose Down
DROP INDEX IF EXISTS idx_project_memories_updated;
DROP INDEX IF EXISTS idx_project_memories_category;
DROP TABLE IF EXISTS project_memories;
