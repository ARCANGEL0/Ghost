package remote

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/swadhinbiswas/ghost/internal/message"
	"github.com/swadhinbiswas/ghost/internal/permission"
	"github.com/swadhinbiswas/ghost/internal/pubsub"
	"github.com/swadhinbiswas/ghost/internal/session"
)

func (s *Server) eventStream(w http.ResponseWriter, r *http.Request) {
	if s.app == nil {
		writeError(w, http.StatusServiceUnavailable, "Ghost is not ready")
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, "event streams are not supported")
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
	apiEvents := s.events.Subscribe(ctx)
	messageEvents := s.app.Messages.Subscribe(ctx)
	sessionEvents := s.app.Sessions.Subscribe(ctx)
	permissionEvents := s.app.Permissions.Subscribe(ctx)
	notificationEvents := s.app.Permissions.SubscribeNotifications(ctx)
	heartbeat := time.NewTicker(15 * time.Second)
	defer heartbeat.Stop()
	pending := make(map[string]permission.PermissionRequest)
	seenTools := make(map[string]bool)
	seenFinished := make(map[string]bool)

	for {
		select {
		case event, open := <-apiEvents:
			if !open || !sendEvent(w, flusher, event.Payload) {
				return
			}
		case event, open := <-messageEvents:
			if !open {
				messageEvents = nil
				continue
			}
			if !s.sendMessageEvents(w, flusher, event, seenTools, seenFinished) {
				return
			}
		case event, open := <-sessionEvents:
			if !open {
				sessionEvents = nil
				continue
			}
			if !s.sendSessionEvent(r, w, flusher, event) {
				return
			}
		case event, open := <-permissionEvents:
			if !open {
				permissionEvents = nil
				continue
			}
			request := event.Payload
			pending[request.ToolCallID] = request
			if !sendEvent(w, flusher, Event{Type: "permission.asked", Data: permissionViewWithSession(request)}) {
				return
			}
		case event, open := <-notificationEvents:
			if !open {
				notificationEvents = nil
				continue
			}
			notice := event.Payload
			if request, ok := pending[notice.ToolCallID]; ok && (notice.Granted || notice.Denied) {
				delete(pending, notice.ToolCallID)
				if !sendEvent(w, flusher, Event{Type: "permission.replied", Data: map[string]string{
					"sessionID": request.SessionID,
					"requestID": request.ID,
				}}) {
					return
				}
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

func (s *Server) sendSessionEvent(r *http.Request, w http.ResponseWriter, flusher http.Flusher, event pubsub.Event[session.Session]) bool {
	item := event.Payload
	switch event.Type {
	case pubsub.CreatedEvent:
		view, err := s.describeSession(r.Context(), item)
		if err != nil {
			return sendEvent(w, flusher, Event{Type: "session.updated", Data: map[string]string{"sessionID": item.ID}})
		}
		return sendEvent(w, flusher, Event{Type: "session.created", Data: map[string]any{
			"sessionID": item.ID,
			"id":        view.ID,
			"title":     view.Title,
			"location":  view.Location,
			"model":     view.Model,
			"tokens":    view.Tokens,
			"time":      view.Time,
			"archived":  view.Archived,
		}})
	case pubsub.DeletedEvent:
		return sendEvent(w, flusher, Event{Type: "session.deleted", Data: map[string]string{"sessionID": item.ID}})
	default:
		return sendEvent(w, flusher, Event{Type: "session.updated", Data: map[string]any{
			"sessionID": item.ID,
			"title":     item.Title,
			"updated":   time.Unix(item.UpdatedAt, 0).UnixMilli(),
		}})
	}
}

func (s *Server) sendMessageEvents(w http.ResponseWriter, flusher http.Flusher, event pubsub.Event[message.Message], seenTools, seenFinished map[string]bool) bool {
	item := event.Payload
	if !sendEvent(w, flusher, Event{Type: "session.updated", Data: map[string]any{
		"sessionID": item.SessionID,
		"messageID": item.ID,
		"role":      item.Role,
		"updated":   time.Unix(item.UpdatedAt, 0).UnixMilli(),
	}}) {
		return false
	}
	if item.Role == message.User && event.Type == pubsub.CreatedEvent {
		if !sendEvent(w, flusher, Event{Type: "session.inbox.enqueued", Data: map[string]any{
			"sessionID": item.SessionID,
			"inboxID":   item.ID,
			"item":      map[string]any{"type": "user", "payload": map[string]string{"text": item.Content().Text}},
		}}) {
			return false
		}
	}
	for _, call := range item.ToolCalls() {
		key := item.SessionID + "/" + call.ID
		if call.Finished && !seenTools[key] {
			seenTools[key] = true
			var input any = map[string]any{}
			if call.Input != "" {
				if err := json.Unmarshal([]byte(call.Input), &input); err != nil {
					input = call.Input
				}
			}
			if !sendEvent(w, flusher, Event{Type: "session.tool.called", Data: map[string]any{
				"sessionID": item.SessionID,
				"id":        call.ID,
				"name":      call.Name,
				"input":     input,
			}}) {
				return false
			}
		}
	}
	for _, result := range item.ToolResults() {
		kind := "session.tool.success"
		if result.IsError {
			kind = "session.tool.failed"
		}
		if !sendEvent(w, flusher, Event{Type: kind, Data: map[string]string{
			"sessionID":  item.SessionID,
			"toolCallID": result.ToolCallID,
		}}) {
			return false
		}
	}
	if item.Role == message.Assistant && item.IsFinished() && !seenFinished[item.ID] {
		seenFinished[item.ID] = true
		if !sendEvent(w, flusher, Event{Type: "session.text.ended", Data: map[string]string{"sessionID": item.SessionID}}) {
			return false
		}
	}
	return true
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
	if _, err := fmt.Fprintf(w, "data: %s\n\n", data); err != nil {
		return false
	}
	flusher.Flush()
	return true
}
