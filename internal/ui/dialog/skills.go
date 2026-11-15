package dialog

import (
	"context"
	"fmt"
	"os"
	"slices"
	"strings"
	"sync/atomic"
	"time"

	"charm.land/bubbles/v2/help"
	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/sahilm/fuzzy"
	"github.com/swadhinbiswas/ghost/internal/config"
	"github.com/swadhinbiswas/ghost/internal/fsext"
	"github.com/swadhinbiswas/ghost/internal/home"
	"github.com/swadhinbiswas/ghost/internal/skills"
	"github.com/swadhinbiswas/ghost/internal/ui/common"
	"github.com/swadhinbiswas/ghost/internal/ui/list"
	"github.com/swadhinbiswas/ghost/internal/ui/styles"
	"github.com/swadhinbiswas/ghost/internal/ui/util"
)

const (
	SkillsID       = "skills"
	ImportSkillsID = "import-skills"
	skillsViewSize = 72
)

type skillsView uint8

const (
	installedView skillsView = iota
	marketplaceView
	clawHubView
)

type Skills struct {
	com       *common.Common
	id        string
	importing bool
	view      skillsView
	items     *list.FilterableList
	input     textinput.Model
	help      help.Model
	query     string
	status    string
	loading   bool
	busy      bool
	confirm   string
	detail    *skills.MarketplaceSkill
	disabled  []string
	searchFor string
	searchSeq atomic.Uint64
	installed []*skills.Skill
	agents    []skills.AgentSkill
	keyMap    struct {
		Select   key.Binding
		Next     key.Binding
		Previous key.Binding
		Tab      key.Binding
		Delete   key.Binding
		GitHub   key.Binding
		Confirm  key.Binding
		Cancel   key.Binding
		Install  key.Binding
		Close    key.Binding
	}
}

type skillItem struct {
	skill    *skills.Skill
	market   *skills.MarketplaceSkill
	source   string
	state    string
	managed  bool
	imported bool
	t        *styles.Styles
	match    fuzzy.Match
	cache    map[int]string
	focused  bool
}

type skillsLoadedMsg struct {
	installed []*skills.Skill
	agents    []skills.AgentSkill
	err       error
}

type marketplaceSearchMsg struct {
	query   string
	catalog string
	items   []skills.MarketplaceSkill
	err     error
}

type skillsChangedMsg struct {
	installed []*skills.Skill
	agents    []skills.AgentSkill
	disabled  []string
	update    bool
	notice    string
	err       error
}

// ActionSkillsUpdated reports saved skill settings to the UI model.
type ActionSkillsUpdated struct {
	Disabled []string
	Update   bool
	Notice   string
}

var (
	_ Dialog   = (*Skills)(nil)
	_ ListItem = (*skillItem)(nil)
)

// NewSkills creates the skill manager or the local import picker.
func NewSkills(com *common.Common, importing bool, query string) *Skills {
	d := &Skills{com: com, importing: importing, query: query}
	d.id = SkillsID
	if importing {
		d.id = ImportSkillsID
	} else if query != "" {
		d.view = marketplaceView
	}
	d.help = help.New()
	d.help.Styles = com.Styles.DialogHelpStyles()
	d.items = list.NewFilterableList()
	d.items.Focus()
	d.input = textinput.New()
	d.input.SetVirtualCursor(false)
	d.input.SetStyles(com.Styles.TextInput)
	d.input.Focus()
	d.input.SetValue(query)
	d.input.Placeholder = "Search installed skills"
	if d.view == marketplaceView {
		d.input.Placeholder = "Search skills.sh"
	} else if d.view == clawHubView {
		d.input.Placeholder = "Search ClawHub"
	}
	selectAction := "toggle"
	if importing {
		selectAction = "import"
	} else if d.view != installedView {
		selectAction = "overview"
	}
	d.keyMap.Select = key.NewBinding(key.WithKeys("enter", "ctrl+y"), key.WithHelp("enter", selectAction))
	d.keyMap.Next = key.NewBinding(key.WithKeys("down", "ctrl+n"), key.WithHelp("↓", "choose"))
	d.keyMap.Previous = key.NewBinding(key.WithKeys("up", "ctrl+p"), key.WithHelp("↑", "choose"))
	d.keyMap.Tab = key.NewBinding(key.WithKeys("tab"), key.WithHelp("tab", "switch view"))
	d.keyMap.Delete = key.NewBinding(key.WithKeys("ctrl+x"), key.WithHelp("ctrl+x", "uninstall"))
	d.keyMap.GitHub = key.NewBinding(key.WithKeys("ctrl+g"), key.WithHelp("ctrl+g", "install GitHub skill"))
	d.keyMap.Confirm = key.NewBinding(key.WithKeys("y"), key.WithHelp("y", "confirm"))
	d.keyMap.Cancel = key.NewBinding(key.WithKeys("n"), key.WithHelp("n", "cancel"))
	d.keyMap.Install = key.NewBinding(key.WithKeys("i", "I"), key.WithHelp("I", "install"))
	d.keyMap.Close = CloseKey
	if cfg := com.Config(); cfg != nil && cfg.Options != nil {
		d.disabled = slices.Clone(cfg.Options.DisabledSkills)
	}
	return d
}

func (d *Skills) ID() string { return d.id }

// RequestUninstall opens the manager with a named removal ready to confirm.
func (d *Skills) RequestUninstall(name string) {
	d.view = installedView
	d.input.Placeholder = "Search installed skills"
	d.input.SetValue(name)
	d.confirm = name
	d.status = "Uninstall " + name + "? Press y to confirm or n to cancel"
}

// OpenMarketplace switches the manager to online skill search.
func (d *Skills) OpenMarketplace(query string) {
	d.view = marketplaceView
	d.input.Placeholder = "Search skills.sh"
	d.input.SetValue(query)
	d.query = query
	d.searchFor = ""
	d.applyFilter()
}

func (d *Skills) OpenClawHub(query string) {
	d.view = clawHubView
	d.input.Placeholder = "Search ClawHub"
	d.input.SetValue(query)
	d.query = query
	d.searchFor = ""
	d.applyFilter()
}

// StartGitHubInstall installs a skill from a GitHub path or URL.
func (d *Skills) StartGitHubInstall(source string) tea.Cmd {
	action := d.installGitHub(source)
	if cmd, ok := action.(ActionCmd); ok {
		return cmd.Cmd
	}
	return nil
}

// StartMarketplaceInstall installs one skills.sh result by ID.
func (d *Skills) StartMarketplaceInstall(id string) tea.Cmd {
	d.busy = true
	root := skills.ManagedDir()
	paths := skillPaths(d.com)
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
		defer cancel()
		installed, err := skills.InstallMarketplace(ctx, root, id)
		if err != nil {
			return skillsChangedMsg{err: err}
		}
		return skillsChangedMsg{installed: discoverSkills(paths), notice: "Installed " + installed.Name}
	}
}

func (d *Skills) StartClawHubInstall(slug string) tea.Cmd {
	d.busy = true
	root := skills.ManagedDir()
	paths := skillPaths(d.com)
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
		defer cancel()
		installed, err := skills.InstallClawHub(ctx, root, slug)
		if err != nil {
			return skillsChangedMsg{err: err}
		}
		return skillsChangedMsg{installed: discoverSkills(paths), notice: "Installed " + installed.Name}
	}
}

func (d *Skills) InitialCmd() tea.Cmd {
	workingDir := d.com.Store().WorkingDir()
	paths := []string(nil)
	if cfg := d.com.Config(); cfg != nil && cfg.Options != nil {
		paths = slices.Clone(cfg.Options.SkillsPaths)
	}
	if !d.importing && d.view != installedView {
		return d.searchCmd()
	}
	d.loading = true
	d.status = "Loading skills..."
	load := func() tea.Msg {
		if d.importing {
			return skillsLoadedMsg{agents: skills.DiscoverAgentSkills(workingDir, skills.ManagedDir())}
		}
		return skillsLoadedMsg{installed: discoverSkills(paths)}
	}
	if d.query != "" {
		return tea.Batch(load, d.searchCmd())
	}
	return load
}

func (d *Skills) HandleMsg(msg tea.Msg) Action {
	switch msg := msg.(type) {
	case skillsLoadedMsg:
		d.loading = false
		if msg.err != nil {
			d.status = msg.err.Error()
			return nil
		}
		if d.importing {
			d.setAgentItems(msg.agents)
		} else {
			d.setInstalledItems(msg.installed)
		}
		return nil
	case marketplaceSearchMsg:
		if d.importing || msg.catalog != d.catalogName() || msg.query != d.input.Value() {
			return nil
		}
		d.loading = false
		if msg.err != nil {
			d.status = msg.err.Error()
			d.items.SetItems()
			return nil
		}
		d.status = fmt.Sprintf("%d skills found on %s", len(msg.items), msg.catalog)
		d.setMarketplaceItems(msg.items)
		return nil
	case skillsChangedMsg:
		d.busy = false
		if msg.err != nil {
			return ActionCmd{Cmd: reportSkillError(msg.err)}
		}
		if msg.update {
			d.disabled = slices.Clone(msg.disabled)
		}
		if d.importing {
			if msg.agents != nil {
				d.setAgentItems(msg.agents)
			} else {
				d.setAgentItems(d.agents)
			}
		} else if msg.installed != nil {
			d.setInstalledItems(msg.installed)
			d.view = installedView
			d.input.Placeholder = "Search installed skills"
			d.input.SetValue("")
			d.items.SetFilter("")
		} else {
			d.setInstalledItems(d.installed)
		}
		d.status = msg.notice
		return ActionSkillsUpdated{Disabled: slices.Clone(msg.disabled), Update: msg.update, Notice: msg.notice}
	case tea.KeyPressMsg:
		return d.handleKey(msg)
	case tea.PasteMsg:
		if d.busy {
			return nil
		}
		var cmd tea.Cmd
		d.input, cmd = d.input.Update(msg)
		d.applyFilter()
		return ActionCmd{Cmd: tea.Batch(cmd, d.searchCmd())}
	}
	return nil
}

func (d *Skills) handleKey(msg tea.KeyPressMsg) Action {
	if key.Matches(msg, d.keyMap.Close) {
		if d.confirm != "" {
			d.confirm = ""
			d.status = ""
			return nil
		}
		if d.detail != nil {
			d.detail = nil
			return nil
		}
		return ActionClose{}
	}
	if d.busy {
		return nil
	}
	if d.detail != nil {
		if key.Matches(msg, d.keyMap.Install) {
			item := &skillItem{market: d.detail, skill: &skills.Skill{Name: d.detail.Name}}
			return d.installMarketplace(item)
		}
		return nil
	}
	if d.confirm != "" {
		switch {
		case key.Matches(msg, d.keyMap.Confirm):
			return d.uninstall(d.confirm)
		case key.Matches(msg, d.keyMap.Cancel):
			d.confirm = ""
			d.status = ""
			return nil
		default:
			return nil
		}
	}
	switch {
	case key.Matches(msg, d.keyMap.Previous):
		if d.items.IsSelectedFirst() {
			d.items.SelectLast()
		} else {
			d.items.SelectPrev()
		}
		d.items.ScrollToSelected()
	case key.Matches(msg, d.keyMap.Next):
		if d.items.IsSelectedLast() {
			d.items.SelectFirst()
		} else {
			d.items.SelectNext()
		}
		d.items.ScrollToSelected()
	case !d.importing && key.Matches(msg, d.keyMap.Tab):
		d.searchSeq.Add(1)
		switch d.view {
		case installedView:
			d.view = marketplaceView
			d.input.Placeholder = "Search skills.sh"
		case marketplaceView:
			d.view = clawHubView
			d.input.Placeholder = "Search ClawHub"
		default:
			d.view = installedView
			d.input.Placeholder = "Search installed skills"
		}
		if d.view == installedView {
			d.keyMap.Select.SetHelp("enter", "toggle")
		} else {
			d.keyMap.Select.SetHelp("enter", "overview")
		}
		d.searchFor = ""
		d.status = ""
		d.applyFilter()
		return ActionCmd{Cmd: d.searchCmd()}
	case key.Matches(msg, d.keyMap.Select):
		item, ok := d.items.SelectedItem().(*skillItem)
		if !ok {
			return nil
		}
		if d.importing {
			if item.imported {
				d.status = "Already installed in Ghost"
				return nil
			}
			return d.importSkill(item)
		}
		if d.view != installedView {
			if item.market == nil {
				return nil
			}
			market := *item.market
			d.detail = &market
			return nil
		}
		return d.toggleSkill(item)
	case !d.importing && d.view == installedView && key.Matches(msg, d.keyMap.Delete):
		item, ok := d.items.SelectedItem().(*skillItem)
		if !ok {
			return nil
		}
		if !item.managed {
			d.status = "Only skills installed by Ghost can be uninstalled here"
			return nil
		}
		d.confirm = item.skill.Name
		d.status = "Uninstall " + item.skill.Name + "? Press y to confirm or n to cancel"
		return nil
	case !d.importing && d.view != installedView && key.Matches(msg, d.keyMap.GitHub):
		if strings.TrimSpace(d.input.Value()) == "" {
			d.status = "Enter a GitHub skill path or URL first"
			return nil
		}
		return d.installGitHub(strings.TrimSpace(d.input.Value()))
	default:
		var cmd tea.Cmd
		d.input, cmd = d.input.Update(msg)
		d.applyFilter()
		return ActionCmd{Cmd: tea.Batch(cmd, d.searchCmd())}
	}
	return nil
}

func (d *Skills) toggleSkill(item *skillItem) Action {
	name := item.skill.Name
	disabled := slices.Clone(d.disabled)
	if slices.Contains(disabled, name) {
		disabled = slices.DeleteFunc(disabled, func(value string) bool { return value == name })
		d.status = "Enabling " + name
	} else {
		disabled = append(disabled, name)
		d.status = "Disabling " + name
	}
	slices.Sort(disabled)
	d.busy = true
	store := d.com.Store()
	return ActionCmd{Cmd: func() tea.Msg {
		if err := store.SetConfigField(config.ScopeGlobal, "options.disabled_skills", disabled); err != nil {
			return skillsChangedMsg{err: err}
		}
		state := "enabled"
		if slices.Contains(disabled, name) {
			state = "disabled"
		}
		return skillsChangedMsg{disabled: disabled, update: true, notice: fmt.Sprintf("Skill %s %s", name, state)}
	}}
}

func (d *Skills) uninstall(name string) Action {
	d.confirm = ""
	d.busy = true
	root := skills.ManagedDir()
	paths := skillPaths(d.com)
	store := d.com.Store()
	disabled := slices.DeleteFunc(slices.Clone(d.disabled), func(value string) bool { return value == name })
	return ActionCmd{Cmd: func() tea.Msg {
		if err := skills.Uninstall(root, name); err != nil {
			return skillsChangedMsg{err: err}
		}
		if err := store.SetConfigField(config.ScopeGlobal, "options.disabled_skills", disabled); err != nil {
			return skillsChangedMsg{err: err}
		}
		return skillsChangedMsg{
			installed: discoverSkills(paths),
			disabled:  disabled,
			update:    true,
			notice:    "Uninstalled " + name,
		}
	}}
}

func (d *Skills) importSkill(item *skillItem) Action {
	d.busy = true
	root := skills.ManagedDir()
	source := item.skill.Path
	return ActionCmd{Cmd: func() tea.Msg {
		installed, err := skills.CopySkill(root, source)
		if err != nil {
			return skillsChangedMsg{err: err}
		}
		return skillsChangedMsg{
			agents: skills.DiscoverAgentSkills(d.com.Store().WorkingDir(), root),
			notice: "Imported " + installed.Name,
		}
	}}
}

func (d *Skills) installMarketplace(item *skillItem) Action {
	if item.market == nil {
		return nil
	}
	d.busy = true
	root := skills.ManagedDir()
	paths := skillPaths(d.com)
	return ActionCmd{Cmd: func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
		defer cancel()
		var installed *skills.Skill
		var err error
		if item.market.Catalog == "clawhub" {
			installed, err = skills.InstallClawHub(ctx, root, item.market.ID)
		} else {
			installed, err = skills.InstallMarketplace(ctx, root, item.market.ID)
		}
		if err != nil {
			return skillsChangedMsg{err: err}
		}
		return skillsChangedMsg{installed: discoverSkills(paths), notice: "Installed " + installed.Name}
	}}
}

func (d *Skills) installGitHub(source string) Action {
	d.busy = true
	root := skills.ManagedDir()
	paths := skillPaths(d.com)
	return ActionCmd{Cmd: func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
		defer cancel()
		installed, err := skills.InstallGitHubSkill(ctx, root, source)
		if err != nil {
			return skillsChangedMsg{err: err}
		}
		return skillsChangedMsg{installed: discoverSkills(paths), notice: "Installed " + installed.Name}
	}}
}

func (d *Skills) searchCmd() tea.Cmd {
	if d.importing || d.view == installedView {
		return nil
	}
	query := strings.TrimSpace(d.input.Value())
	d.query = query
	catalog := d.catalogName()
	if len([]rune(query)) < 2 {
		d.searchSeq.Add(1)
		d.loading = false
		d.searchFor = ""
		d.status = "Type at least 2 characters to search " + catalog
		d.items.SetItems()
		return nil
	}
	if query == d.searchFor {
		return nil
	}
	d.loading = true
	d.searchFor = query
	d.status = "Searching " + catalog + "..."
	searchID := d.searchSeq.Add(1)
	return func() tea.Msg {
		time.Sleep(250 * time.Millisecond)
		if d.searchSeq.Load() != searchID {
			return nil
		}
		ctx, cancel := context.WithTimeout(context.Background(), 22*time.Second)
		defer cancel()
		var items []skills.MarketplaceSkill
		var err error
		if catalog == "ClawHub" {
			items, err = skills.SearchClawHub(ctx, query)
		} else {
			items, err = skills.SearchMarketplace(ctx, query)
		}
		return marketplaceSearchMsg{query: query, catalog: catalog, items: items, err: err}
	}
}

func (d *Skills) catalogName() string {
	if d.view == clawHubView {
		return "ClawHub"
	}
	return "skills.sh"
}

func (d *Skills) applyFilter() {
	if d.view != installedView && !d.importing {
		return
	}
	d.items.SetFilter(d.input.Value())
	d.items.SetSelected(0)
	d.items.ScrollToTop()
}

func (d *Skills) setInstalledItems(found []*skills.Skill) {
	d.installed = found
	root := skills.ManagedDir()
	items := make([]list.FilterableItem, 0, len(found))
	for _, skill := range found {
		state := "enabled"
		if slices.Contains(d.disabled, skill.Name) {
			state = "disabled"
		}
		managed := skill.Path == root || strings.HasPrefix(skill.Path, root+string(os.PathSeparator))
		items = append(items, &skillItem{skill: skill, source: home.Short(skill.Path), state: state, managed: managed, t: d.com.Styles})
	}
	d.items.SetItems(items...)
	d.items.SetFilter(d.input.Value())
	d.items.SetSelected(0)
}

func (d *Skills) setAgentItems(found []skills.AgentSkill) {
	d.agents = found
	items := make([]list.FilterableItem, 0, len(found))
	for _, entry := range found {
		state := "available to import"
		if entry.Installed {
			state = "already imported"
		}
		items = append(items, &skillItem{
			skill:    entry.Skill,
			source:   entry.Agent,
			state:    state,
			imported: entry.Installed,
			t:        d.com.Styles,
		})
	}
	d.items.SetItems(items...)
	d.items.SetFilter(d.input.Value())
	d.items.SetSelected(0)
}

func (d *Skills) setMarketplaceItems(found []skills.MarketplaceSkill) {
	items := make([]list.FilterableItem, 0, len(found))
	for i := range found {
		market := found[i]
		detail := market.Source
		if market.Installs > 0 {
			detail = fmt.Sprintf("%s · %s installs", detail, compactCount(market.Installs))
		}
		items = append(items, &skillItem{market: &market, skill: &skills.Skill{Name: market.Name}, source: detail, state: market.Catalog, t: d.com.Styles})
	}
	d.items.SetItems(items...)
	d.items.SetFilter(d.input.Value())
	d.items.SetSelected(0)
}

func discoverSkills(paths []string) []*skills.Skill {
	expanded := make([]string, 0, len(paths))
	for _, path := range paths {
		if value, err := fsext.Expand(path); err == nil {
			expanded = append(expanded, value)
		} else {
			expanded = append(expanded, path)
		}
	}
	return skills.Discover(expanded)
}

func skillPaths(com *common.Common) []string {
	if cfg := com.Config(); cfg != nil && cfg.Options != nil {
		return slices.Clone(cfg.Options.SkillsPaths)
	}
	return nil
}

func compactCount(count int) string {
	switch {
	case count >= 1_000_000:
		return fmt.Sprintf("%.1fm", float64(count)/1_000_000)
	case count >= 1_000:
		return fmt.Sprintf("%.1fk", float64(count)/1_000)
	default:
		return fmt.Sprint(count)
	}
}

func reportSkillError(err error) tea.Cmd {
	return util.ReportError(err)
}

func (d *Skills) Cursor() *tea.Cursor {
	return InputCursor(d.com.Styles, d.input.Cursor())
}

func (d *Skills) Draw(scr uv.Screen, area uv.Rectangle) *tea.Cursor {
	t := d.com.Styles
	width := max(0, min(skillsViewSize, area.Dx()-t.Dialog.View.GetHorizontalBorderSize()))
	height := max(0, min(defaultDialogHeight, area.Dy()-t.Dialog.View.GetVerticalBorderSize()))
	innerWidth := width - t.Dialog.View.GetHorizontalFrameSize()
	heightOffset := t.Dialog.Title.GetVerticalFrameSize() + titleContentHeight +
		t.Dialog.InputPrompt.GetVerticalFrameSize() + inputContentHeight +
		t.Dialog.HelpView.GetVerticalFrameSize() + t.Dialog.View.GetVerticalFrameSize() + 5
	d.input.SetWidth(max(0, innerWidth-t.Dialog.InputPrompt.GetHorizontalFrameSize()-1))
	d.items.SetSize(innerWidth, height-heightOffset)
	d.help.SetWidth(innerWidth)
	rc := NewRenderContext(t, width)
	if d.detail != nil {
		rc.Title = "Skill Overview"
		rc.AddPart(t.Dialog.SelectedItem.Render(d.detail.Name))
		rc.AddPart(t.Dialog.SecondaryText.Render("Description"))
		description := strings.TrimSpace(d.detail.Description)
		if description == "" {
			description = "No description was provided by the catalog."
		}
		rc.AddPart(lipgloss.NewStyle().Width(max(1, innerWidth)).Render(description))
		rc.AddPart(t.Dialog.SecondaryText.Render("Catalog: " + d.detail.Catalog))
		location := d.detail.Source
		if d.detail.Slug != "" {
			location = strings.Trim(strings.Join([]string{location, d.detail.Slug}, "/"), "/")
		}
		if location != "" {
			rc.AddPart(lipgloss.NewStyle().Width(max(1, innerWidth)).Render("Location: " + location))
		}
		rc.AddPart(lipgloss.NewStyle().Width(max(1, innerWidth)).Render("Skill ID: " + d.detail.ID))
		url := strings.TrimSpace(d.detail.URL)
		if url == "" {
			url = strings.TrimSpace(d.detail.InstallURL)
		}
		if url != "" {
			rc.AddPart(lipgloss.NewStyle().Width(max(1, innerWidth)).Render("URL: " + url))
		} else {
			rc.AddPart(t.Dialog.SecondaryText.Render("URL: not provided by catalog"))
		}
		if d.detail.InstallURL != "" && d.detail.InstallURL != url {
			rc.AddPart(lipgloss.NewStyle().Width(max(1, innerWidth)).Render("Install URL: " + d.detail.InstallURL))
		}
		if d.detail.Installs > 0 {
			rc.AddPart(t.Dialog.SecondaryText.Render(compactCount(d.detail.Installs) + " installs"))
		}
		rc.Help = d.help.View(d)
	} else if d.importing {
		rc.Title = "Import Skills"
		rc.AddPart(t.Dialog.SecondaryText.Render("Choose a skill installed by another coding agent."))
	} else {
		rc.Title = "Skills"
		if d.view == marketplaceView {
			rc.AddPart(t.Dialog.SecondaryText.Render("Search skills.sh for skills shared across coding agents."))
		} else if d.view == clawHubView {
			rc.AddPart(t.Dialog.SecondaryText.Render("Search ClawHub for community skills."))
		} else {
			rc.AddPart(t.Dialog.SecondaryText.Render("Enable, disable, or remove Ghost skills."))
		}
	}
	if d.detail == nil {
		rc.AddPart(t.Dialog.InputPrompt.Render(d.input.View()))
	}
	if !d.importing && d.detail == nil {
		installed := "Installed"
		marketplace := "skills.sh"
		clawHub := "ClawHub"
		if d.view == installedView {
			installed = t.Dialog.SelectedItem.Render("[ Installed ]")
			marketplace = t.Dialog.SecondaryText.Render("  skills.sh  ")
		} else if d.view == marketplaceView {
			installed = t.Dialog.SecondaryText.Render("  Installed  ")
			marketplace = t.Dialog.SelectedItem.Render("[ skills.sh ]")
		} else {
			installed = t.Dialog.SecondaryText.Render("  Installed  ")
			marketplace = t.Dialog.SecondaryText.Render("  skills.sh  ")
			clawHub = t.Dialog.SelectedItem.Render("[ ClawHub ]")
		}
		if d.view != clawHubView {
			clawHub = t.Dialog.SecondaryText.Render("  ClawHub  ")
		}
		rc.AddPart(lipgloss.JoinHorizontal(lipgloss.Top, installed, marketplace, clawHub))
	}
	if d.detail == nil {
		rc.AddPart(t.Dialog.List.Height(d.items.Height()).Render(d.items.Render()))
		if d.status != "" {
			rc.AddPart(t.Dialog.SecondaryText.Render(d.status))
		}
		rc.Help = d.help.View(d)
	}
	view := rc.Render()
	var cur *tea.Cursor
	if d.detail == nil {
		cur = d.Cursor()
	}
	DrawCenterCursor(scr, area, view, cur)
	return cur
}

func (d *Skills) ShortHelp() []key.Binding {
	if d.detail != nil {
		back := key.NewBinding(key.WithKeys("esc", "alt+esc"), key.WithHelp("esc", "back"))
		return []key.Binding{d.keyMap.Install, back}
	}
	bindings := []key.Binding{d.keyMap.Previous, d.keyMap.Next, d.keyMap.Select}
	if !d.importing {
		bindings = append(bindings, d.keyMap.Tab)
		if d.view == installedView {
			bindings = append(bindings, d.keyMap.Delete)
		} else {
			bindings = append(bindings, d.keyMap.GitHub)
		}
	}
	bindings = append(bindings, d.keyMap.Close)
	return bindings
}

func (d *Skills) FullHelp() [][]key.Binding { return [][]key.Binding{d.ShortHelp()} }

func (i *skillItem) Filter() string {
	if i.market != nil {
		return i.market.Name + " " + i.market.Source + " " + i.market.ID
	}
	return i.skill.Name + " " + i.skill.Description + " " + i.source
}

func (i *skillItem) ID() string {
	if i.market != nil {
		return i.market.ID
	}
	return i.skill.Name + "@" + i.skill.Path
}

func (i *skillItem) SetFocused(focused bool) {
	if i.focused != focused {
		i.cache = nil
	}
	i.focused = focused
}

func (i *skillItem) SetMatch(match fuzzy.Match) {
	i.cache = nil
	i.match = match
}

func (i *skillItem) Render(width int) string {
	styles := ListItemStyles{
		ItemBlurred:     i.t.Dialog.NormalItem,
		ItemFocused:     i.t.Dialog.SelectedItem,
		InfoTextBlurred: i.t.Subtle,
		InfoTextFocused: i.t.Base,
	}
	return renderItem(styles, i.skill.Name, i.state+" · "+i.source, i.focused, width, i.cache, &i.match)
}
