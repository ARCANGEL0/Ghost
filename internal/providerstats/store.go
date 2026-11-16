package providerstats

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

type Store struct {
	db *sql.DB
}

type Row struct {
	Provider       string
	Model          string
	Attempts       int64
	Successes      int64
	Failures       int64
	Cancelled      int64
	RateLimits     int64
	TotalLatencyMS int64
	LastError      string
	LastSuccessAt  int64
}

func New(conn *sql.DB) *Store {
	return &Store{db: conn}
}

func (s *Store) Record(ctx context.Context, provider, model string, latency time.Duration, success, rateLimit, cancelled bool, lastError string) error {
	if s == nil || s.db == nil {
		return nil
	}
	var successes, failures, cancels, limits int64
	if success {
		successes = 1
	} else if cancelled {
		cancels = 1
	} else {
		failures = 1
	}
	if rateLimit {
		limits = 1
	}
	now := time.Now().UnixMilli()
	lastSuccess := int64(0)
	if success {
		lastSuccess = now
	}
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO provider_health (
			provider, model, attempts, successes, failures, cancelled, rate_limits,
			total_latency_ms, last_error, last_success_at, updated_at
		) VALUES (?, ?, 1, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(provider, model) DO UPDATE SET
			attempts = provider_health.attempts + 1,
			successes = provider_health.successes + excluded.successes,
			failures = provider_health.failures + excluded.failures,
			cancelled = provider_health.cancelled + excluded.cancelled,
			rate_limits = provider_health.rate_limits + excluded.rate_limits,
			total_latency_ms = provider_health.total_latency_ms + excluded.total_latency_ms,
			last_error = CASE WHEN excluded.last_error != '' THEN excluded.last_error ELSE provider_health.last_error END,
			last_success_at = CASE WHEN excluded.successes > 0 THEN excluded.last_success_at ELSE provider_health.last_success_at END,
			updated_at = excluded.updated_at`,
		provider, model, successes, failures, cancels, limits, latency.Milliseconds(), lastError,
		lastSuccess, now,
	)
	if err != nil {
		return fmt.Errorf("record provider request: %w", err)
	}
	return nil
}

func (s *Store) List(ctx context.Context) ([]Row, error) {
	if s == nil || s.db == nil {
		return nil, nil
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT provider, model, attempts, successes, failures, cancelled,
			rate_limits, total_latency_ms, last_error, last_success_at
		FROM provider_health
		ORDER BY attempts DESC, provider, model`)
	if err != nil {
		return nil, fmt.Errorf("list provider statistics: %w", err)
	}
	defer rows.Close()
	var stats []Row
	for rows.Next() {
		var row Row
		if err := rows.Scan(
			&row.Provider, &row.Model, &row.Attempts, &row.Successes, &row.Failures,
			&row.Cancelled, &row.RateLimits, &row.TotalLatencyMS, &row.LastError, &row.LastSuccessAt,
		); err != nil {
			return nil, fmt.Errorf("read provider statistics: %w", err)
		}
		stats = append(stats, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read provider statistics: %w", err)
	}
	return stats, nil
}
