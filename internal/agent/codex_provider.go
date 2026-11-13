package agent

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"path/filepath"
	"strings"

	"charm.land/fantasy"
	"github.com/swadhinbiswas/ghost/internal/agent/tools"
	"github.com/swadhinbiswas/ghost/internal/permission"
	"github.com/swadhinbiswas/ghost/internal/session"
	"github.com/swadhinbiswas/ghost/internal/version"
)

type codexProvider struct {
	permissions permission.Service
	sessions    session.Service
	workingDir  string
}

type codexModel struct {
	provider codexProvider
	modelID  string
}

type codexRPC struct {
	cmd         *exec.Cmd
	stdin       io.WriteCloser
	scanner     *bufio.Scanner
	pending     []codexRPCMessage
	permissions permission.Service
	nextID      int
}

type codexRPCMessage struct {
	ID     json.RawMessage `json:"id"`
	Method string          `json:"method"`
	Params json.RawMessage `json:"params"`
	Result json.RawMessage `json:"result"`
	Error  *codexRPCError  `json:"error"`
}

type codexRPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func (e *codexRPCError) Error() string {
	return e.Message
}

func (p codexProvider) Name() string {
	return "codex"
}

func (p codexProvider) LanguageModel(_ context.Context, modelID string) (fantasy.LanguageModel, error) {
	if _, err := exec.LookPath("codex"); err != nil {
		return nil, fmt.Errorf("Codex CLI is required; run `ghost login codex` after installing it: %w", err)
	}
	return &codexModel{provider: p, modelID: modelID}, nil
}

func (m *codexModel) Provider() string { return "codex" }

func (m *codexModel) Model() string { return m.modelID }

func (m *codexModel) Generate(ctx context.Context, call fantasy.Call) (*fantasy.Response, error) {
	stream, err := m.Stream(ctx, call)
	if err != nil {
		return nil, err
	}
	var text strings.Builder
	for part := range stream {
		switch part.Type {
		case fantasy.StreamPartTypeTextDelta:
			text.WriteString(part.Delta)
		case fantasy.StreamPartTypeError:
			return nil, part.Error
		}
	}
	if text.Len() == 0 {
		return nil, errors.New("Codex returned an empty response")
	}
	return &fantasy.Response{
		Content:      fantasy.ResponseContent{fantasy.TextContent{Text: text.String()}},
		FinishReason: fantasy.FinishReasonStop,
	}, nil
}

func (m *codexModel) Stream(ctx context.Context, call fantasy.Call) (fantasy.StreamResponse, error) {
	return func(yield func(fantasy.StreamPart) bool) {
		stopped := false
		err := m.run(ctx, call.Prompt, func(delta string) bool {
			if delta == "" {
				return true
			}
			if !yield(fantasy.StreamPart{Type: fantasy.StreamPartTypeTextDelta, Delta: delta}) {
				stopped = true
				return false
			}
			return true
		})
		if stopped {
			return
		}
		if err != nil {
			yield(fantasy.StreamPart{Type: fantasy.StreamPartTypeError, Error: err})
			return
		}
		yield(fantasy.StreamPart{Type: fantasy.StreamPartTypeFinish, FinishReason: fantasy.FinishReasonStop})
	}, nil
}

func (m *codexModel) GenerateObject(ctx context.Context, call fantasy.ObjectCall) (*fantasy.ObjectResponse, error) {
	response, err := m.Generate(ctx, fantasy.Call{Prompt: call.Prompt, MaxOutputTokens: call.MaxOutputTokens})
	if err != nil {
		return nil, err
	}
	var object any
	if err := json.Unmarshal([]byte(response.Content.Text()), &object); err != nil {
		return nil, fmt.Errorf("parse Codex JSON response: %w", err)
	}
	return &fantasy.ObjectResponse{Object: object, RawText: response.Content.Text(), FinishReason: response.FinishReason}, nil
}

func (m *codexModel) StreamObject(ctx context.Context, call fantasy.ObjectCall) (fantasy.ObjectStreamResponse, error) {
	return func(yield func(fantasy.ObjectStreamPart) bool) {
		response, err := m.GenerateObject(ctx, call)
		if err != nil {
			yield(fantasy.ObjectStreamPart{Type: fantasy.ObjectStreamPartTypeError, Error: err})
			return
		}
		yield(fantasy.ObjectStreamPart{Type: fantasy.ObjectStreamPartTypeObject, Object: response.Object})
		yield(fantasy.ObjectStreamPart{Type: fantasy.ObjectStreamPartTypeFinish, FinishReason: response.FinishReason})
	}, nil
}

func (m *codexModel) run(ctx context.Context, prompt fantasy.Prompt, onDelta func(string) bool) error {
	cmd := exec.CommandContext(ctx, "codex", "app-server", "--listen", "stdio://")
	cmd.Dir = m.provider.workingDir
	cmd.Stderr = io.Discard
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return fmt.Errorf("start Codex app-server: %w", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("start Codex app-server: %w", err)
	}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start Codex app-server: %w", err)
	}
	defer func() {
		_ = stdin.Close()
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
		}
		_ = cmd.Wait()
	}()

	rpc := &codexRPC{
		cmd:         cmd,
		stdin:       stdin,
		scanner:     bufio.NewScanner(stdout),
		permissions: m.provider.permissions,
	}
	rpc.scanner.Buffer(make([]byte, 64*1024), 16<<20)
	if _, err := rpc.request(ctx, "initialize", map[string]any{
		"clientInfo": map[string]string{
			"name":    "ghost",
			"title":   "Ghost",
			"version": version.Version,
		},
	}); err != nil {
		return fmt.Errorf("connect to Codex app-server: %w", err)
	}
	if err := rpc.notify("initialized", map[string]any{}); err != nil {
		return err
	}

	sessionID, _ := ctx.Value(tools.SessionIDContextKey).(string)
	threadID, resumed, err := m.openThread(ctx, rpc, sessionID, prompt)
	if err != nil {
		return err
	}
	text := codexPrompt(prompt)
	if resumed {
		if last := codexLastUser(prompt); last != "" {
			text = last
		}
	}
	approvalPolicy, sandboxPolicy := m.turnAccess(sessionID != "")
	_, err = rpc.request(ctx, "turn/start", map[string]any{
		"threadId":       threadID,
		"input":          []map[string]string{{"type": "text", "text": text}},
		"cwd":            m.provider.workingDir,
		"model":          m.modelID,
		"approvalPolicy": approvalPolicy,
		"sandboxPolicy":  sandboxPolicy,
	}, sessionID)
	if err != nil {
		return fmt.Errorf("start Codex turn: %w", err)
	}

	var output strings.Builder
	for {
		msg, err := rpc.next(ctx)
		if err != nil {
			return fmt.Errorf("read Codex app-server: %w", err)
		}
		if msg.Method == "item/agentMessage/delta" {
			var event struct {
				Delta string `json:"delta"`
			}
			if json.Unmarshal(msg.Params, &event) == nil && event.Delta != "" {
				output.WriteString(event.Delta)
				if !onDelta(event.Delta) {
					return nil
				}
			}
			continue
		}
		if msg.Method == "turn/completed" {
			status, finalText, turnErr := codexTurnResult(msg.Params)
			if output.Len() == 0 && finalText != "" {
				output.WriteString(finalText)
				if !onDelta(finalText) {
					return nil
				}
			}
			if status != "completed" {
				if turnErr == "" {
					turnErr = "turn " + status
				}
				return fmt.Errorf("Codex %s", turnErr)
			}
			if output.Len() == 0 {
				return errors.New("Codex returned an empty response")
			}
			return nil
		}
		if msg.Method == "error" {
			var event struct {
				Error codexRPCError `json:"error"`
			}
			if json.Unmarshal(msg.Params, &event) == nil && event.Error.Message != "" {
				return fmt.Errorf("Codex app-server: %s", event.Error.Message)
			}
		}
		if hasRPCID(msg.ID) && msg.Method != "" {
			if err := rpc.handleRequest(ctx, msg, sessionID, m.provider.workingDir); err != nil {
				return err
			}
		}
	}
}

func (m *codexModel) openThread(ctx context.Context, rpc *codexRPC, sessionID string, prompt fantasy.Prompt) (string, bool, error) {
	if sessionID != "" {
		link, found, err := m.provider.sessions.ExternalSession(ctx, sessionID)
		if err != nil {
			return "", false, err
		}
		if found && link.Provider == "codex" {
			result, err := rpc.request(ctx, "thread/resume", map[string]any{
				"threadId":     link.ID,
				"excludeTurns": true,
			})
			if err == nil {
				var response struct {
					Thread struct {
						ID string `json:"id"`
					} `json:"thread"`
				}
				if json.Unmarshal(result, &response) == nil && response.Thread.ID != "" {
					return response.Thread.ID, true, nil
				}
			}
		}
	}

	approvalPolicy, _ := m.turnAccess(sessionID != "")
	sandbox := "read-only"
	if sessionID != "" {
		sandbox = "workspace-write"
	}
	result, err := rpc.request(ctx, "thread/start", map[string]any{
		"model":          m.modelID,
		"cwd":            m.provider.workingDir,
		"approvalPolicy": approvalPolicy,
		"sandbox":        sandbox,
	})
	if err != nil {
		return "", false, fmt.Errorf("start Codex thread: %w", err)
	}
	var response struct {
		Thread struct {
			ID string `json:"id"`
		} `json:"thread"`
	}
	if err := json.Unmarshal(result, &response); err != nil || response.Thread.ID == "" {
		return "", false, errors.New("Codex app-server did not return a thread ID")
	}
	if sessionID != "" {
		if err := m.provider.sessions.SetExternalSession(ctx, sessionID, "codex", response.Thread.ID); err != nil {
			return "", false, err
		}
	}
	return response.Thread.ID, false, nil
}

func (m *codexModel) turnAccess(hasSession bool) (string, map[string]any) {
	if !hasSession {
		return "never", map[string]any{"type": "readOnly"}
	}
	return "on-request", map[string]any{
		"type":          "workspaceWrite",
		"writableRoots": []string{m.provider.workingDir},
		"networkAccess": false,
	}
}

func (r *codexRPC) request(ctx context.Context, method string, params any, sessionIDs ...string) (json.RawMessage, error) {
	r.nextID++
	id, _ := json.Marshal(r.nextID)
	sessionID := ""
	if len(sessionIDs) > 0 {
		sessionID = sessionIDs[0]
	}
	if err := r.write(map[string]any{"id": r.nextID, "method": method, "params": params}); err != nil {
		return nil, err
	}
	for {
		msg, err := r.readLine(ctx)
		if err != nil {
			return nil, err
		}
		if msg.Method != "" && hasRPCID(msg.ID) {
			if err := r.handleRequest(ctx, msg, sessionID, r.cmd.Dir); err != nil {
				return nil, err
			}
			continue
		}
		if bytes.Equal(msg.ID, id) {
			if msg.Error != nil {
				return nil, msg.Error
			}
			return msg.Result, nil
		}
		r.pending = append(r.pending, msg)
	}
}

func (r *codexRPC) notify(method string, params any) error {
	return r.write(map[string]any{"method": method, "params": params})
}

func (r *codexRPC) write(value any) error {
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	if _, err := r.stdin.Write(append(data, '\n')); err != nil {
		return fmt.Errorf("write Codex app-server request: %w", err)
	}
	return nil
}

func (r *codexRPC) readLine(ctx context.Context) (codexRPCMessage, error) {
	if err := ctx.Err(); err != nil {
		return codexRPCMessage{}, err
	}
	if !r.scanner.Scan() {
		if err := ctx.Err(); err != nil {
			return codexRPCMessage{}, err
		}
		if err := r.scanner.Err(); err != nil {
			return codexRPCMessage{}, err
		}
		return codexRPCMessage{}, io.EOF
	}
	var msg codexRPCMessage
	if err := json.Unmarshal(r.scanner.Bytes(), &msg); err != nil {
		return codexRPCMessage{}, fmt.Errorf("decode Codex app-server message: %w", err)
	}
	return msg, nil
}

func (r *codexRPC) next(ctx context.Context) (codexRPCMessage, error) {
	if len(r.pending) > 0 {
		msg := r.pending[0]
		r.pending = r.pending[1:]
		return msg, nil
	}
	return r.readLine(ctx)
}

func (r *codexRPC) handleRequest(ctx context.Context, msg codexRPCMessage, sessionID, workingDir string) error {
	commandApproval := msg.Method == "item/commandExecution/requestApproval"
	fileApproval := msg.Method == "item/fileChange/requestApproval"
	permissionApproval := msg.Method == "item/permissions/requestApproval"
	if !commandApproval && !fileApproval && !permissionApproval {
		return r.replyError(msg.ID, -32601, "unsupported Codex request")
	}

	var request struct {
		ItemID                 string   `json:"itemId"`
		Reason                 string   `json:"reason"`
		Cwd                    string   `json:"cwd"`
		Command                []string `json:"command"`
		NetworkApprovalContext struct {
			Host     string `json:"host"`
			Protocol string `json:"protocol"`
		} `json:"networkApprovalContext"`
	}
	if err := json.Unmarshal(msg.Params, &request); err != nil {
		return r.replyError(msg.ID, -32602, "invalid approval request")
	}
	if sessionID == "" || r.cmd == nil {
		return r.replyError(msg.ID, -32601, "approval is unavailable")
	}

	if permissionApproval {
		var fields map[string]json.RawMessage
		if err := json.Unmarshal(msg.Params, &fields); err != nil {
			return r.replyError(msg.ID, -32602, "invalid permission request")
		}
		requested := fields["permissions"]
		if len(requested) == 0 {
			requested = fields["requestedPermissions"]
		}
		if len(requested) == 0 {
			return r.write(map[string]any{"id": msg.ID, "result": map[string]any{"permissions": map[string]any{}, "scope": "turn"}})
		}
		description := request.Reason
		if description == "" {
			description = "Allow Codex additional workspace or network access"
		}
		permissionID := request.ItemID
		if permissionID == "" {
			permissionID = fmt.Sprintf("codex-%d", r.nextID)
		}
		requestDir := request.Cwd
		if requestDir == "" {
			requestDir = workingDir
		}
		allowed, err := r.permissions.Request(ctx, permission.CreatePermissionRequest{
			SessionID:   sessionID,
			ToolCallID:  permissionID,
			ToolName:    "Codex access",
			Description: description,
			Action:      "grant",
			Params:      json.RawMessage(msg.Params),
			Path:        filepath.Clean(requestDir),
		})
		if err != nil {
			return err
		}
		var grants any = map[string]any{}
		if allowed {
			if err := json.Unmarshal(requested, &grants); err != nil {
				return r.replyError(msg.ID, -32602, "invalid requested permissions")
			}
		}
		return r.write(map[string]any{"id": msg.ID, "result": map[string]any{"permissions": grants, "scope": "turn"}})
	}

	command := strings.Join(request.Command, " ")
	toolName, action, description := "Codex command", "execute", command
	if request.NetworkApprovalContext.Host != "" {
		toolName, action = "Codex network", "connect"
		description = request.NetworkApprovalContext.Protocol + " " + request.NetworkApprovalContext.Host
	}
	if fileApproval {
		toolName, action = "Codex file change", "write"
		description = request.Reason
		if description == "" {
			description = "Apply proposed file changes"
		}
	}
	if description == "" {
		description = "Run a Codex command"
	}
	requestDir := request.Cwd
	if requestDir == "" {
		requestDir = workingDir
	}
	permissionID := request.ItemID
	if permissionID == "" {
		permissionID = fmt.Sprintf("codex-%d", r.nextID)
	}
	allowed, err := r.permissions.Request(ctx, permission.CreatePermissionRequest{
		SessionID:   sessionID,
		ToolCallID:  permissionID,
		ToolName:    toolName,
		Description: description,
		Action:      action,
		Params:      json.RawMessage(msg.Params),
		Path:        filepath.Clean(requestDir),
	})
	if err != nil {
		return err
	}
	decision := "decline"
	if allowed {
		decision = "accept"
	}
	return r.write(map[string]any{"id": msg.ID, "result": map[string]string{"decision": decision}})
}

func (r *codexRPC) replyError(id json.RawMessage, code int, message string) error {
	return r.write(map[string]any{"id": id, "error": map[string]any{"code": code, "message": message}})
}

func hasRPCID(id json.RawMessage) bool {
	return len(id) > 0 && string(id) != "null"
}

func codexPrompt(prompt fantasy.Prompt) string {
	var out strings.Builder
	for _, msg := range prompt {
		text := codexMessageText(msg)
		if text == "" {
			continue
		}
		fmt.Fprintf(&out, "[%s]\n%s\n\n", msg.Role, text)
	}
	return strings.TrimSpace(out.String())
}

func codexLastUser(prompt fantasy.Prompt) string {
	for i := len(prompt) - 1; i >= 0; i-- {
		if prompt[i].Role == fantasy.MessageRoleUser {
			return codexMessageText(prompt[i])
		}
	}
	return ""
}

func codexMessageText(msg fantasy.Message) string {
	var out []string
	for _, part := range msg.Content {
		switch part := part.(type) {
		case fantasy.TextPart:
			out = append(out, part.Text)
		case fantasy.ReasoningPart:
			out = append(out, part.Text)
		case fantasy.FilePart:
			if strings.HasPrefix(part.MediaType, "text/") || part.MediaType == "application/json" {
				out = append(out, fmt.Sprintf("Attachment %s:\n%s", part.Filename, strings.ToValidUTF8(string(part.Data), "�")))
			} else {
				out = append(out, fmt.Sprintf("Attachment %s (%s)", part.Filename, part.MediaType))
			}
		case fantasy.ToolCallPart:
			out = append(out, "Tool call "+part.ToolName+": "+part.Input)
		case fantasy.ToolResultPart:
			data, err := json.Marshal(part.Output)
			if err == nil {
				out = append(out, "Tool result: "+string(data))
			}
		}
	}
	return strings.Join(out, "\n")
}

func codexTurnResult(raw json.RawMessage) (string, string, string) {
	var response struct {
		Turn struct {
			Status string          `json:"status"`
			Error  json.RawMessage `json:"error"`
			Items  []struct {
				Type    string `json:"type"`
				Content []struct {
					Type string `json:"type"`
					Text string `json:"text"`
				} `json:"content"`
			} `json:"items"`
		} `json:"turn"`
	}
	if json.Unmarshal(raw, &response) != nil {
		return "", "", ""
	}
	var final []string
	for _, item := range response.Turn.Items {
		if item.Type != "agentMessage" && item.Type != "agent_message" {
			continue
		}
		for _, content := range item.Content {
			if content.Type == "output_text" || content.Type == "text" {
				final = append(final, content.Text)
			}
		}
	}
	var turnErr string
	if len(response.Turn.Error) > 0 && string(response.Turn.Error) != "null" {
		turnErr = strings.Trim(string(response.Turn.Error), `"`)
	}
	return response.Turn.Status, strings.Join(final, ""), turnErr
}
