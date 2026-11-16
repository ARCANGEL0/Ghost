package permission

import (
	"context"
	"testing"
	"time"
)

func TestReplyResolvesPendingRequestOnce(t *testing.T) {
	service := NewPermissionService(t.TempDir(), false, nil)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	requests := service.Subscribe(ctx)
	result := make(chan bool, 1)
	go func() {
		allowed, _ := service.Request(ctx, CreatePermissionRequest{
			SessionID:   "session-1",
			ToolCallID:  "tool-1",
			ToolName:    "Bash",
			Action:      "run",
			Description: "Run the build",
		})
		result <- allowed
	}()

	var request PermissionRequest
	select {
	case event := <-requests:
		request = event.Payload
	case <-ctx.Done():
		t.Fatal("permission request was not published")
	}
	if pending := service.Pending("session-1"); len(pending) != 1 || pending[0].ID != request.ID {
		t.Fatalf("unexpected pending request: %#v", pending)
	}
	if !service.Reply(request.ID, "once") {
		t.Fatal("first reply was not accepted")
	}
	if service.Reply(request.ID, "once") {
		t.Fatal("second reply should be ignored")
	}
	select {
	case allowed := <-result:
		if !allowed {
			t.Fatal("request was denied")
		}
	case <-ctx.Done():
		t.Fatal("permission request did not finish")
	}
	if pending := service.Pending("session-1"); len(pending) != 0 {
		t.Fatalf("pending request remains: %#v", pending)
	}
}
