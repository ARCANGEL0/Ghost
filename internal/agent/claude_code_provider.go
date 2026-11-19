package agent

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"

	"charm.land/fantasy"
	"github.com/swadhinbiswas/ghost/internal/agent/tools"
	"github.com/swadhinbiswas/ghost/internal/claudecode"
	"github.com/swadhinbiswas/ghost/internal/permission"
	"github.com/swadhinbiswas/ghost/internal/session"
	"github.com/swadhinbiswas/ghost/internal/version"
)

const claudeACPVersion = "0.109.0"

var errClaudeStreamClosed = errors.New("Claude stream closed")

type claudeCodeProvider struct {
	permissions permission.Service
	sessions    session.Service
	workingDir  string
}

type claudeCodeModel struct {
	provider claudeCodeProvider
	modelID  string
}

type claudeACP struct {
	cmd         *exec.Cmd
	stdin       io.WriteCloser
	scanner     *bufio.Scanner
	permissions permission.Service
	ghostID     string
	nextID      int
}

type claudeACPLogs struct {
	mu   sync.Mutex
	text bytes.Buffer
}

func (b *claudeACPLogs) Write(data []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.text.Write(data)
}

func (b *claudeACPLogs) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.text.String()
}

type claudeACPMessage struct {
	ID     json.RawMessage `json:"id"`
	Method string          `json:"method"`
	Params json.RawMessage `json:"params"`
	Result json.RawMessage `json:"result"`
	Error  *claudeACPError `json:"error"`
}

type claudeACPError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func (e *claudeACPError) Error() string { return e.Message }

func (p claudeCodeProvider) Name() string { return "claude-code" }

func (p claudeCodeProvider) LanguageModel(ctx context.Context, modelID string) (fantasy.LanguageModel, error) {
	if _, err := claudecode.Check(ctx, false); err != nil {
		return nil, err
	}
	return &claudeCodeModel{provider: p, modelID: modelID}, nil
}

func (m *claudeCodeModel) Provider() string { return "claude-code" }

func (m *claudeCodeModel) Model() string { return m.modelID }

func (m *claudeCodeModel) Generate(ctx context.Context, call fantasy.Call) (*fantasy.Response, error) {
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
		return nil, errors.New("Claude Code returned an empty response")
	}
	return &fantasy.Response{
		Content:      fantasy.ResponseContent{fantasy.TextContent{Text: text.String()}},
		FinishReason: fantasy.FinishReasonStop,
	}, nil
}

func (m *claudeCodeModel) Stream(ctx context.Context, call fantasy.Call) (fantasy.StreamResponse, error) {
	return func(yield func(fantasy.StreamPart) bool) {
		started := false
		stopped := false
		err := m.run(ctx, call.Prompt, func(delta string) bool {
			if delta == "" {
				return true
			}
			if !started {
				if !yield(fantasy.StreamPart{Type: fantasy.StreamPartTypeTextStart, ID: "claude-code"}) {
					stopped = true
					return false
				}
				started = true
			}
			if !yield(fantasy.StreamPart{Type: fantasy.StreamPartTypeTextDelta, ID: "claude-code", Delta: delta}) {
				stopped = true
				return false
			}
			return true
		})
		if stopped || errors.Is(err, errClaudeStreamClosed) {
			return
		}
		if err != nil {
			yield(fantasy.StreamPart{Type: fantasy.StreamPartTypeError, Error: err})
			return
		}
		if started {
			yield(fantasy.StreamPart{Type: fantasy.StreamPartTypeTextEnd, ID: "claude-code"})
		}
		yield(fantasy.StreamPart{Type: fantasy.StreamPartTypeFinish, FinishReason: fantasy.FinishReasonStop})
	}, nil
}

func (m *claudeCodeModel) GenerateObject(ctx context.Context, call fantasy.ObjectCall) (*fantasy.ObjectResponse, error) {
	response, err := m.Generate(ctx, fantasy.Call{Prompt: call.Prompt, MaxOutputTokens: call.MaxOutputTokens})
	if err != nil {
		return nil, err
	}
	var object any
	if err := json.Unmarshal([]byte(response.Content.Text()), &object); err != nil {
		return nil, fmt.Errorf("parse Claude Code JSON response: %w", err)
	}
	return &fantasy.ObjectResponse{Object: object, RawText: response.Content.Text(), FinishReason: response.FinishReason}, nil
}

func (m *claudeCodeModel) StreamObject(ctx context.Context, call fantasy.ObjectCall) (fantasy.ObjectStreamResponse, error) {
	return func(yield func(fantasy.ObjectStreamPart) bool) {
		response, err := m.GenerateObject(ctx, call)
		if err != nil {
			yield(fantasy.ObjectStreamPart{Type: fantasy.ObjectStreamPartTypeError, Error: err})
			return
		}
		if !yield(fantasy.ObjectStreamPart{Type: fantasy.ObjectStreamPartTypeObject, Object: response.Object}) {
			return
		}
		yield(fantasy.ObjectStreamPart{Type: fantasy.ObjectStreamPartTypeFinish, FinishReason: response.FinishReason})
	}, nil
}

func (m *claudeCodeModel) run(ctx context.Context, prompt fantasy.Prompt, onDelta func(string) bool) error {
	cli, err := exec.LookPath("claude")
	if err != nil {
		return errors.New("Claude Code is not installed or is missing from PATH")
	}
	cmd, err := claudeACPCommand(ctx)
	if err != nil {
		return err
	}
	cmd.Dir = m.provider.workingDir
	cmd.Env = claudecode.CommandEnv(os.Environ(), cli)
	stderr := &claudeACPLogs{}
	cmd.Stderr = stderr
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return fmt.Errorf("start Claude Code ACP: %w", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("start Claude Code ACP: %w", err)
	}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start Claude Code ACP: %w", err)
	}
	defer func() {
		_ = stdin.Close()
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
		}
		_ = cmd.Wait()
	}()

	rpc := &claudeACP{
		cmd:         cmd,
		stdin:       stdin,
		scanner:     bufio.NewScanner(stdout),
		permissions: m.provider.permissions,
	}
	rpc.scanner.Buffer(make([]byte, 64*1024), 16<<20)
	if _, err := rpc.request(ctx, "initialize", map[string]any{
		"protocolVersion": 1,
		"clientInfo": map[string]string{
			"name":    "ghost",
			"title":   "Ghost",
			"version": version.Version,
		},
		"clientCapabilities": map[string]any{"terminal": false},
	}, nil); err != nil {
		return claudeACPFailure("connect to Claude Code ACP", err, stderr)
	}
	if err := rpc.notify("initialized", map[string]any{}); err != nil {
		return err
	}

	ghostID, _ := ctx.Value(tools.SessionIDContextKey).(string)
	rpc.ghostID = ghostID
	acpID, resumed := m.loadSession(ctx, rpc, ghostID)
	if !resumed {
		acpID, err = m.newSession(ctx, rpc, ghostID)
		if err != nil {
			return claudeACPFailure("start Claude Code session", err, stderr)
		}
	}
	if _, err := rpc.request(ctx, "session/set_mode", map[string]any{
		"sessionId": acpID,
		"modeId":    "default",
	}, nil); err != nil {
		return claudeACPFailure("set Claude Code permission mode", err, stderr)
	}
	text := codexPrompt(prompt)
	if resumed {
		if latest := codexLastUser(prompt); latest != "" {
			text = latest
		}
	}
	_, err = rpc.request(ctx, "session/prompt", map[string]any{
		"sessionId": acpID,
		"prompt":    []map[string]string{{"type": "text", "text": text}},
	}, onDelta)
	if err != nil {
		if errors.Is(err, errClaudeStreamClosed) {
			return err
		}
		return claudeACPFailure("Claude Code request failed", err, stderr)
	}
	return nil
}

func claudeACPFailure(action string, err error, stderr *claudeACPLogs) error {
	detail := strings.TrimSpace(stderr.String())
	if len(detail) > 300 {
		detail = detail[len(detail)-300:]
	}
	if detail != "" {
		return fmt.Errorf("%s: %w: %s", action, err, detail)
	}
	return fmt.Errorf("%s: %w", action, err)
}

func claudeACPCommand(ctx context.Context) (*exec.Cmd, error) {
	if path, err := exec.LookPath("claude-agent-acp"); err == nil {
		return exec.CommandContext(ctx, path), nil
	}
	npx, err := exec.LookPath("npx")
	if err != nil {
		return nil, errors.New("Claude Code ACP is missing; install Node.js 22 or `@agentclientprotocol/claude-agent-acp`")
	}
	return exec.CommandContext(ctx, npx, "--yes", "--package=@agentclientprotocol/claude-agent-acp@"+claudeACPVersion, "--", "claude-agent-acp"), nil
}

func (m *claudeCodeModel) loadSession(ctx context.Context, rpc *claudeACP, ghostID string) (string, bool) {
	if ghostID == "" {
		return "", false
	}
	link, found, err := m.provider.sessions.ExternalSession(ctx, ghostID)
	if err != nil || !found || link.Provider != "claude-code" {
		return "", false
	}
	result, err := rpc.request(ctx, "session/load", map[string]any{
		"sessionId":  link.ID,
		"cwd":        m.provider.workingDir,
		"mcpServers": []any{},
	}, nil)
	if err != nil {
		return "", false
	}
	var response struct {
		SessionID string `json:"sessionId"`
	}
	if json.Unmarshal(result, &response) != nil || response.SessionID == "" {
		return "", false
	}
	return response.SessionID, true
}

func (m *claudeCodeModel) newSession(ctx context.Context, rpc *claudeACP, ghostID string) (string, error) {
	result, err := rpc.request(ctx, "session/new", map[string]any{
		"cwd":        m.provider.workingDir,
		"mcpServers": []any{},
	}, nil)
	if err != nil {
		return "", fmt.Errorf("start Claude Code session: %w", err)
	}
	var response struct {
		SessionID string `json:"sessionId"`
	}
	if err := json.Unmarshal(result, &response); err != nil || response.SessionID == "" {
		return "", errors.New("Claude Code ACP did not return a session ID")
	}
	if ghostID != "" {
		if err := m.provider.sessions.SetExternalSession(ctx, ghostID, "claude-code", response.SessionID); err != nil {
			return "", err
		}
	}
	return response.SessionID, nil
}

func (r *claudeACP) request(ctx context.Context, method string, params any, onDelta func(string) bool) (json.RawMessage, error) {
	r.nextID++
	id := r.nextID
	if err := r.write(map[string]any{"id": id, "method": method, "params": params}); err != nil {
		return nil, err
	}
	wantID, _ := json.Marshal(id)
	for {
		msg, err := r.next(ctx)
		if err != nil {
			return nil, err
		}
		if msg.Method != "" && hasRPCID(msg.ID) {
			if err := r.handleRequest(ctx, msg); err != nil {
				return nil, err
			}
			continue
		}
		if msg.Method == "session/update" {
			if onDelta != nil {
				if delta := acpTextDelta(msg.Params); delta != "" && !onDelta(delta) {
					return nil, errClaudeStreamClosed
				}
			}
			continue
		}
		if bytes.Equal(msg.ID, wantID) {
			if msg.Error != nil {
				return nil, msg.Error
			}
			return msg.Result, nil
		}
	}
}

func (r *claudeACP) notify(method string, params any) error {
	return r.write(map[string]any{"method": method, "params": params})
}

func (r *claudeACP) write(value map[string]any) error {
	value["jsonrpc"] = "2.0"
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	if _, err := r.stdin.Write(append(data, '\n')); err != nil {
		return fmt.Errorf("write Claude Code ACP request: %w", err)
	}
	return nil
}

func (r *claudeACP) next(ctx context.Context) (claudeACPMessage, error) {
	if err := ctx.Err(); err != nil {
		return claudeACPMessage{}, err
	}
	if !r.scanner.Scan() {
		if err := ctx.Err(); err != nil {
			return claudeACPMessage{}, err
		}
		if err := r.scanner.Err(); err != nil {
			return claudeACPMessage{}, err
		}
		return claudeACPMessage{}, io.EOF
	}
	var msg claudeACPMessage
	if err := json.Unmarshal(r.scanner.Bytes(), &msg); err != nil {
		return claudeACPMessage{}, fmt.Errorf("decode Claude Code ACP message: %w", err)
	}
	return msg, nil
}

func (r *claudeACP) handleRequest(ctx context.Context, msg claudeACPMessage) error {
	if msg.Method != "session/request_permission" {
		return r.replyError(msg.ID, -32601, "unsupported Claude Code ACP request")
	}
	var request struct {
		ToolCall struct {
			ToolCallID string `json:"toolCallId"`
			Title      string `json:"title"`
			Kind       string `json:"kind"`
			Content    []struct {
				Text string `json:"text"`
			} `json:"content"`
		} `json:"toolCall"`
		Options []struct {
			OptionID string `json:"optionId"`
			Kind     string `json:"kind"`
		} `json:"options"`
	}
	if err := json.Unmarshal(msg.Params, &request); err != nil {
		return r.replyError(msg.ID, -32602, "invalid permission request")
	}
	allowID := ""
	for _, option := range request.Options {
		if option.Kind == "allow_once" || option.Kind == "allow" {
			allowID = option.OptionID
			break
		}
	}
	if allowID == "" || r.permissions == nil || r.ghostID == "" {
		return r.replyPermission(msg.ID, "cancelled", "")
	}
	description := request.ToolCall.Title
	if len(request.ToolCall.Content) > 0 && request.ToolCall.Content[0].Text != "" {
		description = request.ToolCall.Content[0].Text
	}
	action := request.ToolCall.Kind
	if action == "edit" || action == "delete" {
		action = "write"
	}
	if action == "" {
		action = "use"
	}
	allowed, err := r.permissions.Request(ctx, permission.CreatePermissionRequest{
		SessionID:   r.ghostID,
		ToolCallID:  request.ToolCall.ToolCallID,
		ToolName:    request.ToolCall.Title,
		Description: description,
		Action:      action,
		Params:      json.RawMessage(msg.Params),
		Path:        r.cmd.Dir,
	})
	if err != nil {
		return err
	}
	if !allowed {
		return r.replyPermission(msg.ID, "cancelled", "")
	}
	return r.replyPermission(msg.ID, "selected", allowID)
}

func (r *claudeACP) replyPermission(id json.RawMessage, outcome, optionID string) error {
	result := map[string]any{"outcome": map[string]string{"outcome": outcome}}
	if optionID != "" {
		result["outcome"] = map[string]string{"outcome": outcome, "optionId": optionID}
	}
	return r.write(map[string]any{"id": id, "result": result})
}

func (r *claudeACP) replyError(id json.RawMessage, code int, message string) error {
	return r.write(map[string]any{"id": id, "error": map[string]any{"code": code, "message": message}})
}

func acpTextDelta(data json.RawMessage) string {
	var event struct {
		Update struct {
			SessionUpdate string `json:"sessionUpdate"`
			Content       struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"content"`
		} `json:"update"`
	}
	if json.Unmarshal(data, &event) != nil || event.Update.SessionUpdate != "agent_message_chunk" {
		return ""
	}
	return event.Update.Content.Text
}
