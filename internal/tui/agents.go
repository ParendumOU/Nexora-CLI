package tui

import (
	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
	"gitlab.com/parendum/nexora/nexora-cli/internal/api"
)

type agentItem struct{ a api.Agent }

func (i agentItem) Title() string {
	mark := ""
	if !i.a.IsActive {
		mark = " (inactive)"
	}
	return i.a.Name + mark
}
func (i agentItem) Description() string {
	d := i.a.Description
	if d == "" {
		d = i.a.AgentType
	}
	return d
}
func (i agentItem) FilterValue() string { return i.a.Name }

func toAgentItems(agents []api.Agent) []list.Item {
	items := make([]list.Item, 0, len(agents))
	for _, a := range agents {
		items = append(items, agentItem{a})
	}
	return items
}

// selectAgent starts a new chat bound to the highlighted agent.
func (m *model) selectAgent() (tea.Model, tea.Cmd) {
	it, ok := m.agentList.SelectedItem().(agentItem)
	if !ok {
		return m, nil
	}
	m.activeAgentID = it.a.ID
	m.status = "Starting chat with " + it.a.Name + "…"
	return m, m.newChat(it.a.ID, "Chat with "+it.a.Name)
}

// agentsKey handles agent-screen intents. Returns (cmd, handled). When the list is
// filtering, letter keys belong to the filter, so we don't intercept.
func (m *model) agentsKey(k tea.KeyMsg) (tea.Cmd, bool) {
	filtering := m.agentList.FilterState() == list.Filtering
	switch k.String() {
	case "enter":
		if filtering {
			return nil, false
		}
		_, cmd := m.selectAgent()
		return cmd, true
	case "c": // 'c' = chat (enter also works); kept explicit
		if filtering {
			return nil, false
		}
		_, cmd := m.selectAgent()
		return cmd, true
	case "n":
		if filtering {
			return nil, false
		}
		m.openAgentForm(nil)
		return nil, true
	case "e":
		if filtering {
			return nil, false
		}
		if it, ok := m.agentList.SelectedItem().(agentItem); ok {
			m.openAgentForm(&it.a)
		}
		return nil, true
	case "d":
		if filtering {
			return nil, false
		}
		if it, ok := m.agentList.SelectedItem().(agentItem); ok {
			m.openConfirm("agent_delete", it.a.ID, "", "Delete agent \""+it.a.Name+"\"? (soft delete)")
		}
		return nil, true
	case "s": // skills multi-select
		if filtering {
			return nil, false
		}
		m.openAgentMulti("agent_skills", "Select skills", m.skillCat, nil)
		return nil, true
	case "o": // tools multi-select
		if filtering {
			return nil, false
		}
		m.openAgentMulti("agent_tools", "Select tools", m.toolCat, nil)
		return nil, true
	case "M": // mcps multi-select
		if filtering {
			return nil, false
		}
		m.openAgentMulti("agent_mcps", "Select MCP servers", m.mcpCat, nil)
		return nil, true
	case "p": // persona picker (applies soul + system prompt)
		if filtering {
			return nil, false
		}
		return m.openPersonaPicker(), true
	}
	return nil, false
}

// openAgentMulti opens a skills/tools/mcps multi-select for the highlighted agent,
// preselecting the agent's current values.
func (m *model) openAgentMulti(kind, title string, catalog []api.CatalogItem, _ []string) {
	it, ok := m.agentList.SelectedItem().(agentItem)
	if !ok {
		m.status = "select an agent first"
		return
	}
	if len(catalog) == 0 {
		m.status = "catalog still loading — try again"
		return
	}
	var current map[string]bool = map[string]bool{}
	switch kind {
	case "agent_skills":
		for _, s := range it.a.Skills {
			current[s] = true
		}
	case "agent_tools":
		for _, s := range it.a.Tools {
			current[s] = true
		}
	}
	items := make([]msItem, 0, len(catalog))
	for _, ci := range catalog {
		label := ci.Name
		if label == "" {
			label = ci.Key
		}
		items = append(items, msItem{key: ci.Key, label: label + "  (" + ci.Key + ")", selected: current[ci.Key]})
	}
	m.ms = newMultiSelect(kind+":"+it.a.ID, title+" — "+it.a.Name, items)
}

func (m *model) openPersonaPicker() tea.Cmd {
	it, ok := m.agentList.SelectedItem().(agentItem)
	if !ok {
		m.status = "select an agent first"
		return nil
	}
	if len(m.personaCat) == 0 {
		m.status = "personas still loading — try again"
		return m.loadCatalogs()
	}
	items := make([]list.Item, 0, len(m.personaCat)+1)
	items = append(items, personaPickItem{api.PersonaItem{Key: "", Name: "(none)"}})
	for _, p := range m.personaCat {
		items = append(items, personaPickItem{p})
	}
	m.picker.SetItems(items)
	m.picker.Title = "Apply persona → " + it.a.Name
	m.pickerKind = "agent_persona:" + it.a.ID
	m.pickerOpen = true
	return nil
}

type personaPickItem struct{ p api.PersonaItem }

func (i personaPickItem) Title() string       { return i.p.Name }
func (i personaPickItem) Description() string  { return i.p.Key }
func (i personaPickItem) FilterValue() string { return i.p.Name }

func (m *model) openAgentForm(a *api.Agent) {
	kind, title := "agent_new", "New agent"
	name, desc, model := "", "", ""
	if a != nil {
		kind = "agent_edit:" + a.ID
		title = "Edit agent"
		name, desc, model = a.Name, a.Description, a.ModelPref
	}
	m.form = newForm(kind, title, []fieldSpec{
		{key: "name", label: "Name", placeholder: "Agent name", value: name},
		{key: "description", label: "Description", placeholder: "What it does", value: desc},
		{key: "system_prompt", label: "System prompt", placeholder: "(optional)"},
		{key: "model_pref", label: "Model preference", placeholder: "e.g. claude-sonnet-4-6", value: model},
		{key: "temperature", label: "Temperature", placeholder: "0.3"},
	})
}
