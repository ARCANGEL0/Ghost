-- +goose Up
-- +goose StatementBegin
CREATE TABLE session_run_locks (
    session_id TEXT PRIMARY KEY,
    owner TEXT NOT NULL,
    expires_at INTEGER NOT NULL,
    cancel_requested INTEGER NOT NULL DEFAULT 0,
    FOREIGN KEY (session_id) REFERENCES sessions (id) ON DELETE CASCADE
);

CREATE TABLE remote_events (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    type TEXT NOT NULL,
    session_id TEXT NOT NULL DEFAULT '',
    message_id TEXT NOT NULL DEFAULT '',
    payload TEXT NOT NULL DEFAULT '{}',
    created_at INTEGER NOT NULL
);

CREATE INDEX idx_remote_events_session ON remote_events (session_id, id);

CREATE TRIGGER record_run_started
AFTER INSERT ON session_run_locks
BEGIN
    INSERT INTO remote_events (type, session_id, created_at)
    VALUES ('session.execution.started', new.session_id, CAST(strftime('%s', 'now') AS INTEGER) * 1000);
    DELETE FROM remote_events
    WHERE id <= (SELECT COALESCE(MAX(id), 0) - 10000 FROM remote_events);
END;

CREATE TRIGGER record_run_ended
AFTER DELETE ON session_run_locks
BEGIN
    INSERT INTO remote_events (type, session_id, created_at)
    VALUES ('session.execution.ended', old.session_id, CAST(strftime('%s', 'now') AS INTEGER) * 1000);
    DELETE FROM remote_events
    WHERE id <= (SELECT COALESCE(MAX(id), 0) - 10000 FROM remote_events);
END;

CREATE TRIGGER record_run_replaced
AFTER UPDATE OF owner ON session_run_locks
WHEN old.owner != new.owner
BEGIN
    INSERT INTO remote_events (type, session_id, created_at)
    VALUES ('session.execution.started', new.session_id, CAST(strftime('%s', 'now') AS INTEGER) * 1000);
END;

CREATE TRIGGER record_session_created
AFTER INSERT ON sessions
BEGIN
    INSERT INTO remote_events (type, session_id, created_at)
    VALUES ('session.created', new.id, CAST(strftime('%s', 'now') AS INTEGER) * 1000);
    DELETE FROM remote_events
    WHERE id <= (SELECT COALESCE(MAX(id), 0) - 10000 FROM remote_events);
END;

CREATE TRIGGER record_session_updated
AFTER UPDATE ON sessions
WHEN old.title != new.title OR old.prompt_tokens != new.prompt_tokens
    OR old.completion_tokens != new.completion_tokens OR old.cost != new.cost
    OR old.summary_message_id IS NOT new.summary_message_id
BEGIN
    INSERT INTO remote_events (type, session_id, created_at)
    VALUES ('session.updated', new.id, CAST(strftime('%s', 'now') AS INTEGER) * 1000);
    DELETE FROM remote_events
    WHERE id <= (SELECT COALESCE(MAX(id), 0) - 10000 FROM remote_events);
END;

CREATE TRIGGER record_session_deleted
AFTER DELETE ON sessions
BEGIN
    INSERT INTO remote_events (type, session_id, created_at)
    VALUES ('session.deleted', old.id, CAST(strftime('%s', 'now') AS INTEGER) * 1000);
    DELETE FROM remote_events
    WHERE id <= (SELECT COALESCE(MAX(id), 0) - 10000 FROM remote_events);
END;

CREATE TRIGGER record_message_created
AFTER INSERT ON messages
BEGIN
    INSERT INTO remote_events (type, session_id, message_id, created_at)
    VALUES ('message.created', new.session_id, new.id, CAST(strftime('%s', 'now') AS INTEGER) * 1000);
    DELETE FROM remote_events
    WHERE id <= (SELECT COALESCE(MAX(id), 0) - 10000 FROM remote_events);
END;

CREATE TRIGGER record_message_updated
AFTER UPDATE ON messages
BEGIN
    INSERT INTO remote_events (type, session_id, message_id, created_at)
    VALUES ('message.updated', new.session_id, new.id, CAST(strftime('%s', 'now') AS INTEGER) * 1000);
    DELETE FROM remote_events
    WHERE id <= (SELECT COALESCE(MAX(id), 0) - 10000 FROM remote_events);
END;

CREATE TRIGGER record_message_deleted
AFTER DELETE ON messages
BEGIN
    INSERT INTO remote_events (type, session_id, message_id, created_at)
    VALUES ('message.deleted', old.session_id, old.id, CAST(strftime('%s', 'now') AS INTEGER) * 1000);
    DELETE FROM remote_events
    WHERE id <= (SELECT COALESCE(MAX(id), 0) - 10000 FROM remote_events);
END;

-- +goose StatementEnd
-- +goose Down
DROP TRIGGER IF EXISTS record_message_deleted;
DROP TRIGGER IF EXISTS record_message_updated;
DROP TRIGGER IF EXISTS record_message_created;
DROP TRIGGER IF EXISTS record_session_deleted;
DROP TRIGGER IF EXISTS record_session_updated;
DROP TRIGGER IF EXISTS record_session_created;
DROP TRIGGER IF EXISTS record_run_ended;
DROP TRIGGER IF EXISTS record_run_replaced;
DROP TRIGGER IF EXISTS record_run_started;
DROP INDEX IF EXISTS idx_remote_events_session;
DROP TABLE IF EXISTS remote_events;
DROP TABLE IF EXISTS session_run_locks;
