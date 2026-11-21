package logo

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"strings"
	"sync"

	"charm.land/lipgloss/v2"

	"github.com/swadhinbiswas/ghost/internal/ui/assets"
	"github.com/swadhinbiswas/ghost/internal/ui/styles"
)

const diag = "╱"

const ghostFrameCount = 6

type Opts struct {
	FieldColor   color.Color
	TitleColorA  color.Color
	TitleColorB  color.Color
	CharmColor   color.Color
	VersionColor color.Color
	Width        int
}

var ghostLines = []string{
	` ________  ___  ___  ________  ________  _________   `,
	`|\   ____\|\  \|\  \|\   __  \|\   ____\|\___   ___\ `,
	`\ \  \___|\ \  \\\  \ \  \|\  \ \  \___|\|___ \  \_| `,
	` \ \  \  __\ \   __  \ \  \\\  \ \_____  \   \ \  \  `,
	`  \ \  \|\  \ \  \ \  \ \  \\\  \|____|\  \   \ \  \ `,
	`   \ \_______\ \__\ \__\ \_______\____\_\  \   \ \__\`,
	`    \|_______|\|__|\|__|\|_______|\_________\   \|__|`,
	`                                 \|_________|        `,
}

var (
	ghostFramesOnce sync.Once
	ghostFrames     []image.Image
)

// Render renders the full ASCII art Ghost logo using theme colors.
func Render(s *styles.Styles, version string, compact bool, o Opts) string {
	primary := lipgloss.NewStyle().Foreground(s.Primary)

	rendered := make([]string, len(ghostLines))
	for i, line := range ghostLines {
		rendered[i] = primary.Render(line)
	}

	logo := strings.Join(rendered, "\n")

	if version != "" {
		ver := lipgloss.NewStyle().
			Foreground(s.Secondary).
			Faint(true).
			Render("v" + version)
		logo = fmt.Sprintf("%s  %s", logo, ver)
	}

	return logo
}

// SmallRender renders a compact "Ghost" text logo with a gradient and
// trailing diagonal characters that fill the given width.
func SmallRender(t *styles.Styles, width int) string {
	title := styles.ApplyBoldForegroundGrad(t, "Ghost", t.Secondary, t.Primary)
	remainingWidth := width - lipgloss.Width(title) - 1
	if remainingWidth > 0 {
		lines := strings.Repeat(diag, remainingWidth)
		title = fmt.Sprintf("%s %s", title, t.Base.Foreground(t.Primary).Render(lines))
	}

	return title
}

// RenderAnimated pairs the Ghost wordmark with a floating pixel mascot.
func RenderAnimated(s *styles.Styles, version string, compact bool, o Opts, frame int) string {
	wordmark := renderWordmark()
	if sprite := loadGhostFrame(frame); sprite != nil {
		wordmark = renderBanner(s, wordmark, sprite, o.Width)
	}

	logo := strings.Join(wordmark, "\n")
	if version != "" {
		ver := lipgloss.NewStyle().
			Foreground(s.Secondary).
			Faint(true).
			Render("v" + version)
		logo = fmt.Sprintf("%s  %s", logo, ver)
	}

	return logo
}

func renderWordmark() []string {
	title := renderTitle()
	rule := lipgloss.NewStyle().
		Foreground(color.White).
		Faint(true).
		Render("─────────")
	return []string{title, rule}
}

func renderTitle() string {
	return lipgloss.NewStyle().
		Foreground(color.White).
		Bold(true).
		Render("G H O S T")
}

func renderBanner(s *styles.Styles, wordmark []string, sprite image.Image, width int) []string {
	wordmarkWidth := lipgloss.Width(wordmark[0])
	if width <= 0 {
		width = wordmarkWidth + 30
	}
	if width < wordmarkWidth+14 {
		wordmark = []string{renderTitle()}
		wordmarkWidth = lipgloss.Width(wordmark[0])
	}
	if width < wordmarkWidth+10 {
		return wordmark
	}

	pixelWidth := min(24, max(8, width-wordmarkWidth-2))
	spriteRows := (pixelWidth + 1) / 2
	trackWidth := pixelWidth
	trackHeight := spriteRows + 1
	shadowY := spriteRows
	wordmarkY := max(0, (trackHeight-len(wordmark))/2)
	background := s.BgBase

	lines := make([]string, trackHeight)
	for y := range lines {
		var line strings.Builder
		if y == shadowY {
			line.WriteString(strings.Repeat(" ", pixelWidth/4))
			shadow := lipgloss.NewStyle().
				Foreground(s.FgSubtle).
				Faint(true).
				Render(strings.Repeat("━", max(4, pixelWidth/2)))
			line.WriteString(shadow)
			line.WriteString(strings.Repeat(" ", trackWidth-lipgloss.Width(shadow)-pixelWidth/4))
		} else if y >= spriteRows {
			line.WriteString(strings.Repeat(" ", trackWidth))
		} else {
			line.WriteString(renderSpriteRow(sprite, background, pixelWidth, y))
		}
		line.WriteString("  ")
		wordmarkRow := y - wordmarkY
		if wordmarkRow >= 0 && wordmarkRow < len(wordmark) {
			line.WriteString(wordmark[wordmarkRow])
		} else {
			line.WriteString(strings.Repeat(" ", wordmarkWidth))
		}
		lines[y] = line.String()
	}
	return lines
}

func loadGhostFrame(frame int) image.Image {
	ghostFramesOnce.Do(func() {
		sheet, err := png.Decode(bytes.NewReader(assets.GhostFramesPNG))
		if err != nil {
			return
		}

		bounds := sheet.Bounds()
		frameWidth := bounds.Dx() / ghostFrameCount
		if frameWidth == 0 {
			return
		}
		keyFrames := make([]image.Image, ghostFrameCount)
		ghostFrames = make([]image.Image, ghostFrameCount)
		for i := range keyFrames {
			rect := image.Rect(
				bounds.Min.X+i*frameWidth,
				bounds.Min.Y,
				bounds.Min.X+(i+1)*frameWidth,
				bounds.Max.Y,
			)
			sprite := image.NewRGBA(image.Rect(0, 0, frameWidth, bounds.Dy()))
			draw.Draw(sprite, sprite.Bounds(), sheet, rect.Min, draw.Src)
			keyFrames[i] = sprite
			ghostFrames[i] = sprite
		}
	})
	if len(ghostFrames) == 0 {
		return nil
	}
	frame %= len(ghostFrames)
	if frame < 0 {
		frame += len(ghostFrames)
	}
	return ghostFrames[frame]
}

func renderSpriteRow(sprite image.Image, background color.Color, width, row int) string {
	bounds := sprite.Bounds()
	pixelHeight := width
	var line strings.Builder
	for x := range width {
		pixelX := bounds.Min.X + x*bounds.Dx()/width
		topY := min(row*2, pixelHeight-1)
		bottomY := min(row*2+1, pixelHeight-1)
		top := sprite.At(pixelX, bounds.Min.Y+topY*bounds.Dy()/pixelHeight)
		bottom := sprite.At(pixelX, bounds.Min.Y+bottomY*bounds.Dy()/pixelHeight)
		_, _, _, topAlpha := top.RGBA()
		_, _, _, bottomAlpha := bottom.RGBA()
		if topAlpha == 0 && bottomAlpha == 0 {
			line.WriteByte(' ')
			continue
		}

		style := lipgloss.NewStyle().
			Foreground(blendPixel(top, background)).
			Background(blendPixel(bottom, background))
		line.WriteString(style.Render("▀"))
	}
	return line.String()
}

func blendPixel(pixel, background color.Color) color.Color {
	pr, pg, pb, pa := pixel.RGBA()
	br, bg, bb, _ := background.RGBA()
	clear := uint64(0xffff - pa)

	return color.RGBA{
		R: uint8((uint64(pr) + uint64(br)*clear/0xffff) >> 8),
		G: uint8((uint64(pg) + uint64(bg)*clear/0xffff) >> 8),
		B: uint8((uint64(pb) + uint64(bb)*clear/0xffff) >> 8),
		A: 0xff,
	}
}
