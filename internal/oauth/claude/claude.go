// Package claude implements Anthropic's Claude Code OAuth 2.0 flow.
//
// Claude Pro/Max subscriptions authenticate through claude.ai with PKCE and
// issue bearer tokens that work against the public Anthropic API when the
// OAuth beta header is present. This is the same public client Claude Code
// uses, so an existing Claude Code sign-in on disk can be imported directly.
package claude

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/swadhinbiswas/ghost/internal/oauth"
)

// Claude Code's public OAuth client. It is a public client: no secret.
const (
	ClientID     = "9d1c250a-e61b-44d9-88ed-5944d1962f5e"
	authorizeURL = "https://claude.ai/oauth/authorize"
	redirectURI  = "https://console.anthropic.com/oauth/code/callback"
	Scopes       = "org:create_api_key user:profile user:inference user:sessions:claude_code user:mcp_servers user:file_upload"

	// BetaHeader gates OAuth-backed inference on the Anthropic API.
	BetaHeader = "oauth-2025-04-20"

	userAgent = "claude-cli/2.1.2 (external, cli)"
)

// tokenEndpoints is ordered newest-first. Older deployments still answer on
// console.anthropic.com, so the exchange falls back when a host is unknown.
var tokenEndpoints = []string{
	"https://platform.claude.com/v1/oauth/token",
	"https://console.anthropic.com/v1/oauth/token",
}

var httpClient = &http.Client{Timeout: 30 * time.Second}

// PKCE holds a verifier and its S256 challenge.
type PKCE struct {
	Verifier  string
	Challenge string
}

// NewPKCE generates a fresh PKCE verifier and challenge.
func NewPKCE() (PKCE, error) {
	verifier, err := randomToken(32)
	if err != nil {
		return PKCE{}, err
	}
	sum := sha256.Sum256([]byte(verifier))
	return PKCE{
		Verifier:  verifier,
		Challenge: base64.RawURLEncoding.EncodeToString(sum[:]),
	}, nil
}

// NewState generates a random OAuth state value.
func NewState() (string, error) {
	return randomToken(32)
}

// SignInURL builds the claude.ai authorization URL.
func SignInURL(pkce PKCE, state string) string {
	u, _ := url.Parse(authorizeURL)
	q := u.Query()
	q.Set("code", "true")
	q.Set("client_id", ClientID)
	q.Set("response_type", "code")
	q.Set("redirect_uri", redirectURI)
	q.Set("scope", Scopes)
	q.Set("code_challenge", pkce.Challenge)
	q.Set("code_challenge_method", "S256")
	q.Set("state", state)
	u.RawQuery = q.Encode()
	return u.String()
}

// Exchange trades the pasted authorization code for an access token.
func Exchange(ctx context.Context, codeWithState string, verifier string) (*oauth.Token, error) {
	code, state := splitAuthorizationCode(codeWithState)
	if code == "" {
		return nil, errors.New("claude: empty authorization code")
	}
	return postToken(ctx, map[string]string{
		"grant_type":    "authorization_code",
		"code":          code,
		"state":         state,
		"client_id":     ClientID,
		"redirect_uri":  redirectURI,
		"code_verifier": verifier,
	})
}

// Refresh rotates an expired access token. Anthropic rotates the refresh
// token as well; callers must persist the returned token.
func Refresh(ctx context.Context, refreshToken string) (*oauth.Token, error) {
	if refreshToken == "" {
		return nil, errors.New("claude: missing refresh token")
	}
	return postToken(ctx, map[string]string{
		"grant_type":    "refresh_token",
		"refresh_token": refreshToken,
		"client_id":     ClientID,
	})
}

// Headers returns the headers required for OAuth-backed inference.
func Headers() map[string]string {
	return map[string]string{
		"anthropic-beta": BetaHeader,
	}
}

// SystemPromptPrefix is the identity line the API expects from Claude Code
// clients. Ghost prepends it only when the anthropic provider uses OAuth.
func SystemPromptPrefix() string {
	return "You are Claude Code, Anthropic's official CLI for Claude."
}

func splitAuthorizationCode(value string) (string, string) {
	value = strings.TrimSpace(value)
	if code, state, ok := strings.Cut(value, "#"); ok {
		return strings.TrimSpace(code), strings.TrimSpace(state)
	}
	return value, ""
}

func randomToken(size int) (string, error) {
	buf := make([]byte, size)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("claude: generate random token: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

type tokenResponse struct {
	AccessToken      string `json:"access_token"`
	RefreshToken     string `json:"refresh_token"`
	ExpiresIn        int    `json:"expires_in"`
	Scope            string `json:"scope"`
	Error            string `json:"error"`
	ErrorDescription string `json:"error_description"`
}

func postToken(ctx context.Context, payload map[string]string) (*oauth.Token, error) {
	var lastErr error
	for i, endpoint := range tokenEndpoints {
		token, retry, err := postTokenTo(ctx, endpoint, payload)
		if err == nil {
			return token, nil
		}
		lastErr = err
		if !retry || i == len(tokenEndpoints)-1 {
			return nil, err
		}
	}
	return nil, lastErr
}

func postTokenTo(ctx context.Context, endpoint string, payload map[string]string) (*oauth.Token, bool, error) {
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, false, fmt.Errorf("claude: encode token request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, false, fmt.Errorf("claude: build token request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("anthropic-beta", BetaHeader)
	req.Header.Set("User-Agent", userAgent)

	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, true, fmt.Errorf("claude: token request: %w", err)
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, false, fmt.Errorf("claude: read token response: %w", err)
	}

	switch resp.StatusCode {
	case http.StatusNotFound, http.StatusMethodNotAllowed, http.StatusGone:
		return nil, true, fmt.Errorf("claude: token endpoint %s is unavailable", endpoint)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, false, fmt.Errorf("claude: token request failed: %s: %s", resp.Status, tokenError(data))
	}

	var result tokenResponse
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, false, fmt.Errorf("claude: decode token response: %w", err)
	}
	if result.AccessToken == "" {
		return nil, false, fmt.Errorf("claude: token response had no access token: %s", tokenError(data))
	}

	expiresIn := result.ExpiresIn
	if expiresIn <= 0 {
		expiresIn = 3600
	}
	token := &oauth.Token{
		AccessToken:  result.AccessToken,
		RefreshToken: result.RefreshToken,
		ExpiresIn:    expiresIn,
	}
	token.SetExpiresAt()
	return token, false, nil
}

func tokenError(data []byte) string {
	var result tokenResponse
	if json.Unmarshal(data, &result) == nil {
		if result.ErrorDescription != "" {
			return result.Error + ": " + result.ErrorDescription
		}
		if result.Error != "" {
			return result.Error
		}
	}
	text := strings.TrimSpace(string(data))
	if len(text) > 200 {
		text = text[:200]
	}
	return text
}
