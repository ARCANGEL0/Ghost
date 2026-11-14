package cmd

import (
	"bufio"
	"cmp"
	"context"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/atotto/clipboard"
	"github.com/pkg/browser"
	"github.com/spf13/cobra"
	hyperp "github.com/swadhinbiswas/ghost/internal/agent/hyper"
	"github.com/swadhinbiswas/ghost/internal/config"
	"github.com/swadhinbiswas/ghost/internal/oauth"
	"github.com/swadhinbiswas/ghost/internal/oauth/claude"
	"github.com/swadhinbiswas/ghost/internal/oauth/copilot"
	"github.com/swadhinbiswas/ghost/internal/oauth/hyper"
	"golang.org/x/term"
)

var loginCmd = &cobra.Command{
	Aliases: []string{"auth"},
	Use:     "login [platform]",
	Short:   "Login Ghost to a platform or save an API key",
	Long: `Login Ghost to a specified platform or save an API key for any AI provider.
The platform or provider ID should be provided as an argument.
Hyper and GitHub Copilot use OAuth sign-in. Codex uses its official CLI device
flow. Claude reuses a Claude Code sign-in or runs Anthropic's OAuth flow, so a
Pro/Max subscription can be used instead of an API key. Other providers
securely prompt for an API key and save it locally.`,
	Example: `
# Authenticate with Charm Hyper
Ghost login

# Authenticate with GitHub Copilot
Ghost login copilot

# Sign in to Codex with a device code
Ghost login codex

# Sign in with a Claude Pro/Max subscription
Ghost login claude

# Save a gateway API key
Ghost login openrouter
Ghost login nvidia-nim
  `,
	ValidArgs: []cobra.Completion{
		"hyper",
		"copilot",
		"github-copilot",
		"codex",
		"claude",
		"claude-code",
		"nvidia-nim",
		"openrouter",
		"kilo",
		"groq",
		"cerebras",
		"googleai",
		"cloudflare",
		"cohere",
		"amd-radeon",
		"aion",
		"huggingface",
		"llm7",
		"typhoon",
		"nous-portal",
		"openai",
		"anthropic",
		"google",
	},
	Args: cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		app, err := setupAppWithProgressBar(cmd)
		if err != nil {
			return err
		}
		defer app.Shutdown()

		provider := "hyper"
		if len(args) > 0 {
			provider = args[0]
		}
		switch provider {
		case "hyper":
			return loginHyper(app.Store())
		case "copilot", "github", "github-copilot":
			return loginCopilot(app.Store())
		case "codex":
			return loginCodex()
		case "claude", "claude-code":
			return loginClaude(app.Store())
		default:
			return loginAPIKey(app.Store(), provider)
		}
	},
}

func loginCodex() error {
	if _, err := exec.LookPath("codex"); err != nil {
		return fmt.Errorf("Codex CLI is required for device sign-in: %w", err)
	}

	cmd := exec.Command("codex", "login", "--device-auth")
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("Codex device sign-in failed: %w", err)
	}
	return nil
}

func loginClaude(cfg *config.ConfigStore) error {
	ctx := getLoginContext()

	if token, ok := claude.TokenFromDisk(); ok {
		if token.IsExpired() && token.RefreshToken != "" {
			oldRefresh := token.RefreshToken
			refreshed, err := claude.Refresh(ctx, oldRefresh)
			if err != nil {
				fmt.Println("Existing Claude Code sign-in could not be refreshed; starting a new sign-in.")
			} else {
				claude.UpdateDiskToken(oldRefresh, refreshed)
				token = refreshed
			}
		}
		if token.AccessToken != "" && !token.IsExpired() {
			if err := cfg.SetProviderAPIKey(config.ScopeGlobal, "anthropic", token); err != nil {
				return err
			}
			fmt.Println("Imported the existing Claude Code sign-in for the anthropic provider.")
			return nil
		}
	}

	pkce, err := claude.NewPKCE()
	if err != nil {
		return err
	}
	state, err := claude.NewState()
	if err != nil {
		return err
	}
	signInURL := claude.SignInURL(pkce, state)

	fmt.Println("Open the following URL and sign in with your Claude Pro/Max account:")
	fmt.Println()
	fmt.Println(lipgloss.NewStyle().Hyperlink(signInURL, "id=claude").Render(signInURL))
	fmt.Println()
	fmt.Println("Claude will show an authorization code. Paste it here (format: code#state):")

	_ = browser.OpenURL(signInURL)

	line, readErr := bufio.NewReader(os.Stdin).ReadString('\n')
	if readErr != nil && line == "" {
		return fmt.Errorf("failed to read authorization code: %w", readErr)
	}
	code := strings.TrimSpace(line)
	if code == "" {
		return fmt.Errorf("authorization code cannot be empty")
	}

	token, err := claude.Exchange(ctx, code, pkce.Verifier)
	if err != nil {
		return err
	}
	if err := cfg.SetProviderAPIKey(config.ScopeGlobal, "anthropic", token); err != nil {
		return err
	}

	fmt.Println()
	fmt.Println("You're now authenticated with Claude (Pro/Max subscription).")
	return nil
}

func loginHyper(cfg *config.ConfigStore) error {
	if !hyperp.Enabled() {
		return fmt.Errorf("hyper not enabled")
	}
	ctx := getLoginContext()

	resp, err := hyper.InitiateDeviceAuth(ctx)
	if err != nil {
		return err
	}

	if clipboard.WriteAll(resp.UserCode) == nil {
		fmt.Println("The following code should be on clipboard already:")
	} else {
		fmt.Println("Copy the following code:")
	}

	fmt.Println()
	fmt.Println(lipgloss.NewStyle().Bold(true).Render(resp.UserCode))
	fmt.Println()
	fmt.Println("Press enter to open this URL, and then paste it there:")
	fmt.Println()
	fmt.Println(lipgloss.NewStyle().Hyperlink(resp.VerificationURL, "id=hyper").Render(resp.VerificationURL))
	fmt.Println()
	waitEnter()
	if err := browser.OpenURL(resp.VerificationURL); err != nil {
		fmt.Println("Could not open the URL. You'll need to manually open the URL in your browser.")
	}

	fmt.Println("Exchanging authorization code...")
	refreshToken, err := hyper.PollForToken(ctx, resp.DeviceCode, resp.ExpiresIn)
	if err != nil {
		return err
	}

	fmt.Println("Exchanging refresh token for access token...")
	token, err := hyper.ExchangeToken(ctx, refreshToken)
	if err != nil {
		return err
	}

	fmt.Println("Verifying access token...")
	introspect, err := hyper.IntrospectToken(ctx, token.AccessToken)
	if err != nil {
		return fmt.Errorf("token introspection failed: %w", err)
	}
	if !introspect.Active {
		return fmt.Errorf("access token is not active")
	}

	if err := cmp.Or(
		cfg.SetConfigField(config.ScopeGlobal, "providers.hyper.api_key", token.AccessToken),
		cfg.SetConfigField(config.ScopeGlobal, "providers.hyper.oauth", token),
	); err != nil {
		return err
	}

	fmt.Println()
	fmt.Println("You're now authenticated with Hyper!")
	return nil
}

func loginCopilot(cfg *config.ConfigStore) error {
	ctx := getLoginContext()

	if cfg.HasConfigField(config.ScopeGlobal, "providers.copilot.oauth") {
		fmt.Println("You are already logged in to GitHub Copilot.")
		return nil
	}

	diskToken, hasDiskToken := copilot.RefreshTokenFromDisk()
	var token *oauth.Token

	switch {
	case hasDiskToken:
		fmt.Println("Found existing GitHub Copilot token on disk. Using it to authenticate...")

		t, err := copilot.RefreshToken(ctx, diskToken)
		if err != nil {
			return fmt.Errorf("unable to refresh token from disk: %w", err)
		}
		token = t
	default:
		fmt.Println("Requesting device code from GitHub...")
		dc, err := copilot.RequestDeviceCode(ctx)
		if err != nil {
			return err
		}

		fmt.Println()
		fmt.Println("Open the following URL and follow the instructions to authenticate with GitHub Copilot:")
		fmt.Println()
		fmt.Println(lipgloss.NewStyle().Hyperlink(dc.VerificationURI, "id=copilot").Render(dc.VerificationURI))
		fmt.Println()
		fmt.Println("Code:", lipgloss.NewStyle().Bold(true).Render(dc.UserCode))
		fmt.Println()
		fmt.Println("Waiting for authorization...")

		t, err := copilot.PollForToken(ctx, dc)
		if err == copilot.ErrNotAvailable {
			fmt.Println()
			fmt.Println("GitHub Copilot is unavailable for this account. To signup, go to the following page:")
			fmt.Println()
			fmt.Println(lipgloss.NewStyle().Hyperlink(copilot.SignupURL, "id=copilot-signup").Render(copilot.SignupURL))
			fmt.Println()
			fmt.Println("You may be able to request free access if eligible. For more information, see:")
			fmt.Println()
			fmt.Println(lipgloss.NewStyle().Hyperlink(copilot.FreeURL, "id=copilot-free").Render(copilot.FreeURL))
		}
		if err != nil {
			return err
		}
		token = t
	}

	if err := cmp.Or(
		cfg.SetConfigField(config.ScopeGlobal, "providers.copilot.api_key", token.AccessToken),
		cfg.SetConfigField(config.ScopeGlobal, "providers.copilot.oauth", token),
	); err != nil {
		return err
	}

	fmt.Println()
	fmt.Println("You're now authenticated with GitHub Copilot!")
	return nil
}

func getLoginContext() context.Context {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, os.Kill)
	go func() {
		<-ctx.Done()
		cancel()
		os.Exit(1)
	}()
	return ctx
}

func waitEnter() {
	_, _ = fmt.Scanln()
}

func loginAPIKey(cfg *config.ConfigStore, provider string) error {
	fmt.Printf("Enter API key for %s: ", provider)
	keyBytes, err := term.ReadPassword(int(os.Stdin.Fd()))
	if err != nil {
		return fmt.Errorf("failed to read API key: %w", err)
	}
	fmt.Println()

	apiKey := string(keyBytes)
	if apiKey == "" {
		return fmt.Errorf("API key cannot be empty")
	}

	err = cfg.SetProviderAPIKey(config.ScopeGlobal, provider, apiKey)
	if err != nil {
		return fmt.Errorf("failed to save API key: %w", err)
	}

	fmt.Printf("Successfully saved API key for provider '%s' to configuration store.\n", provider)
	return nil
}
