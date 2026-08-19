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
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/mattn/go-runewidth"
	"github.com/sahilm/fuzzy"

	"github.com/dsaad68/kaku-tab/internal/action"
	"github.com/dsaad68/kaku-tab/internal/agent"
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
	agent  agent.Record
	last   bool // last child of its group -> └
	// merged marks a row that stands in for its own group header, because that
	// group has exactly one child and the header would only have repeated it.
	merged bool
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
	Query        string          `json:"query"`
	Cursor       int             `json:"cursor"`
	Offset       int             `json:"offset"`
	Preview      bool            `json:"preview"`
	PaneMode     bool            `json:"pane_mode"`
	HideDetached bool            `json:"hide_detached"`
	AgentsOnly   bool            `json:"agents_only"`
	MergeSingle  bool            `json:"merge_single"`
	Collapse     map[string]bool `json:"collapse"`
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

	// HideDetached drops sessions with no terminal tab from the list.
	HideDetached bool

	// MergeSingle folds a group of one into a single row. A session with one
	// window needs no header: there is nothing to group and nothing to fold, and
	// the header only repeats the badge and agent state of the row below it.
	MergeSingle bool

	// AgentsOnly narrows the list to windows holding an agent that wants you —
	// blocked on permission, asking a question, finished, or failed. A window
	// whose agent is merely working is not one of them: the point of the filter
	// is "what is waiting on me", not "where are the agents".
	AgentsOnly bool
}

// layout is every column width in the table, computed once from the rows the
// table actually holds rather than from fixed proportions of the frame.
//
// Sizing to content is the point: a fixed 22%% of the width for a column that
// holds "1", and 34%% for one that holds "zsh", spent seventy cells on padding
// and pushed the badge — the one column this tool exists to show — an eyeful
// away from the name it belongs to.
//
// A width of 0 means the column is not drawn at all. Two of them earn that
// regularly: the pane count when nothing has a second pane, and the flags when
// nothing is flagged. A column whose every cell reads the same carries no
// information, and this table is mostly such rows.
type layout struct {
	agent int // 0, or agentCells when any row has an agent
	label int
	name  int
	path  int
	panes int // 0, or paneCountCells when any window has more than one pane
	flags int // 0, or flagCells when any window carries a flag
	badge int
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
	lay         layout // column widths, shared by every row
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

// Measure reports the size the picker would like: exactly wide enough for its
// widest row and tall enough for all of them, frame and footer included.
//
// The caller clamps this to whatever maximum it is willing to open. It exists
// because tmux cannot resize a popup once it is open — display-popup takes its
// -w/-h at creation — so the geometry has to be decided before the picker is
// ever drawn, and only the picker knows how wide its own table is.
func Measure(ws []model.Window, opt Options) (cols, rows int) {
	m := New(ws, opt)
	// Measure against a width no real terminal reaches, so the proportional
	// caps in relayout never bind and every column reports its natural size.
	m.width, m.height = 10000, 10000
	m.relayout()

	l := m.lay
	content := m.fixedCells(l) + l.label + l.name + l.path
	// scrollbar gutter + frame border
	cols = maxInt(minPopupCols, content+scrollbarCells+2)

	// Count the footer at the width we will actually open at, not at the
	// measuring width: the help bar wraps, and a popup sized against a
	// one-line footer opens with three lines of it eating the list.
	m.width = cols
	// frame top+bottom, prompt, rule, blank, footer.
	rows = len(m.rows) + 5 + len(m.footerLines())
	return cols, rows
}

// minPopupCols is a floor on the fitted width. Below roughly this the help bar
// wraps into more lines than the list has rows, and a popup that is mostly
// footer is no better than one that is mostly blank.
const minPopupCols = 80

// State captures what a relaunch needs to restore.
func (m *Model) State() State {
	return State{
		Query: m.query, Cursor: m.cursor, Offset: m.offset,
		Preview: m.opt.Preview, PaneMode: m.opt.PaneMode,
		HideDetached: m.opt.HideDetached, AgentsOnly: m.opt.AgentsOnly,
		MergeSingle: m.opt.MergeSingle, Collapse: m.collapse,
	}
}

func (m *Model) Result() Result { return m.result }

// build turns resolved windows into tree rows.
func (m *Model) build() {
	m.rows = nil

	// tmux marks every window of a client-less session Detached, so this drops
	// whole sessions rather than punching holes in one — which is the point:
	// what is left is exactly what you can switch between right now.
	windows := m.windows
	if m.opt.HideDetached {
		windows = make([]model.Window, 0, len(m.windows))
		for _, w := range m.windows {
			if w.Status != model.Detached {
				windows = append(windows, w)
			}
		}
	}
	// Applied after HideDetached, not instead of it: a detached session is
	// exactly where an agent is most likely to have finished unnoticed, so this
	// filter must be able to surface one.
	if m.opt.AgentsOnly {
		kept := make([]model.Window, 0, len(windows))
		for _, w := range windows {
			if w.Agent.Attention() {
				kept = append(kept, w)
			}
		}
		windows = kept
	}

	groups := map[string][]model.Window{}
	var order []string
	for _, w := range windows {
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
		hagents := make([]agent.Record, 0, len(ws))
		n := 0
		for _, w := range ws {
			if m.opt.PaneMode {
				n += len(w.Panes_)
			} else {
				n++
			}
			hagents = append(hagents, w.Agent)
			if w.Status > hstat {
				hstat, htab = w.Status, w.TabID
			} else if htab == "" && w.TabID != "" {
				htab = w.TabID
			}
		}
		// A group of one becomes one row: the header would carry the same badge
		// and the same agent state as the single child directly beneath it, and
		// there is nothing to fold. Half the rows in a list of one-window
		// sessions were that duplicate.
		merge := m.opt.Tree && m.opt.MergeSingle && n == 1
		if m.opt.Tree && !merge {
			m.rows = append(m.rows, row{
				kind: kindHeader, group: g, search: g, count: n,
				status: hstat, tabID: htab, agent: agent.Best(hagents),
				win: pickHeaderWindow(ws, m.opt.Sort == SortMRU),
			})
		}
		for i, w := range ws {
			if m.opt.PaneMode {
				for j, p := range w.Panes_ {
					m.rows = append(m.rows, row{
						kind: kindPane, group: g, win: w, pane: p, merged: merge,
						search: strings.Join([]string{w.Session, w.Index, p.Index, p.Cmd, p.Path, p.Agent.Agent}, " "),
						status: w.Status, tabID: w.TabID, agent: p.Agent,
						last: j == len(w.Panes_)-1,
					})
				}
				continue
			}
			m.rows = append(m.rows, row{
				kind: kindWindow, group: g, win: w, merged: merge,
				// The agent name joins the search text so typing "claude"
				// narrows to agent windows; it is never rendered as text.
				search: strings.Join([]string{w.Session, w.Index, w.Name, w.Path, w.Agent.Agent}, " "),
				status: w.Status, tabID: w.TabID, agent: w.Agent,
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
		// Never hide a merged row: it has no header, so nothing could unfold it
		// again. A group collapsed before a reload merged it would otherwise
		// vanish for good.
		if r.kind != kindHeader && !r.merged && m.collapse[r.group] {
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

	m.relayout()
	m.ensureVisible()
}

// relayout sizes every column once for the whole table.
//
// Once for the whole table, never per row: deriving a width from each row's own
// content gives every row a different layout, which is what made this table look
// ragged before. Measured in display cells — ansi.StringWidth for anything that
// may carry styling, runewidth for plain text — because a byte or rune count
// treats escape sequences and nerd-font glyphs as one cell each and silently
// narrows whichever row happens to carry them.
func (m *Model) relayout() {
	var l layout
	for _, r := range m.rows {
		if !r.agent.Empty() {
			l.agent = agentCells
		}
		if r.kind == kindHeader {
			// Headers are truncated rather than padded, so their badge does not
			// set the column width.
			continue
		}
		if w := ansi.StringWidth(m.badge(r.status, r.tabID, false)); w > l.badge {
			l.badge = w
		}
		l.label = maxInt(l.label, runewidth.StringWidth(m.rowLabel(r)))
		l.name = maxInt(l.name, runewidth.StringWidth(m.rowName(r)))
		l.path = maxInt(l.path, runewidth.StringWidth(m.rowPath(r)))
		if r.kind == kindWindow {
			if r.win.Panes > 1 {
				l.panes = paneCountCells
			}
			if rowFlags(r) != "" {
				l.flags = flagCells
			}
		}
	}

	// Cap each flexible column so one outlier — a 90-character path, a session
	// named after a git branch — cannot squeeze the others out. The caps are
	// generous because they only ever bind on outliers; the common case is that
	// none of them apply and the row is exactly as wide as its content.
	avail := maxInt(20, m.rowWidth()-m.fixedCells(l))
	l.label = minInt(l.label, avail*30/100)
	l.name = minInt(l.name, avail*40/100)
	l.path = minInt(l.path, maxInt(8, avail-l.label-l.name))
	m.lay = l
}

// fixedCells is everything in a row that is not one of the flexible columns:
// the cursor, the tree indent, the status glyph, the agent column, the pane
// count or active marker, the flags, the badge, the single space after each of
// them, and the right margin.
//
// This sum is the one piece of arithmetic here that has to be exact. Wrong by
// even a few cells and the row runs past the list width, where truncateANSI
// eats the badge — rightmost, and the point of the tool.
func (m *Model) fixedCells(l layout) int {
	// status glyph and its space; a trailing space after each of label, name
	// and path.
	n := cursorCells + m.indentCells() + rightMargin + l.badge + 2 + 3
	if l.agent > 0 {
		n += l.agent + 1
	}
	if m.opt.PaneMode {
		n += markerCells + 1
	} else {
		if l.panes > 0 {
			n += l.panes
		}
		if l.flags > 0 {
			n += l.flags + 1
		}
	}
	return n
}

// indentCells is the width of the tree connector on a child row.
func (m *Model) indentCells() int {
	if m.opt.Tree {
		return 3 // " ├ "
	}
	return 1
}

// innerW is the drawable width inside the frame border.
func (m *Model) innerW() int { return maxInt(20, m.width-2) }

func (m *Model) helpPairs() [][2]string {
	preview := "hide preview"
	if !m.opt.Preview {
		preview = "show preview"
	}
	detached := "hide detached"
	if m.opt.HideDetached {
		detached = "show detached"
	}
	agents := "waiting agents"
	if m.opt.AgentsOnly {
		agents = "all windows"
	}
	pairs := [][2]string{
		{"enter", "switch"}, {"^/", preview}, {"^t", "new tab"}, {"tab", "fold"},
		{"^p", "panes"}, {"^e", detached}, {"^a", agents}, {"^r", "rename"},
		{"^x", "kill"}, {"^d", "detach"}, {"^u", "clear"},
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

// footerLines is everything below the list: the selected row's agent, spelled
// out, and the help bar. One source of truth so listHeight reserves exactly the
// rows View is about to draw.
func (m *Model) footerLines() []string {
	var out []string
	if !m.renaming && m.status == "" {
		if r, ok := m.current(); ok {
			if words := agentWords(r.agent); words != "" {
				out = append(out, agentCell(r.agent)+" "+cHead.Render(words))
			}
		}
	}
	if m.status != "" {
		return append(out, cFlag.Render(m.status))
	}
	return append(out, m.helpLines()...)
}

func (m *Model) listHeight() int {
	// frame top+bottom (2) + prompt + rule + blank + footer
	h := m.height - 5 - len(m.footerLines())
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
		// Column caps are a proportion of the available width, so a resize can
		// change them even though the rows have not.
		m.relayout()
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
		if r.merged {
			return m, nil // no header to fold into
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

	case "ctrl+e":
		// Hold the cursor on the same row across the toggle. Unhiding inserts
		// whole sessions above it, so without this the selection lands on
		// something unrelated and Enter does the wrong thing.
		keepID, keepHeader := "", false
		if r, ok := m.current(); ok {
			keepID, keepHeader = r.win.ID, r.kind == kindHeader
		}
		m.opt.HideDetached = !m.opt.HideDetached
		m.build()
		m.focusRow(keepID, keepHeader)
		return m, m.previewCmd()

	case "ctrl+a":
		keepID, keepHeader := "", false
		if r, ok := m.current(); ok {
			keepID, keepHeader = r.win.ID, r.kind == kindHeader
		}
		m.opt.AgentsOnly = !m.opt.AgentsOnly
		m.build()
		m.focusRow(keepID, keepHeader)
		// Cleared on the way out too: left standing, the message would still be
		// in the footer after the filter was toggled back off and the full list
		// restored, which reads as a warning about the list you are looking at.
		m.status = ""
		if m.opt.AgentsOnly && len(m.view) == 0 {
			// An empty list after a filter reads as a broken picker. Say why.
			m.status = "no agent is waiting on you"
		}
		return m, m.previewCmd()

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

// focusRow puts the cursor back on a window's row after the rows are rebuilt.
// A no-op when that row is gone, leaving refilter's clamp to decide.
func (m *Model) focusRow(id string, header bool) {
	if id == "" {
		return
	}
	for i, vi := range m.view {
		if r := m.rows[vi]; r.win.ID == id && (r.kind == kindHeader) == header {
			m.cursor = i
			m.ensureVisible()
			return
		}
	}
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

// Fixed column widths. paneCountCells covers "NNp " including its own trailing
// space; markerCells is the active-pane "*".
const (
	paneCountCells = 4
	flagCells      = 2
	markerCells    = 1
)

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

// rowLabel, rowName, rowPath and rowFlags are the single source of truth for
// what each flexible column holds. relayout measures exactly what renderRow
// draws — deriving the two separately is how a column ends up sized for text
// that is not in it.
func (m *Model) rowLabel(r row) string {
	if r.merged {
		// A merged row stands in for its own session header, so it carries the
		// session name the header would have shown.
		return r.group
	}
	if r.kind == kindPane {
		if m.opt.Tree {
			return r.win.Index + "." + r.pane.Index
		}
		return r.win.Session + ":" + r.win.Index + "." + r.pane.Index
	}
	if m.opt.Tree {
		return r.win.Index
	}
	return r.win.Session + ":" + r.win.Index
}

func (m *Model) rowName(r row) string {
	if r.kind == kindPane {
		return strings.TrimSpace(r.pane.Cmd)
	}
	return strings.TrimSpace(r.win.Name)
}

func (m *Model) rowPath(r row) string {
	if r.kind == kindPane {
		return tilde(r.pane.Path)
	}
	return tilde(r.win.Path)
}

func rowFlags(r row) string {
	f := ""
	if r.win.Activity {
		f += "!"
	}
	if r.win.Zoomed {
		f += "z"
	}
	return f
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
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

// agentCells is the width of the agent column: one cell naming the agent, a
// space, one cell naming what it wants. The space is not decoration — flush
// against each other the two glyphs read as a single smudged symbol, which
// defeats the whole point of splitting identity from state.
//
// Reserved on every row of a table that has any agent at all, and dropped
// entirely from one that has none.
const agentCells = 3

// agentCell renders that column. Always exactly agentCells wide.
func agentCell(r agent.Record) string {
	if r.Empty() {
		return strings.Repeat(" ", agentCells)
	}
	id := cIDClaude.Render(glyphClaude)
	if r.Agent == agent.Devin {
		id = cIDDevin.Render(glyphDevin)
	}
	var state string
	switch r.State {
	case agent.Perm:
		state = cAgentPerm.Render(glyphPerm)
	case agent.Ask:
		state = cAgentAsk.Render(glyphAsk)
	case agent.Err:
		state = cAgentErr.Render(glyphErr)
	case agent.Done:
		state = cAgentDone.Render(glyphDone)
	default:
		state = cAgentBusy.Render(glyphBusy)
	}
	return id + " " + state
}

// agentWords spells out an agent record for the footer. The glyphs are compact
// but nothing on screen says what they mean; this is where you find out, on
// whichever row the cursor is on.
func agentWords(r agent.Record) string {
	if r.Empty() {
		return ""
	}
	var what string
	switch r.State {
	case agent.Perm:
		what = "waiting for permission"
	case agent.Ask:
		what = "waiting for an answer"
	case agent.Done:
		what = "finished a turn"
	case agent.Err:
		what = "turn failed"
	default:
		what = "working"
	}
	out := r.Agent + " · " + what
	if r.At > 0 {
		if d := time.Since(time.Unix(r.At, 0)); d >= time.Second {
			out += " · " + d.Round(time.Second).String() + " ago"
		}
	}
	return out
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

// listWidth is the whole list column, scrollbar gutter included. The preview
// is sized from what is left of innerW after it.
func (m *Model) listWidth() int {
	if m.opt.Preview && m.sideBySide() {
		return m.innerW()*55/100 - 2
	}
	return m.innerW()
}

// rowWidth is what a row's own content gets: the list column less the gutter.
func (m *Model) rowWidth() int { return maxInt(20, m.listWidth()-scrollbarCells) }

func (m *Model) renderRow(r row, selected bool) string {
	lw := m.rowWidth()
	// ➤ (U+27A4), an arrowhead rather than the ▸ (U+25B8) triangle a collapsed
	// session folds with. Different shape, not just a different weight, which
	// is what keeps the two readable in the one place they meet — a selected,
	// folded header — along with the two spaces between them.
	//
	// Both variants are exactly cursorCells wide on screen; the selected one
	// just carries colour. Every candidate glyph was checked at 1 cell in tmux
	// first: an ambiguous-width marker would shift every column on the selected
	// row and nowhere else.
	cursor := "   "
	if selected {
		cursor = cCursor.Render("➤") + "  "
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
			cDim.Render(fmt.Sprintf("%d %s", r.count, unit)) + "  " +
			agentCell(r.agent) + " " + m.badge(r.status, r.tabID, true)
		return truncateANSI(line, lw)
	}

	// The badge is reserved first: it is the one column the whole tool exists to
	// show, so it must never be what gets truncated.
	indent := " ├ "
	if r.last {
		indent = " └ "
	}
	if !m.opt.Tree || r.merged {
		// A merged row has no header above it, so a tree connector would point
		// at nothing.
		indent = strings.Repeat(" ", m.indentCells())
	}

	l := m.lay
	badge := m.badge(r.status, r.tabID, false)
	badgeCol := strings.Repeat(" ", maxInt(0, l.badge-ansi.StringWidth(badge))) + badge

	label := pad(m.rowLabel(r), l.label)
	if r.merged {
		// Styled like the header it replaces, so a session still reads as a
		// session rather than as a stray window index.
		label = cGroup.Render(label)
	}
	name := cName.Render(pad(m.rowName(r), l.name))
	path := cDim.Render(pad(padLeft(m.rowPath(r), l.path), l.path))

	var mid string
	if r.kind == kindPane {
		// The active-pane marker gets a column of its own, reserved on every
		// pane row. Rendered flush against the glyph it read as one smudged
		// symbol, and appearing only on the active row it shifted that row's
		// every other column one cell right of its neighbours'.
		marker := " "
		if r.pane.Active {
			marker = cFlag.Render("*")
		}
		mid = marker + " "
	}

	tail := ""
	if l.panes > 0 {
		tail += fmt.Sprintf("%2dp ", r.win.Panes)
	}
	if l.flags > 0 {
		tail += cFlag.Render(pad(rowFlags(r), l.flags)) + " "
	}

	line := cursor + cDim.Render(indent) + glyph(r.status) + " " + mid +
		m.agentCol(r) + label + " " + name + " " + tail + path + " " + badgeCol
	return truncateANSI(line, lw)
}

// agentCol renders the agent column plus its trailing space, or nothing at all
// when no row in the table has an agent.
func (m *Model) agentCol(r row) string {
	if m.lay.agent == 0 {
		return ""
	}
	return agentCell(r.agent) + " "
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
		line := padToWidth(m.renderRow(r, i == m.cursor), m.rowWidth())
		if i == m.cursor {
			line = cSel.Render(line)
		}
		lines = append(lines, line)
	}
	if len(lines) == 0 {
		// An empty list with no query typed is the filter's doing, not the
		// query's — say which, or it reads as "you have no sessions".
		msg := "  no matches"
		if m.opt.HideDetached && strings.TrimSpace(m.query) == "" {
			msg = "  nothing attached — ^e shows detached sessions"
		}
		lines = append(lines, cDim.Render(padToWidth(msg, m.rowWidth())))
	}
	for len(lines) < h {
		lines = append(lines, strings.Repeat(" ", m.rowWidth()))
	}
	lines = withScrollbar(lines, m.rowWidth(), len(m.view), m.offset)
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
	fl := m.footerLines()
	for i := range fl {
		fl[i] = footerPad + truncateANSI(fl[i], w-len(footerPad))
	}
	help := strings.Join(fl, "\n")

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
