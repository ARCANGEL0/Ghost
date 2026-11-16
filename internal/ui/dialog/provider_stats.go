package dialog

import (
	"context"
	"fmt"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/swadhinbiswas/ghost/internal/providerstats"
	"github.com/swadhinbiswas/ghost/internal/ui/common"
)

const ProviderStatsID = "provider_stats"

type providerStatsLoadedMsg struct {
	rows []providerstats.Row
	err  error
}

type ProviderStats struct {
	com  *common.Common
	rows []providerstats.Row
	err  error
}

var _ Dialog = (*ProviderStats)(nil)

func NewProviderStats(com *common.Common) *ProviderStats {
	return &ProviderStats{com: com}
}

func (p *ProviderStats) ID() string { return ProviderStatsID }

func (p *ProviderStats) InitialCmd() tea.Cmd { return p.refreshCmd() }

func (p *ProviderStats) refreshCmd() tea.Cmd {
	return func() tea.Msg {
		rows, err := p.com.App.ProviderStats.List(context.Background())
		return providerStatsLoadedMsg{rows: rows, err: err}
	}
}

func (p *ProviderStats) HandleMsg(msg tea.Msg) Action {
	switch msg := msg.(type) {
	case providerStatsLoadedMsg:
		p.rows = msg.rows
		p.err = msg.err
	case tea.KeyPressMsg:
		switch msg.String() {
		case "esc", "alt+esc":
			return ActionClose{}
		case "r":
			return ActionCmd{Cmd: p.refreshCmd()}
		}
	}
	return nil
}

func (p *ProviderStats) Draw(scr uv.Screen, area uv.Rectangle) *tea.Cursor {
	sty := p.com.Styles
	width := min(defaultDialogMaxWidth, area.Dx()-sty.Dialog.View.GetHorizontalBorderSize())
	height := min(defaultDialogHeight, area.Dy()-sty.Dialog.View.GetVerticalBorderSize())
	innerWidth := max(20, width-sty.Dialog.View.GetHorizontalFrameSize())
	lines := []string{}
	switch {
	case p.err != nil:
		lines = append(lines, "Could not load provider statistics: "+p.err.Error())
	case len(p.rows) == 0:
		lines = append(lines, "No provider requests recorded yet")
	default:
		for _, row := range p.rows {
			finished := row.Attempts - row.Cancelled
			rate := 0.0
			if finished > 0 {
				rate = float64(row.Successes) / float64(finished) * 100
			}
			avg := int64(0)
			if row.Attempts > 0 {
				avg = row.TotalLatencyMS / row.Attempts
			}
			line := fmt.Sprintf("%s / %s  %3.0f%% ok  %d/%d  %d ms avg  %d rate limits",
				row.Provider, row.Model, rate, row.Successes, finished, avg, row.RateLimits)
			lines = append(lines, sty.Dialog.NormalItem.Width(innerWidth).Render(line))
			detail := fmt.Sprintf("  %d failed · %d cancelled", row.Failures, row.Cancelled)
			if row.LastSuccessAt > 0 {
				detail += " · last success: " + time.UnixMilli(row.LastSuccessAt).Format("Jan 02 15:04")
			} else {
				detail += " · no successful request yet"
			}
			if row.LastError != "" {
				detail += " · last error: " + row.LastError
			}
			lines = append(lines, sty.HalfMuted.Render(detail))
		}
	}
	maxContent := max(1, height-7)
	if len(lines) > maxContent {
		lines = append(lines[:maxContent-1], "… more providers")
	}
	lines = append(lines, "", "r refresh · esc close")
	rc := NewRenderContext(sty, width)
	rc.Title = "Provider Stats"
	rc.AddPart(strings.Join(lines, "\n"))
	DrawCenter(scr, area, rc.Render())
	return nil
}
