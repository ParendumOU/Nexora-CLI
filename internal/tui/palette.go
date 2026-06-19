package tui

import (
	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type paletteItem struct {
	id, label, desc string
}

func (i paletteItem) Title() string       { return i.label }
func (i paletteItem) Description() string  { return i.desc }
func (i paletteItem) FilterValue() string  { return i.label }

func paletteItems() []list.Item {
	return []list.Item{
		paletteItem{"new", "New chat", "Pick an agent and start a fresh chat"},
		paletteItem{"sessions", "Open session", "Browse and open an existing chat"},
		paletteItem{"agents", "Agents", "Create / edit / delete agents"},
		paletteItem{"providers", "Providers", "Manage LLM provider accounts"},
		paletteItem{"kb", "Knowledge bases", "Manage knowledge bases and files"},
		paletteItem{"board", "Board", "Kanban board of tasks by status"},
		paletteItem{"issues", "Issues", "Project issue tracker"},
		paletteItem{"schedules", "Schedules", "Recurring agent jobs"},
		paletteItem{"market", "Marketplace", "Browse and install packages"},
		paletteItem{"settings", "Settings", "Profile, orgs, usage, devices, backup"},
		paletteItem{"projects", "Projects", "Browse org projects"},
		paletteItem{"tasks", "Tasks", "Tasks for the current chat"},
		paletteItem{"refresh", "Refresh", "Reload current data"},
		paletteItem{"quit", "Quit", "Exit NexoraCLI"},
	}
}

func (m *model) updatePalette(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc", "ctrl+k":
		m.paletteOpen = false
		return m, nil
	case "enter":
		m.paletteOpen = false
		it, ok := m.palette.SelectedItem().(paletteItem)
		if !ok {
			return m, nil
		}
		return m.runPaletteAction(it.id)
	}
	var cmd tea.Cmd
	m.palette, cmd = m.palette.Update(msg)
	return m, cmd
}

func (m *model) runPaletteAction(id string) (tea.Model, tea.Cmd) {
	switch id {
	case "new", "agents":
		m.activeTab = tabAgents
		return m, m.loadAgents()
	case "sessions":
		m.activeTab = tabSessions
		return m, m.loadChats()
	case "providers":
		m.activeTab = tabProviders
		return m, tea.Batch(m.loadProviders(), m.loadProviderTypes())
	case "kb":
		m.activeTab = tabKB
		m.currentKB = nil
		return m, m.loadKBs()
	case "board":
		m.activeTab = tabBoard
		return m, m.loadBoard()
	case "issues":
		m.activeTab = tabIssues
		return m, tea.Batch(m.loadIssues(), m.loadProjects())
	case "schedules":
		m.activeTab = tabSchedules
		return m, m.loadSchedules()
	case "market":
		m.activeTab = tabMarket
		return m, m.loadMarket("")
	case "settings":
		m.activeTab = tabSettings
		return m, m.loadSettings()
	case "projects":
		m.activeTab = tabProjects
		return m, m.loadProjects()
	case "tasks":
		m.activeTab = tabTasks
		if m.currentChat != nil {
			return m, m.loadTasks(m.currentChat.ID)
		}
	case "refresh":
		cmds := []tea.Cmd{m.loadAgents(), m.loadChats()}
		if m.currentChat != nil {
			cmds = append(cmds, m.loadTasks(m.currentChat.ID))
		}
		return m, tea.Batch(cmds...)
	case "quit":
		m.cleanup()
		return m, tea.Quit
	}
	return m, nil
}

func (m *model) overlayPalette(background string) string {
	box := m.theme.Border.BorderForeground(m.theme.Accent).Render(m.palette.View())
	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, box)
}
