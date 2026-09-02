package tui

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

// listView is a minimal vim-keyed list: j/k/gg/G movement, "/" filter.
// Rendering of each row is delegated so screens control markers.
type listView struct {
	items     []string
	cursor    int
	filtering bool
	filter    string
	lastG     bool
	height    int
}

func newListView(items []string) listView {
	return listView{items: items, height: 20}
}

// visible returns indices of items matching the filter, in order.
func (l *listView) visible() []int {
	var out []int
	f := strings.ToLower(l.filter)
	for i, it := range l.items {
		if f == "" || strings.Contains(strings.ToLower(it), f) {
			out = append(out, i)
		}
	}
	return out
}

// selected returns the item index under the cursor, or -1.
func (l *listView) selected() int {
	vis := l.visible()
	if len(vis) == 0 {
		return -1
	}
	if l.cursor >= len(vis) {
		l.cursor = len(vis) - 1
	}
	return vis[l.cursor]
}

// handleKey processes a key. Returns true when the key was consumed.
func (l *listView) handleKey(msg tea.KeyMsg) bool {
	if l.filtering {
		switch msg.Type {
		case tea.KeyEnter, tea.KeyEsc:
			l.filtering = false
			if msg.Type == tea.KeyEsc {
				l.filter = ""
			}
		case tea.KeyBackspace:
			if len(l.filter) > 0 {
				l.filter = l.filter[:len(l.filter)-1]
			}
		case tea.KeyRunes:
			l.filter += string(msg.Runes)
		}
		l.cursor = 0
		return true
	}
	s := msg.String()
	n := len(l.visible())
	switch s {
	case "j", "down":
		if l.cursor < n-1 {
			l.cursor++
		}
	case "k", "up":
		if l.cursor > 0 {
			l.cursor--
		}
	case "g":
		if l.lastG {
			l.cursor = 0
			l.lastG = false
		} else {
			l.lastG = true
			return true
		}
	case "G":
		if n > 0 {
			l.cursor = n - 1
		}
	case "/":
		l.filtering = true
		l.filter = ""
		l.cursor = 0
	default:
		l.lastG = false
		return false
	}
	l.lastG = s == "g" && l.lastG
	return true
}

// render draws the list using row to format each item.
func (l *listView) render(row func(index int, selected bool) string) string {
	var b strings.Builder
	for pos, idx := range l.visible() {
		b.WriteString(row(idx, pos == l.cursor))
		b.WriteByte('\n')
	}
	if l.filtering || l.filter != "" {
		b.WriteString("/" + l.filter + "\n")
	}
	return b.String()
}
