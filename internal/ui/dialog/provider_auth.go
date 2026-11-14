package dialog

import (
	"fmt"

	"charm.land/bubbles/v2/help"
	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"charm.land/catwalk/pkg/catwalk"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/sahilm/fuzzy"
	"github.com/swadhinbiswas/ghost/internal/config"
	"github.com/swadhinbiswas/ghost/internal/ui/common"
	"github.com/swadhinbiswas/ghost/internal/ui/list"
	"github.com/swadhinbiswas/ghost/internal/ui/styles"
)

// ProviderAuthID is the identifier for the provider auth dialog.
const ProviderAuthID = "provider_auth"

const providerAuthDialogMaxWidth = 60

// ProviderAuth asks which sign-in method to use for a provider. It is only
// shown when a provider has more than one method.
type ProviderAuth struct {
	com      *common.Common
	provider catwalk.Provider
	methods  []config.AuthMethod
	help     help.Model
	list     *list.FilterableList

	keyMap struct {
		Select   key.Binding
		Next     key.Binding
		Previous key.Binding
		UpDown   key.Binding
		Close    key.Binding
	}
}

// ProviderAuthItem is one sign-in method row.
type ProviderAuthItem struct {
	method  config.AuthMethod
	t       *styles.Styles
	m       fuzzy.Match
	cache   map[int]string
	focused bool
}

var (
	_ Dialog   = (*ProviderAuth)(nil)
	_ ListItem = (*ProviderAuthItem)(nil)
)

// NewProviderAuth creates the sign-in method dialog.
func NewProviderAuth(com *common.Common, provider catwalk.Provider, methods []config.AuthMethod) *ProviderAuth {
	p := &ProviderAuth{
		com:      com,
		provider: provider,
		methods:  methods,
	}

	p.help = help.New()
	p.help.Styles = com.Styles.DialogHelpStyles()

	p.list = list.NewFilterableList()
	p.list.Focus()

	p.keyMap.Select = key.NewBinding(
		key.WithKeys("enter", "ctrl+y"),
		key.WithHelp("enter", "choose"),
	)
	p.keyMap.Next = key.NewBinding(
		key.WithKeys("down", "ctrl+n", "tab"),
		key.WithHelp("↓", "next item"),
	)
	p.keyMap.Previous = key.NewBinding(
		key.WithKeys("up", "ctrl+p", "shift+tab"),
		key.WithHelp("↑", "previous item"),
	)
	p.keyMap.UpDown = key.NewBinding(
		key.WithKeys("up", "down"),
		key.WithHelp("↑/↓", "choose"),
	)
	p.keyMap.Close = CloseKey

	items := make([]list.FilterableItem, 0, len(methods))
	for _, method := range methods {
		items = append(items, &ProviderAuthItem{method: method, t: com.Styles})
	}
	p.list.SetItems(items...)
	p.list.SetSelected(0)
	return p
}

// ID implements Dialog.
func (p *ProviderAuth) ID() string {
	return ProviderAuthID
}

// HandleMsg implements Dialog.
func (p *ProviderAuth) HandleMsg(msg tea.Msg) Action {
	keyMsg, ok := msg.(tea.KeyPressMsg)
	if !ok {
		return nil
	}

	switch {
	case key.Matches(keyMsg, p.keyMap.Close):
		return ActionClose{}
	case key.Matches(keyMsg, p.keyMap.Previous):
		if p.list.IsSelectedFirst() {
			p.list.SelectLast()
			p.list.ScrollToBottom()
			break
		}
		p.list.SelectPrev()
		p.list.ScrollToSelected()
	case key.Matches(keyMsg, p.keyMap.Next):
		if p.list.IsSelectedLast() {
			p.list.SelectFirst()
			p.list.ScrollToTop()
			break
		}
		p.list.SelectNext()
		p.list.ScrollToSelected()
	case key.Matches(keyMsg, p.keyMap.Select):
		item, ok := p.list.SelectedItem().(*ProviderAuthItem)
		if !ok {
			break
		}
		return ActionSelectProviderAuth{Provider: p.provider, Method: item.method}
	}
	return nil
}

// Draw implements Dialog.
func (p *ProviderAuth) Draw(scr uv.Screen, area uv.Rectangle) *tea.Cursor {
	t := p.com.Styles
	width := max(0, min(providerAuthDialogMaxWidth, area.Dx()-t.Dialog.View.GetHorizontalBorderSize()))
	height := max(0, min(defaultDialogHeight, area.Dy()-t.Dialog.View.GetVerticalBorderSize()))
	innerWidth := width - t.Dialog.View.GetHorizontalFrameSize()
	heightOffset := t.Dialog.Title.GetVerticalFrameSize() + titleContentHeight +
		t.Dialog.HelpView.GetVerticalFrameSize() +
		t.Dialog.View.GetVerticalFrameSize()

	p.list.SetSize(innerWidth, height-heightOffset)
	p.help.SetWidth(innerWidth)

	rc := NewRenderContext(t, width)
	rc.Title = fmt.Sprintf("Connect to %s", p.provider.Name)
	rc.AddPart(t.Dialog.SecondaryText.Render("Choose how Ghost should sign in."))
	rc.AddPart(t.Dialog.List.Height(p.list.Height()).Render(p.list.Render()))
	rc.Help = p.help.View(p)

	view := rc.Render()
	DrawCenter(scr, area, view)
	return nil
}

// ShortHelp implements help.KeyMap.
func (p *ProviderAuth) ShortHelp() []key.Binding {
	return []key.Binding{
		p.keyMap.UpDown,
		p.keyMap.Select,
		p.keyMap.Close,
	}
}

// FullHelp implements help.KeyMap.
func (p *ProviderAuth) FullHelp() [][]key.Binding {
	return [][]key.Binding{p.ShortHelp()}
}

// Filter implements list.FilterableItem.
func (p *ProviderAuthItem) Filter() string {
	return p.method.Label() + " " + p.method.Hint()
}

// ID implements ListItem.
func (p *ProviderAuthItem) ID() string {
	return string(p.method)
}

// SetFocused implements list.Focusable.
func (p *ProviderAuthItem) SetFocused(focused bool) {
	if p.focused != focused {
		p.cache = nil
	}
	p.focused = focused
}

// SetMatch implements list.MatchSettable.
func (p *ProviderAuthItem) SetMatch(m fuzzy.Match) {
	p.cache = nil
	p.m = m
}

// Render implements list.Item.
func (p *ProviderAuthItem) Render(width int) string {
	itemStyles := ListItemStyles{
		ItemBlurred:     p.t.Dialog.NormalItem,
		ItemFocused:     p.t.Dialog.SelectedItem,
		InfoTextBlurred: p.t.Base,
		InfoTextFocused: p.t.Base,
	}
	return renderItem(itemStyles, p.method.Label(), p.method.Hint(), p.focused, width, p.cache, &p.m)
}
