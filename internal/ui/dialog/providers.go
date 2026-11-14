package dialog

import (
	"cmp"
	"strings"

	"charm.land/bubbles/v2/help"
	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/catwalk/pkg/catwalk"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/sahilm/fuzzy"
	"github.com/swadhinbiswas/ghost/internal/config"
	"github.com/swadhinbiswas/ghost/internal/ui/common"
	"github.com/swadhinbiswas/ghost/internal/ui/list"
	"github.com/swadhinbiswas/ghost/internal/ui/styles"
)

// ProvidersID is the identifier for the provider dialog.
const ProvidersID = "providers"

const providersDialogMaxWidth = 73

// Providers lists every provider Ghost can configure. Selecting a row opens
// the sign-in choices for that provider.
type Providers struct {
	com   *common.Common
	help  help.Model
	list  *list.FilterableList
	input textinput.Model

	keyMap struct {
		Select   key.Binding
		Next     key.Binding
		Previous key.Binding
		UpDown   key.Binding
		Close    key.Binding
	}
}

// ProviderItem is one provider row in the provider dialog.
type ProviderItem struct {
	provider catwalk.Provider
	info     string
	t        *styles.Styles
	m        fuzzy.Match
	cache    map[int]string
	focused  bool
}

var (
	_ Dialog   = (*Providers)(nil)
	_ ListItem = (*ProviderItem)(nil)
)

// NewProviders creates the provider dialog.
func NewProviders(com *common.Common) (*Providers, error) {
	p := &Providers{com: com}

	p.help = help.New()
	p.help.Styles = com.Styles.DialogHelpStyles()

	p.list = list.NewFilterableList()
	p.list.Focus()

	p.input = textinput.New()
	p.input.SetVirtualCursor(false)
	p.input.Placeholder = "Search providers"
	p.input.SetStyles(com.Styles.TextInput)
	p.input.Focus()

	p.keyMap.Select = key.NewBinding(
		key.WithKeys("enter", "ctrl+y"),
		key.WithHelp("enter", "configure"),
	)
	p.keyMap.Next = key.NewBinding(
		key.WithKeys("down", "ctrl+n"),
		key.WithHelp("↓", "next item"),
	)
	p.keyMap.Previous = key.NewBinding(
		key.WithKeys("up", "ctrl+p"),
		key.WithHelp("↑", "previous item"),
	)
	p.keyMap.UpDown = key.NewBinding(
		key.WithKeys("up", "down"),
		key.WithHelp("↑/↓", "choose"),
	)
	p.keyMap.Close = CloseKey

	p.setProviderItems()
	return p, nil
}

// ID implements Dialog.
func (p *Providers) ID() string {
	return ProvidersID
}

// HandleMsg implements Dialog.
func (p *Providers) HandleMsg(msg tea.Msg) Action {
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		switch {
		case key.Matches(msg, p.keyMap.Close):
			return ActionClose{}
		case key.Matches(msg, p.keyMap.Previous):
			p.list.Focus()
			if p.list.IsSelectedFirst() {
				p.list.SelectLast()
				p.list.ScrollToBottom()
				break
			}
			p.list.SelectPrev()
			p.list.ScrollToSelected()
		case key.Matches(msg, p.keyMap.Next):
			p.list.Focus()
			if p.list.IsSelectedLast() {
				p.list.SelectFirst()
				p.list.ScrollToTop()
				break
			}
			p.list.SelectNext()
			p.list.ScrollToSelected()
		case key.Matches(msg, p.keyMap.Select):
			item, ok := p.list.SelectedItem().(*ProviderItem)
			if !ok {
				break
			}
			return ActionSelectProvider{Provider: item.provider}
		default:
			var cmd tea.Cmd
			p.input, cmd = p.input.Update(msg)
			p.list.SetFilter(p.input.Value())
			p.list.SetSelected(0)
			return ActionCmd{cmd}
		}
	case tea.PasteMsg:
		var cmd tea.Cmd
		p.input, cmd = p.input.Update(msg)
		p.list.SetFilter(p.input.Value())
		p.list.SetSelected(0)
		return ActionCmd{cmd}
	}
	return nil
}

// Cursor returns the cursor position relative to the dialog.
func (p *Providers) Cursor() *tea.Cursor {
	return InputCursor(p.com.Styles, p.input.Cursor())
}

// Draw implements Dialog.
func (p *Providers) Draw(scr uv.Screen, area uv.Rectangle) *tea.Cursor {
	t := p.com.Styles
	width := max(0, min(providersDialogMaxWidth, area.Dx()-t.Dialog.View.GetHorizontalBorderSize()))
	height := max(0, min(defaultDialogHeight, area.Dy()-t.Dialog.View.GetVerticalBorderSize()))
	innerWidth := width - t.Dialog.View.GetHorizontalFrameSize()
	heightOffset := t.Dialog.Title.GetVerticalFrameSize() + titleContentHeight +
		t.Dialog.InputPrompt.GetVerticalFrameSize() + inputContentHeight +
		t.Dialog.HelpView.GetVerticalFrameSize() +
		t.Dialog.View.GetVerticalFrameSize()

	p.input.SetWidth(max(0, innerWidth-t.Dialog.InputPrompt.GetHorizontalFrameSize()-1))
	p.list.SetSize(innerWidth, height-heightOffset)
	p.help.SetWidth(innerWidth)

	rc := NewRenderContext(t, width)
	rc.Title = "Providers"
	rc.AddPart(t.Dialog.SecondaryText.Render("Choose a provider to add or re-authenticate."))
	rc.AddPart(t.Dialog.InputPrompt.Render(p.input.View()))
	rc.AddPart(t.Dialog.List.Height(p.list.Height()).Render(p.list.Render()))
	rc.Help = p.help.View(p)

	view := rc.Render()
	cur := p.Cursor()
	DrawCenterCursor(scr, area, view, cur)
	return cur
}

// ShortHelp implements help.KeyMap.
func (p *Providers) ShortHelp() []key.Binding {
	return []key.Binding{
		p.keyMap.UpDown,
		p.keyMap.Select,
		p.keyMap.Close,
	}
}

// FullHelp implements help.KeyMap.
func (p *Providers) FullHelp() [][]key.Binding {
	return [][]key.Binding{p.ShortHelp()}
}

func (p *Providers) setProviderItems() {
	cfg := p.com.Config()
	catalog := config.ProviderCatalog(cfg)

	items := make([]list.FilterableItem, 0, len(catalog))
	for _, provider := range catalog {
		info, configured := providerStatus(cfg, string(provider.ID))
		if !configured {
			info = providerMethodSummary(string(provider.ID))
		}
		items = append(items, &ProviderItem{
			provider: provider,
			info:     info,
			t:        p.com.Styles,
		})
	}

	p.list.SetItems(items...)
	if len(items) > 0 {
		p.list.SetSelected(0)
	}
}

// providerStatus describes a provider that is already present in the config.
func providerStatus(cfg *config.Config, id string) (string, bool) {
	if cfg == nil || cfg.Providers == nil {
		return "", false
	}
	providerConfig, ok := cfg.Providers.Get(id)
	if !ok || providerConfig.Disable {
		return "", false
	}
	status := "enabled"
	key := cmp.Or(providerConfig.APIKey, providerConfig.APIKeyTemplate)
	switch {
	case providerConfig.OAuthToken != nil:
		status = "OAuth connected"
	case key == "no-key-needed":
		status = "no sign-in"
	case key != "":
		status = "API key set"
	}
	if tier := config.ProviderTierLabel(id); tier != "" {
		status += " · " + tier
	}
	return status, true
}

// providerMethodSummary is the short hint shown for providers that are not
// configured yet.
func providerMethodSummary(id string) string {
	methods := config.ProviderAuthMethods(id)
	labels := make([]string, 0, len(methods))
	for _, method := range methods {
		switch method {
		case config.AuthOAuth:
			labels = append(labels, "OAuth")
		case config.AuthAPIKey:
			labels = append(labels, "API key")
		case config.AuthCLI:
			labels = append(labels, "CLI")
		case config.AuthNone:
			labels = append(labels, "no sign-in")
		default:
			labels = append(labels, string(method))
		}
	}
	status := strings.Join(labels, " / ")
	if tier := config.ProviderTierLabel(id); tier != "" {
		status += " · " + tier
	}
	return status
}

// Filter implements list.FilterableItem.
func (p *ProviderItem) Filter() string {
	return p.provider.Name + " " + string(p.provider.ID)
}

// ID implements ListItem.
func (p *ProviderItem) ID() string {
	return string(p.provider.ID)
}

// SetFocused implements list.Focusable.
func (p *ProviderItem) SetFocused(focused bool) {
	if p.focused != focused {
		p.cache = nil
	}
	p.focused = focused
}

// SetMatch implements list.MatchSettable.
func (p *ProviderItem) SetMatch(m fuzzy.Match) {
	p.cache = nil
	p.m = m
}

// Render implements list.Item.
func (p *ProviderItem) Render(width int) string {
	itemStyles := ListItemStyles{
		ItemBlurred:     p.t.Dialog.NormalItem,
		ItemFocused:     p.t.Dialog.SelectedItem,
		InfoTextBlurred: p.t.Base,
		InfoTextFocused: p.t.Base,
	}
	return renderItem(itemStyles, p.provider.Name, p.info, p.focused, width, p.cache, &p.m)
}
