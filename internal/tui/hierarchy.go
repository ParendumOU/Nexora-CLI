package tui

import (
	"sort"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"gitlab.com/parendum/nexora/nexora-cli/internal/api"
)

// treeRow is one flattened, indented row of the chat hierarchy tree.
type treeRow struct {
	node  api.HierarchyNode
	depth int // render indentation depth (root = 0)
}

// treeRows flattens the chat hierarchy into ordered, indented rows (mirrors the web's
// buildTree: a node whose parent is in the set nests under it, otherwise it's a root).
func (m *model) treeRows() []treeRow {
	nodes := m.hierNodes
	if len(nodes) == 0 {
		return nil
	}
	inSet := map[string]bool{}
	for _, n := range nodes {
		inSet[n.ID] = true
	}
	children := map[string][]api.HierarchyNode{}
	var roots []api.HierarchyNode
	for _, n := range nodes {
		if n.ParentChatID != "" && inSet[n.ParentChatID] {
			children[n.ParentChatID] = append(children[n.ParentChatID], n)
		} else {
			roots = append(roots, n)
		}
	}
	// stable order: by depth then title
	less := func(a, b api.HierarchyNode) bool {
		if a.Depth != b.Depth {
			return a.Depth < b.Depth
		}
		return a.Title < b.Title
	}
	sort.SliceStable(roots, func(i, j int) bool { return less(roots[i], roots[j]) })
	for k := range children {
		ch := children[k]
		sort.SliceStable(ch, func(i, j int) bool { return less(ch[i], ch[j]) })
		children[k] = ch
	}
	var rows []treeRow
	var walk func(n api.HierarchyNode, d int)
	walk = func(n api.HierarchyNode, d int) {
		rows = append(rows, treeRow{node: n, depth: d})
		for _, c := range children[n.ID] {
			walk(c, d+1)
		}
	}
	for _, r := range roots {
		walk(r, 0)
	}
	return rows
}

// renderTreePanel draws the interactive chat-hierarchy tree (↑↓ select · enter load).
func (m *model) renderTreePanel(w int) string {
	t := m.theme
	rows := m.treeRows()
	if len(rows) == 0 {
		return t.Help.Render("No hierarchy yet.\nThe tree appears once the chat has sub-agents or a parent.")
	}
	if m.treeSel >= len(rows) {
		m.treeSel = len(rows) - 1
	}
	if m.treeSel < 0 {
		m.treeSel = 0
	}
	var b strings.Builder
	b.WriteString(t.Help.Render("Hierarchy ("+itoa(len(rows))+")  ↑↓ select · enter load") + "\n\n")
	for i, r := range rows {
		n := r.node
		indent := strings.Repeat("  ", r.depth)
		marker := "•"
		switch n.NodeType {
		case "current":
			marker = "◆"
		case "ancestor":
			marker = "↑"
		case "descendant":
			marker = "↳"
		}
		name := orDefault(n.Title, orDefault(n.AgentName, "chat"))
		st := ""
		if n.Status != "" && n.Status != "idle" {
			st = " " + statusIcon(n.Status)
		}
		line := indent + marker + " " + truncate(name, max(8, w-len(indent)-6)) + st
		switch {
		case i == m.treeSel:
			b.WriteString(lipgloss.NewStyle().Foreground(t.Accent).Bold(true).Render("▸ "+line) + "\n")
		case n.NodeType == "current":
			b.WriteString("  " + lipgloss.NewStyle().Foreground(t.Accent2).Bold(true).Render(line) + "\n")
		default:
			b.WriteString("  " + line + "\n")
		}
	}
	return b.String()
}

// openTreeSel loads the chat for the highlighted tree node (enter in the Tree panel).
func (m *model) openTreeSel() tea.Cmd {
	rows := m.treeRows()
	if m.treeSel < 0 || m.treeSel >= len(rows) {
		return nil
	}
	n := rows[m.treeSel].node
	if m.currentChat != nil && n.ID == m.currentChat.ID {
		m.status = "already in this chat"
		return nil
	}
	if m.currentChat != nil {
		m.chatStack = append(m.chatStack, m.currentChat)
	}
	return m.openChat(&api.Chat{ID: n.ID, Title: orDefault(n.Title, n.AgentName)})
}

// renderInfoPanel shows summary stats for the current chat (mirrors /info).
func (m *model) renderInfoPanel(w int) string {
	t := m.theme
	if m.currentChat == nil {
		return t.Help.Render("No chat open.")
	}
	c := m.currentChat
	// counts
	msgs := len(m.messages)
	toolCalls := 0
	for _, mm := range m.messages {
		if mm.Metadata != nil {
			if v, ok := mm.Metadata["tool_call_count"]; ok {
				switch n := v.(type) {
				case float64:
					toolCalls += int(n)
				case int:
					toolCalls += n
				}
			}
		}
	}
	subAgents := len(m.subChats)
	row := func(k, v string) string {
		return lipgloss.NewStyle().Foreground(t.Subtle).Render(padRight(k, 12)) + v + "\n"
	}
	var b strings.Builder
	b.WriteString(t.AgentName.Render("Chat info") + "\n\n")
	b.WriteString(row("ID", c.ID))
	b.WriteString(row("Title", truncate(orDefault(c.Title, "(untitled)"), w-14)))
	if c.AgentName != "" {
		b.WriteString(row("Agent", c.AgentName))
	}
	if c.ParentChatID != "" {
		b.WriteString(row("Parent", c.ParentChatID))
	} else {
		b.WriteString(row("Parent", t.Help.Render("(root chat)")))
	}
	b.WriteString("\n")
	b.WriteString(row("Messages", itoa(msgs)))
	b.WriteString(row("Tool calls", itoa(toolCalls)))
	b.WriteString(row("Sub-chats", itoa(subAgents)))
	b.WriteString(row("Tree nodes", itoa(len(m.hierNodes))))
	if !c.CreatedAt.IsZero() {
		b.WriteString(row("Created", c.CreatedAt.Format("2006-01-02 15:04")))
	}
	return b.String()
}
