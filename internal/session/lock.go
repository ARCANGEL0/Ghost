package session

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
)

var ErrSessionRunLocked = errors.New("session is already running in another Ghost process")

const runLockLease = time.Minute

type RunLocker struct {
	db *sql.DB
}

func NewRunLocker(db *sql.DB) *RunLocker {
	return &RunLocker{db: db}
}

func (l *RunLocker) IsBusy(ctx context.Context, sessionID string) (bool, error) {
	var expiresAt int64
	err := l.db.QueryRowContext(ctx,
		"SELECT expires_at FROM session_run_locks WHERE session_id = ?", sessionID,
	).Scan(&expiresAt)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("read session lock: %w", err)
	}
	return expiresAt > time.Now().UnixMilli(), nil
}

func (l *RunLocker) Acquire(ctx context.Context, sessionID string) (context.Context, func(), error) {
	owner := uuid.NewString()
	now := time.Now().UnixMilli()
	result, err := l.db.ExecContext(ctx, `
		INSERT INTO session_run_locks (session_id, owner, expires_at, cancel_requested)
		VALUES (?, ?, ?, 0)
		ON CONFLICT(session_id) DO UPDATE SET
			owner = excluded.owner,
			expires_at = excluded.expires_at,
			cancel_requested = 0
		WHERE session_run_locks.expires_at <= ?`,
		sessionID, owner, now+runLockLease.Milliseconds(), now,
	)
	if err != nil {
		return nil, nil, fmt.Errorf("acquire session lock: %w", err)
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return nil, nil, fmt.Errorf("check session lock: %w", err)
	}
	if changed == 0 {
		return nil, nil, ErrSessionRunLocked
	}

	runCtx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	go l.keepAlive(runCtx, owner, sessionID, cancel, done)
	release := func() {
		cancel()
		<-done
		_, _ = l.db.ExecContext(context.WithoutCancel(ctx),
			"DELETE FROM session_run_locks WHERE session_id = ? AND owner = ?", sessionID, owner)
	}
	return runCtx, release, nil
}

func (l *RunLocker) RequestCancel(ctx context.Context, sessionID string) (bool, error) {
	result, err := l.db.ExecContext(ctx, `
		UPDATE session_run_locks SET cancel_requested = 1
		WHERE session_id = ? AND expires_at > ?`, sessionID, time.Now().UnixMilli())
	if err != nil {
		return false, fmt.Errorf("request session cancel: %w", err)
	}
	changed, err := result.RowsAffected()
	return changed > 0, err
}

func (l *RunLocker) keepAlive(ctx context.Context, owner, sessionID string, cancel context.CancelFunc, done chan struct{}) {
	defer close(done)
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	nextRenew := time.Now().Add(runLockLease / 3)
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			var cancelRequested bool
			err := l.db.QueryRowContext(ctx, `
				SELECT cancel_requested FROM session_run_locks
				WHERE session_id = ? AND owner = ?`, sessionID, owner).Scan(&cancelRequested)
			if err != nil || cancelRequested {
				cancel()
				return
			}
			if time.Now().Before(nextRenew) {
				continue
			}
			result, err := l.db.ExecContext(ctx, `
				UPDATE session_run_locks SET expires_at = ?
				WHERE session_id = ? AND owner = ?`,
				time.Now().Add(runLockLease).UnixMilli(), sessionID, owner,
			)
			if err != nil {
				cancel()
				return
			}
			if changed, err := result.RowsAffected(); err != nil || changed == 0 {
				cancel()
				return
			}
			nextRenew = time.Now().Add(runLockLease / 3)
		}
	}
}
