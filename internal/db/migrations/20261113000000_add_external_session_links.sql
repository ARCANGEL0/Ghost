-- +goose Up
CREATE TABLE session_external_links (
    session_id TEXT PRIMARY KEY,
    provider TEXT NOT NULL,
    external_id TEXT NOT NULL,
    FOREIGN KEY (session_id) REFERENCES sessions (id) ON DELETE CASCADE
);

-- +goose Down
DROP TABLE session_external_links;
