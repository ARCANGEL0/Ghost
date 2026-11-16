package remote

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/swadhinbiswas/ghost/internal/message"
	"github.com/swadhinbiswas/ghost/internal/permission"
)

type storedEvent struct {
	id        int64
	typeName  string
	sessionID string
	messageID string
	payload   string
}

func (s *Server) eventStream(w http.ResponseWriter, r *http.Request) {
	if s.app == nil || s.app.DB == nil {
		writeError(w, http.StatusServiceUnavailable, "Ghost is not ready")
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, "event streams are not supported")
		return
	}
	lastID, err := eventCursor(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)
	if !sendEvent(w, flusher, Event{Type: "server.connected", Data: map[string]any{}}) {
		return
	}

	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()
	stop := context.AfterFunc(s.ctx, cancel)
	defer stop()
	permissions := s.app.Permissions.Subscribe(ctx)
	notifications := s.app.Permissions.SubscribeNotifications(ctx)
	permissionRequests := make(map[string]permission.PermissionRequest)
	poll := time.NewTicker(300 * time.Millisecond)
	heartbeat := time.NewTicker(15 * time.Second)
	defer poll.Stop()
	defer heartbeat.Stop()

	for {
		select {
		case event, open := <-permissions:
			if !open {
				permissions = nil
				continue
			}
			request := event.Payload
			permissionRequests[request.ToolCallID] = request
			if !sendEvent(w, flusher, Event{Type: "permission.asked", Data: permissionViewWithSession(request)}) {
				return
			}
		case event, open := <-notifications:
			if !open {
				notifications = nil
				continue
			}
			notice := event.Payload
			if request, ok := permissionRequests[notice.ToolCallID]; ok && (notice.Granted || notice.Denied) {
				delete(permissionRequests, notice.ToolCallID)
				if !sendEvent(w, flusher, Event{Type: "permission.replied", Data: map[string]string{
					"sessionID": request.SessionID,
					"requestID": request.ID,
				}}) {
					return
				}
			}
		case <-poll.C:
			events, err := s.readEvents(ctx, lastID)
			if err != nil {
				if !sendEvent(w, flusher, Event{Type: "server.error", Data: map[string]string{"message": "could not read event history"}}) {
					return
				}
				continue
			}
			for _, item := range events {
				for _, event := range s.eventsFor(item) {
					event.ID = item.id
					if !sendEvent(w, flusher, event) {
						return
					}
				}
				lastID = item.id
			}
		case <-heartbeat.C:
			if _, err := fmt.Fprint(w, ": heartbeat\n\n"); err != nil {
				return
			}
			flusher.Flush()
		case <-ctx.Done():
			return
		}
	}
}

func eventCursor(r *http.Request) (int64, error) {
	value := strings.TrimSpace(r.Header.Get("Last-Event-ID"))
	if value == "" {
		value = r.URL.Query().Get("since")
	}
	if value == "" {
		return 0, nil
	}
	id, err := strconv.ParseInt(value, 10, 64)
	if err != nil || id < 0 {
		return 0, errors.New("Last-Event-ID must be a positive integer")
	}
	return id, nil
}

func (s *Server) readEvents(ctx context.Context, afterID int64) ([]storedEvent, error) {
	rows, err := s.app.DB.QueryContext(ctx, `
		SELECT id, type, session_id, message_id, payload
		FROM remote_events
		WHERE id > ?
		ORDER BY id
		LIMIT 100`, afterID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var events []storedEvent
	for rows.Next() {
		var item storedEvent
		if err := rows.Scan(&item.id, &item.typeName, &item.sessionID, &item.messageID, &item.payload); err != nil {
			return nil, err
		}
		events = append(events, item)
	}
	return events, rows.Err()
}

func (s *Server) eventsFor(item storedEvent) []Event {
	if item.typeName == "custom" {
		var event Event
		if json.Unmarshal([]byte(item.payload), &event) == nil && event.Type != "" {
			return []Event{event}
		}
		return nil
	}
	if strings.HasPrefix(item.typeName, "message.") {
		return s.messageEvents(item)
	}
	switch item.typeName {
	case "session.created":
		sess, err := s.app.Sessions.Get(s.ctx, item.sessionID)
		if err != nil {
			return []Event{{Type: "session.created", Data: map[string]string{"sessionID": item.sessionID}}}
		}
		view, err := s.describeSession(s.ctx, sess)
		if err != nil {
			return nil
		}
		return []Event{{Type: item.typeName, Data: map[string]any{
			"sessionID": item.sessionID,
			"id":        view.ID,
			"title":     view.Title,
			"location":  view.Location,
			"model":     view.Model,
			"tokens":    view.Tokens,
			"time":      view.Time,
			"archived":  view.Archived,
		}}}
	case "session.updated":
		data := map[string]any{"sessionID": item.sessionID}
		if sess, err := s.app.Sessions.Get(s.ctx, item.sessionID); err == nil {
			view, err := s.describeSession(s.ctx, sess)
			if err == nil {
				data["title"] = view.Title
				data["model"] = view.Model
				data["tokens"] = view.Tokens
				data["time"] = view.Time
			}
		}
		return []Event{{Type: item.typeName, Data: data}}
	case "session.deleted", "session.execution.started", "session.execution.ended":
		return []Event{{Type: item.typeName, Data: map[string]string{"sessionID": item.sessionID}}}
	default:
		return nil
	}
}

func (s *Server) messageEvents(record storedEvent) []Event {
	item, err := s.app.Messages.Get(s.ctx, record.messageID)
	if err != nil {
		if strings.HasSuffix(record.typeName, "deleted") {
			return []Event{{Type: "session.updated", Data: map[string]any{
				"sessionID": record.sessionID,
				"messageID": record.messageID,
			}}}
		}
		return nil
	}
	events := []Event{{Type: "session.updated", Data: map[string]any{
		"sessionID": record.sessionID,
		"messageID": record.messageID,
		"message":   convertMessage(item),
	}}}
	if item.Role == message.User && record.typeName == "message.created" {
		events = append(events, Event{Type: "session.inbox.enqueued", Data: map[string]any{
			"sessionID": record.sessionID,
			"inboxID":   item.ID,
			"item":      map[string]any{"type": "user", "payload": map[string]string{"text": item.Content().Text}},
		}})
	}
	if item.Role == message.Assistant {
		for _, call := range item.ToolCalls() {
			if !call.Finished {
				continue
			}
			var input any = map[string]any{}
			if call.Input != "" && json.Unmarshal([]byte(call.Input), &input) != nil {
				input = call.Input
			}
			events = append(events, Event{Type: "session.tool.called", Data: map[string]any{
				"sessionID": record.sessionID,
				"id":        call.ID,
				"name":      call.Name,
				"input":     input,
			}})
		}
		if item.IsFinished() {
			events = append(events, Event{Type: "session.text.ended", Data: map[string]string{"sessionID": record.sessionID}})
			outcome := "completed"
			switch item.FinishReason() {
			case message.FinishReasonCanceled:
				outcome = "interrupted"
			case message.FinishReasonError, message.FinishReasonPermissionDenied:
				outcome = "failed"
			}
			events = append(events, Event{Type: "session.turn.ended", Data: map[string]string{
				"sessionID": record.sessionID,
				"outcome":   outcome,
			}})
		}
	}
	return events
}

func permissionViewWithSession(request permission.PermissionRequest) map[string]any {
	view := permissionView(request)
	view["sessionID"] = request.SessionID
	return view
}

func sendEvent(w http.ResponseWriter, flusher http.Flusher, event Event) bool {
	data, err := json.Marshal(event)
	if err != nil {
		return false
	}
	if event.ID > 0 {
		if _, err := fmt.Fprintf(w, "id: %d\n", event.ID); err != nil {
			return false
		}
	}
	if _, err := fmt.Fprintf(w, "data: %s\n\n", data); err != nil {
		return false
	}
	flusher.Flush()
	return true
}
