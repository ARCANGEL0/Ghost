package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path"
	"runtime"
	"strings"
	"time"

	"charm.land/fantasy"
	"github.com/swadhinbiswas/ghost/internal/config"
)

const (
	hookSessionStart     = "session_start"
	hookUserPromptSubmit = "user_prompt_submit"
	hookPreToolUse       = "pre_tool_use"
	hookPostToolUse      = "post_tool_use"
	hookPreCompact       = "pre_compact"
	hookStop             = "stop"
	hookOutputLimit      = 1 << 20
)

type hookInput struct {
	Event      string `json:"event"`
	SessionID  string `json:"session_id,omitempty"`
	Prompt     string `json:"prompt,omitempty"`
	ToolName   string `json:"tool_name,omitempty"`
	ToolInput  string `json:"tool_input,omitempty"`
	ToolOutput string `json:"tool_output,omitempty"`
	IsError    bool   `json:"is_error,omitempty"`
}

type hookDecision struct {
	Decision string `json:"decision,omitempty"`
	Reason   string `json:"reason,omitempty"`
}

type hookRunner struct {
	hooks      []config.Hook
	workingDir string
}

type hookTool struct {
	fantasy.AgentTool
	hooks     *hookRunner
	sessionID string
}

type limitedBuffer struct {
	buf   bytes.Buffer
	limit int
}

func newHookRunner(store *config.ConfigStore) *hookRunner {
	if store == nil || store.Config() == nil || store.Config().Options == nil {
		return nil
	}

	var hooks []config.Hook
	for _, hook := range store.Config().Options.Hooks {
		if !validHookEvent(hook.Event) || strings.TrimSpace(hook.Command) == "" {
			slog.Warn("Ignoring invalid agent hook", "event", hook.Event)
			continue
		}
		hooks = append(hooks, hook)
	}
	if len(hooks) == 0 {
		return nil
	}
	return &hookRunner{hooks: hooks, workingDir: store.WorkingDir()}
}

func validHookEvent(event string) bool {
	switch event {
	case hookSessionStart, hookUserPromptSubmit, hookPreToolUse, hookPostToolUse, hookPreCompact, hookStop:
		return true
	default:
		return false
	}
}

func (r *hookRunner) run(ctx context.Context, input hookInput) (hookDecision, error) {
	if r == nil {
		return hookDecision{}, nil
	}

	for _, hook := range r.hooks {
		if hook.Event != input.Event || !hookMatches(hook.Matcher, input.ToolName) {
			continue
		}

		data, err := json.Marshal(input)
		if err != nil {
			return hookDecision{}, err
		}
		timeout := time.Duration(hook.TimeoutSeconds) * time.Second
		if timeout <= 0 {
			timeout = 10 * time.Second
		}
		if timeout > time.Minute {
			timeout = time.Minute
		}
		hookCtx, cancel := context.WithTimeout(ctx, timeout)
		cmd := hookCommand(hookCtx, hook.Command)
		cmd.Dir = r.workingDir
		cmd.Env = append(os.Environ(),
			"GHOST_HOOK_EVENT="+input.Event,
			"GHOST_SESSION_ID="+input.SessionID,
			"GHOST_TOOL_NAME="+input.ToolName,
		)
		cmd.Stdin = bytes.NewReader(data)
		var output, stderr limitedBuffer
		output.limit = hookOutputLimit
		stderr.limit = hookOutputLimit
		cmd.Stdout = &output
		cmd.Stderr = &stderr
		err = cmd.Run()
		cancel()
		if err != nil {
			message := strings.TrimSpace(stderr.buf.String())
			if message == "" {
				message = err.Error()
			}
			return hookDecision{}, fmt.Errorf("%s hook failed: %s", input.Event, message)
		}

		var decision hookDecision
		if err := json.Unmarshal(output.buf.Bytes(), &decision); err == nil && decision.Decision == "deny" {
			if input.Event == hookPreToolUse {
				return decision, nil
			}
		}
	}
	return hookDecision{}, nil
}

func hookCommand(ctx context.Context, command string) *exec.Cmd {
	if runtime.GOOS == "windows" {
		return exec.CommandContext(ctx, "powershell.exe", "-NoProfile", "-NonInteractive", "-Command", command)
	}
	return exec.CommandContext(ctx, "sh", "-c", command)
}

func hookMatches(pattern, toolName string) bool {
	if pattern == "" || pattern == "*" {
		return true
	}
	matched, err := path.Match(pattern, toolName)
	return err == nil && matched
}

func (b *limitedBuffer) Write(data []byte) (int, error) {
	length := len(data)
	remaining := b.limit - b.buf.Len()
	if remaining > 0 {
		if len(data) > remaining {
			data = data[:remaining]
		}
		_, _ = b.buf.Write(data)
	}
	return length, nil
}

func (a *sessionAgent) fireHook(ctx context.Context, input hookInput) {
	_, err := a.hooks.run(ctx, input)
	if err != nil {
		slog.Warn("Agent lifecycle hook failed", "event", input.Event, "error", err)
	}
}

func (a *sessionAgent) wrapToolsWithHooks(sessionID string, agentTools []fantasy.AgentTool) []fantasy.AgentTool {
	if a.hooks == nil {
		return agentTools
	}

	wrapped := make([]fantasy.AgentTool, len(agentTools))
	for i, agentTool := range agentTools {
		wrapped[i] = &hookTool{AgentTool: agentTool, hooks: a.hooks, sessionID: sessionID}
	}
	return wrapped
}

func (t *hookTool) Run(ctx context.Context, call fantasy.ToolCall) (fantasy.ToolResponse, error) {
	input := hookInput{
		SessionID: t.sessionID,
		ToolName:  call.Name,
		ToolInput: call.Input,
	}
	decision, err := t.hooks.run(ctx, hookInput{
		Event:     hookPreToolUse,
		SessionID: input.SessionID,
		ToolName:  input.ToolName,
		ToolInput: input.ToolInput,
	})
	if err != nil {
		return fantasy.NewTextErrorResponse(err.Error()), nil
	}
	if decision.Decision == "deny" {
		reason := strings.TrimSpace(decision.Reason)
		if reason == "" {
			reason = "blocked by pre_tool_use hook"
		}
		return fantasy.NewTextErrorResponse(reason), nil
	}

	response, runErr := t.AgentTool.Run(ctx, call)
	input.ToolOutput = response.Content
	input.IsError = runErr != nil || response.IsError
	if runErr != nil {
		input.ToolOutput = runErr.Error()
	}
	_, hookErr := t.hooks.run(context.WithoutCancel(ctx), hookInput{
		Event:      hookPostToolUse,
		SessionID:  input.SessionID,
		ToolName:   input.ToolName,
		ToolInput:  input.ToolInput,
		ToolOutput: input.ToolOutput,
		IsError:    input.IsError,
	})
	if hookErr != nil {
		slog.Warn("Agent lifecycle hook failed", "event", hookPostToolUse, "tool", call.Name, "error", hookErr)
	}
	return response, runErr
}

func (b *limitedBuffer) String() string {
	return b.buf.String()
}

var _ fantasy.AgentTool = (*hookTool)(nil)
