package remote

import (
	"bytes"
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"charm.land/fantasy"
	"github.com/swadhinbiswas/ghost/internal/agent"
	"github.com/swadhinbiswas/ghost/internal/app"
	"github.com/swadhinbiswas/ghost/internal/message"
	"github.com/swadhinbiswas/ghost/internal/permission"
	"github.com/swadhinbiswas/ghost/internal/pubsub"
	"github.com/swadhinbiswas/ghost/internal/session"
)

func TestInfoRequiresBearerToken(t *testing.T) {
	handler := NewServer(context.Background(), nil, "secret").Handler()
	tests := []struct {
		name   string
		header string
		status int
	}{
		{name: "missing", status: http.StatusUnauthorized},
		{name: "wrong", header: "Bearer other", status: http.StatusUnauthorized},
		{name: "wrong scheme", header: "Basic secret", status: http.StatusUnauthorized},
		{name: "valid", header: "Bearer secret", status: http.StatusOK},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/api/info", nil)
			if test.header != "" {
				req.Header.Set("Authorization", test.header)
			}
			res := httptest.NewRecorder()
			handler.ServeHTTP(res, req)
			if res.Code != test.status {
				t.Fatalf("status = %d, want %d", res.Code, test.status)
			}
			if test.status == http.StatusOK {
				var info map[string]string
				if err := json.Unmarshal(res.Body.Bytes(), &info); err != nil {
					t.Fatal(err)
				}
				if info["app"] != "ghost" || info["api"] != apiVersion {
					t.Fatalf("unexpected info response: %#v", info)
				}
			}
		})
	}
}

func TestListenReportsPortInUse(t *testing.T) {
	listener, err := Listen("127.0.0.1", 0)
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()

	_, port, err := net.SplitHostPort(listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	address, err := net.ResolveTCPAddr("tcp", net.JoinHostPort("127.0.0.1", port))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Listen("127.0.0.1", address.Port); err == nil {
		t.Fatal("expected a port-in-use error")
	}
}

func TestLoadTokenStoresPrivateFile(t *testing.T) {
	configDir := t.TempDir()
	t.Setenv("GHOST_GLOBAL_CONFIG", configDir)
	token, err := LoadToken("")
	if err != nil {
		t.Fatal(err)
	}
	if token == "" {
		t.Fatal("token was empty")
	}
	info, err := os.Stat(filepath.Join(configDir, "remote.json"))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("token file mode = %o, want 600", info.Mode().Perm())
	}
	if got, err := LoadToken(""); err != nil || got != token {
		t.Fatalf("second token = %q, %v; want %q", got, err, token)
	}
}

func TestConvertMessagesKeepsToolStateAndStableIDs(t *testing.T) {
	items := []message.Message{
		{
			ID:        "assistant-1",
			Role:      message.Assistant,
			CreatedAt: 1,
			Parts: []message.ContentPart{
				message.TextContent{Text: "Done"},
				message.ToolCall{ID: "tool-1", Name: "bash", Input: `{"command":"go test"}`, Finished: true},
				message.Finish{Reason: message.FinishReasonError, Time: time.Unix(2, 0).Unix()},
			},
		},
		{
			ID:   "tool-message",
			Role: message.Tool,
			Parts: []message.ContentPart{
				message.ToolResult{ToolCallID: "tool-1", Content: "failed", IsError: true},
			},
		},
	}

	first := convertMessages(items)
	second := convertMessages(items)
	if len(first) != 2 || first[0].ID != second[0].ID || first[1].ID != second[1].ID {
		t.Fatalf("message ids changed: %#v / %#v", first, second)
	}
	tool := first[0].Content[1]
	if tool.State == nil || tool.State.Status != "error" || tool.State.Error == nil {
		t.Fatalf("tool state was not carried over: %#v", tool)
	}
	if first[1].Type != "idle" || first[1].Outcome != "failed" {
		t.Fatalf("unexpected turn completion: %#v", first[1])
	}
}

func TestPromptReturnsSavedMessageID(t *testing.T) {
	messages := &testMessages{
		Broker: pubsub.NewBroker[message.Message](),
	}
	sessions := testSessions{
		Broker: pubsub.NewBroker[session.Session](),
		item:   session.Session{ID: "session-1", Title: "Test"},
	}
	coordinator := &testCoordinator{messages: messages, done: make(chan struct{})}
	ghostApp := &app.App{
		Sessions:         sessions,
		Messages:         messages,
		Permissions:      permission.NewPermissionService(t.TempDir(), false, nil),
		AgentCoordinator: coordinator,
	}
	handler := NewServer(context.Background(), ghostApp, "secret").Handler()
	req := httptest.NewRequest(http.MethodPost, "/api/session/session-1/prompt", bytes.NewBufferString(`{"text":"Check the build"}`))
	req.Header.Set("Authorization", "Bearer secret")
	req.Header.Set("Content-Type", "application/json")
	res := httptest.NewRecorder()
	handler.ServeHTTP(res, req)
	if res.Code != http.StatusAccepted {
		t.Fatalf("status = %d, body = %s", res.Code, res.Body.String())
	}
	var response struct {
		Data apiMessage `json:"data"`
	}
	if err := json.Unmarshal(res.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if response.Data.ID != "user-1" || response.Data.Text != "Check the build" {
		t.Fatalf("unexpected user message: %#v", response.Data)
	}
	select {
	case <-coordinator.done:
	case <-time.After(time.Second):
		t.Fatal("prompt run did not finish")
	}
}

func TestEventStreamPublishesUserMessages(t *testing.T) {
	messages := &testMessages{Broker: pubsub.NewBroker[message.Message]()}
	sessions := testSessions{Broker: pubsub.NewBroker[session.Session]()}
	ghostApp := &app.App{
		Sessions:    sessions,
		Messages:    messages,
		Permissions: permission.NewPermissionService(t.TempDir(), false, nil),
	}
	handler := NewServer(context.Background(), ghostApp, "secret").Handler()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	writer := &streamWriter{header: make(http.Header), writes: make(chan string, 10)}
	req := httptest.NewRequest(http.MethodGet, "/api/event", nil).WithContext(ctx)
	req.Header.Set("Authorization", "Bearer secret")
	done := make(chan struct{})
	go func() {
		handler.ServeHTTP(writer, req)
		close(done)
	}()
	select {
	case event := <-writer.writes:
		if !bytes.Contains([]byte(event), []byte("server.connected")) {
			t.Fatalf("first event = %s", event)
		}
	case <-time.After(time.Second):
		t.Fatal("server.connected event was not sent")
	}
	_, err := messages.Create(ctx, "session-1", message.CreateMessageParams{
		Role:  message.User,
		Parts: []message.ContentPart{message.TextContent{Text: "Check the build"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	seen := false
	for !seen {
		select {
		case event := <-writer.writes:
			seen = bytes.Contains([]byte(event), []byte("session.inbox.enqueued"))
		case <-time.After(time.Second):
			t.Fatal("inbox event was not sent")
		}
	}
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("event stream did not close")
	}
}

type testSessions struct {
	session.Service
	*pubsub.Broker[session.Session]
	item session.Session
}

func (s testSessions) Subscribe(ctx context.Context) <-chan pubsub.Event[session.Session] {
	return s.Broker.Subscribe(ctx)
}

func (s testSessions) Get(context.Context, string) (session.Session, error) {
	return s.item, nil
}

type testMessages struct {
	message.Service
	*pubsub.Broker[message.Message]
	items []message.Message
}

func (m *testMessages) Subscribe(ctx context.Context) <-chan pubsub.Event[message.Message] {
	return m.Broker.Subscribe(ctx)
}

func (m *testMessages) Create(_ context.Context, sessionID string, params message.CreateMessageParams) (message.Message, error) {
	item := message.Message{
		ID:        "user-1",
		Role:      params.Role,
		SessionID: sessionID,
		Parts:     params.Parts,
		CreatedAt: time.Now().Unix(),
	}
	m.items = append(m.items, item)
	m.Broker.Publish(pubsub.CreatedEvent, item)
	return item, nil
}

type testCoordinator struct {
	agent.Coordinator
	messages *testMessages
	done     chan struct{}
}

func (c *testCoordinator) Run(ctx context.Context, sessionID, prompt string, _ ...message.Attachment) (*fantasy.AgentResult, error) {
	defer close(c.done)
	_, err := c.messages.Create(ctx, sessionID, message.CreateMessageParams{
		Role:  message.User,
		Parts: []message.ContentPart{message.TextContent{Text: prompt}},
	})
	return nil, err
}

func (*testCoordinator) IsSessionBusy(string) bool { return false }

func (*testCoordinator) Model() agent.Model { return agent.Model{} }

type streamWriter struct {
	header http.Header
	writes chan string
}

func (w *streamWriter) Header() http.Header { return w.header }

func (w *streamWriter) WriteHeader(int) {}

func (w *streamWriter) Write(data []byte) (int, error) {
	w.writes <- string(data)
	return len(data), nil
}

func (*streamWriter) Flush() {}
