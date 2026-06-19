package tui

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// msItem is one toggleable row in a multi-select overlay. desc is an optional
// single-line second column (e.g. an MCP tool description).
type msItem struct {
	key, label string
	desc       string
	selected   bool
}

// multiSelectModel is a checkbox list overlay (space toggles, enter confirms).
type multiSelectModel struct {
	kind, title string
	items       []msItem
	cursor      int
	offset      int
}

type msSubmitMsg struct {
	kind string
	keys []string
}
type msCancelMsg struct{}

func newMultiSelect(kind, title string, items []msItem) *multiSelectModel {
	return &multiSelectModel{kind: kind, title: title, items: items}
}

func (ms *multiSelectModel) update(k tea.KeyMsg) tea.Msg {
	switch k.String() {
	case "esc":
		return msCancelMsg{}
	case "enter", "ctrl+s":
		var keys []string
		for _, it := range ms.items {
			if it.selected {
				keys = append(keys, it.key)
			}
		}
		return msSubmitMsg{kind: ms.kind, keys: keys}
	case "up", "k":
		if ms.cursor > 0 {
			ms.cursor--
		}
	case "down", "j":
		if ms.cursor < len(ms.items)-1 {
			ms.cursor++
		}
	case "pgup", "ctrl+u":
		ms.cursor = max(0, ms.cursor-10)
	case "pgdown", "ctrl+d":
		ms.cursor = min(len(ms.items)-1, ms.cursor+10)
	case "home", "g":
		ms.cursor = 0
	case "end", "G":
		ms.cursor = len(ms.items) - 1
	case "a": // toggle all
		all := true
		for _, it := range ms.items {
			if !it.selected {
				all = false
				break
			}
		}
		for i := range ms.items {
			ms.items[i].selected = !all
		}
	case " ", "x":
		if ms.cursor < len(ms.items) {
			ms.items[ms.cursor].selected = !ms.items[ms.cursor].selected
		}
	}
	return nil
}

func (ms *multiSelectModel) view(th Theme, width, height int) string {
	var b strings.Builder
	sel := 0
	for _, it := range ms.items {
		if it.selected {
			sel++
		}
	}
	b.WriteString(th.AgentName.Render(ms.title) + "  " +
		th.Help.Render(itoa(sel)+"/"+itoa(len(ms.items))+" selected") + "\n\n")

	// name column width = longest label, capped; leaves room for the description.
	nameW := 8
	for _, it := range ms.items {
		if l := len(it.label); l > nameW {
			nameW = l
		}
	}
	nameW = min(nameW, 30)
	rowW := max(20, width)
	descW := max(0, rowW-nameW-8) // 8 = "  [x] " + gap

	vis := max(3, height-5)
	if ms.cursor < ms.offset {
		ms.offset = ms.cursor
	}
	if ms.cursor >= ms.offset+vis {
		ms.offset = ms.cursor - vis + 1
	}
	end := min(len(ms.items), ms.offset+vis)
	for i := ms.offset; i < end; i++ {
		it := ms.items[i]
		box := "[ ]"
		if it.selected {
			box = "[x]"
		}
		name := padRight(truncate(it.label, nameW), nameW)
		row := box + " " + name
		if it.desc != "" && descW > 4 {
			row += "  " + truncate(it.desc, descW)
		}
		if i == ms.cursor {
			b.WriteString(lipgloss.NewStyle().Foreground(th.Accent).Bold(true).Render("▸ "+row) + "\n")
		} else {
			prefix := "  "
			if it.selected {
				prefix = "  " // box already shows [x]
			}
			b.WriteString(prefix + row + "\n")
		}
	}
	if end < len(ms.items) {
		b.WriteString(th.Help.Render("  ↓"+itoa(len(ms.items)-end)+" more") + "\n")
	}
	b.WriteString("\n" + th.Help.Render("[space] toggle  [a] all  [↑↓/pgup/pgdn] move  [enter] confirm  [esc] cancel"))
	return b.String()
}

// padRight left-aligns s within width w (spaces on the right).
func padRight(s string, w int) string {
	if len(s) >= w {
		return s
	}
	return s + strings.Repeat(" ", w-len(s))
}
