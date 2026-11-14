package dialog

import (
	"strings"

	"charm.land/bubbles/v2/help"
	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/swadhinbiswas/ghost/internal/sessionimport"
	"github.com/swadhinbiswas/ghost/internal/ui/common"
	"github.com/swadhinbiswas/ghost/internal/ui/list"
	"github.com/swadhinbiswas/ghost/internal/ui/util"
)

const (
	ImportAgentID     = "import_agent"
	ImportReferenceID = "import_reference"
)

type ImportAgent struct {
	com   *common.Common
	help  help.Model
	list  *list.FilterableList
	input textinput.Model
	keys  importKeys
}

type ImportReference struct {
	com    *common.Common
	help   help.Model
	input  textinput.Model
	keys   importKeys
	source string
}

type importKeys struct {
	Select   key.Binding
	Next     key.Binding
	Previous key.Binding
	Close    key.Binding
}

var _ Dialog = (*ImportAgent)(nil)
var _ Dialog = (*ImportReference)(nil)

func NewImportAgent(com *common.Common) *ImportAgent {
	d := &ImportAgent{com: com}
	d.help = newImportHelp(com)
	d.list = list.NewFilterableList()
	d.list.Focus()
	d.list.SetSelected(0)
	d.input = newImportInput(com, "Filter agents")
	d.keys = newImportKeys()
	items := make([]list.FilterableItem, 0)
	for _, source := range sessionimport.Sources() {
		items = append(items, NewCommandItem(com.Styles, source.ID, source.Name, "", ActionSelectImportSource{Source: source.ID}))
	}
	d.list.SetItems(items...)
	return d
}

func NewImportReference(com *common.Common, source string) *ImportReference {
	placeholder := "Session ID or title or prompt"
	if source == "file" {
		placeholder = "Path to exported transcript"
	}
	return &ImportReference{
		com:    com,
		help:   newImportHelp(com),
		input:  newImportInput(com, placeholder),
		keys:   newImportKeys(),
		source: source,
	}
}

func newImportInput(com *common.Common, placeholder string) textinput.Model {
	input := textinput.New()
	input.SetVirtualCursor(false)
	input.Placeholder = placeholder
	input.SetStyles(com.Styles.TextInput)
	input.Focus()
	return input
}

func newImportHelp(com *common.Common) help.Model {
	h := help.New()
	h.Styles = com.Styles.DialogHelpStyles()
	return h
}

func newImportKeys() importKeys {
	return importKeys{
		Select:   key.NewBinding(key.WithKeys("enter", "ctrl+y"), key.WithHelp("enter", "choose")),
		Next:     key.NewBinding(key.WithKeys("down", "ctrl+n"), key.WithHelp("↓", "next")),
		Previous: key.NewBinding(key.WithKeys("up", "ctrl+p"), key.WithHelp("↑", "previous")),
		Close:    CloseKey,
	}
}

func (d *ImportAgent) ID() string { return ImportAgentID }

func (d *ImportAgent) HandleMsg(msg tea.Msg) Action {
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		switch {
		case key.Matches(msg, d.keys.Close):
			return ActionClose{}
		case key.Matches(msg, d.keys.Next):
			d.list.SelectNext()
			d.list.ScrollToSelected()
		case key.Matches(msg, d.keys.Previous):
			d.list.SelectPrev()
			d.list.ScrollToSelected()
		case key.Matches(msg, d.keys.Select):
			if item, ok := d.list.SelectedItem().(*CommandItem); ok && item != nil {
				return item.Action()
			}
		default:
			var cmd tea.Cmd
			d.input, cmd = d.input.Update(msg)
			d.list.SetFilter(d.input.Value())
			d.list.SetSelected(0)
			return ActionCmd{Cmd: cmd}
		}
	case tea.PasteMsg:
		var cmd tea.Cmd
		d.input, cmd = d.input.Update(msg)
		d.list.SetFilter(d.input.Value())
		return ActionCmd{Cmd: cmd}
	}
	return nil
}

func (d *ImportAgent) Draw(scr uv.Screen, area uv.Rectangle) *tea.Cursor {
	t := d.com.Styles
	width := max(0, min(defaultDialogMaxWidth, area.Dx()))
	height := max(0, min(defaultDialogHeight, area.Dy()))
	innerWidth := width - t.Dialog.View.GetHorizontalFrameSize()
	heightOffset := t.Dialog.Title.GetVerticalFrameSize() + titleContentHeight +
		t.Dialog.InputPrompt.GetVerticalFrameSize() + inputContentHeight +
		t.Dialog.HelpView.GetVerticalFrameSize() + t.Dialog.View.GetVerticalFrameSize()
	d.input.SetWidth(max(0, innerWidth-t.Dialog.InputPrompt.GetHorizontalFrameSize()-1))
	d.list.SetSize(innerWidth, height-heightOffset)
	d.help.SetWidth(innerWidth)

	rc := NewRenderContext(t, width)
	rc.Title = "Import Session"
	rc.AddPart(t.Dialog.InputPrompt.Render(d.input.View()))
	rc.AddPart(t.Dialog.List.Height(d.list.Height()).Render(d.list.Render()))
	rc.Help = d.help.View(d)
	view := rc.Render()
	cur := InputCursor(t, d.input.Cursor())
	DrawCenterCursor(scr, area, view, cur)
	return cur
}

func (d *ImportAgent) ShortHelp() []key.Binding {
	return []key.Binding{d.keys.Next, d.keys.Select, d.keys.Close}
}

func (d *ImportAgent) FullHelp() [][]key.Binding {
	return [][]key.Binding{{d.keys.Select, d.keys.Next, d.keys.Previous, d.keys.Close}}
}

func (d *ImportReference) ID() string { return ImportReferenceID }

func (d *ImportReference) HandleMsg(msg tea.Msg) Action {
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		switch {
		case key.Matches(msg, d.keys.Close):
			return ActionClose{}
		case key.Matches(msg, d.keys.Select):
			reference := strings.TrimSpace(d.input.Value())
			if reference == "" {
				return ActionCmd{Cmd: util.ReportWarn("Enter a session ID or title")}
			}
			return ActionImportExternalSession{Source: d.source, Reference: reference}
		default:
			var cmd tea.Cmd
			d.input, cmd = d.input.Update(msg)
			return ActionCmd{Cmd: cmd}
		}
	case tea.PasteMsg:
		var cmd tea.Cmd
		d.input, cmd = d.input.Update(msg)
		return ActionCmd{Cmd: cmd}
	}
	return nil
}

func (d *ImportReference) Draw(scr uv.Screen, area uv.Rectangle) *tea.Cursor {
	t := d.com.Styles
	width := max(0, min(defaultDialogMaxWidth, area.Dx()))
	innerWidth := width - t.Dialog.View.GetHorizontalFrameSize()
	d.input.SetWidth(max(0, innerWidth-t.Dialog.InputPrompt.GetHorizontalFrameSize()-1))
	d.help.SetWidth(innerWidth)

	rc := NewRenderContext(t, width)
	rc.Title = "Import " + sessionimport.SourceName(d.source) + " Session"
	if d.source == "file" {
		rc.Title = "Import Transcript File"
	}
	rc.AddPart(t.Dialog.InputPrompt.Render(d.input.View()))
	if d.source == "file" {
		rc.AddPart(t.Dialog.SecondaryText.Render("Enter the path to an exported transcript."))
	} else {
		rc.AddPart(t.Dialog.SecondaryText.Render("Search by session ID or part of its title or first prompt."))
	}
	rc.AddPart(t.Dialog.SecondaryText.Render("Ghost imports the transcript into a new session."))
	rc.Help = d.help.View(d)
	view := rc.Render()
	cur := InputCursor(t, d.input.Cursor())
	DrawCenterCursor(scr, area, view, cur)
	return cur
}

func (d *ImportReference) ShortHelp() []key.Binding {
	return []key.Binding{d.keys.Select, d.keys.Close}
}

func (d *ImportReference) FullHelp() [][]key.Binding {
	return [][]key.Binding{{d.keys.Select, d.keys.Close}}
}
