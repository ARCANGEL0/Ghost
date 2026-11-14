package dialog

import (
	"context"
	"strings"
	"time"

	"charm.land/bubbles/v2/help"
	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/spinner"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/catwalk/pkg/catwalk"
	"charm.land/lipgloss/v2"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/pkg/browser"
	"github.com/swadhinbiswas/ghost/internal/config"
	"github.com/swadhinbiswas/ghost/internal/oauth"
	"github.com/swadhinbiswas/ghost/internal/oauth/claude"
	"github.com/swadhinbiswas/ghost/internal/ui/common"
	"github.com/swadhinbiswas/ghost/internal/ui/util"
)

// ClaudeOAuthID is the identifier for the Claude sign-in dialog.
const ClaudeOAuthID = "claude_oauth"

const claudeOAuthDialogWidth = 64

// ClaudeOAuth runs Anthropic's browser sign-in. The browser shows a code which
// the user pastes back into Ghost, so this dialog is text input, not the device
// flow used by Copilot and Hyper.
type ClaudeOAuth struct {
	com      *common.Common
	provider catwalk.Provider

	pkce   claude.PKCE
	state  string
	signIn string

	input   textinput.Model
	spinner spinner.Model
	help    help.Model

	busy   bool
	failed string

	keyMap struct {
		Submit key.Binding
		Close  key.Binding
	}
}

// claudeExchangeDoneMsg carries the result of the token exchange back to the
// dialog.
type claudeExchangeDoneMsg struct {
	token *oauth.Token
	err   error
}

var _ Dialog = (*ClaudeOAuth)(nil)

// NewClaudeOAuth creates the Claude sign-in dialog and opens the browser.
func NewClaudeOAuth(com *common.Common, provider catwalk.Provider) (*ClaudeOAuth, tea.Cmd) {
	pkce, err := claude.NewPKCE()
	if err != nil {
		return nil, tea.Cmd(func() tea.Msg { return util.ReportError(err) })
	}
	state, err := claude.NewState()
	if err != nil {
		return nil, tea.Cmd(func() tea.Msg { return util.ReportError(err) })
	}

	m := &ClaudeOAuth{
		com:      com,
		provider: provider,
		pkce:     pkce,
		state:    state,
		signIn:   claude.SignInURL(pkce, state),
	}

	m.input = textinput.New()
	m.input.SetVirtualCursor(false)
	m.input.Placeholder = "Paste code#state"
	m.input.SetStyles(com.Styles.TextInput)
	m.input.Focus()

	m.spinner = spinner.New(
		spinner.WithSpinner(spinner.Dot),
		spinner.WithStyle(com.Styles.Base.Foreground(com.Styles.Green)),
	)

	m.help = help.New()
	m.help.Styles = com.Styles.DialogHelpStyles()

	m.keyMap.Submit = key.NewBinding(
		key.WithKeys("enter", "ctrl+y"),
		key.WithHelp("enter", "open browser / submit"),
	)
	m.keyMap.Close = CloseKey

	open := tea.Cmd(func() tea.Msg {
		_ = browser.OpenURL(m.signIn)
		return nil
	})
	return m, open
}

// ID implements Dialog.
func (m *ClaudeOAuth) ID() string {
	return ClaudeOAuthID
}

// HandleMsg implements Dialog.
func (m *ClaudeOAuth) HandleMsg(msg tea.Msg) Action {
	switch msg := msg.(type) {
	case spinner.TickMsg:
		if m.busy {
			var cmd tea.Cmd
			m.spinner, cmd = m.spinner.Update(msg)
			if cmd != nil {
				return ActionCmd{cmd}
			}
		}

	case claudeExchangeDoneMsg:
		m.busy = false
		if msg.err != nil {
			m.failed = msg.err.Error()
			return nil
		}
		err := m.com.Store().SetProviderAPIKey(config.ScopeGlobal, string(m.provider.ID), msg.token)
		if err != nil {
			m.failed = err.Error()
			return nil
		}
		return ActionProviderConfigured{Provider: m.provider, Method: config.AuthOAuth}

	case tea.KeyPressMsg:
		if m.busy {
			break
		}
		switch {
		case key.Matches(msg, m.keyMap.Close):
			return ActionClose{}
		case key.Matches(msg, m.keyMap.Submit):
			code := strings.TrimSpace(m.input.Value())
			if code == "" {
				return ActionCmd{tea.Cmd(func() tea.Msg {
					_ = browser.OpenURL(m.signIn)
					return nil
				})}
			}
			m.busy = true
			m.failed = ""
			return ActionCmd{tea.Batch(m.spinner.Tick, m.exchange(code))}
		default:
			var cmd tea.Cmd
			m.input, cmd = m.input.Update(msg)
			return ActionCmd{cmd}
		}

	case tea.PasteMsg:
		if m.busy {
			break
		}
		var cmd tea.Cmd
		m.input, cmd = m.input.Update(msg)
		return ActionCmd{cmd}
	}
	return nil
}

func (m *ClaudeOAuth) exchange(code string) tea.Cmd {
	verifier := m.pkce.Verifier
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		token, err := claude.Exchange(ctx, code, verifier)
		return claudeExchangeDoneMsg{token: token, err: err}
	}
}

// Cursor returns the cursor position relative to the dialog.
func (m *ClaudeOAuth) Cursor() *tea.Cursor {
	return InputCursor(m.com.Styles, m.input.Cursor())
}

// Draw implements Dialog.
func (m *ClaudeOAuth) Draw(scr uv.Screen, area uv.Rectangle) *tea.Cursor {
	t := m.com.Styles
	dialogStyle := t.Dialog.View.Width(claudeOAuthDialogWidth)
	innerWidth := claudeOAuthDialogWidth - dialogStyle.GetHorizontalFrameSize()

	m.input.SetWidth(max(0, innerWidth-t.Dialog.InputPrompt.GetHorizontalFrameSize()-1))

	linkStyle := lipgloss.NewStyle().Foreground(t.GreenDark).Underline(true)
	link := linkStyle.Hyperlink(m.signIn, "id=claude-signin").Render(m.signIn)

	status := ""
	if m.failed != "" {
		status = t.Dialog.TitleError.Render(m.failed)
	} else if m.busy {
		status = m.spinner.View() + t.Dialog.SecondaryText.Render(" Exchanging code...")
	}

	content := strings.Join([]string{
		common.DialogTitle(t, t.Dialog.Title.Render("Connect to Claude"), innerWidth, t.Primary, t.Secondary),
		t.Dialog.SecondaryText.Render("Sign in with your Claude Pro or Max account, then paste the code Claude shows."),
		"",
		link,
		"",
		t.Dialog.InputPrompt.Render(m.input.View()),
		status,
		"",
		t.Dialog.HelpView.Render(m.help.View(m)),
	}, "\n")

	view := dialogStyle.Render(content)
	cur := m.Cursor()
	DrawCenterCursor(scr, area, view, cur)
	return cur
}

// ShortHelp implements help.KeyMap.
func (m *ClaudeOAuth) ShortHelp() []key.Binding {
	return []key.Binding{m.keyMap.Submit, m.keyMap.Close}
}

// FullHelp implements help.KeyMap.
func (m *ClaudeOAuth) FullHelp() [][]key.Binding {
	return [][]key.Binding{m.ShortHelp()}
}
