package tui

import (
	"github.com/charmbracelet/bubbles/table"
	"github.com/charmbracelet/lipgloss"
	"gitlab.com/parendum/nexora/nexora-cli/internal/api"
)

func toTaskRows(tasks []api.Task, th Theme) []table.Row {
	rows := make([]table.Row, 0, len(tasks))
	for _, t := range tasks {
		indent := ""
		if t.ParentID != "" {
			indent = "  └ "
		}
		status := lipgloss.NewStyle().Foreground(th.statusColor(t.Status)).Render(t.Status)
		rows = append(rows, table.Row{status, indent + t.Title, t.AssignedAgent})
	}
	return rows
}

func (m *model) viewTasks() string {
	if m.currentChat == nil {
		return lipgloss.NewStyle().Padding(1, 2).Render("Open a chat first — tasks are scoped to a session.")
	}
	if len(m.tasks) == 0 {
		return lipgloss.NewStyle().Padding(1, 2).Render("No tasks in this session yet.")
	}
	return m.taskTable.View()
}
