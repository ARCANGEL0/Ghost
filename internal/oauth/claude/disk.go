package claude

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"

	"github.com/swadhinbiswas/ghost/internal/oauth"
)

// credentialsFile mirrors the on-disk shape written by Claude Code.
type credentialsFile struct {
	ClaudeAiOauth struct {
		AccessToken      string   `json:"accessToken"`
		RefreshToken     string   `json:"refreshToken"`
		ExpiresAt        int64    `json:"expiresAt"` // Unix milliseconds
		Scopes           []string `json:"scopes"`
		SubscriptionType string   `json:"subscriptionType"`
	} `json:"claudeAiOauth"`
}

// TokenFromDisk imports the current Claude Code sign-in, if one exists.
// macOS keeps the same JSON in the login keychain, which is checked as a
// fallback when the credentials file is absent.
func TokenFromDisk() (*oauth.Token, bool) {
	data, ok := readCredentials()
	if !ok {
		return nil, false
	}
	return parseCredentials(data)
}

func parseCredentials(data []byte) (*oauth.Token, bool) {
	var credentials credentialsFile
	if err := json.Unmarshal(data, &credentials); err != nil {
		return nil, false
	}
	auth := credentials.ClaudeAiOauth
	if auth.AccessToken == "" {
		return nil, false
	}

	token := &oauth.Token{
		AccessToken:  auth.AccessToken,
		RefreshToken: auth.RefreshToken,
	}
	if auth.ExpiresAt > 0 {
		token.ExpiresAt = auth.ExpiresAt / 1000
		token.SetExpiresIn()
	}
	return token, true
}

// UpdateDiskToken keeps an existing Claude Code credentials file in sync after
// a refresh. It only writes when the file still holds oldRefresh, so it never
// overwrites a newer sign-in performed by the Claude Code CLI.
func UpdateDiskToken(oldRefresh string, token *oauth.Token) bool {
	path := credentialsPath()
	if path == "" || token == nil || oldRefresh == "" {
		return false
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return false
	}

	var root map[string]any
	if err := json.Unmarshal(data, &root); err != nil {
		return false
	}
	auth, ok := root["claudeAiOauth"].(map[string]any)
	if !ok {
		return false
	}
	if current, _ := auth["refreshToken"].(string); current != oldRefresh {
		return false
	}

	auth["accessToken"] = token.AccessToken
	auth["refreshToken"] = token.RefreshToken
	auth["expiresAt"] = token.ExpiresAt * 1000

	updated, err := json.MarshalIndent(root, "", "  ")
	if err != nil {
		return false
	}
	updated = append(updated, '\n')
	return os.WriteFile(path, updated, 0o600) == nil
}

func credentialsPath() string {
	dir := os.Getenv("CLAUDE_CONFIG_DIR")
	if dir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return ""
		}
		dir = filepath.Join(home, ".claude")
	}
	return filepath.Join(dir, ".credentials.json")
}

func readCredentials() ([]byte, bool) {
	if data, err := os.ReadFile(credentialsPath()); err == nil {
		return data, true
	}

	if runtime.GOOS == "darwin" {
		out, err := exec.Command(
			"security", "find-generic-password",
			"-s", "Claude Code-credentials",
			"-w",
		).Output()
		if err == nil {
			if data := bytes.TrimSpace(out); len(data) > 0 {
				return data, true
			}
		}
	}
	return nil, false
}
