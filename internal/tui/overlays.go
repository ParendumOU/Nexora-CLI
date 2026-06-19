package tui

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"gitlab.com/parendum/nexora/nexora-cli/internal/api"
	"gitlab.com/parendum/nexora/nexora-cli/internal/localexec"
)

// parseContactInput turns "Label=Value, Label2=Value2" into contact rows.
func parseContactInput(s string) []api.ContactRow {
	var out []api.ContactRow
	s = strings.ReplaceAll(s, "\n", ",")
	for _, part := range strings.Split(s, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		k, v, ok := strings.Cut(part, "=")
		k = strings.TrimSpace(k)
		if !ok || k == "" {
			continue
		}
		out = append(out, api.ContactRow{Key: k, Value: strings.TrimSpace(v)})
	}
	return out
}

// ── form overlay ────────────────────────────────────────────────────────────────

func (m *model) handleFormKey(k tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg := m.form.update(k).(type) {
	case formCancelMsg:
		m.form = nil
		return m, nil
	case formSubmitMsg:
		return m.onFormSubmit(msg)
	}
	return m, nil
}

func (m *model) onFormSubmit(msg formSubmitMsg) (tea.Model, tea.Cmd) {
	v := msg.values
	kind, arg, _ := strings.Cut(msg.kind, ":")
	m.form = nil
	c := m.client

	switch kind {
	case "agent_new":
		in := agentInputFrom(v)
		return m, func() tea.Msg {
			if _, err := c.CreateAgent(context.Background(), in); err != nil {
				return errMsg{err}
			}
			return chainCmd(okMsg{"Agent created"}, m.loadAgents())
		}
	case "agent_edit":
		in := agentInputFrom(v)
		return m, func() tea.Msg {
			if _, err := c.UpdateAgent(context.Background(), arg, in); err != nil {
				return errMsg{err}
			}
			return chainCmd(okMsg{"Agent updated"}, m.loadAgents())
		}
	case "provider_new":
		in := api.ProviderInput{Name: v["name"], ProviderType: arg, BaseURL: v["base_url"], ModelName: v["model"]}
		if v["api_key"] != "" {
			in.Credentials = map[string]any{"api_key": v["api_key"]}
		}
		return m, func() tea.Msg {
			if _, err := c.CreateProvider(context.Background(), in); err != nil {
				return errMsg{err}
			}
			return chainCmd(okMsg{"Provider added"}, m.loadProviders())
		}
	case "kb_new":
		in := api.KBInput{Name: v["name"], Description: v["description"]}
		return m, func() tea.Msg {
			if _, err := c.CreateKB(context.Background(), in); err != nil {
				return errMsg{err}
			}
			return chainCmd(okMsg{"Knowledge base created"}, m.loadKBs())
		}
	case "kb_upload":
		path := v["path"]
		return m, func() tea.Msg {
			if _, err := c.UploadKBFile(context.Background(), arg, path); err != nil {
				return errMsg{err}
			}
			return chainCmd(okMsg{"File uploaded (ingesting…)"}, m.loadKBFiles(arg))
		}
	case "kb_url":
		u := v["url"]
		return m, func() tea.Msg {
			if err := c.IngestURL(context.Background(), arg, u); err != nil {
				return errMsg{err}
			}
			return chainCmd(okMsg{"URL ingested"}, m.loadKBFiles(arg))
		}
	case "issue_new":
		in := api.IssueInput{
			Title: v["title"], Description: v["description"],
			Priority: orDefault(v["priority"], "medium"), ProjectID: v["project_id"],
		}
		return m, func() tea.Msg {
			if _, err := c.CreateIssue(context.Background(), in); err != nil {
				return errMsg{err}
			}
			return chainCmd(okMsg{"Issue created"}, m.loadIssues())
		}
	case "schedule_new":
		in := api.ScheduleInput{
			Name: v["name"], Prompt: v["prompt"],
			CronExpr: v["cron_expr"], AgentID: v["agent_id"],
		}
		if in.CronExpr == "" {
			in.IntervalMinutes = atoiSafe(v["interval_minutes"])
		}
		return m, func() tea.Msg {
			if _, err := c.CreateSchedule(context.Background(), in); err != nil {
				return errMsg{err}
			}
			return chainCmd(okMsg{"Schedule created (inactive)"}, m.loadSchedules())
		}
	case "market_import":
		u := v["url"]
		// Route through the risk-acknowledgment gate (same as the registry list path).
		return m, m.importMarketURL(u, "", false)
	case "env_var_add":
		key := strings.TrimSpace(v["key"])
		name := strings.TrimSpace(v["name"])
		if name == "" {
			name = key
		}
		scope := strings.ToLower(strings.TrimSpace(v["scope"]))
		if scope != "org" {
			scope = "user"
		}
		val := v["value"]
		return m, func() tea.Msg {
			if key == "" || val == "" {
				return errMsg{fmt.Errorf("key and value are required")}
			}
			orgID := ""
			if scope == "org" && len(m.orgs) > 0 {
				orgID = m.orgs[0].ID
			}
			if _, err := c.CreateEnvVar(context.Background(), scope, orgID, key, name, val, ""); err != nil {
				return errMsg{err}
			}
			return chainCmd(okMsg{"Saved " + key}, m.loadSettings())
		}
	case "provider_edit":
		in := api.ProviderInput{Name: v["name"], BaseURL: v["base_url"], ModelName: v["model"]}
		if v["api_key"] != "" {
			in.Credentials = map[string]any{"api_key": v["api_key"]}
		}
		return m, func() tea.Msg {
			if err := c.UpdateProvider(context.Background(), arg, in); err != nil {
				return errMsg{err}
			}
			return chainCmd(okMsg{"Provider updated"}, m.loadProviders())
		}
	case "kb_edit":
		in := api.KBInput{Name: v["name"], Description: v["description"]}
		return m, func() tea.Msg {
			if err := c.UpdateKB(context.Background(), arg, in); err != nil {
				return errMsg{err}
			}
			return chainCmd(okMsg{"Knowledge base updated"}, m.loadKBs())
		}
	case "task_edit":
		in := api.TaskInput{Title: v["title"], Description: v["description"], Status: v["status"], Priority: v["priority"]}
		reload := m.loadTasks("")
		if m.currentChat != nil {
			reload = m.loadTasks(m.currentChat.ID)
		}
		return m, func() tea.Msg {
			if err := c.UpdateTask(context.Background(), arg, in); err != nil {
				return errMsg{err}
			}
			return chainCmd(okMsg{"Task updated"}, tea.Batch(reload, m.loadBoard()))
		}
	case "issue_edit":
		in := api.IssueInput{Title: v["title"], Description: v["description"], Priority: v["priority"], Status: v["status"]}
		return m, func() tea.Msg {
			if _, err := c.UpdateIssue(context.Background(), arg, in); err != nil {
				return errMsg{err}
			}
			return chainCmd(okMsg{"Issue updated"}, m.loadIssues())
		}
	case "schedule_edit":
		in := api.ScheduleInput{Name: v["name"], Prompt: v["prompt"], CronExpr: v["cron_expr"], AgentID: v["agent_id"]}
		if in.CronExpr == "" {
			in.IntervalMinutes = atoiSafe(v["interval_minutes"])
		}
		return m, func() tea.Msg {
			if err := c.UpdateSchedule(context.Background(), arg, in); err != nil {
				return errMsg{err}
			}
			return chainCmd(okMsg{"Schedule updated"}, m.loadSchedules())
		}
	case "mkt_key":
		key := v["key"]
		return m, func() tea.Msg {
			if err := c.SetMarketplaceKey(context.Background(), key); err != nil {
				return errMsg{err}
			}
			return chainCmd(okMsg{"Marketplace key saved"}, m.loadSettings())
		}
	case "market_search":
		return m, m.loadMarket(v["q"])
	case "market_tag":
		m.marketTag = v["tag"]
		return m, m.loadMarket(m.marketQuery) // tags filtered server-side by the registry
	case "project_new":
		in := api.ProjectInput{Name: v["name"], Description: v["description"], RepoURL: v["repo_url"], RepoType: v["repo_type"]}
		return m, func() tea.Msg {
			if _, err := c.CreateProject(context.Background(), in); err != nil {
				return errMsg{err}
			}
			return chainCmd(okMsg{"Project created"}, m.loadProjects())
		}
	case "proj_env":
		env := parseEnvVars(v["vars"])
		body := map[string]any{"env_vars": env}
		return m, func() tea.Msg {
			if err := c.UpdateProject(context.Background(), arg, body); err != nil {
				return errMsg{err}
			}
			return chainCmd(okMsg{"Env vars updated (" + itoa(len(env)) + ")"}, m.loadProjectDetail(arg))
		}
	case "profile_edit":
		name, avatar := v["full_name"], v["avatar_emoji"]
		return m, func() tea.Msg {
			if _, err := c.UpdateMe(context.Background(), api.MeUpdate{FullName: &name, AvatarEmoji: &avatar}); err != nil {
				return errMsg{err}
			}
			return chainCmd(okMsg{"Profile updated"}, m.loadMe())
		}
	case "memory_edit":
		notes := v["notes"]
		return m, func() tea.Msg {
			if _, err := c.UpdateMe(context.Background(), api.MeUpdate{Notes: &notes}); err != nil {
				return errMsg{err}
			}
			return chainCmd(okMsg{"AI memory updated"}, m.loadMe())
		}
	case "integration_new":
		name, typ, token := v["name"], orDefault(v["type"], "telegram"), v["bot_token"]
		cfg := map[string]any{}
		if token != "" {
			cfg["bot_token"] = token
		}
		return m, func() tea.Msg {
			if _, err := c.CreateIntegration(context.Background(), name, typ, cfg); err != nil {
				return errMsg{err}
			}
			return chainCmd(okMsg{"Account added"}, m.loadChannels())
		}
	case "integration_edit":
		body := map[string]any{}
		if v["name"] != "" {
			body["name"] = v["name"]
		}
		if v["bot_token"] != "" {
			body["config"] = map[string]any{"bot_token": v["bot_token"]}
		}
		return m, func() tea.Msg {
			if err := c.UpdateIntegration(context.Background(), arg, body); err != nil {
				return errMsg{err}
			}
			return chainCmd(okMsg{"Account updated"}, m.loadChannels())
		}
	case "contact_edit":
		rows := parseContactInput(v["rows"])
		var enc string
		if len(rows) > 0 {
			b, _ := json.Marshal(rows)
			enc = string(b)
		}
		return m, func() tea.Msg {
			if _, err := c.UpdateMe(context.Background(), api.MeUpdate{ContactInfo: &enc}); err != nil {
				return errMsg{err}
			}
			return chainCmd(okMsg{"Contact info updated (" + itoa(len(rows)) + ")"}, m.loadMe())
		}
	}
	return m, nil
}

func atoiSafe(s string) int {
	n, err := strconv.Atoi(strings.TrimSpace(s))
	if err != nil {
		return 0
	}
	return n
}

func agentInputFrom(v map[string]string) api.AgentInput {
	temp := 0.3
	if t, err := strconv.ParseFloat(v["temperature"], 64); err == nil {
		temp = t
	}
	return api.AgentInput{
		Name:         v["name"],
		AgentType:    "custom",
		Description:  v["description"],
		SystemPrompt: v["system_prompt"],
		ModelPref:    v["model_pref"],
		Temperature:  temp,
	}
}

// chainCmd lets a Cmd emit one msg now and schedule a follow-up Cmd. Bubble Tea has no
// "return msg + cmd" from inside a Cmd, so we wrap: return the note, the caller re-dispatches.
// Implemented as a batch via a tiny wrapper message.
type chainedMsg struct {
	now  tea.Msg
	next tea.Cmd
}

func chainCmd(now tea.Msg, next tea.Cmd) tea.Msg { return chainedMsg{now, next} }

// ── picker overlay ──────────────────────────────────────────────────────────────

func (m *model) handlePickerKey(k tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch k.String() {
	case "esc":
		m.pickerOpen = false
		return m, nil
	case "enter":
		m.pickerOpen = false
		return m.onPick()
	}
	var cmd tea.Cmd
	m.picker, cmd = m.picker.Update(k)
	return m, cmd
}

func (m *model) onPick() (tea.Model, tea.Cmd) {
	// prefix-keyed pickers (carry an id after ':')
	if kind, arg, found := strings.Cut(m.pickerKind, ":"); found {
		switch kind {
		case "agent_persona":
			if it, ok := m.picker.SelectedItem().(personaPickItem); ok {
				c := m.client
				in := api.AgentInput{Soul: it.p.Soul, SystemPrompt: it.p.SystemPrompt}
				name := it.p.Name
				return m, func() tea.Msg {
					if _, err := c.UpdateAgent(context.Background(), arg, in); err != nil {
						return errMsg{err}
					}
					return chainCmd(okMsg{"Persona → " + name}, m.loadAgents())
				}
			}
		case "channel_agent":
			// arg = integration id; selected agent → assign + start the bot
			if it, ok := m.picker.SelectedItem().(agentPickItem); ok {
				c := m.client
				intID, agentID := arg, it.a.ID
				return m, func() tea.Msg {
					body := map[string]any{"config": map[string]any{"channel_agent_id": agentID}}
					if err := c.UpdateIntegration(context.Background(), intID, body); err != nil {
						return errMsg{err}
					}
					if err := c.SetIntegrationActive(context.Background(), intID, true); err != nil {
						return errMsg{err}
					}
					return chainCmd(okMsg{"Channel created — bot started"}, m.loadChannels())
				}
			}
		case "proj_mcp_pick":
			if it, ok := m.picker.SelectedItem().(mcpPickItem); ok {
				s := it.s
				m.pendingMcp = &s
				// preselect: existing entry's allowed_tools; an existing entry with no
				// explicit list means "all", so preselect every known tool.
				cur := map[string]bool{}
				if existing := findProjMcp(m.currentProject, s); existing != nil {
					allowed := mcpAllowed(existing)
					if len(allowed) == 0 {
						for _, kt := range s.KnownTools {
							cur[kt.Name] = true
						}
					} else {
						for _, a := range allowed {
							cur[a] = true
						}
					}
				}
				items := make([]msItem, 0, len(s.KnownTools))
				for _, kt := range s.KnownTools {
					items = append(items, msItem{
						key: kt.Name, label: kt.Name, desc: oneLine(kt.Description), selected: cur[kt.Name],
					})
				}
				if len(items) == 0 {
					m.status = s.Name + " exposes no tools"
					return m, nil
				}
				m.ms = newMultiSelect("proj_mcp_tools:"+arg, s.Name+" — select callable tools", items)
			}
		}
		return m, nil
	}
	switch m.pickerKind {
	case "provider_type":
		if it, ok := m.picker.SelectedItem().(ptItem); ok {
			m.openProviderForm(it.pt)
		}
	case "agent_select":
		if it, ok := m.picker.SelectedItem().(agentPickItem); ok {
			m.activeAgentID = it.a.ID
			if it.a.ID == "" {
				m.status = "agent detached — general chat"
			} else {
				m.status = "agent → " + it.a.Name
			}
		}
	case "chain_select":
		if it, ok := m.picker.SelectedItem().(chainPickItem); ok {
			m.activeChainID = it.c.ID
			m.status = "chain → " + it.c.Name
		}
	case "org_select":
		if it, ok := m.picker.SelectedItem().(orgPickItem); ok {
			return m, m.switchOrg(it.o)
		}
	case "channel_int":
		// account picked → now choose an agent for the channel
		if it, ok := m.picker.SelectedItem().(channelItem); ok {
			items := make([]list.Item, 0, len(m.agents)+1)
			items = append(items, agentPickItem{api.Agent{ID: "", Name: "(none — general)"}})
			for _, a := range m.agents {
				items = append(items, agentPickItem{a})
			}
			m.picker.SetItems(items)
			m.picker.Title = "Pick the agent for this channel"
			m.pickerKind = "channel_agent:" + it.i.ID
			m.pickerOpen = true
		}
	case "subchat_nav":
		if it, ok := m.picker.SelectedItem().(subChatNavItem); ok {
			return m, m.navSubChat(it)
		}
	case "device_revoke":
		if it, ok := m.picker.SelectedItem().(devicePickItem); ok {
			c := m.client
			id, name := it.d.ID, it.d.Name
			return m, func() tea.Msg {
				if err := c.RevokeDevice(context.Background(), id); err != nil {
					return errMsg{err}
				}
				return chainCmd(okMsg{"Revoked " + name}, m.loadSettings())
			}
		}
	}
	return m, nil
}

// onMultiSelect applies a confirmed skills/tools/mcps selection to the edited agent.
func (m *model) onMultiSelect(msg msSubmitMsg) (tea.Model, tea.Cmd) {
	m.ms = nil
	kind, id, _ := strings.Cut(msg.kind, ":")
	c := m.client
	keys := msg.keys
	switch kind {
	case "agent_skills":
		return m, func() tea.Msg {
			if _, err := c.UpdateAgent(context.Background(), id, api.AgentInput{Skills: keys}); err != nil {
				return errMsg{err}
			}
			return chainCmd(okMsg{"Skills updated (" + itoa(len(keys)) + ")"}, m.loadAgents())
		}
	case "agent_tools":
		return m, func() tea.Msg {
			if _, err := c.UpdateAgent(context.Background(), id, api.AgentInput{Tools: keys}); err != nil {
				return errMsg{err}
			}
			return chainCmd(okMsg{"Tools updated (" + itoa(len(keys)) + ")"}, m.loadAgents())
		}
	case "agent_mcps":
		mcps := make([]any, 0, len(keys))
		for _, k := range keys {
			mcps = append(mcps, map[string]any{"name": k})
		}
		return m, func() tea.Msg {
			if _, err := c.UpdateAgent(context.Background(), id, api.AgentInput{Mcps: mcps}); err != nil {
				return errMsg{err}
			}
			return chainCmd(okMsg{"MCPs updated (" + itoa(len(keys)) + ")"}, m.loadAgents())
		}
	case "proj_tools":
		body := map[string]any{"tools": keys}
		return m, func() tea.Msg {
			if err := c.UpdateProject(context.Background(), id, body); err != nil {
				return errMsg{err}
			}
			return chainCmd(okMsg{"Project tools updated (" + itoa(len(keys)) + ")"}, m.loadProjectDetail(id))
		}
	case "proj_mcp_tools":
		s := m.pendingMcp
		m.pendingMcp = nil
		if s == nil || m.currentProject == nil {
			return m, nil
		}
		// rebuild mcps: drop any entry for this server, then re-add unless deselected entirely
		mcps := make([]any, 0, len(m.currentProject.Mcps)+1)
		for _, e := range m.currentProject.Mcps {
			if !mcpMatches(e, *s) {
				mcps = append(mcps, e)
			}
		}
		note := "Removed " + s.Name + " from project"
		if len(keys) > 0 {
			mcps = append(mcps, map[string]any{
				"server_id": s.ID, "name": s.Name, "url": s.URL, "allowed_tools": keys,
			})
			note = s.Name + " → " + itoa(len(keys)) + " tools"
		}
		body := map[string]any{"mcps": mcps}
		return m, func() tea.Msg {
			if err := c.UpdateProject(context.Background(), id, body); err != nil {
				return errMsg{err}
			}
			return chainCmd(okMsg{note}, m.loadProjectDetail(id))
		}
	}
	return m, nil
}

// findProjMcp returns the project's stored MCP entry for server s, or nil.
func findProjMcp(p *api.Project, s api.McpServer) map[string]any {
	if p == nil {
		return nil
	}
	for _, e := range p.Mcps {
		if mcpMatches(e, s) {
			if mm, ok := e.(map[string]any); ok {
				return mm
			}
		}
	}
	return nil
}

// mcpMatches reports whether a stored MCP entry refers to server s (by id, then name).
func mcpMatches(entry any, s api.McpServer) bool {
	mm, ok := entry.(map[string]any)
	if !ok {
		return false
	}
	if id, _ := mm["server_id"].(string); id != "" && id == s.ID {
		return true
	}
	if n, _ := mm["name"].(string); n != "" && n == s.Name {
		return true
	}
	return false
}

func (m *model) switchOrg(o api.Org) tea.Cmd {
	c := m.client
	return func() tea.Msg {
		if err := c.SwitchOrg(context.Background(), o.ID); err != nil {
			return errMsg{err}
		}
		return chainCmd(okMsg{"Switched to " + o.Name}, m.loadSettings())
	}
}

// ── confirm overlay ─────────────────────────────────────────────────────────────

func (m *model) openConfirm(kind, id, id2, text string) {
	m.confirmOpen = true
	m.confirmKind = kind
	m.confirmID = id
	m.confirmID2 = id2
	m.confirmText = text
}

func (m *model) handleConfirmKey(k tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch k.String() {
	case "y", "enter":
		m.confirmOpen = false
		return m.runConfirm()
	case "n", "esc":
		m.confirmOpen = false
		if m.confirmKind == "market_risk_ack" {
			m.pendingRiskURL = ""
			m.status = "import cancelled"
			return m, nil
		}
		// A denied local-exec request must still answer the server, or the turn hangs
		// until the request times out.
		if m.confirmKind == "local_exec" && m.pendingLocal != nil {
			id := m.pendingLocal.id
			m.pendingLocal = nil
			m.status = "✗ denied local command"
			return m, m.sendToolResultCmd(id, map[string]any{"error": "command denied by user"})
		}
		return m, nil
	}
	return m, nil
}

func (m *model) runConfirm() (tea.Model, tea.Cmd) {
	c := m.client
	kind, id, id2 := m.confirmKind, m.confirmID, m.confirmID2
	switch kind {
	case "local_exec":
		if m.pendingLocal != nil {
			req := *m.pendingLocal
			m.pendingLocal = nil
			idx := m.pushLocal(localexec.Summary(req.tool, req.args))
			return m, m.runLocalTool(req, idx)
		}
		return m, nil
	case "agent_delete":
		return m, func() tea.Msg {
			if err := c.DeleteAgent(context.Background(), id); err != nil {
				return errMsg{err}
			}
			return chainCmd(okMsg{"Agent deleted"}, m.loadAgents())
		}
	case "provider_delete":
		return m, func() tea.Msg {
			if err := c.DeleteProvider(context.Background(), id); err != nil {
				return errMsg{err}
			}
			return chainCmd(okMsg{"Provider deleted"}, m.loadProviders())
		}
	case "kb_delete":
		return m, func() tea.Msg {
			if err := c.DeleteKB(context.Background(), id); err != nil {
				return errMsg{err}
			}
			return chainCmd(okMsg{"Knowledge base deleted"}, m.loadKBs())
		}
	case "kbfile_delete":
		return m, func() tea.Msg {
			if err := c.DeleteKBFile(context.Background(), id, id2); err != nil {
				return errMsg{err}
			}
			return chainCmd(okMsg{"File deleted"}, m.loadKBFiles(id))
		}
	case "issue_delete":
		return m, func() tea.Msg {
			if err := c.DeleteIssue(context.Background(), id); err != nil {
				return errMsg{err}
			}
			return chainCmd(okMsg{"Issue deleted"}, m.loadIssues())
		}
	case "schedule_delete":
		return m, func() tea.Msg {
			if err := c.DeleteSchedule(context.Background(), id); err != nil {
				return errMsg{err}
			}
			return chainCmd(okMsg{"Schedule deleted"}, m.loadSchedules())
		}
	case "chat_delete":
		return m, func() tea.Msg {
			if err := c.DeleteChat(context.Background(), id); err != nil {
				return errMsg{err}
			}
			return chainCmd(okMsg{"Session deleted"}, m.loadChats())
		}
	case "seed_uninstall":
		kind := id2
		return m, func() tea.Msg {
			if err := c.UninstallSeed(context.Background(), kind, id); err != nil {
				return errMsg{err}
			}
			return chainCmd(okMsg{"Uninstalled"}, m.loadInstalled())
		}
	case "integration_delete":
		return m, func() tea.Msg {
			if err := c.DeleteIntegration(context.Background(), id); err != nil {
				return errMsg{err}
			}
			return chainCmd(okMsg{"Account deleted"}, m.loadChannels())
		}
	case "env_var_delete":
		return m, func() tea.Msg {
			if err := c.DeleteEnvVar(context.Background(), id); err != nil {
				return errMsg{err}
			}
			return chainCmd(okMsg{"Variable deleted"}, m.loadSettings())
		}
	case "market_risk_ack":
		// id = import URL, id2 = display name. Re-run the import with the risk
		// acknowledged, then proceed through the normal post-import flow.
		url, name := id, id2
		if url == "" {
			url = m.pendingRiskURL
		}
		m.pendingRiskURL = ""
		m.status = "Installing " + name + "…"
		return m, m.importMarketURL(url, name, true)
	}
	return m, nil
}

func (m *model) confirmView() string {
	t := m.theme
	return lipgloss.NewStyle().Width(min(60, m.width-6)).Render(
		t.AgentName.Render("Confirm") + "\n\n" + m.confirmText + "\n\n" +
			t.Help.Render("[y] yes   [n] no"))
}
