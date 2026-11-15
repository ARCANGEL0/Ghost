package dialog

import (
	"context"
	"fmt"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"
	"github.com/swadhinbiswas/ghost/internal/agent"
	"github.com/swadhinbiswas/ghost/internal/ui/common"
	"github.com/swadhinbiswas/ghost/internal/ui/util"
)

const RunsID = "runs"

type runSnapshotMsg struct {
	runs []agent.AgentRun
}

type runRefreshMsg time.Time

type runActionDoneMsg struct {
	label string
	err   error
}

type Runs struct {
	com      *common.Common
	runs     []agent.AgentRun
	selected int
}

var _ Dialog = (*Runs)(nil)

func NewRuns(com *common.Common) *Runs {
	return &Runs{com: com}
}

func (r *Runs) ID() string { return RunsID }

func (r *Runs) InitialCmd() tea.Cmd { return r.refreshCmd() }

func (r *Runs) refreshCmd() tea.Cmd {
	return func() tea.Msg {
		return runSnapshotMsg{runs: r.com.App.AgentCoordinator.Runs()}
	}
}

func (r *Runs) HandleMsg(msg tea.Msg) Action {
	switch msg := msg.(type) {
	case runSnapshotMsg:
		r.runs = msg.runs
		if r.selected >= len(r.runs) {
			r.selected = max(0, len(r.runs)-1)
		}
		return ActionCmd{Cmd: tea.Tick(time.Second, func(now time.Time) tea.Msg { return runRefreshMsg(now) })}
	case runRefreshMsg:
		return ActionCmd{Cmd: r.refreshCmd()}
	case runActionDoneMsg:
		var status tea.Cmd
		if msg.err != nil {
			status = util.ReportError(msg.err)
		} else {
			status = util.ReportInfo(msg.label)
		}
		return ActionCmd{Cmd: tea.Batch(r.refreshCmd(), status)}
	case tea.KeyPressMsg:
		switch msg.String() {
		case "esc", "alt+esc":
			return ActionClose{}
		case "up", "k":
			if r.selected > 0 {
				r.selected--
			}
		case "down", "j":
			if r.selected+1 < len(r.runs) {
				r.selected++
			}
		case "p":
			return r.runAction("pause or resume", func(run agent.AgentRun) error {
				if run.State == "paused" {
					return r.com.App.AgentCoordinator.ResumeRun(run.ID)
				}
				return r.com.App.AgentCoordinator.PauseRun(run.ID)
			})
		case "c":
			return r.runAction("run cancelled", func(run agent.AgentRun) error {
				return r.com.App.AgentCoordinator.CancelRun(run.ID)
			})
		case "r":
			return r.runAction("run retried", func(run agent.AgentRun) error {
				return r.com.App.AgentCoordinator.RetryRun(context.Background(), run.ID)
			})
		}
	}
	return nil
}

func (r *Runs) runAction(label string, action func(agent.AgentRun) error) Action {
	if r.selected >= len(r.runs) {
		return ActionCmd{Cmd: util.ReportWarn("No run selected")}
	}
	run := r.runs[r.selected]
	return ActionCmd{Cmd: func() tea.Msg { return runActionDoneMsg{label: label, err: action(run)} }}
}

func (r *Runs) Draw(scr uv.Screen, area uv.Rectangle) *tea.Cursor {
	sty := r.com.Styles
	width := min(defaultDialogMaxWidth, area.Dx()-sty.Dialog.View.GetHorizontalBorderSize())
	height := min(defaultDialogHeight, area.Dy()-sty.Dialog.View.GetVerticalBorderSize())
	innerWidth := max(20, width-sty.Dialog.View.GetHorizontalFrameSize())
	rows := max(1, min(len(r.runs), height-7))
	lines := make([]string, 0, rows*2+3)
	if len(r.runs) == 0 {
		lines = append(lines, "No agent runs yet")
	} else {
		start := max(0, min(r.selected-rows/2, len(r.runs)-rows))
		for i := start; i < min(start+rows, len(r.runs)); i++ {
			run := r.runs[i]
			line := fmt.Sprintf("%s  %s · %s/%s · %s", run.State, run.Title, run.Provider, run.Model, run.Duration.Round(time.Second))
			if i == r.selected {
				line = sty.Dialog.SelectedItem.Width(innerWidth).Render(line)
			} else {
				line = sty.Dialog.NormalItem.Width(innerWidth).Render(line)
			}
			lines = append(lines, line)
			details := fmt.Sprintf("  %d tools · %d retries · %d tokens · $%.4f", run.ToolCalls, run.Retries, run.InputTokens+run.OutputTokens, run.Cost)
			lines = append(lines, sty.HalfMuted.Render(details))
		}
	}
	if len(r.runs) > 0 && r.selected < len(r.runs) {
		lines = append(lines, "", "Task: "+compactRunText(r.runs[r.selected].Prompt, innerWidth-6))
	}
	lines = append(lines, "", "↑/↓ select · p pause/resume · c cancel · r retry · esc close")
	view := strings.Join(lines, "\n")
	rc := NewRenderContext(sty, width)
	rc.Title = "Agent Runs"
	rc.AddPart(view)
	DrawCenter(scr, area, rc.Render())
	return nil
}

func compactRunText(text string, width int) string {
	text = strings.Join(strings.Fields(text), " ")
	if width < 1 {
		return text
	}
	return ansi.Truncate(text, width, "…")
}
