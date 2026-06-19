package tui

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"gitlab.com/parendum/nexora/nexora-cli/internal/api"
)

// boardColumns is the fixed status column order (matches backend board grouping).
var boardColumns = []string{"pending", "queued", "in_progress", "paused", "completed", "failed"}

func (m *model) colTasks(col int) []api.Task {
	if m.board == nil || col < 0 || col >= len(boardColumns) {
		return nil
	}
	return m.board[boardColumns[col]]
}

func (m *model) clampBoardCursor() {
	if m.boardCol < 0 {
		m.boardCol = 0
	}
	if m.boardCol >= len(boardColumns) {
		m.boardCol = len(boardColumns) - 1
	}
	n := len(m.colTasks(m.boardCol))
	if m.boardRow >= n {
		m.boardRow = n - 1
	}
	if m.boardRow < 0 {
		m.boardRow = 0
	}
	// keep the cursor inside the visible scroll window
	vis := m.boardVisibleRows()
	if m.boardRow < m.boardOffset {
		m.boardOffset = m.boardRow
	}
	if m.boardRow >= m.boardOffset+vis {
		m.boardOffset = m.boardRow - vis + 1
	}
	if m.boardOffset < 0 {
		m.boardOffset = 0
	}
}

// boardVisibleRows = task rows that fit in a column under the header.
func (m *model) boardVisibleRows() int {
	bodyH := max(3, m.height-4)
	return max(1, bodyH-2) // header line + count footer
}

func (m *model) boardKey(k tea.KeyMsg) (tea.Cmd, bool) {
	switch k.String() {
	case "left", "h":
		m.boardCol--
		m.boardOffset = 0
		m.boardRow = 0
		m.clampBoardCursor()
		return nil, true
	case "right", "l":
		m.boardCol++
		m.boardOffset = 0
		m.boardRow = 0
		m.clampBoardCursor()
		return nil, true
	case "up", "k":
		m.boardRow--
		m.clampBoardCursor()
		return nil, true
	case "down", "j":
		m.boardRow++
		m.clampBoardCursor()
		return nil, true
	case "r":
		return m.loadBoard(), true
	case ">", ".":
		return m.moveTask(+1), true
	case "<", ",":
		return m.moveTask(-1), true
	case "e":
		tasks := m.colTasks(m.boardCol)
		if m.boardRow >= 0 && m.boardRow < len(tasks) {
			t := tasks[m.boardRow]
			m.form = newForm("task_edit:"+t.ID, "Edit task", []fieldSpec{
				{key: "title", label: "Title", value: t.Title},
				{key: "description", label: "Description", value: t.Description},
				{key: "priority", label: "Priority (low/medium/high/critical)", value: t.Priority},
				{key: "status", label: "Status (pending/queued/in_progress/paused/completed/failed)", value: t.Status},
			})
		}
		return nil, true
	}
	return nil, false
}

// moveTask shifts the selected task to the adjacent status column.
func (m *model) moveTask(dir int) tea.Cmd {
	tasks := m.colTasks(m.boardCol)
	if m.boardRow < 0 || m.boardRow >= len(tasks) {
		return nil
	}
	target := m.boardCol + dir
	if target < 0 || target >= len(boardColumns) {
		return nil
	}
	t := tasks[m.boardRow]
	newStatus := boardColumns[target]
	c := m.client
	return func() tea.Msg {
		if err := c.SetTaskStatus(ctxBg(), t.ID, newStatus); err != nil {
			return errMsg{err}
		}
		return chainCmd(okMsg{"Moved to " + newStatus}, m.loadBoard())
	}
}

func (m *model) viewBoard() string {
	if m.board == nil {
		return lipgloss.NewStyle().Padding(1, 2).Render("Loading board… (or no tasks)")
	}
	t := m.theme
	n := len(boardColumns)
	colW := max(14, (m.width-(n-1))/n)
	vis := m.boardVisibleRows()

	var cols []string
	for ci, name := range boardColumns {
		tasks := m.board[name]
		focused := ci == m.boardCol

		head := lipgloss.NewStyle().Foreground(t.statusColor(name)).Bold(true).
			Render(truncate(name, colW-2) + " (" + itoa(len(tasks)) + ")")

		// scroll window: only the focused column scrolls with the cursor; others show top.
		start := 0
		if focused {
			start = m.boardOffset
		}
		end := min(len(tasks), start+vis)

		var rows []string
		rows = append(rows, head)
		if start > 0 {
			rows = append(rows, t.Help.Render("  ↑"+itoa(start)+" more"))
		}
		for ri := start; ri < end; ri++ {
			tk := tasks[ri]
			title := truncate(tk.Title, colW-3)
			style := lipgloss.NewStyle().Width(colW - 1)
			if focused && ri == m.boardRow {
				style = style.Foreground(t.Accent).Bold(true)
				title = "▸ " + title
			} else {
				title = "  " + title
			}
			rows = append(rows, style.Render(title))
		}
		if end < len(tasks) {
			rows = append(rows, t.Help.Render("  ↓"+itoa(len(tasks)-end)+" more"))
		}

		colBox := lipgloss.NewStyle().Width(colW).Render(strings.Join(rows, "\n"))
		cols = append(cols, colBox)
	}
	return lipgloss.JoinHorizontal(lipgloss.Top, cols...)
}

func truncate(s string, n int) string {
	if n <= 1 {
		return s
	}
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n-1]) + "…"
}
