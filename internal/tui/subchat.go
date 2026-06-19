package tui

import (
	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
	"gitlab.com/parendum/nexora/nexora-cli/internal/api"
)

// subChatNavItem is one entry in the sub-chat navigator (ctrl+g): either a spawned
// sub-agent's chat to open, or a "back to parent" entry.
type subChatNavItem struct {
	id, title string
	isBack    bool
}

func (i subChatNavItem) Title() string {
	if i.isBack {
		return "← back to " + i.title
	}
	return "🤖 " + i.title
}
func (i subChatNavItem) Description() string {
	if i.isBack {
		return "return to the parent chat"
	}
	return "open this sub-chat — read it and talk in it"
}
func (i subChatNavItem) FilterValue() string { return i.title }

// subChatEntry is one navigable row: a sub-chat or a back-to-parent action, plus status.
type subChatEntry struct {
	nav    subChatNavItem
	status string
}

// subChatEntries builds the current chat's sub-chat list (back-to-parent first if we're
// inside a sub-chat, then each spawned sub-agent's chat). Shared by the panel + ctrl+g.
func (m *model) subChatEntries() []subChatEntry {
	out := make([]subChatEntry, 0, len(m.subChats)+len(m.subAgents)+1)
	if len(m.chatStack) > 0 {
		p := m.chatStack[len(m.chatStack)-1]
		out = append(out, subChatEntry{nav: subChatNavItem{id: p.ID, title: orDefault(p.Title, "parent chat"), isBack: true}})
	}
	seen := map[string]bool{}
	// Persisted descendant sub-chats (survive navigation).
	for _, n := range m.subChats {
		if n.ID == "" || seen[n.ID] {
			continue
		}
		seen[n.ID] = true
		title := orDefault(n.Title, orDefault(n.AgentName, "sub-chat"))
		out = append(out, subChatEntry{nav: subChatNavItem{id: n.ID, title: title}, status: n.Status})
	}
	// Live sub-agents from this turn's frames (may not be in the fetched tree yet).
	for _, sa := range m.subAgents {
		if sa.subChatID == "" || seen[sa.subChatID] {
			continue
		}
		seen[sa.subChatID] = true
		title := sa.name
		if sa.title != "" && sa.title != sa.name {
			title = sa.name + " · " + sa.title
		}
		out = append(out, subChatEntry{nav: subChatNavItem{id: sa.subChatID, title: title}, status: sa.status})
	}
	return out
}

// openSubChatNav shows a quick picker of the current chat's sub-chats (ctrl+g) — open one
// to read/talk, or "← back to parent". The Agents tree panel is the richer navigator.
func (m *model) openSubChatNav() tea.Cmd {
	entries := m.subChatEntries()
	if len(entries) == 0 {
		m.status = "no sub-chats yet — they appear when the agent delegates"
		return nil
	}
	items := make([]list.Item, 0, len(entries))
	for _, e := range entries {
		items = append(items, e.nav)
	}
	m.picker.SetItems(items)
	m.picker.Title = "Sub-chats  (enter to open · esc cancel)"
	m.pickerKind = "subchat_nav"
	m.pickerOpen = true
	return nil
}

// navSubChat opens a chat by id. When entering a sub-chat it pushes the current chat onto
// the back-stack; "back" pops it. Mirrors the web's parent↔sub navigation.
func (m *model) navSubChat(it subChatNavItem) tea.Cmd {
	if it.isBack {
		if len(m.chatStack) > 0 {
			m.chatStack = m.chatStack[:len(m.chatStack)-1]
		}
		return m.openChat(&api.Chat{ID: it.id, Title: it.title})
	}
	if m.currentChat != nil && m.currentChat.ID != it.id {
		m.chatStack = append(m.chatStack, m.currentChat)
	}
	return m.openChat(&api.Chat{ID: it.id, Title: it.title})
}
