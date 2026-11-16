package remote

import (
	"context"
	"crypto/subtle"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/swadhinbiswas/ghost/internal/agent"
	"github.com/swadhinbiswas/ghost/internal/app"
	"github.com/swadhinbiswas/ghost/internal/message"
	"github.com/swadhinbiswas/ghost/internal/permission"
	"github.com/swadhinbiswas/ghost/internal/pubsub"
	"github.com/swadhinbiswas/ghost/internal/session"
)

const apiVersion = "1"

// Server exposes Ghost sessions to authenticated clients.
type Server struct {
	ctx       context.Context
	app       *app.App
	token     string
	events    *pubsub.Broker[Event]
	promptMu  sync.Mutex
	prompting map[string]bool
}

// Event is a change sent to remote clients.
type Event struct {
	Type string `json:"type"`
	Data any    `json:"data"`
}

type sessionView struct {
	ID       string       `json:"id"`
	Title    string       `json:"title"`
	Location locationView `json:"location"`
	Model    modelView    `json:"model"`
	Tokens   tokenView    `json:"tokens"`
	Time     timeView     `json:"time"`
	Archived bool         `json:"archived"`
	Waiting  *waitingView `json:"waiting,omitempty"`
}

type locationView struct {
	Directory string `json:"directory"`
}

type modelView struct {
	ID         string `json:"id"`
	ProviderID string `json:"providerID"`
}

type tokenView struct {
	Input  int64          `json:"input"`
	Output int64          `json:"output"`
	Cache  cacheTokenView `json:"cache"`
}

type cacheTokenView struct {
	Read int64 `json:"read"`
}

type timeView struct {
	Created int64 `json:"created"`
	Updated int64 `json:"updated"`
}

type waitingView struct {
	Kind   string `json:"kind"`
	Tool   string `json:"tool,omitempty"`
	Detail string `json:"detail,omitempty"`
}

type apiMessage struct {
	ID      string       `json:"id"`
	Type    string       `json:"type"`
	Text    string       `json:"text,omitempty"`
	Content []apiPart    `json:"content,omitempty"`
	Outcome string       `json:"outcome,omitempty"`
	Time    *messageTime `json:"time,omitempty"`
}

type apiPart struct {
	Type  string     `json:"type"`
	Text  string     `json:"text,omitempty"`
	ID    string     `json:"id,omitempty"`
	Name  string     `json:"name,omitempty"`
	State *toolState `json:"state,omitempty"`
}

type toolState struct {
	Input  any        `json:"input"`
	Status string     `json:"status"`
	Error  *errorView `json:"error,omitempty"`
}

type errorView struct {
	Message string `json:"message"`
}

type messageTime struct {
	Created int64 `json:"created"`
}

type errorResponse struct {
	Error string `json:"error"`
}

// NewServer creates the authenticated session API.
func NewServer(ctx context.Context, app *app.App, token string) *Server {
	return &Server{
		ctx:       ctx,
		app:       app,
		token:     token,
		events:    pubsub.NewBroker[Event](),
		prompting: make(map[string]bool),
	}
}

// Handler returns the API routes behind bearer token authentication.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/info", s.info)
	mux.HandleFunc("GET /api/event", s.eventStream)
	mux.HandleFunc("GET /api/session", s.listSessions)
	mux.HandleFunc("POST /api/session", s.createSession)
	mux.HandleFunc("GET /api/session/{id}", s.getSession)
	mux.HandleFunc("GET /api/session/{id}/message", s.listMessages)
	mux.HandleFunc("POST /api/session/{id}/prompt", s.prompt)
	mux.HandleFunc("POST /api/session/{id}/interrupt", s.interrupt)
	mux.HandleFunc("POST /api/session/{id}/compact", s.compact)
	mux.HandleFunc("POST /api/session/{id}/model", s.switchModel)
	mux.HandleFunc("GET /api/session/{id}/permission", s.listPermissions)
	mux.HandleFunc("POST /api/session/{id}/permission/{requestID}/reply", s.replyPermission)
	mux.HandleFunc("GET /api/session/{id}/form", s.listForms)
	mux.HandleFunc("POST /api/session/{id}/form/{formID}/reply", s.replyForm)
	mux.HandleFunc("/", func(w http.ResponseWriter, _ *http.Request) {
		writeError(w, http.StatusNotFound, "route not found")
	})

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !s.authorized(r) {
			writeError(w, http.StatusUnauthorized, "a valid bearer token is required")
			return
		}
		mux.ServeHTTP(w, r)
	})
}

func (s *Server) authorized(r *http.Request) bool {
	scheme, value, ok := strings.Cut(r.Header.Get("Authorization"), " ")
	if !ok || !strings.EqualFold(scheme, "Bearer") {
		return false
	}
	value = strings.TrimSpace(value)
	return value != "" && len(value) == len(s.token) && subtle.ConstantTimeCompare([]byte(value), []byte(s.token)) == 1
}

func (s *Server) info(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"version": apiVersion, "app": "ghost", "api": apiVersion})
}

func (s *Server) listSessions(w http.ResponseWriter, r *http.Request) {
	if s.app == nil {
		writeError(w, http.StatusServiceUnavailable, "Ghost is not ready")
		return
	}
	limit, err := queryLimit(r.URL.Query(), 40)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	order, err := queryOrder(r.URL.Query())
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	sessions, err := s.app.Sessions.List(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not list sessions")
		return
	}
	views := make([]sessionView, 0, min(limit, len(sessions)))
	for _, item := range sessions {
		view, err := s.describeSession(r.Context(), item)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "could not read session details")
			return
		}
		views = append(views, view)
	}
	sort.SliceStable(views, func(i, j int) bool {
		if order == "asc" {
			return views[i].Time.Updated < views[j].Time.Updated
		}
		return views[i].Time.Updated > views[j].Time.Updated
	})
	if len(views) > limit {
		views = views[:limit]
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": views})
}

func (s *Server) getSession(w http.ResponseWriter, r *http.Request) {
	item, ok := s.getSessionByID(w, r, r.PathValue("id"))
	if !ok {
		return
	}
	view, err := s.describeSession(r.Context(), item)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not read session details")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": view})
}

func (s *Server) createSession(w http.ResponseWriter, r *http.Request) {
	if s.app == nil {
		writeError(w, http.StatusServiceUnavailable, "Ghost is not ready")
		return
	}
	var input struct {
		CWD      string `json:"cwd"`
		Location struct {
			Directory string `json:"directory"`
		} `json:"location"`
	}
	if err := decodeJSON(r, &input); err != nil {
		writeError(w, http.StatusBadRequest, "invalid session request")
		return
	}
	cwd := input.Location.Directory
	if cwd == "" {
		cwd = input.CWD
	}
	workingDir := s.app.Store().WorkingDir()
	if cwd != "" && filepath.Clean(cwd) != filepath.Clean(workingDir) {
		writeError(w, http.StatusBadRequest, "this server can only create sessions in its working directory")
		return
	}
	item, err := s.app.Sessions.Create(r.Context(), "New session")
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not create session")
		return
	}
	view, err := s.describeSession(r.Context(), item)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not read session details")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"data": view})
}

func (s *Server) listMessages(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.getSessionByID(w, r, r.PathValue("id")); !ok {
		return
	}
	limit, err := queryLimit(r.URL.Query(), 60)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	order, err := queryOrder(r.URL.Query())
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	items, err := s.app.Messages.List(r.Context(), r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not list messages")
		return
	}
	data := convertMessages(items)
	if order == "desc" {
		reverseMessages(data)
	}
	if len(data) > limit {
		data = data[:limit]
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": data})
}

func (s *Server) prompt(w http.ResponseWriter, r *http.Request) {
	if s.app == nil || s.app.AgentCoordinator == nil {
		writeError(w, http.StatusServiceUnavailable, "the Ghost agent is not configured")
		return
	}
	id := r.PathValue("id")
	if _, ok := s.getSessionByID(w, r, id); !ok {
		return
	}
	var input struct {
		Text string `json:"text"`
	}
	if err := decodeJSON(r, &input); err != nil || strings.TrimSpace(input.Text) == "" {
		writeError(w, http.StatusBadRequest, "prompt text is required")
		return
	}
	if len(s.app.Permissions.Pending(id)) > 0 {
		writeError(w, http.StatusConflict, "answer the pending permission request before sending another prompt")
		return
	}

	s.promptMu.Lock()
	if s.prompting[id] || s.app.AgentCoordinator.IsSessionBusy(id) {
		s.promptMu.Unlock()
		writeError(w, http.StatusConflict, "this session is busy; wait for its current turn to finish")
		return
	}
	s.prompting[id] = true
	s.promptMu.Unlock()

	messageEvents := s.app.Messages.Subscribe(r.Context())
	done := make(chan error, 1)
	go func() {
		defer func() {
			s.promptMu.Lock()
			delete(s.prompting, id)
			s.promptMu.Unlock()
		}()
		before, _ := s.app.Sessions.Get(s.ctx, id)
		model := s.app.AgentCoordinator.Model()
		s.publish("session.step.started", map[string]any{
			"sessionID": id,
			"model":     modelView{ID: model.ModelCfg.Model, ProviderID: model.ModelCfg.Provider},
		})
		_, err := s.app.AgentCoordinator.Run(s.ctx, id, input.Text)
		after, getErr := s.app.Sessions.Get(s.ctx, id)
		if getErr == nil {
			s.publish("session.step.ended", map[string]any{
				"sessionID": id,
				"tokens": map[string]int64{
					"input":  max(0, after.PromptTokens-before.PromptTokens),
					"output": max(0, after.CompletionTokens-before.CompletionTokens),
				},
			})
		}
		switch {
		case errors.Is(err, context.Canceled), errors.Is(err, agent.ErrRequestCancelled):
			s.publish("session.execution.interrupted", map[string]any{"sessionID": id})
		case err != nil:
			s.publish("session.execution.failed", map[string]any{"sessionID": id, "error": errorView{Message: err.Error()}})
		default:
			s.publish("session.execution.succeeded", map[string]any{"sessionID": id})
		}
		done <- err
	}()

	timer := time.NewTimer(10 * time.Second)
	defer timer.Stop()
	for {
		select {
		case event, open := <-messageEvents:
			if !open {
				writeError(w, http.StatusServiceUnavailable, "session message stream closed")
				return
			}
			item := event.Payload
			if event.Type != pubsub.CreatedEvent || item.SessionID != id || item.Role != message.User || item.Content().Text != input.Text {
				continue
			}
			s.publish("session.execution.started", map[string]any{"sessionID": id})
			writeJSON(w, http.StatusAccepted, map[string]any{"data": convertMessage(item)})
			return
		case err := <-done:
			if err != nil {
				writeError(w, http.StatusConflict, "the prompt could not be started")
				return
			}
		case <-timer.C:
			writeError(w, http.StatusGatewayTimeout, "Ghost did not accept the prompt in time")
			return
		case <-r.Context().Done():
			return
		case <-s.ctx.Done():
			return
		}
	}
}

func (s *Server) interrupt(w http.ResponseWriter, r *http.Request) {
	if s.app == nil || s.app.AgentCoordinator == nil {
		writeError(w, http.StatusServiceUnavailable, "the Ghost agent is not configured")
		return
	}
	if _, ok := s.getSessionByID(w, r, r.PathValue("id")); !ok {
		return
	}
	s.app.AgentCoordinator.Cancel(r.PathValue("id"))
	writeJSON(w, http.StatusOK, map[string]any{})
}

func (s *Server) compact(w http.ResponseWriter, r *http.Request) {
	if s.app == nil || s.app.AgentCoordinator == nil {
		writeError(w, http.StatusServiceUnavailable, "the Ghost agent is not configured")
		return
	}
	id := r.PathValue("id")
	if _, ok := s.getSessionByID(w, r, id); !ok {
		return
	}
	go func() {
		if err := s.app.AgentCoordinator.Summarize(s.ctx, id); err != nil {
			s.publish("session.updated", map[string]any{"sessionID": id, "error": err.Error()})
		}
	}()
	writeJSON(w, http.StatusAccepted, map[string]any{})
}

func (s *Server) switchModel(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.getSessionByID(w, r, r.PathValue("id")); !ok {
		return
	}
	writeError(w, http.StatusNotImplemented, "model switching is not available while Ghost is running")
}

func (s *Server) listPermissions(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.getSessionByID(w, r, r.PathValue("id")); !ok {
		return
	}
	requests := s.app.Permissions.Pending(r.PathValue("id"))
	data := make([]map[string]any, 0, len(requests))
	for _, request := range requests {
		data = append(data, permissionView(request))
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": data})
}

func (s *Server) replyPermission(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Decision string `json:"decision"`
	}
	if err := decodeJSON(r, &input); err != nil || (input.Decision != "once" && input.Decision != "always" && input.Decision != "reject") {
		writeError(w, http.StatusBadRequest, "decision must be once, always, or reject")
		return
	}
	id := r.PathValue("requestID")
	var request *permission.PermissionRequest
	for _, pending := range s.app.Permissions.Pending("") {
		if pending.ID == id && pending.SessionID == r.PathValue("id") {
			request = &pending
			break
		}
	}
	if request == nil || !s.app.Permissions.Reply(id, input.Decision) {
		writeError(w, http.StatusNotFound, "permission request is no longer pending")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{})
}

func (s *Server) listForms(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.getSessionByID(w, r, r.PathValue("id")); !ok {
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": []any{}})
}

func (s *Server) replyForm(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.getSessionByID(w, r, r.PathValue("id")); !ok {
		return
	}
	writeError(w, http.StatusNotImplemented, "Ghost does not currently ask for remote form answers")
}

func (s *Server) getSessionByID(w http.ResponseWriter, r *http.Request, id string) (session.Session, bool) {
	if s.app == nil {
		writeError(w, http.StatusServiceUnavailable, "Ghost is not ready")
		return session.Session{}, false
	}
	item, err := s.app.Sessions.Get(r.Context(), id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeError(w, http.StatusNotFound, "session not found")
		} else {
			writeError(w, http.StatusInternalServerError, "could not read session")
		}
		return session.Session{}, false
	}
	return item, true
}

func (s *Server) describeSession(ctx context.Context, item session.Session) (sessionView, error) {
	workingDir := s.app.Store().WorkingDir()
	view := sessionView{
		ID:       item.ID,
		Title:    item.Title,
		Location: locationView{Directory: workingDir},
		Tokens: tokenView{
			Input:  item.PromptTokens,
			Output: item.CompletionTokens,
		},
		Time: timeView{
			Created: time.Unix(item.CreatedAt, 0).UnixMilli(),
			Updated: time.Unix(item.UpdatedAt, 0).UnixMilli(),
		},
	}
	messages, err := s.app.Messages.List(ctx, item.ID)
	if err != nil {
		return sessionView{}, err
	}
	for _, msg := range messages {
		if msg.UpdatedAt > 0 && msg.UpdatedAt*1000 > view.Time.Updated {
			view.Time.Updated = msg.UpdatedAt * 1000
		}
		if msg.Role == message.Assistant && msg.Model != "" {
			view.Model = modelView{ID: msg.Model, ProviderID: msg.Provider}
		}
	}
	if view.Model.ID == "" && s.app.AgentCoordinator != nil {
		model := s.app.AgentCoordinator.Model()
		view.Model = modelView{ID: model.ModelCfg.Model, ProviderID: model.ModelCfg.Provider}
	}
	view.Archived = false
	if pending := s.app.Permissions.Pending(item.ID); len(pending) > 0 {
		view.Waiting = &waitingView{
			Kind:   "permission",
			Tool:   pending[0].ToolName,
			Detail: pending[0].Description,
		}
	}
	return view, nil
}

func permissionView(request permission.PermissionRequest) map[string]any {
	resources := []string{}
	if request.Path != "" {
		resources = append(resources, request.Path)
	}
	if request.Params != nil {
		if raw, err := json.Marshal(request.Params); err == nil {
			var params map[string]any
			if json.Unmarshal(raw, &params) == nil {
				for _, key := range []string{"command", "url", "uri", "resource"} {
					if value, ok := params[key].(string); ok && value != "" {
						resources = append(resources, value)
					}
				}
			}
		}
	}
	return map[string]any{
		"id":        request.ID,
		"action":    request.Action,
		"message":   request.Description,
		"resources": resources,
	}
}

func (s *Server) publish(kind string, data any) {
	s.events.Publish(pubsub.CreatedEvent, Event{Type: kind, Data: data})
}

func decodeJSON(r *http.Request, target any) error {
	defer r.Body.Close()
	decoder := json.NewDecoder(io.LimitReader(r.Body, 1<<20))
	return decoder.Decode(target)
}

func writeJSON(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(data)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, errorResponse{Error: message})
}

func queryLimit(values url.Values, fallback int) (int, error) {
	if values.Get("limit") == "" {
		return fallback, nil
	}
	limit, err := strconv.Atoi(values.Get("limit"))
	if err != nil || limit < 1 || limit > 200 {
		return 0, fmt.Errorf("limit must be between 1 and 200")
	}
	return limit, nil
}

func queryOrder(values url.Values) (string, error) {
	order := values.Get("order")
	if order == "" {
		return "desc", nil
	}
	if order != "asc" && order != "desc" {
		return "", errors.New("order must be asc or desc")
	}
	return order, nil
}

// Listen opens the requested API address.
func Listen(bind string, port int) (net.Listener, error) {
	if port < 0 || port > 65535 {
		return nil, errors.New("port must be between 0 and 65535")
	}
	ip := net.ParseIP(bind)
	if ip == nil {
		return nil, errors.New("bind must be an IP address")
	}
	return net.Listen("tcp", net.JoinHostPort(ip.String(), strconv.Itoa(port)))
}
