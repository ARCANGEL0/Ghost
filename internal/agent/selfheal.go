package agent

import (
	"context"
	"fmt"
	"log/slog"
	"os/exec"
	"strings"
	"sync"
	"time"

	"charm.land/fantasy"
)

const SelfHealingPrompt = `

# Self-Healing Verification Loops
After successful file changes from a model step finish, Ghost runs the configured verification command (for example, a build or test suite).
If the verification command fails, you will receive a tool error response structured like this:
<verification_failure>
Your changes broke the build. Here is the compiler/test error:
[error output]
Please use the edit/write tools to fix the errors and run the compilation/verification again.
</verification_failure>

If you receive this error, do NOT ask the user for help or stop. You must analyze the error output, figure out the cause of the failure (e.g., compile error, import issue, syntax mistake, type mismatch, or failed unit test), and call the appropriate edit/write tools to fix it.
`

// VerifyBuild runs the verification command and returns build/test output if it fails.
func (a *sessionAgent) VerifyBuild(ctx context.Context) (bool, string, error) {
	if a.cfg == nil {
		return true, "", nil
	}

	cmdStr := a.cfg.Config().Options.VerificationCommand
	if cmdStr == "" {
		return true, "", nil
	}

	slog.Info("Running verification build/test command", "command", cmdStr)

	// Create context with a timeout so a hanging test/build command doesn't block forever
	verifyCtx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()

	var cmd *exec.Cmd
	// Use shell to run the command to support complex pipelines (e.g. "npm run build && npm run test")
	if strings.Contains(cmdStr, " ") || strings.Contains(cmdStr, "&&") || strings.Contains(cmdStr, "||") || strings.Contains(cmdStr, "|") {
		cmd = exec.CommandContext(verifyCtx, "bash", "-c", cmdStr)
	} else {
		cmd = exec.CommandContext(verifyCtx, cmdStr)
	}

	cmd.Dir = a.cfg.WorkingDir()

	out, err := cmd.CombinedOutput()
	if err != nil {
		slog.Warn("Verification command failed", "error", err, "output", string(out))
		return false, string(out), nil
	}

	slog.Info("Verification command passed successfully")
	return true, "", nil
}

type verificationWrappedTool struct {
	fantasy.AgentTool
	a         *sessionAgent
	sessionID string
	batch     *verificationBatch
}

type verificationBatch struct {
	mu      sync.Mutex
	pending map[string]struct{}
	dirty   bool
}

func (b *verificationBatch) reset() {
	b.mu.Lock()
	b.pending = make(map[string]struct{})
	b.dirty = false
	b.mu.Unlock()
}

func (b *verificationBatch) add(id string) {
	if id == "" {
		return
	}
	b.mu.Lock()
	if b.pending == nil {
		b.pending = make(map[string]struct{})
	}
	b.pending[id] = struct{}{}
	b.mu.Unlock()
}

func (b *verificationBatch) finish(id string, changed bool) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	if _, ok := b.pending[id]; !ok {
		return false
	}
	delete(b.pending, id)
	b.dirty = b.dirty || changed
	if len(b.pending) > 0 || !b.dirty {
		return false
	}
	b.dirty = false
	return true
}

func (w *verificationWrappedTool) Run(ctx context.Context, params fantasy.ToolCall) (fantasy.ToolResponse, error) {
	resp, err := w.AgentTool.Run(ctx, params)
	if w.batch != nil {
		verify := w.batch.finish(params.ID, err == nil && !resp.IsError)
		if verify {
			checked, checkErr := w.verify(ctx)
			if checkErr != nil || checked.IsError {
				return checked, checkErr
			}
		}
		return resp, err
	}
	if err != nil || resp.IsError {
		return resp, err
	}

	if w.a.cfg == nil {
		return resp, nil
	}

	cmdStr := w.a.cfg.Config().Options.VerificationCommand
	if cmdStr == "" {
		return resp, nil
	}

	checked, checkErr := w.verify(ctx)
	if checkErr != nil || checked.IsError {
		return checked, checkErr
	}
	return resp, nil
}

func (w *verificationWrappedTool) verify(ctx context.Context) (fantasy.ToolResponse, error) {
	if w.a.cfg == nil {
		return fantasy.ToolResponse{}, nil
	}
	if w.a.cfg.Config().Options.VerificationCommand == "" {
		return fantasy.ToolResponse{}, nil
	}

	maxRetries := w.a.cfg.Config().Options.MaxVerificationRetries
	if maxRetries <= 0 {
		maxRetries = 3
	}

	w.a.verificationMu.Lock()
	retries := w.a.verificationRetries[w.sessionID]
	w.a.verificationMu.Unlock()

	if retries >= maxRetries {
		slog.Warn("Max verification retries reached, returning original result", "retries", retries, "sessionID", w.sessionID)
		return fantasy.ToolResponse{}, nil
	}

	// Run verification command
	ok, out, err := w.a.VerifyBuild(ctx)
	if err != nil {
		return fantasy.ToolResponse{}, err
	}

	if ok {
		return fantasy.ToolResponse{}, nil
	}

	// Increment retry counter
	w.a.verificationMu.Lock()
	w.a.verificationRetries[w.sessionID] = retries + 1
	w.a.verificationMu.Unlock()

	// Return verification failure XML response block
	failMsg := fmt.Sprintf("<verification_failure>\nYour changes broke the build. Here is the compiler/test error:\n%s\nPlease use the edit/write tools to fix the errors and run the compilation/verification again.\n</verification_failure>", out)

	return fantasy.NewTextErrorResponse(failMsg), nil
}

func (a *sessionAgent) wrapToolsWithVerification(ctx context.Context, sessionID string, tools []fantasy.AgentTool) []fantasy.AgentTool {
	return a.wrapToolsWithVerificationBatch(ctx, sessionID, tools, nil)
}

func (a *sessionAgent) wrapToolsWithVerificationBatch(ctx context.Context, sessionID string, tools []fantasy.AgentTool, batch *verificationBatch) []fantasy.AgentTool {
	if a.cfg == nil {
		return tools
	}
	cmdStr := a.cfg.Config().Options.VerificationCommand
	if cmdStr == "" {
		return tools
	}

	wrapped := make([]fantasy.AgentTool, len(tools))
	for i, t := range tools {
		name := t.Info().Name
		if isVerifiedWriteTool(name) {
			wrapped[i] = &verificationWrappedTool{
				AgentTool: t,
				a:         a,
				sessionID: sessionID,
				batch:     batch,
			}
		} else {
			wrapped[i] = t
		}
	}
	return wrapped
}

func isVerifiedWriteTool(name string) bool {
	switch name {
	case "edit", "write", "multiedit", "atomic_edit":
		return true
	default:
		return false
	}
}
