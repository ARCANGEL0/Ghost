-- +goose Up
CREATE TABLE provider_health (
    provider TEXT NOT NULL,
    model TEXT NOT NULL,
    attempts INTEGER NOT NULL DEFAULT 0,
    successes INTEGER NOT NULL DEFAULT 0,
    failures INTEGER NOT NULL DEFAULT 0,
    cancelled INTEGER NOT NULL DEFAULT 0,
    rate_limits INTEGER NOT NULL DEFAULT 0,
    total_latency_ms INTEGER NOT NULL DEFAULT 0,
    last_error TEXT NOT NULL DEFAULT '',
    last_success_at INTEGER NOT NULL DEFAULT 0,
    updated_at INTEGER NOT NULL,
    PRIMARY KEY (provider, model)
);

CREATE INDEX idx_provider_health_attempts ON provider_health (attempts DESC);

-- +goose Down
DROP INDEX IF EXISTS idx_provider_health_attempts;
DROP TABLE IF EXISTS provider_health;
