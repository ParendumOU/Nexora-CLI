package tui

import (
	"context"
	"strings"

	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"gitlab.com/parendum/nexora/nexora-cli/internal/api"
)

// channelItem is one external channel (a telegram/… integration).
type channelItem struct{ i api.Integration }

func (c channelItem) Title() string {
	mark := "○"
	if c.i.IsActive {
		mark = "●"
	}
	return mark + " " + c.i.Name
}
func (c channelItem) Description() string { return c.i.IntegrationType }
func (c channelItem) FilterValue() string { return c.i.Name }

func toChannelItems(items []api.Integration) []list.Item {
	out := make([]list.Item, 0, len(items))
	for _, it := range items {
		out = append(out, channelItem{it})
	}
	return out
}

// ── loaders / msgs ──────────────────────────────────────────────────────────────

func (m *model) loadChannels() tea.Cmd {
	c := m.client
	return func() tea.Msg {
		items, err := c.ListIntegrations(context.Background())
		if err != nil {
			return errMsg{err}
		}
		return channelsMsg{items}
	}
}

func (m *model) loadChannelConvs(intID string) tea.Cmd {
	c := m.client
	return func() tea.Msg {
		items, err := c.ChannelConversations(context.Background(), intID)
		if err != nil {
			return errMsg{err}
		}
		return channelConvsMsg{items}
	}
}

type channelsMsg struct{ items []api.Integration }
type channelConvsMsg struct{ items []api.ChannelConv }

// ── keys ──────────────────────────────────────────────────────────────────────

func (m *model) channelsKey(k tea.KeyMsg) (tea.Cmd, bool) {
	// conversation list (a channel is open)
	if m.currentChannel != nil {
		switch k.String() {
		case "esc":
			m.currentChannel = nil
			return nil, true
		case "r":
			return m.loadChannelConvs(m.currentChannel.ID), true
		case "up", "k":
			if m.channelConvSel > 0 {
				m.channelConvSel--
			}
			return nil, true
		case "down", "j":
			if m.channelConvSel < len(m.channelConvs)-1 {
				m.channelConvSel++
			}
			return nil, true
		case "enter":
			if m.channelConvSel >= 0 && m.channelConvSel < len(m.channelConvs) {
				conv := m.channelConvs[m.channelConvSel]
				m.chatStack = nil
				m.status = "Opening " + orDefault(conv.Title, "conversation") + "…"
				return m.openChat(&api.Chat{ID: conv.ChatID, Title: orDefault(conv.Title, "channel chat")}), true
			}
		}
		return nil, false
	}

	// channel list
	if m.channelList.FilterState() == list.Filtering {
		return nil, false
	}
	switch k.String() {
	case "r":
		return m.loadChannels(), true
	case "n":
		// new channel: pick a configured account (integration) → then an agent.
		if len(m.channels) == 0 {
			m.status = "no accounts yet — add one in Settings → Integrations"
			return nil, true
		}
		items := make([]list.Item, 0, len(m.channels))
		for _, it := range m.channels {
			items = append(items, channelItem{it})
		}
		m.picker.SetItems(items)
		m.picker.Title = "New channel — pick a connected account"
		m.pickerKind = "channel_int"
		m.pickerOpen = true
		return m.loadAgents(), true // ensure agents are loaded for the next step
	case "enter":
		if it, ok := m.channelList.SelectedItem().(channelItem); ok {
			ch := it.i
			m.currentChannel = &ch
			m.channelConvSel = 0
			m.status = "Loading " + ch.Name + " conversations…"
			return m.loadChannelConvs(ch.ID), true
		}
	case "a":
		// toggle active (start/stop bot)
		if it, ok := m.channelList.SelectedItem().(channelItem); ok {
			ch := it.i
			c := m.client
			want := !ch.IsActive
			return func() tea.Msg {
				if err := c.SetIntegrationActive(context.Background(), ch.ID, want); err != nil {
					return errMsg{err}
				}
				verb := "started"
				if !want {
					verb = "stopped"
				}
				return chainCmd(okMsg{ch.Name + " " + verb}, m.loadChannels())
			}, true
		}
	}
	return nil, false
}

// ── view ──────────────────────────────────────────────────────────────────────

func (m *model) viewChannels() string {
	t := m.theme
	if m.currentChannel == nil {
		if len(m.channels) == 0 {
			return lipgloss.NewStyle().Padding(1, 2).Render(
				t.Help.Render("No channels.\nCreate a Telegram channel in the web app (Channels → New), then it appears here.\n[enter] open · [a] start/stop · [r]efresh"))
		}
		return m.channelList.View()
	}

	// conversation list for the open channel
	ch := m.currentChannel
	bodyH := max(4, m.height-4)
	var b strings.Builder
	b.WriteString(t.AgentName.Render("📡 "+ch.Name) + "  " +
		t.Help.Render("("+ch.IntegrationType+")  [esc] back · ↑↓ select · [enter] open · [r]efresh") + "\n\n")
	if len(m.channelConvs) == 0 {
		b.WriteString(t.Help.Render("No conversations yet.\nActivate the channel ([a] on the list) and message the bot."))
		return lipgloss.NewStyle().Padding(1, 2).MaxHeight(bodyH).Render(b.String())
	}
	if m.channelConvSel >= len(m.channelConvs) {
		m.channelConvSel = len(m.channelConvs) - 1
	}
	for i, conv := range m.channelConvs {
		title := orDefault(conv.Title, "(untitled)")
		preview := oneLine(conv.LastMessage)
		if conv.LastMessageRole != "" && preview != "" {
			preview = conv.LastMessageRole + ": " + preview
		}
		line := truncate(title, 28)
		if preview != "" {
			line += "  " + t.Help.Render(truncate(preview, m.width-40))
		}
		if i == m.channelConvSel {
			b.WriteString(lipgloss.NewStyle().Foreground(t.Accent).Bold(true).Render("▸ "+line) + "\n")
		} else {
			b.WriteString("  " + line + "\n")
		}
	}
	return lipgloss.NewStyle().Padding(1, 2).MaxHeight(bodyH).Render(b.String())
}
