package agent

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	"charm.land/fantasy"
)

type AgentRun struct {
	ID           string
	ParentID     string
	SessionID    string
	Title        string
	TaskType     string
	Prompt       string
	Provider     string
	Model        string
	State        string
	StartedAt    time.Time
	FinishedAt   time.Time
	Duration     time.Duration
	ToolCalls    int
	Retries      int
	InputTokens  int64
	OutputTokens int64
	Cost         float64
	Error        string
}

type runRecord struct {
	AgentRun
	cancel context.CancelFunc
	wake   chan struct{}
}

type runTracker struct {
	mu      sync.Mutex
	nextID  atomic.Uint64
	runs    map[string]*runRecord
	latency map[string]time.Duration
	samples map[string]int
}

type runContextKey struct{}

func withRunID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, runContextKey{}, id)
}

func parentRunID(ctx context.Context) string {
	id, _ := ctx.Value(runContextKey{}).(string)
	return id
}

func newRunTracker() *runTracker {
	return &runTracker{
		runs:    make(map[string]*runRecord),
		latency: make(map[string]time.Duration),
		samples: make(map[string]int),
	}
}

func (t *runTracker) start(sessionID, parentID, title, prompt string, model Model, cancel context.CancelFunc) string {
	if t == nil {
		return ""
	}
	id := fmt.Sprintf("run-%d", t.nextID.Add(1))
	now := time.Now()
	t.mu.Lock()
	t.runs[id] = &runRecord{
		AgentRun: AgentRun{
			ID:        id,
			ParentID:  parentID,
			SessionID: sessionID,
			Title:     title,
			TaskType:  taskKindOf(prompt),
			Prompt:    prompt,
			Provider:  model.ModelCfg.Provider,
			Model:     model.ModelCfg.Model,
			State:     "running",
			StartedAt: now,
		},
		cancel: cancel,
		wake:   make(chan struct{}),
	}
	if len(t.runs) > 100 {
		t.pruneLocked()
	}
	t.mu.Unlock()
	return id
}

func (t *runTracker) finish(id string, runErr error) {
	if t == nil || id == "" {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	run := t.runs[id]
	if run == nil {
		return
	}
	now := time.Now()
	run.FinishedAt = now
	run.Duration = now.Sub(run.StartedAt)
	run.cancel = nil
	switch {
	case errors.Is(runErr, context.Canceled):
		run.State = "cancelled"
	case runErr != nil:
		run.State = "failed"
		run.Error = runErr.Error()
	default:
		run.State = "completed"
	}
	if run.wake != nil {
		close(run.wake)
		run.wake = nil
	}
	t.addLatencyLocked(run.Provider+"/"+run.Model, run.Duration)
}

func (t *runTracker) pause(id string) error {
	if t == nil {
		return errors.New("run tracking is unavailable")
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	run := t.runs[id]
	if run != nil && run.State == "paused" {
		return nil
	}
	if run == nil || run.State != "running" {
		return errors.New("run is not active")
	}
	run.State = "paused"
	run.wake = make(chan struct{})
	return nil
}

func (t *runTracker) resume(id string) error {
	if t == nil {
		return errors.New("run tracking is unavailable")
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	run := t.runs[id]
	if run == nil || run.State != "paused" {
		return errors.New("run is not paused")
	}
	run.State = "running"
	if run.wake != nil {
		close(run.wake)
		run.wake = nil
	}
	return nil
}

func (t *runTracker) pruneLocked() {
	var finished []AgentRun
	for _, run := range t.runs {
		if run.State != "running" && run.State != "paused" {
			finished = append(finished, run.AgentRun)
		}
	}
	sort.Slice(finished, func(i, j int) bool { return finished[i].StartedAt.Before(finished[j].StartedAt) })
	for len(t.runs) > 80 && len(finished) > 0 {
		delete(t.runs, finished[0].ID)
		finished = finished[1:]
	}
}

func (t *runTracker) wait(ctx context.Context, id string) error {
	if t == nil || id == "" {
		return nil
	}
	for {
		t.mu.Lock()
		run := t.runs[id]
		if run == nil || run.State != "paused" {
			t.mu.Unlock()
			return nil
		}
		wake := run.wake
		t.mu.Unlock()
		select {
		case <-wake:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

func (t *runTracker) cancelRun(id string) error {
	if t == nil {
		return errors.New("run tracking is unavailable")
	}
	t.mu.Lock()
	run := t.runs[id]
	if run == nil || run.cancel == nil {
		t.mu.Unlock()
		return errors.New("run is not active")
	}
	cancel := run.cancel
	t.mu.Unlock()
	cancel()
	return nil
}

func (t *runTracker) toolCall(id string) {
	t.update(id, func(run *runRecord) { run.ToolCalls++ })
}

func (t *runTracker) retry(id string) {
	t.update(id, func(run *runRecord) { run.Retries++ })
}

func (t *runTracker) usage(id string, usage fantasy.Usage, cost float64) {
	t.update(id, func(run *runRecord) {
		run.InputTokens += usage.InputTokens + usage.CacheCreationTokens + usage.CacheReadTokens
		run.OutputTokens += usage.OutputTokens
		run.Cost += cost
	})
}

func (t *runTracker) update(id string, fn func(*runRecord)) {
	if t == nil || id == "" {
		return
	}
	t.mu.Lock()
	if run := t.runs[id]; run != nil {
		fn(run)
	}
	t.mu.Unlock()
}

func (t *runTracker) list() []AgentRun {
	if t == nil {
		return nil
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	runs := make([]AgentRun, 0, len(t.runs))
	for _, run := range t.runs {
		entry := run.AgentRun
		if entry.State == "running" || entry.State == "paused" {
			entry.Duration = time.Since(entry.StartedAt)
		}
		runs = append(runs, entry)
	}
	sort.Slice(runs, func(i, j int) bool { return runs[i].StartedAt.After(runs[j].StartedAt) })
	return runs
}

func (t *runTracker) get(id string) (AgentRun, bool) {
	if t == nil {
		return AgentRun{}, false
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	run := t.runs[id]
	if run == nil {
		return AgentRun{}, false
	}
	entry := run.AgentRun
	if entry.State == "running" || entry.State == "paused" {
		entry.Duration = time.Since(entry.StartedAt)
	}
	return entry, true
}

func (t *runTracker) averageLatency(modelKey string) time.Duration {
	if t == nil {
		return 0
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.latency[modelKey]
}

func (t *runTracker) addLatencyLocked(modelKey string, duration time.Duration) {
	count := t.samples[modelKey]
	if count == 0 {
		t.latency[modelKey] = duration
	} else {
		previous := t.latency[modelKey]
		t.latency[modelKey] = (previous*time.Duration(count) + duration) / time.Duration(count+1)
	}
	t.samples[modelKey] = count + 1
}
