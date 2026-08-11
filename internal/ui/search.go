// SPDX-License-Identifier: MIT

package ui

import (
	"fmt"
	"runtime"
	"strings"
	"sync"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/dsaad68/kaku-tab/internal/action"
	"github.com/dsaad68/kaku-tab/internal/model"
	"github.com/dsaad68/kaku-tab/internal/tmux"
)

// Scrollback search.
//
// The shell version re-ran `tmux capture-pane | grep` across every pane on
// EVERY keystroke — 646ms per character with 27 panes. Here the scrollback is
// captured once, concurrently, into memory; each keystroke is then a scan over
// a slice.

type hit struct {
	win  model.Window
	pane model.Pane
	line int
	text string
	low  string // lowercased once, for case-insensitive matching
}

type indexedMsg struct {
	hits []hit
	n    int
}

// SearchModel is the scrollback picker.
type SearchModel struct {
	opt      Options
	windows  []model.Window
	hits     []hit
	view     []int
	cursor   int
	offset   int
	query    string
	width    int
	height   int
	indexing bool
	panes    int
	result   Result
	quitting bool
}

func NewSearch(ws []model.Window, opt Options, initial string) *SearchModel {
	return &SearchModel{
		opt: opt, windows: ws, query: initial,
		width: 120, height: 30, indexing: true,
	}
}

func (m *SearchModel) Result() Result { return m.result }

func (m *SearchModel) Init() tea.Cmd { return m.index(m.opt.Depth) }

// index captures every pane's scrollback once, in parallel.
func (m *SearchModel) index(depth int) tea.Cmd {
	ws := m.windows
	return func() tea.Msg {
		type job struct {
			w model.Window
			p model.Pane
		}
		var jobs []job
		for _, w := range ws {
			for _, p := range w.Panes_ {
				jobs = append(jobs, job{w, p})
			}
		}

		workers := runtime.NumCPU()
		if workers > 8 {
			workers = 8
		}
		if workers < 1 {
			workers = 1
		}

		var mu sync.Mutex
		var all []hit
		ch := make(chan job)
		var wg sync.WaitGroup
		for i := 0; i < workers; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for j := range ch {
					out, err := tmux.CapturePane(j.p.ID, depth)
					if err != nil {
						continue
					}
					var local []hit
					for n, line := range strings.Split(out, "\n") {
						line = strings.TrimRight(line, "\r")
						if strings.TrimSpace(stripANSI(line)) == "" {
							continue
						}
						plain := stripANSI(line)
						local = append(local, hit{
							win: j.w, pane: j.p, line: n + 1,
							text: plain, low: strings.ToLower(plain),
						})
					}
					mu.Lock()
					all = append(all, local...)
					mu.Unlock()
				}
			}()
		}
		for _, j := range jobs {
			ch <- j
		}
		close(ch)
		wg.Wait()
		return indexedMsg{hits: all, n: len(jobs)}
	}
}

func (m *SearchModel) refilter() {
	m.view = nil
	q := strings.ToLower(strings.TrimSpace(m.query))
	if q == "" {
		m.cursor, m.offset = 0, 0
		return
	}
	for i := range m.hits {
		if strings.Contains(m.hits[i].low, q) {
			m.view = append(m.view, i)
			if len(m.view) >= 2000 {
				break
			}
		}
	}
	if m.cursor >= len(m.view) {
		m.cursor = len(m.view) - 1
	}
	if m.cursor < 0 {
		m.cursor = 0
	}
	m.ensureVisible()
}

func (m *SearchModel) listHeight() int {
	// frame top+bottom + prompt + rule + blank + however many help lines
	h := m.height - 5 - len(helpBarLines([][2]string{
		{"enter", "jump"}, {"^t", "new tab"}, {"^u", "clear"}, {"esc", "cancel"},
	}, maxInt(20, m.width-2)-2))
	if h < 3 {
		h = 3
	}
	return h
}

func (m *SearchModel) ensureVisible() {
	h := m.listHeight()
	if m.cursor < m.offset {
		m.offset = m.cursor
	}
	if m.cursor >= m.offset+h {
		m.offset = m.cursor - h + 1
	}
	if m.offset < 0 {
		m.offset = 0
	}
}

func (m *SearchModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.ensureVisible()
		return m, nil

	case indexedMsg:
		m.hits, m.panes, m.indexing = msg.hits, msg.n, false
		m.refilter()
		return m, nil

	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c", "esc":
			m.quitting = true
			return m, tea.Quit
		case "up", "ctrl+k":
			m.cursor--
		case "down", "ctrl+j":
			m.cursor++
		case "pgup":
			m.cursor -= m.listHeight()
		case "pgdown":
			m.cursor += m.listHeight()
		case "enter", "ctrl+t":
			if m.cursor >= 0 && m.cursor < len(m.view) {
				h := m.hits[m.view[m.cursor]]
				mode := m.opt.OpenMode
				if msg.String() == "ctrl+t" {
					mode = action.New
				}
				m.result = Result{Chosen: true, Window: h.win, PaneID: h.pane.ID, Mode: mode}
				m.quitting = true
				return m, tea.Quit
			}
			return m, nil
		case "backspace":
			if n := len(m.query); n > 0 {
				m.query = m.query[:n-1]
				m.refilter()
			}
			return m, nil
		case "ctrl+u":
			m.query = ""
			m.refilter()
			return m, nil
		default:
			if msg.Type == tea.KeyRunes || msg.Type == tea.KeySpace {
				m.query += string(msg.Runes)
				m.refilter()
				return m, nil
			}
			return m, nil
		}
		if m.cursor < 0 {
			m.cursor = 0
		}
		if m.cursor >= len(m.view) {
			m.cursor = len(m.view) - 1
		}
		m.ensureVisible()
		return m, nil
	}
	return m, nil
}

func (m *SearchModel) View() string {
	if m.quitting {
		return ""
	}
	w := maxInt(20, m.width-2)

	promptLeft := cPrompt.Render("  scrollback ❯ ") + cText.Render(m.query) + cPrompt.Render("▏")
	var right string
	switch {
	case m.indexing:
		right = cDim.Render("indexing…")
	case strings.TrimSpace(m.query) == "":
		right = cDim.Render(fmt.Sprintf("%d lines · %d panes", len(m.hits), m.panes))
	default:
		right = cDim.Render(fmt.Sprintf("%d matches", len(m.view)))
	}
	gap := w - lipgloss.Width(promptLeft) - lipgloss.Width(right) - 1
	prompt := promptLeft + strings.Repeat(" ", maxInt(1, gap)) + right + " "

	h := m.listHeight()
	rw := maxInt(20, w-scrollbarCells)
	lines := make([]string, 0, h)
	switch {
	case m.indexing:
		lines = append(lines, cDim.Render("   capturing scrollback from every pane…"))
	case strings.TrimSpace(m.query) == "":
		lines = append(lines, cDim.Render(fmt.Sprintf(
			"   %d lines from %d panes indexed — type to search", len(m.hits), m.panes)))
	case len(m.view) == 0:
		lines = append(lines, cDim.Render("   no matches"))
	default:
		for i := m.offset; i < len(m.view) && i < m.offset+h; i++ {
			hit := m.hits[m.view[i]]
			loc := fmt.Sprintf("%s:%s.%s", hit.win.Session, hit.win.Index, hit.pane.Index)
			prefix := "   "
			if i == m.cursor {
				prefix = cCursor.Render("▌") + "  "
			}
			line := prefix + cGroup.Render(pad(loc, 20)) + " " + m.badge(hit.win) + " " +
				cDim.Render(fmt.Sprintf("%5d ", hit.line)) + highlight(hit.text, m.query)
			line = padToWidth(truncateANSI(line, rw), rw)
			if i == m.cursor {
				line = cSel.Render(line)
			}
			lines = append(lines, line)
		}
	}
	for len(lines) < h {
		lines = append(lines, strings.Repeat(" ", rw))
	}
	// A scrollback grep routinely returns hundreds of hits, so this list
	// overflows far more often than the window list does.
	lines = withScrollbar(lines, rw, len(m.view), m.offset)

	hl := helpBarLines([][2]string{
		{"enter", "jump"}, {"^t", "new tab"}, {"^u", "clear"}, {"esc", "cancel"},
	}, w-2)
	for i := range hl {
		hl[i] = footerPad + hl[i]
	}
	help := strings.Join(hl, "\n")

	content := strings.Join([]string{prompt, rule(w), strings.Join(lines, "\n"), "", help}, "\n")
	return frame("scrollback search", content, w)
}

func (m *SearchModel) badge(w model.Window) string {
	switch w.Status {
	case model.Visible:
		return cVisible.Render("⟦kaku " + w.TabID + "⟧")
	case model.AttachedHidden:
		return cHidden.Render("⟦hidden⟧")
	default:
		return cDetached.Render("⟦new tab⟧")
	}
}

// highlight marks the matched substring so the hit is findable in a long line.
func highlight(text, query string) string {
	q := strings.TrimSpace(query)
	if q == "" {
		return text
	}
	i := strings.Index(strings.ToLower(text), strings.ToLower(q))
	if i < 0 {
		return text
	}
	hl := lipgloss.NewStyle().Foreground(lipgloss.Color("214")).Bold(true)
	return text[:i] + hl.Render(text[i:i+len(q)]) + text[i+len(q):]
}
