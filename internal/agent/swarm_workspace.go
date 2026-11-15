package agent

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

func createSwarmWorktree(ctx context.Context, workingDir string) (string, error) {
	root, err := runGit(ctx, workingDir, "rev-parse", "--show-toplevel")
	if err != nil {
		return "", fmt.Errorf("find project root: %w", err)
	}
	root = strings.TrimSpace(root)

	changes, err := runGit(ctx, root, "diff", "--binary", "HEAD")
	if err != nil {
		return "", fmt.Errorf("read current changes: %w", err)
	}

	workDir, err := os.MkdirTemp("", "ghost-swarm-")
	if err != nil {
		return "", fmt.Errorf("create swarm workspace: %w", err)
	}
	if err := os.Remove(workDir); err != nil {
		return "", fmt.Errorf("prepare swarm workspace: %w", err)
	}

	if _, err := runGit(ctx, root, "worktree", "add", "--detach", workDir, "HEAD"); err != nil {
		return "", fmt.Errorf("create Git worktree: %w", err)
	}

	if len(changes) > 0 {
		cmd := exec.CommandContext(ctx, "git", "-C", workDir, "apply", "--binary", "-")
		cmd.Stdin = bytes.NewReader([]byte(changes))
		if output, err := cmd.CombinedOutput(); err != nil {
			_, _ = runGit(context.WithoutCancel(ctx), root, "worktree", "remove", "--force", workDir)
			return "", fmt.Errorf("copy current changes into swarm workspace: %s", strings.TrimSpace(string(output)))
		}
	}

	return filepath.Clean(workDir), nil
}

func swarmWorktreeStatus(ctx context.Context, workDir string) string {
	status, err := runGit(ctx, workDir, "status", "--short")
	if err != nil {
		return "could not read worktree status"
	}
	status = strings.TrimSpace(status)
	if status == "" {
		return "no file changes"
	}
	return status
}

func runGit(ctx context.Context, workingDir string, args ...string) (string, error) {
	allArgs := append([]string{"-C", workingDir}, args...)
	cmd := exec.CommandContext(ctx, "git", allArgs...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("git %s: %s: %w", strings.Join(args, " "), strings.TrimSpace(string(output)), err)
	}
	return string(output), nil
}
