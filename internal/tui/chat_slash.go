package tui

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
	"gitlab.com/parendum/nexora/nexora-cli/internal/api"
)

// slashCmd is one in-composer command shown in the autocomplete popup. adv marks a
// command that needs the advanced interface mode; perm is a permission key the caller
// must hold (empty = none). Both mirror the tab/panel gating so a command hidden from
// the menu can't be reached via the composer either.
type slashCmd struct {
	name, desc string
	arg        bool
	adv        bool
	perm       string
}

var slashCmds = []slashCmd{
	{name: "/new", desc: "start a fresh chat"},
	{name: "/agent", desc: "attach / switch the agent (picker)"},
	{name: "/model", desc: "set the model for this chat", arg: true},
	{name: "/chain", desc: "set the provider chain (picker)", perm: "providers.view"},
	{name: "/copy", desc: "copy the last reply to clipboard"},
	{name: "/usage", desc: "show this chat's token/tool usage (sidebar)", adv: true},
	{name: "/agents", desc: "show the chat's agent hierarchy tree (sidebar)", perm: "agents.view"},
	{name: "/info", desc: "show chat info (id, counts) in the sidebar"},
	{name: "/yolo", desc: "toggle auto-approve for commands (no prompt)"},
	{name: "/cd", desc: "set the host working dir for commands", arg: true},
	{name: "/pwd", desc: "show the host working dir for commands"},
	{name: "/clearagent", desc: "detach the agent (general chat)"},
	{name: "/help", desc: "show available commands"},
	{name: "/quit", desc: "exit the CLI"},
}

// slashAlias maps command aliases to their canonical entry in slashCmds.
var slashAlias = map[string]string{
	"/stats": "/usage", "/hierarchy": "/agents", "/tree": "/agents", "/?": "/help",
	"/exit": "/quit",
}

// lookupSlash resolves a typed command (or alias) to its slashCmd definition.
func lookupSlash(name string) (slashCmd, bool) {
	if canon, ok := slashAlias[name]; ok {
		name = canon
	}
	for _, c := range slashCmds {
		if c.name == name {
			return c, true
		}
	}
	return slashCmd{}, false
}

// slashAllowed reports whether the caller may run a command given the active interface
// mode and org permissions.
func (m *model) slashAllowed(c slashCmd) bool {
	if c.adv && m.uiMode == "simple" {
		return false
	}
	if c.perm != "" && !m.can(c.perm) {
		return false
	}
	return true
}

// slashMatches returns the commands matching the current input prefix, filtered to the
// ones the caller is allowed to run.
func (m *model) slashMatches(input string) []slashCmd {
	input = strings.TrimSpace(input)
	if !strings.HasPrefix(input, "/") || strings.Contains(input, " ") {
		return nil
	}
	q := strings.ToLower(input)
	var out []slashCmd
	for _, c := range slashCmds {
		if strings.HasPrefix(c.name, q) && m.slashAllowed(c) {
			out = append(out, c)
		}
	}
	return out
}

// runSlash handles in-composer slash commands (mirrors the web's quick-switchers).
func (m *model) runSlash(line string) (tea.Model, tea.Cmd) {
	fields := strings.Fields(line)
	cmd := strings.ToLower(fields[0])
	arg := strings.TrimSpace(strings.TrimPrefix(line, fields[0]))

	if c, ok := lookupSlash(cmd); ok && !m.slashAllowed(c) {
		m.status = cmd + " is not available with your interface mode or permissions"
		return m, nil
	}

	switch cmd {
	case "/help", "/?":
		names := make([]string, 0, len(slashCmds))
		for _, c := range slashCmds {
			if m.slashAllowed(c) {
				names = append(names, c.name)
			}
		}
		m.status = strings.Join(names, " ") + " · ctrl+p pick-msg · ctrl+y copy · pgup/pgdn scroll"
		return m, nil

	case "/quit", "/exit":
		m.cleanup()
		return m, tea.Quit

	case "/copy":
		return m, m.copyLastAssistant()

	case "/usage", "/stats":
		if m.currentChat == nil {
			m.status = "open a chat first"
			return m, nil
		}
		m.sidebarOpen = true
		m.sidebarPanel = panUsage
		m.layout()
		m.rebuildBlocks()
		return m, m.loadChatUsage(m.currentChat.ID)

	case "/info":
		if m.currentChat == nil {
			m.status = "open a chat first"
			return m, nil
		}
		m.sidebarOpen = true
		m.sidebarPanel = panInfo
		m.layout()
		m.rebuildBlocks()
		return m, m.loadHierarchy(m.currentChat.ID)

	case "/agents", "/hierarchy", "/tree":
		if m.currentChat == nil {
			m.status = "open a chat first"
			return m, nil
		}
		m.sidebarOpen = true
		m.sidebarPanel = panTree
		m.treeSel = 0
		m.layout()
		m.rebuildBlocks()
		return m, m.loadHierarchy(m.currentChat.ID)

	case "/new":
		m.currentChat = nil
		m.closeWS()
		m.messages = nil
		m.renderedBlocks = nil
		m.assistantBuf = ""
		m.toolActions = nil
		m.subAgents = nil
		m.localLog = nil
		m.chatStack = nil
		m.renderTranscript()
		m.status = "New chat — type to begin (agentless). /agent to attach one."
		return m, nil

	case "/agent":
		// open the agent picker; selection rebinds activeAgentID for subsequent turns
		items := make([]list.Item, 0, len(m.agents)+1)
		items = append(items, agentPickItem{api.Agent{ID: "", Name: "(none — general chat)"}})
		for _, a := range m.agents {
			items = append(items, agentPickItem{a})
		}
		if len(m.agents) == 0 {
			return m, tea.Batch(m.loadAgents(), func() tea.Msg { m.status = "Loading agents — /agent again"; return nil })
		}
		m.picker.SetItems(items)
		m.picker.Title = "Switch agent"
		m.pickerKind = "agent_select"
		m.pickerOpen = true
		return m, nil

	case "/model":
		if arg == "" {
			m.status = "usage: /model <model-name>  (current: " + orDefault(m.activeModel, "default") + ")"
			return m, nil
		}
		m.activeModel = arg
		m.status = "model → " + arg
		return m, nil

	case "/chain":
		if len(m.chains) == 0 {
			// not loaded yet — fetch, then auto-open the picker when they arrive
			m.wantChainPicker = true
			m.status = "Loading chains…"
			return m, loadChainsCmd(m.client)
		}
		m.openChainPicker()
		return m, nil

	case "/yolo":
		m.localYolo = !m.localYolo
		if m.localYolo {
			m.status = "⚠ YOLO ON — local commands auto-run WITHOUT confirmation"
		} else {
			m.status = "yolo off — you'll confirm each local command"
		}
		m.persistLocal()
		return m, nil

	case "/pwd":
		m.status = "host cwd: " + m.localCwd
		return m, nil

	case "/cd":
		if arg == "" {
			m.status = "usage: /cd <path>  (current: " + m.localCwd + ")"
			return m, nil
		}
		np := arg
		if strings.HasPrefix(np, "~") {
			if home, err := os.UserHomeDir(); err == nil {
				np = home + np[1:]
			}
		}
		if !filepath.IsAbs(np) {
			np = filepath.Join(m.localCwd, np)
		}
		np = filepath.Clean(np)
		info, err := os.Stat(np)
		if err != nil || !info.IsDir() {
			m.status = "not a directory: " + np
			return m, nil
		}
		m.localCwd = np
		m.status = "host cwd → " + m.localCwd
		m.renderTranscript()
		return m, nil

	case "/clearagent":
		m.activeAgentID = ""
		m.status = "agent detached — general chat"
		return m, nil

	default:
		m.status = "unknown command: " + cmd + "  (/help)"
		return m, nil
	}
}

// openChainPicker builds + shows the provider-chain picker from the loaded chains.
func (m *model) openChainPicker() {
	items := make([]list.Item, 0, len(m.chains)+1)
	items = append(items, chainPickItem{api.Chain{ID: "", Name: "(default chain)"}})
	for _, ch := range m.chains {
		if isInternalChain(ch.Name) {
			continue // hide synthetic per-provider solo chains
		}
		items = append(items, chainPickItem{ch})
	}
	m.picker.SetItems(items)
	m.picker.Title = "Switch provider chain"
	m.pickerKind = "chain_select"
	m.pickerOpen = true
}

// isInternalChain hides synthetic chains the backend creates for direct/solo provider use.
func isInternalChain(name string) bool {
	n := strings.TrimSpace(strings.ToLower(name))
	return n == "__solo__" || strings.HasPrefix(n, "__")
}

func loadChainsCmd(c *api.Client) tea.Cmd {
	return func() tea.Msg {
		ch, err := c.ListChains(ctxBg())
		if err != nil {
			return errMsg{err}
		}
		return chainsMsg{ch}
	}
}

type chainsMsg struct{ items []api.Chain }

// picker item wrappers ------------------------------------------------------------

type agentPickItem struct{ a api.Agent }

func (i agentPickItem) Title() string       { return i.a.Name }
func (i agentPickItem) Description() string  { return i.a.AgentType }
func (i agentPickItem) FilterValue() string { return i.a.Name }

type chainPickItem struct{ c api.Chain }

func (i chainPickItem) Title() string { return i.c.Name }
func (i chainPickItem) Description() string {
	if i.c.IsDefault {
		return "default · " + itoa(len(i.c.Steps)) + " steps"
	}
	return itoa(len(i.c.Steps)) + " steps"
}
func (i chainPickItem) FilterValue() string { return i.c.Name }
