package tui

import (
	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
	"gitlab.com/parendum/nexora/nexora-cli/internal/api"
)

type chatItem struct{ c api.Chat }

func (i chatItem) Title() string {
	title := i.c.Title
	if title == "" {
		title = "(untitled)"
	}
	return title
}
func (i chatItem) Description() string {
	if i.c.AgentName != "" {
		return "agent: " + i.c.AgentName + " · " + i.c.CreatedAt.Format("2006-01-02 15:04")
	}
	return i.c.CreatedAt.Format("2006-01-02 15:04")
}
func (i chatItem) FilterValue() string { return i.c.Title }

func toChatItems(chats []api.Chat) []list.Item {
	items := make([]list.Item, 0, len(chats))
	for _, c := range chats {
		// hide sub-chats (those spawned by tasks) from the top-level session list
		if c.ParentChatID != "" {
			continue
		}
		items = append(items, chatItem{c})
	}
	return items
}

// selectSession opens the highlighted chat.
func (m *model) selectSession() (tea.Model, tea.Cmd) {
	it, ok := m.sessionList.SelectedItem().(chatItem)
	if !ok {
		return m, nil
	}
	chat := it.c
	m.chatStack = nil // top-level navigation resets the sub-chat back-stack
	m.status = "Opening " + chat.Title + "…"
	return m, m.openChat(&chat)
}
