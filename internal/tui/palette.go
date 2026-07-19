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

// palettePerm maps a palette action id to the permission key it needs. Ids absent
// from the map (new/sessions/tasks/refresh/quit) are always available. "new" and
// "tasks" are chat-scoped, not gated; "agents" opens the agents screen so it needs
// agents.view.
var palettePerm = map[string]string{
	"agents":    "agents.view",
	"providers": "providers.view",
	"kb":        "knowledge_bases.view",
	"board":     "tasks.view",
	"issues":    "issues.view",
	"schedules": "schedules.view",
	"market":    "marketplace.view",
	"settings":  "settings.view",
	"projects":  "projects.view",
}

func allPaletteItems() []paletteItem {
	return []paletteItem{
		{"new", "New chat", "Pick an agent and start a fresh chat"},
		{"sessions", "Open session", "Browse and open an existing chat"},
		{"agents", "Agents", "Create / edit / delete agents"},
		{"providers", "Providers", "Manage LLM provider accounts"},
		{"kb", "Knowledge bases", "Manage knowledge bases and files"},
		{"board", "Board", "Kanban board of tasks by status"},
		{"issues", "Issues", "Project issue tracker"},
		{"schedules", "Schedules", "Recurring agent jobs"},
		{"market", "Marketplace", "Browse and install packages"},
		{"settings", "Settings", "Profile, orgs, usage, devices, backup"},
		{"projects", "Projects", "Browse org projects"},
		{"tasks", "Tasks", "Tasks for the current chat"},
		{"refresh", "Refresh", "Reload current data"},
		{"quit", "Quit", "Exit NexoraCLI"},
	}
}

// paletteItems is the initial (unfiltered) command list used before the effective
// policy loads — it fails open, matching the tab gating.
func paletteItems() []list.Item {
	all := allPaletteItems()
	out := make([]list.Item, len(all))
	for i := range all {
		out[i] = all[i]
	}
	return out
}

// paletteItems (method) filters the command list by the caller's permissions.
func (m *model) paletteItems() []list.Item {
	out := make([]list.Item, 0, 14)
	for _, it := range allPaletteItems() {
		if m.can(palettePerm[it.id]) {
			out = append(out, it)
		}
	}
	return out
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
	// Defense in depth: never act on a screen the caller may not see.
	if !m.can(palettePerm[id]) {
		return m, nil
	}
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
