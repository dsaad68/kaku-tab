// SPDX-License-Identifier: MIT

package ui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/mattn/go-runewidth"
)

// Adaptive so the picker is legible on a light terminal too. Kaku runs at 0.7
// opacity over the desktop here, so contrast matters more than saturation.
var (
	colAccent = lipgloss.AdaptiveColor{Light: "#0B6BCB", Dark: "#7DCFFF"}
	colGreen  = lipgloss.AdaptiveColor{Light: "#1A7F37", Dark: "#9ECE6A"}
	colAmber  = lipgloss.AdaptiveColor{Light: "#9A6700", Dark: "#E0AF68"}
	colMuted  = lipgloss.AdaptiveColor{Light: "#6E7781", Dark: "#565F89"}
	colText   = lipgloss.AdaptiveColor{Light: "#1F2328", Dark: "#C0CAF5"}
	colName   = lipgloss.AdaptiveColor{Light: "#3B5BDB", Dark: "#7AA2F7"}
	colGroup  = lipgloss.AdaptiveColor{Light: "#6F42C1", Dark: "#BB9AF7"}
	colPink   = lipgloss.AdaptiveColor{Light: "#BF3989", Dark: "#F7768E"}
	colRed    = lipgloss.AdaptiveColor{Light: "#B42318", Dark: "#FF7A93"}
	colSelBg  = lipgloss.AdaptiveColor{Light: "#DDE7F5", Dark: "#283457"}
	colBorder = lipgloss.AdaptiveColor{Light: "#B8BFC7", Dark: "#3B4261"}
)

var (
	cVisible  = lipgloss.NewStyle().Foreground(colGreen)
	cHidden   = lipgloss.NewStyle().Foreground(colAmber)
	cDetached = lipgloss.NewStyle().Foreground(colMuted)
	cDim      = lipgloss.NewStyle().Foreground(colMuted)
	cName     = lipgloss.NewStyle().Foreground(colName)
	cGroup    = lipgloss.NewStyle().Foreground(colGroup).Bold(true)
	cSel      = lipgloss.NewStyle().Background(colSelBg)
	cHere     = lipgloss.NewStyle().Foreground(colPink).Bold(true)
	cFlag     = lipgloss.NewStyle().Foreground(colPink)
	cHead     = lipgloss.NewStyle().Foreground(colMuted)
	cPrompt   = lipgloss.NewStyle().Foreground(colAccent).Bold(true)
	cBorder   = lipgloss.NewStyle().Foreground(colBorder)
	cThumb    = lipgloss.NewStyle().Foreground(colAccent)
	cCursor   = lipgloss.NewStyle().Foreground(colAccent)
	cTitle    = lipgloss.NewStyle().Foreground(colAccent).Bold(true)
	cKey      = lipgloss.NewStyle().Foreground(colText).Bold(true)
	cText     = lipgloss.NewStyle().Foreground(colText)
)

// The agent column is two cells: which agent, then what it wants. Splitting
// them means neither has to be inferred from the other's colour — the shape
// says claude or devin, and the shape beside it says blocked, asking, done or
// failed.
//
// Identity takes mauve and cyan, which no state uses, so the two halves never
// read as one gradient. `busy` is deliberately the only muted state: it is the
// one thing here you do not owe a response to, and a column that shouted on
// every working agent would train you to ignore it.
const (
	glyphClaude = "\uf069"     // nf-fa-asterisk, for Claude Code's mark
	glyphDevin  = "\U000f06a9" // nf-md-robot
	glyphPerm   = "\uf0f3"     // bell: blocked asking permission
	glyphAsk    = "\uf059"     // question: blocked on a question
	glyphDone   = "\uf00c"     // check: finished a turn
	glyphErr    = "\uf071"     // warning: the turn failed
	glyphBusy   = "\U000f051f" // timer-sand: working, nothing owed
)

var (
	cIDClaude  = lipgloss.NewStyle().Foreground(colGroup)
	cIDDevin   = lipgloss.NewStyle().Foreground(colAccent)
	cAgentPerm = lipgloss.NewStyle().Foreground(colAmber).Bold(true)
	cAgentAsk  = lipgloss.NewStyle().Foreground(colPink).Bold(true)
	cAgentErr  = lipgloss.NewStyle().Foreground(colRed).Bold(true)
	cAgentDone = lipgloss.NewStyle().Foreground(colGreen).Bold(true)
	cAgentBusy = lipgloss.NewStyle().Foreground(colMuted)
)

// frame draws a rounded box with a title set into the top border.
//
// lipgloss v1.1.0 has no border-label API, and overlaying text onto a
// Border()-rendered top line means splicing into an already-styled string. The
// border runes are exported, so composing the box directly is both simpler and
// exact.
func frame(title, content string, w int) string {
	b := lipgloss.RoundedBorder()

	lead := 2
	tw := lipgloss.Width(title)
	fill := w - lead - tw - 2 // spaces either side of the title
	if fill < 0 {
		fill = 0
	}
	top := cBorder.Render(b.TopLeft+strings.Repeat(b.Top, lead)+" ") +
		cTitle.Render(title) +
		cBorder.Render(" "+strings.Repeat(b.Top, fill)+b.TopRight)

	full := strings.Repeat("─", w)
	var out strings.Builder
	out.WriteString(top + "\n")
	for _, line := range strings.Split(content, "\n") {
		l, r := b.Left, b.Right
		if stripANSI(line) == full {
			// A divider spanning the whole width should join the frame.
			l, r = b.MiddleLeft, b.MiddleRight
		}
		out.WriteString(cBorder.Render(l) + padToWidth(line, w) + cBorder.Render(r) + "\n")
	}
	out.WriteString(cBorder.Render(b.BottomLeft + strings.Repeat(b.Bottom, w) + b.BottomRight))
	return out.String()
}

// smallBox draws a titled box around body lines, sized to w display cells.
//
// Deliberately not frame(): that one titles the whole picker and is measured
// against the popup width. This is an inline panel, and its lines are padded to
// the same width so a short message does not leave a ragged right border.
func smallBox(title string, body []string, w int) []string {
	b := lipgloss.RoundedBorder()
	if w < 8 {
		w = 8
	}
	inner := w - 2

	tw := ansi.StringWidth(title)
	fill := inner - tw - 3 // "─ " before the title, " " after
	if fill < 0 {
		fill = 0
	}
	out := []string{
		cBorder.Render(b.TopLeft+b.Top+" ") + title +
			cBorder.Render(" "+strings.Repeat(b.Top, fill)+b.TopRight),
	}
	for _, line := range body {
		out = append(out, cBorder.Render(b.Left)+padToWidth(" "+line, inner)+cBorder.Render(b.Right))
	}
	return append(out, cBorder.Render(b.BottomLeft+strings.Repeat(b.Bottom, inner)+b.BottomRight))
}

// wrapCells breaks plain text onto at most max lines of w display cells,
// marking the last one with an ellipsis when there was more. Measured with
// runewidth rather than ansi.StringWidth because this text carries no styling —
// it came out of a hook payload.
func wrapCells(text string, w, max int) []string {
	if w < 4 || max < 1 {
		return nil
	}
	var lines []string
	cur, cut := "", false

	for _, word := range strings.Fields(text) {
		// A word wider than the whole line can never fit; cut it rather than
		// let it overflow the box.
		if runewidth.StringWidth(word) > w {
			word = runewidth.Truncate(word, w, "…")
		}
		cand := word
		if cur != "" {
			cand = cur + " " + word
		}
		if runewidth.StringWidth(cand) <= w {
			cur = cand
			continue
		}
		lines = append(lines, cur)
		if len(lines) == max {
			cut = true
			cur = ""
			break
		}
		cur = word
	}
	if cur != "" {
		lines = append(lines, cur)
	}

	if cut && len(lines) > 0 {
		last := lines[len(lines)-1]
		lines[len(lines)-1] = runewidth.Truncate(last, w-1, "") + "…"
	}
	return lines
}

// rule is a horizontal divider.
func rule(w int) string {
	if w < 1 {
		return ""
	}
	return cBorder.Render(strings.Repeat("─", w))
}

// helpBarLines renders "key label" pairs, wrapping onto as many lines as it
// takes. It used to drop whatever did not fit, which hid half the keys in the
// compact popup — a shortcut nobody can see is a shortcut nobody uses.
func helpBarLines(pairs [][2]string, w int) []string {
	sep := cBorder.Render(" · ")
	const sepW = 3

	var lines []string
	var parts []string
	used := 0
	flush := func() {
		if len(parts) > 0 {
			lines = append(lines, strings.Join(parts, sep))
			parts, used = nil, 0
		}
	}
	for _, p := range pairs {
		// Display cells, not bytes: "↵" is 3 bytes and "⇧⇥" is 6, so len()
		// wildly overstates them and wraps the bar early.
		itemW := ansi.StringWidth(p[0]) + 1 + ansi.StringWidth(p[1])
		extra := itemW
		if len(parts) > 0 {
			extra += sepW
		}
		if len(parts) > 0 && used+extra > w-1 {
			flush()
			extra = itemW
		}
		parts = append(parts, cKey.Render(p[0])+" "+cHead.Render(p[1]))
		used += extra
	}
	flush()
	return lines
}
