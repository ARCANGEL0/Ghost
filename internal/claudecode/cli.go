package claudecode

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
)

type Status struct {
	LoggedIn     bool   `json:"loggedIn"`
	AuthMethod   string `json:"authMethod"`
	APIProvider  string `json:"apiProvider"`
	Subscription string `json:"subscriptionType"`
}

// Check verifies the installed CLI has an active Claude account.
func Check(ctx context.Context, live bool) (Status, error) {
	cli, err := exec.LookPath("claude")
	if err != nil {
		return Status{}, errors.New("Claude Code is not installed or is missing from PATH")
	}

	statusCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(statusCtx, cli, "auth", "status")
	cmd.Env = accountEnv(os.Environ())
	data, err := cmd.Output()
	if err != nil {
		return Status{}, fmt.Errorf("could not read Claude Code login status: %w", err)
	}

	var status Status
	if err := json.Unmarshal(data, &status); err != nil {
		return Status{}, fmt.Errorf("could not read Claude Code login status: %w", err)
	}
	if !status.LoggedIn {
		return Status{}, errors.New("Claude Code is not signed in; run `claude auth login`")
	}
	if status.AuthMethod != "claude.ai" && status.AuthMethod != "oauth_token" {
		return Status{}, errors.New("Claude Code is using API credentials; sign in with `claude auth login`")
	}
	if status.APIProvider != "" && status.APIProvider != "firstParty" {
		return Status{}, errors.New("Claude Code is not using its Anthropic account")
	}
	if status.Subscription == "" {
		return Status{}, errors.New("Claude Code login has no active subscription")
	}
	if !live {
		return status, nil
	}

	checkCtx, cancelCheck := context.WithTimeout(ctx, 90*time.Second)
	defer cancelCheck()
	check := exec.CommandContext(checkCtx, cli, "-p", "--model", "haiku", "--no-session-persistence", "Reply only with OK.")
	check.Env = accountEnv(os.Environ())
	output, err := check.CombinedOutput()
	if err != nil {
		return Status{}, fmt.Errorf("Claude Code could not complete a login check: %s", shortOutput(output))
	}
	if strings.TrimSpace(string(output)) == "" {
		return Status{}, errors.New("Claude Code login check returned no response")
	}
	return status, nil
}

func accountEnv(env []string) []string {
	clean := make([]string, 0, len(env)+1)
	for _, item := range env {
		key, _, _ := strings.Cut(item, "=")
		if isAccountOverride(key) {
			continue
		}
		clean = append(clean, item)
	}
	return clean
}

func isAccountOverride(key string) bool {
	if strings.HasPrefix(key, "ANTHROPIC_") && strings.HasSuffix(key, "_BASE_URL") {
		return true
	}
	switch key {
	case "ANTHROPIC_API_KEY", "ANTHROPIC_AUTH_TOKEN", "ANTHROPIC_AWS_API_KEY",
		"ANTHROPIC_CUSTOM_HEADERS", "ANTHROPIC_FOUNDRY_API_KEY", "AWS_BEARER_TOKEN_BEDROCK",
		"CLAUDE_CODE_USE_ANTHROPIC_AWS", "CLAUDE_CODE_USE_BEDROCK",
		"CLAUDE_CODE_USE_FOUNDRY", "CLAUDE_CODE_USE_VERTEX":
		return true
	default:
		return false
	}
}

// CommandEnv keeps the ACP adapter on the CLI's saved Claude account.
func CommandEnv(env []string, cli string) []string {
	clean := accountEnv(env)
	clean = setEnv(clean, "CLAUDE_CODE_EXECUTABLE", cli)
	clean = setEnv(clean, "CLAUDE_AGENT_ACP_PROTOCOL", "v1")
	return clean
}

func setEnv(env []string, key, value string) []string {
	prefix := key + "="
	clean := make([]string, 0, len(env)+1)
	for _, item := range env {
		if !strings.HasPrefix(item, prefix) {
			clean = append(clean, item)
		}
	}
	return append(clean, prefix+value)
}

func shortOutput(data []byte) string {
	text := strings.TrimSpace(string(bytes.TrimSpace(data)))
	if text == "" {
		return "request failed; check network and account limits"
	}
	if len(text) > 300 {
		text = text[len(text)-300:]
	}
	return text
}
