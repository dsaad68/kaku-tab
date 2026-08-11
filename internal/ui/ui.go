// SPDX-License-Identifier: MIT

// Package ui is the kaku-tab picker.
//
// Owning the UI (rather than driving fzf) buys three things the shell version
// could not have:
//
//   - Search text is separate from display text. fzf's --nth operates on the
//     *displayed* string, so the shell tree had to repeat the session name,
//     dimmed, on every child — otherwise typing "api" matched only the
//     header and its children vanished with it. Here a header matches on behalf
//     of its children.
//   - Real collapsible groups.
//   - Widths measured in display cells via runewidth, so the nerd-font glyphs
//     in these window names line up instead of drifting.
package ui

import (
	"fmt"
	"sort"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/mattn/go-runewidth"
	"github.com/sahilm/fuzzy"

	"github.com/dsaad68/kaku-tab/internal/action"
	"github.com/dsaad68/kaku-tab/internal/kaku"
	"github.com/dsaad68/kaku-tab/internal/model"
	"github.com/dsaad68/kaku-tab/internal/mru"
	"github.com/dsaad68/kaku-tab/internal/tmux"
)

// Sort modes for the session list, from @kaku-tab-sort.
const (
	// SortTabs lists sessions that have a terminal tab first, then the rest,
	// alphabetically within each. The default.
	SortTabs = "tabs"
	// SortMRU lists whatever you most recently switched to first.
	SortMRU = "mru"
	// SortName is plain alphabetical, ignoring whether a session has a tab.
	SortName = "name"
)

type rowKind int

const (
	kindHeader rowKind = iota
	kindWindow
	kindPane
)

type row struct {
	kind   rowKind
	group  string // session, or session:index in pane mode
	win    model.Window
	pane   model.Pane
	search string // what queries match against; never rendered
	count  int    // header only
	status model.Status
	tabID  string
	last   bool // last child of its group -> └
}

// Result is what the picker hands back to main.
type Result struct {
	Chosen bool
	Window model.Window
	PaneID string
	Mode   action.Mode

	// Relaunch asks the caller to reopen the popup at a different size.
	// tmux has no command to resize a popup once it is open (display-popup is
	// the only popup command and takes -w/-h at creation), so changing the
	// preview has to close and reopen it.
	Relaunch bool
	State    State
}

// State survives a relaunch so toggling the preview does not lose your place.
type State struct {
	Query    string          `json:"query"`
	Cursor   int             `json:"cursor"`
	Offset   int             `json:"offset"`
	Preview  bool            `json:"preview"`
	PaneMode bool            `json:"pane_mode"`
	Collapse map[string]bool `json:"collapse"`
}

type Options struct {
	Suffix   string
	SelfTab  string
	Preview  bool
	Tree     bool
	PaneMode bool
	OpenMode action.Mode
	Reload   func(panes bool) ([]model.Window, error)
	Ctx      action.Ctx
	Depth    int      // scrollback lines per pane for search
	Restore  State    // carried across a preview-toggle relaunch
	Sort     string   // SortTabs (default), SortMRU, or SortName
	MRU      []string // window ids, most recent first; only read for SortMRU
}

type previewMsg struct {
	target  string
	content string
}

type reloadedMsg struct {
	windows []model.Window
	err     error
}

type Model struct {
	opt         Options
	windows     []model.Window
	rows        []row
	view        []int // indices into rows after filter+collapse
	cursor      int
	offset      int
	query       string
	collapse    map[string]bool
	width       int
	height      int
	badgeW      int // widest badge across the table; shared by every row
	preview     map[string]string
	renaming    bool
	rename      string
	renameIsSes bool   // renaming a session (header) rather than a window
	renameSes   string // session being renamed, for the header case
	status      string
	result      Result
	quitting    bool
}

func New(ws []model.Window, opt Options) *Model {
	if opt.Suffix == "" {
		opt.Suffix = model.DefaultSatelliteSuffix
	}
	m := &Model{
		opt:      opt,
		windows:  ws,
		collapse: map[string]bool{},
		preview:  map[string]string{},
		width:    120,
		height:   30,
	}
	if opt.Restore.Collapse != nil {
		m.collapse = opt.Restore.Collapse
	}
	m.query = opt.Restore.Query
	m.build()
	m.cursor, m.offset = opt.Restore.Cursor, opt.Restore.Offset
	if m.cursor >= len(m.view) {
		m.cursor = maxInt(0, len(m.view)-1)
	}
	m.ensureVisible()
	return m
}

// State captures what a relaunch needs to restore.
func (m *Model) State() State {
	return State{
		Query: m.query, Cursor: m.cursor, Offset: m.offset,
		Preview: m.opt.Preview, PaneMode: m.opt.PaneMode, Collapse: m.collapse,
	}
}

func (m *Model) Result() Result { return m.result }

// build turns resolved windows into tree rows.
func (m *Model) build() {
	m.rows = nil
	groups := map[string][]model.Window{}
	var order []string
	for _, w := range m.windows {
		g := w.Session
		if m.opt.PaneMode {
			g = w.Session + ":" + w.Index
		}
		if _, seen := groups[g]; !seen {
			order = append(order, g)
		}
		groups[g] = append(groups[g], w)
	}

	// Sessions that actually have a Kaku tab come first — those are the ones
	// you are switching between. Detached sessions sink to the bottom. Ties
	// stay alphabetical so the order is stable between invocations.
	attached := map[string]bool{}
	for g, ws := range groups {
		for _, w := range ws {
			if w.Status != model.Detached {
				attached[g] = true
				break
			}
		}
	}

	// Under SortMRU the recorded order wins, and everything you have never
	// switched to falls back to the rules above — so a fresh tmux server, with
	// nothing recorded yet, looks exactly like SortTabs.
	best := map[string]int{} // group -> best rank in it; absent = never picked
	if m.opt.Sort == SortMRU {
		ranks := mru.Ranks(m.opt.MRU, m.here())
		for g, ws := range groups {
			sortByRank(ws, ranks)
			for _, w := range ws {
				if r, ok := ranks[w.ID]; ok {
					if cur, seen := best[g]; !seen || r < cur {
						best[g] = r
					}
				}
			}
		}
	}

	sort.SliceStable(order, func(i, j int) bool {
		a, b := order[i], order[j]
		if m.opt.Sort == SortMRU {
			ra, oka := best[a]
			rb, okb := best[b]
			if oka != okb {
				return oka
			}
			if oka && ra != rb {
				return ra < rb
			}
		}
		if m.opt.Sort != SortName && attached[a] != attached[b] {
			return attached[a]
		}
		return a < b
	})

	for _, g := range order {
		ws := groups[g]
		// Header inherits the best status among its children, so a session
		// whose tab is showing something reads as attached at a glance.
		hstat, htab := model.Detached, ""
		n := 0
		for _, w := range ws {
			if m.opt.PaneMode {
				n += len(w.Panes_)
			} else {
				n++
			}
			if w.Status > hstat {
				hstat, htab = w.Status, w.TabID
			} else if htab == "" && w.TabID != "" {
				htab = w.TabID
			}
		}
		if m.opt.Tree {
			m.rows = append(m.rows, row{
				kind: kindHeader, group: g, search: g, count: n,
				status: hstat, tabID: htab,
				win: pickHeaderWindow(ws, m.opt.Sort == SortMRU),
			})
		}
		for i, w := range ws {
			if m.opt.PaneMode {
				for j, p := range w.Panes_ {
					m.rows = append(m.rows, row{
						kind: kindPane, group: g, win: w, pane: p,
						search: strings.Join([]string{w.Session, w.Index, p.Index, p.Cmd, p.Path}, " "),
						status: w.Status, tabID: w.TabID,
						last: j == len(w.Panes_)-1,
					})
				}
				continue
			}
			m.rows = append(m.rows, row{
				kind: kindWindow, group: g, win: w,
				search: strings.Join([]string{w.Session, w.Index, w.Name, w.Path}, " "),
				status: w.Status, tabID: w.TabID,
				last: i == len(ws)-1,
			})
		}
	}
	m.refilter()
}

// here is the tmux window displayed in the tab the picker was invoked from, or
// "" when the picker cannot tell (no Kaku CLI, or a client outside a tab).
func (m *Model) here() string {
	if m.opt.SelfTab == "" {
		return ""
	}
	for _, w := range m.windows {
		if w.Status == model.Visible && w.TabID == m.opt.SelfTab {
			return w.ID
		}
	}
	return ""
}

// sortByRank puts recently switched-to windows first. Windows with no recorded
// rank keep resolve's index order behind them, which is why the comparison
// returns false rather than falling through to an index compare.
func sortByRank(ws []model.Window, ranks map[string]int) {
	sort.SliceStable(ws, func(i, j int) bool {
		ri, oki := ranks[ws[i].ID]
		rj, okj := ranks[ws[j].ID]
		if oki != okj {
			return oki
		}
		if !oki {
			return false
		}
		return ri < rj
	})
}

// pickHeaderWindow chooses what Enter on a header targets: the window that
// session is currently showing, else its first.
//
// Under an MRU order the group is already sorted by where you were, and the
// currently-showing window is the one place Enter must not land — so the first
// row wins instead. Without this the top header of an MRU list would target the
// window you are already in.
func pickHeaderWindow(ws []model.Window, byMRU bool) model.Window {
	if byMRU {
		return ws[0]
	}
	for _, w := range ws {
		if w.Status == model.Visible {
			return w
		}
	}
	return ws[0]
}

// refilter recomputes visible rows from the query and collapse state.
//
// A header matches on behalf of its children: this is what lets a child row
// omit the session name entirely and still be findable by session.
func (m *Model) refilter() {
	m.view = nil
	q := strings.TrimSpace(m.query)

	matched := map[int]bool{}
	groupHit := map[string]bool{}
	if q != "" {
		var targets []string
		var idx []int
		for i, r := range m.rows {
			targets = append(targets, r.search)
			idx = append(idx, i)
		}
		for _, mt := range fuzzy.Find(q, targets) {
			i := idx[mt.Index]
			matched[i] = true
			if m.rows[i].kind == kindHeader {
				groupHit[m.rows[i].group] = true
			}
		}
	}

	for i, r := range m.rows {
		if q != "" {
			if r.kind == kindHeader {
				// Keep a header if it matched, or if any of its children did.
				keep := groupHit[r.group]
				if !keep {
					for j, c := range m.rows {
						if c.group == r.group && c.kind != kindHeader && matched[j] {
							keep = true
							break
						}
					}
				}
				if !keep {
					continue
				}
			} else if !matched[i] && !groupHit[r.group] {
				continue
			}
		}
		if r.kind != kindHeader && m.collapse[r.group] {
			continue
		}
		m.view = append(m.view, i)
	}

	if m.cursor >= len(m.view) {
		m.cursor = len(m.view) - 1
	}
	if m.cursor < 0 {
		m.cursor = 0
	}

	// One badge column for the whole table, sized to the widest badge. Sizing
	// it per row gave every row a different column budget, which is what made
	// the table look ragged: "⟦kaku 7⟧ ← here" is nearly twice "⟦kaku 8⟧".
	m.badgeW = 0
	for _, r := range m.rows {
		if r.kind == kindHeader {
			continue
		}
		if w := ansi.StringWidth(m.badge(r.status, r.tabID, false)); w > m.badgeW {
			m.badgeW = w
		}
	}

	m.ensureVisible()
}

// innerW is the drawable width inside the frame border.
func (m *Model) innerW() int { return maxInt(20, m.width-2) }

func (m *Model) helpPairs() [][2]string {
	preview := "hide preview"
	if !m.opt.Preview {
		preview = "show preview"
	}
	pairs := [][2]string{
		{"enter", "switch"}, {"^/", preview}, {"^t", "new tab"}, {"tab", "fold"},
		{"^p", "panes"}, {"^r", "rename"}, {"^x", "kill"}, {"^d", "detach"},
		{"^u", "clear"},
	}
	if m.opt.Tree {
		pairs[3] = [2]string{"tab", "fold (S-tab all)"}
	}
	return pairs
}

func (m *Model) helpLines() []string {
	w := m.innerW() - 2*len(footerPad)
	if m.renaming {
		return helpBarLines([][2]string{
			{"enter", "apply"}, {"^u", "clear"}, {"^w", "del word"}, {"esc", "cancel"},
		}, w)
	}
	return helpBarLines(m.helpPairs(), w)
}

func (m *Model) listHeight() int {
	// frame top+bottom (2) + prompt + rule + blank + help lines
	h := m.height - 5 - len(m.helpLines())
	if !m.sideBySide() && m.opt.Preview {
		h = h/2 - 1
	}
	if h < 3 {
		h = 3
	}
	return h
}

func (m *Model) sideBySide() bool { return m.width >= 120 }

func (m *Model) ensureVisible() {
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

func (m *Model) current() (row, bool) {
	if m.cursor < 0 || m.cursor >= len(m.view) {
		return row{}, false
	}
	return m.rows[m.view[m.cursor]], true
}

func (m *Model) Init() tea.Cmd { return m.previewCmd() }

func (m *Model) previewCmd() tea.Cmd {
	r, ok := m.current()
	if !ok || !m.opt.Preview {
		return nil
	}
	target := r.win.ID
	if r.kind == kindPane {
		target = r.pane.ID
	}
	if _, cached := m.preview[target]; cached {
		return nil
	}
	return func() tea.Msg {
		out, err := tmux.CapturePane(target, 0)
		if err != nil {
			out = ""
		}
		return previewMsg{target: target, content: out}
	}
}

func (m *Model) reloadCmd() tea.Cmd {
	return func() tea.Msg {
		kaku.Invalidate()
		ws, err := m.opt.Reload(m.opt.PaneMode)
		return reloadedMsg{windows: ws, err: err}
	}
}

func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.ensureVisible()
		return m, nil

	case previewMsg:
		m.preview[msg.target] = msg.content
		return m, nil

	case reloadedMsg:
		if msg.err == nil {
			m.windows = msg.windows
			m.build()
		}
		return m, m.previewCmd()

	case tea.KeyMsg:
		if m.renaming {
			return m.updateRename(msg)
		}
		return m.updateKey(msg)
	}
	return m, nil
}

func (m *Model) updateRename(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// The field starts prefilled with the current name, so clearing it must be
	// cheap — without these you have to backspace the old name away one
	// character at a time.
	switch msg.String() {
	case "ctrl+u":
		m.rename = ""
		return m, nil
	case "ctrl+w":
		m.rename = strings.TrimRight(m.rename, " ")
		if i := strings.LastIndexAny(m.rename, " -_/."); i >= 0 {
			m.rename = m.rename[:i]
		} else {
			m.rename = ""
		}
		return m, nil
	case "ctrl+c":
		m.renaming = false
		m.rename = ""
		return m, nil
	}

	switch msg.Type {
	case tea.KeyEsc:
		m.renaming = false
		m.rename = ""
	case tea.KeyEnter:
		name := strings.TrimSpace(m.rename)
		if name != "" {
			var err error
			if m.renameIsSes {
				err = action.RenameSession(m.renameSes, name, m.opt.Suffix)
			} else if r, ok := m.current(); ok {
				err = tmux.RenameWindow(r.win.Session, r.win.ID, name)
			}
			if err != nil {
				m.status = err.Error()
			}
		}
		m.renaming = false
		m.rename = ""
		return m, m.reloadCmd()
	case tea.KeyBackspace:
		if n := len(m.rename); n > 0 {
			m.rename = m.rename[:n-1]
		}
	case tea.KeyRunes, tea.KeySpace:
		m.rename += string(msg.Runes)
	}
	return m, nil
}

func (m *Model) updateKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c", "esc":
		m.quitting = true
		return m, tea.Quit

	case "up", "ctrl+k":
		m.move(-1)
		return m, m.previewCmd()
	case "down", "ctrl+j":
		m.move(1)
		return m, m.previewCmd()
	case "pgup":
		m.move(-m.listHeight())
		return m, m.previewCmd()
	case "pgdown":
		m.move(m.listHeight())
		return m, m.previewCmd()
	case "home":
		m.cursor = 0
		m.ensureVisible()
		return m, m.previewCmd()
	case "end":
		m.cursor = len(m.view) - 1
		m.ensureVisible()
		return m, m.previewCmd()

	case "enter":
		return m.choose(m.opt.OpenMode)
	case "ctrl+t":
		return m.choose(action.New)

	case "tab":
		r, ok := m.current()
		if !ok {
			return m, nil
		}
		if r.kind == kindHeader {
			m.collapse[r.group] = !m.collapse[r.group]
			m.refilter()
			return m, m.previewCmd()
		}
		// On a child: fold the group it belongs to and land on its header,
		// rather than doing nothing.
		m.collapse[r.group] = true
		m.refilter()
		for i, ri := range m.view {
			if m.rows[ri].kind == kindHeader && m.rows[ri].group == r.group {
				m.cursor = i
				break
			}
		}
		m.ensureVisible()
		return m, m.previewCmd()

	case "shift+tab":
		// Fold everything, or unfold everything if nothing is folded.
		anyOpen := false
		for _, r := range m.rows {
			if r.kind == kindHeader && !m.collapse[r.group] {
				anyOpen = true
				break
			}
		}
		for _, r := range m.rows {
			if r.kind == kindHeader {
				m.collapse[r.group] = anyOpen
			}
		}
		m.refilter()
		return m, m.previewCmd()

	case "ctrl+p":
		m.opt.PaneMode = !m.opt.PaneMode
		return m, m.reloadCmd()

	case "ctrl+x":
		if r, ok := m.current(); ok && r.kind != kindHeader {
			if err := action.Kill(r.win, m.opt.Suffix); err != nil {
				m.status = err.Error()
			}
			return m, m.reloadCmd()
		}
		return m, nil

	case "ctrl+r":
		r, ok := m.current()
		if !ok {
			return m, nil
		}
		m.renaming = true
		if r.kind == kindHeader {
			// A header IS the session, so rename that rather than doing nothing.
			m.renameIsSes, m.renameSes = true, r.win.Session
			m.rename = r.win.Session
		} else {
			m.renameIsSes, m.renameSes = false, ""
			m.rename = strings.TrimSpace(r.win.Name)
		}
		return m, nil

	case "ctrl+d":
		if r, ok := m.current(); ok {
			if err := action.Detach(r.win, m.opt.Suffix); err != nil {
				m.status = err.Error()
			}
			return m, m.reloadCmd()
		}
		return m, nil

	case "ctrl+/", "ctrl+_":
		m.opt.Preview = !m.opt.Preview
		m.result = Result{Relaunch: true, State: m.State()}
		m.quitting = true
		return m, tea.Quit

	case "backspace":
		if n := len(m.query); n > 0 {
			m.query = m.query[:n-1]
			m.refilter()
			return m, m.previewCmd()
		}
		return m, nil

	case "ctrl+u":
		m.query = ""
		m.refilter()
		return m, m.previewCmd()
	}

	if msg.Type == tea.KeyRunes || msg.Type == tea.KeySpace {
		m.query += string(msg.Runes)
		m.refilter()
		return m, m.previewCmd()
	}
	return m, nil
}

func (m *Model) move(d int) {
	m.cursor += d
	if m.cursor < 0 {
		m.cursor = 0
	}
	if m.cursor >= len(m.view) {
		m.cursor = len(m.view) - 1
	}
	m.ensureVisible()
}

func (m *Model) choose(mode action.Mode) (tea.Model, tea.Cmd) {
	r, ok := m.current()
	if !ok {
		return m, nil
	}
	paneID := ""
	if r.kind == kindPane {
		paneID = r.pane.ID
	}
	m.result = Result{Chosen: true, Window: r.win, PaneID: paneID, Mode: mode}
	m.quitting = true
	return m, tea.Quit
}

// ---------------------------------------------------------------- rendering

// cursorCells is the on-screen width of the row cursor gutter, styled or not.
// Never measure the styled cursor: runewidth counts ANSI escape bytes as
// printable, which silently narrows the selected row's columns.
const cursorCells = 3

// rightMargin keeps the badge column off the frame border.
const rightMargin = 1

// footerPad indents the help bar to the prompt's column.
const footerPad = "  "

func pad(s string, w int) string {
	// runewidth measures display cells, so double-width nerd-font glyphs and
	// CJK do not skew the columns the way a byte or rune count would.
	if runewidth.StringWidth(s) > w {
		return runewidth.Truncate(s, w, "…")
	}
	return s + strings.Repeat(" ", w-runewidth.StringWidth(s))
}

func padLeft(s string, w int) string {
	if runewidth.StringWidth(s) <= w {
		return s
	}
	r := []rune(s)
	for len(r) > 0 && runewidth.StringWidth(string(r)) > w-1 {
		r = r[1:]
	}
	return "…" + string(r)
}

func (m *Model) badge(st model.Status, tab string, isHeader bool) string {
	switch st {
	case model.Visible:
		s := cVisible.Render("⟦kaku " + tab + "⟧")
		if !isHeader && tab != "" && tab == m.opt.SelfTab {
			s += " " + cHere.Render("<- here")
		}
		return s
	case model.AttachedHidden:
		return cHidden.Render("⟦hidden " + tab + "⟧")
	default:
		if isHeader {
			return cDetached.Render("⟦ detached ⟧")
		}
		return cDetached.Render("⟦ new tab ⟧")
	}
}

func glyph(st model.Status) string {
	switch st {
	case model.Visible:
		return cVisible.Render("●")
	case model.AttachedHidden:
		return cHidden.Render("◍")
	default:
		return cDetached.Render("○")
	}
}

func (m *Model) listWidth() int {
	if m.opt.Preview && m.sideBySide() {
		return m.innerW()*55/100 - 2
	}
	return m.innerW()
}

func (m *Model) renderRow(r row, selected bool) string {
	lw := m.listWidth()
	// Leading space keeps the cursor off the frame border. Both variants are
	// exactly cursorCells wide on screen; the selected one just carries colour.
	cursor := "   "
	if selected {
		cursor = " " + cPrompt.Render("▸ ")
	}

	if r.kind == kindHeader {
		arrow := "▾"
		if m.collapse[r.group] {
			arrow = "▸"
		}
		unit := "windows"
		if m.opt.PaneMode {
			unit = "panes"
		}
		if r.count == 1 {
			unit = strings.TrimSuffix(unit, "s")
		}
		line := cursor + cGroup.Render(arrow+" "+r.group) + "  " +
			cDim.Render(fmt.Sprintf("%d %s", r.count, unit)) + "  " + m.badge(r.status, r.tabID, true)
		return truncateANSI(line, lw)
	}

	// Column budget. The badge is reserved first: it is the one column the
	// whole tool exists to show, so it must never be what gets truncated.
	indent := " ├ "
	if r.last {
		indent = " └ "
	}
	if !m.opt.Tree {
		indent = " "
	}

	// Budget the columns exactly. `avail` is the space the three flexible
	// columns share, so every fixed cell — cursor, indent, glyph, the single
	// spaces between fields, the "NNp " counter, the flags, the badge column
	// and the right margin — must be subtracted here. Getting this sum wrong
	// by even a few cells pushes the row past lw, and the badge (rightmost,
	// and the whole point of the tool) is what truncateANSI eats.
	//
	// badgeW is the table-wide maximum, never this row's own width: sizing it
	// per row gives every row a different layout.
	badge := m.badge(r.status, r.tabID, false)
	badgeCol := strings.Repeat(" ", maxInt(0, m.badgeW-ansi.StringWidth(badge))) + badge
	fixed := cursorCells + ansi.StringWidth(indent) + m.badgeW + rightMargin
	if r.kind == kindPane {
		fixed += 1 + 1 + 4 // glyph, active marker, four single spaces
	} else {
		fixed += 1 + 4 + 2 + 5 // glyph, "NNp ", flags, five single spaces
	}
	avail := lw - fixed
	if avail < 20 {
		avail = 20
	}
	labelW := avail * 22 / 100
	nameW := avail * 34 / 100
	pathW := avail - labelW - nameW
	if pathW < 8 {
		pathW = 8
	}

	var label, name, extra string
	if r.kind == kindPane {
		// In the tree the session is already on the header, so a pane row shows
		// only its own coordinates.
		label = r.win.Index + "." + r.pane.Index
		if !m.opt.Tree {
			label = r.win.Session + ":" + label
		}
		name = strings.TrimSpace(r.pane.Cmd)
		if r.pane.Active {
			extra = "*"
		}
		return truncateANSI(cursor+cDim.Render(indent)+glyph(r.status)+extra+" "+
			pad(label, labelW)+" "+cName.Render(pad(name, nameW))+" "+
			cDim.Render(pad(padLeft(tilde(r.pane.Path), pathW), pathW))+" "+
			badgeCol, lw)
	}

	label = r.win.Index
	if !m.opt.Tree {
		label = r.win.Session + ":" + r.win.Index
	}
	name = strings.TrimSpace(r.win.Name)
	flags := ""
	if r.win.Activity {
		flags += "!"
	}
	if r.win.Zoomed {
		flags += "z"
	}

	line := cursor + cDim.Render(indent) + glyph(r.status) + " " +
		pad(label, labelW) + " " +
		cName.Render(pad(name, nameW)) + " " +
		fmt.Sprintf("%2dp ", r.win.Panes) + cFlag.Render(pad(flags, 2)) + " " +
		cDim.Render(pad(padLeft(tilde(r.win.Path), pathW), pathW)) + " " +
		badgeCol
	return truncateANSI(line, lw)
}

func tilde(p string) string {
	if h := homeDir(); h != "" && strings.HasPrefix(p, h+"/") {
		return "~" + p[len(h):]
	}
	return p
}

func (m *Model) View() string {
	if m.quitting {
		return ""
	}
	w := m.innerW()

	// ── prompt row ────────────────────────────────────────────────────────
	label, text := "  kaku-tab ❯ ", m.query
	if m.renaming {
		label = "  rename window ❯ "
		if m.renameIsSes {
			label = "  rename session ❯ "
		}
		text = m.rename
	}
	shown := 0
	for _, i := range m.view {
		if m.rows[i].kind != kindHeader {
			shown++
		}
	}
	counter := cDim.Render(fmt.Sprintf("%d/%d", shown, countSelectable(m.rows)))
	promptLeft := cPrompt.Render(label) + cText.Render(text) + cPrompt.Render("▏")
	gap := w - lipgloss.Width(promptLeft) - lipgloss.Width(counter) - 1
	prompt := promptLeft + strings.Repeat(" ", maxInt(1, gap)) + counter + " "

	// ── list ──────────────────────────────────────────────────────────────
	h := m.listHeight()
	lines := make([]string, 0, h)
	for i := m.offset; i < len(m.view) && i < m.offset+h; i++ {
		r := m.rows[m.view[i]]
		line := padToWidth(m.renderRow(r, i == m.cursor), m.listWidth())
		if i == m.cursor {
			line = cSel.Render(line)
		}
		lines = append(lines, line)
	}
	if len(lines) == 0 {
		lines = append(lines, cDim.Render(padToWidth("  no matches", m.listWidth())))
	}
	for len(lines) < h {
		lines = append(lines, strings.Repeat(" ", m.listWidth()))
	}
	list := strings.Join(lines, "\n")

	// ── body: list, optionally beside the preview ─────────────────────────
	body := list
	if m.opt.Preview {
		if m.sideBySide() {
			sep := make([]string, len(lines))
			for i := range sep {
				sep[i] = cBorder.Render(" │ ")
			}
			body = lipgloss.JoinHorizontal(lipgloss.Top,
				list, strings.Join(sep, "\n"), m.renderPreview(h))
		} else {
			body = list + "\n" + rule(w) + "\n" + m.renderPreview(h)
		}
	}

	// ── help ──────────────────────────────────────────────────────────────
	// Indent the footer to the same column as the prompt, and give it a blank
	// line of separation from the list so it reads as a footer rather than
	// another row.
	hl := m.helpLines()
	for i := range hl {
		hl[i] = footerPad + hl[i]
	}
	help := strings.Join(hl, "\n")
	if m.status != "" {
		help = footerPad + cFlag.Render(truncateANSI(m.status, w-len(footerPad)))
	}

	content := strings.Join([]string{prompt, rule(w), body, "", help}, "\n")
	return frame("tmux ⇄ kaku", content, w)
}

func (m *Model) renderPreview(h int) string {
	r, ok := m.current()
	if !ok {
		return ""
	}
	target := r.win.ID
	if r.kind == kindPane {
		target = r.pane.ID
	}
	w := m.innerW() - m.listWidth() - 3
	if !m.sideBySide() {
		w = m.innerW()
		h = m.height - h - 7
	}
	if w < 10 || h < 2 {
		return ""
	}

	title := fmt.Sprintf("%s:%s  [%s]", r.win.Session, r.win.Index, r.status)
	out := []string{
		cTitle.Render(truncateANSI(title, w)),
		cBorder.Render(strings.Repeat("─", maxInt(1, w))),
	}

	content := m.preview[target]
	if content == "" {
		out = append(out, cDim.Render("…"))
	} else {
		body := strings.Split(strings.TrimRight(content, "\n"), "\n")
		if len(body) > h-2 {
			body = body[len(body)-(h-2):]
		}
		for _, l := range body {
			out = append(out, truncateANSI(l, w))
		}
	}
	return strings.Join(out, "\n")
}

func countSelectable(rows []row) int {
	n := 0
	for _, r := range rows {
		if r.kind != kindHeader {
			n++
		}
	}
	return n
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
