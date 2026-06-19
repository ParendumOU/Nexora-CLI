package tui

import (
	"encoding/json"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"gitlab.com/parendum/nexora/nexora-cli/internal/api"
)

const backupPoll = 3 * time.Second

// settings subtabs (mirrors the web profile page: compact, editable sections)
const (
	setProfile = iota
	setInterface
	setMemory
	setContact
	setIntegrations
	setDevices
	setTelegram
	setVariables
	setAccount
	setUsage
	setBackup
)

var settingsSubtabs = []string{"Profile", "Interface", "Memory", "Contact", "Integrations", "Devices", "Telegram", "Variables", "Account", "Usage", "Backup"}

// settingsContent renders the active settings subtab (header bar + section body).
func (m *model) settingsContent() string {
	t := m.theme
	if m.settingsSub < 0 || m.settingsSub >= len(settingsSubtabs) {
		m.settingsSub = 0
	}
	// subtab bar
	var tabs []string
	for i, name := range settingsSubtabs {
		label := itoa(i+1) + " " + name
		if i == m.settingsSub {
			tabs = append(tabs, lipgloss.NewStyle().Foreground(t.Accent).Bold(true).Underline(true).Render(label))
		} else {
			tabs = append(tabs, t.Help.Render(label))
		}
	}
	head := strings.Join(tabs, t.Help.Render(" · ")) + "\n" +
		lipgloss.NewStyle().Foreground(t.Subtle).Render(strings.Repeat("─", max(0, m.width-4))) + "\n\n"

	var body string
	switch m.settingsSub {
	case setProfile:
		body = m.viewSetProfile()
	case setInterface:
		body = m.viewSetInterface()
	case setMemory:
		body = m.viewSetMemory()
	case setContact:
		body = m.viewSetContact()
	case setIntegrations:
		body = m.viewSetIntegrations()
	case setDevices:
		body = m.viewSetDevices()
	case setTelegram:
		body = m.viewSetTelegram()
	case setVariables:
		body = m.viewSetVariables()
	case setAccount:
		body = m.viewSetAccount()
	case setUsage:
		body = m.viewSetUsage()
	case setBackup:
		body = m.viewSetBackup()
	}
	return lipgloss.NewStyle().Padding(1, 2).Render(head + body)
}

func (m *model) viewSetProfile() string {
	t := m.theme
	if m.me == nil {
		return t.Help.Render("loading…")
	}
	var b strings.Builder
	b.WriteString(t.AgentName.Render("Profile") + "  " + t.Help.Render("[e] edit") + "\n\n")
	b.WriteString(row(t, "Avatar", orDefault(m.me.AvatarEmoji, "(none)")))
	b.WriteString(row(t, "Name", orDefault(m.me.FullName, "(no name)")))
	b.WriteString(row(t, "Email", m.me.Email))
	if m.me.IsSuperuser {
		b.WriteString(row(t, "Role", "superuser"))
	}
	return b.String()
}

func (m *model) viewSetInterface() string {
	t := m.theme
	var b strings.Builder
	b.WriteString(t.AgentName.Render("Interface") + "  " + t.Help.Render("[space] toggle") + "\n\n")
	mark := func(mode, desc string) string {
		sel := "  "
		name := mode
		if m.uiMode == mode {
			sel = t.AgentName.Render("● ")
			name = t.AgentName.Render(mode)
		}
		return sel + name + "  " + t.Help.Render(desc) + "\n"
	}
	b.WriteString(mark("simple", "compact — only Chat, Sessions, Settings tabs"))
	b.WriteString(mark("advanced", "everything — all tabs + panels"))
	b.WriteString("\n" + t.Help.Render("Other tabs stay reachable via ctrl+k in simple mode."))
	return b.String()
}

func (m *model) viewSetMemory() string {
	t := m.theme
	var b strings.Builder
	b.WriteString(t.AgentName.Render("AI Memory") + "  " + t.Help.Render("[e] edit") + "\n\n")
	b.WriteString(t.Help.Render("Notes the AI knows about you (preferences, role, timezone, style).") + "\n\n")
	notes := ""
	if m.me != nil {
		notes = strings.TrimSpace(m.me.Notes)
	}
	if notes == "" {
		b.WriteString(t.Help.Render("(empty — press [e] to add)"))
	} else {
		b.WriteString(wrap(notes, m.width-6))
	}
	return b.String()
}

func (m *model) viewSetContact() string {
	t := m.theme
	var b strings.Builder
	b.WriteString(t.AgentName.Render("Contact Info") + "  " + t.Help.Render("[e] edit") + "\n\n")
	b.WriteString(t.Help.Render("Links/handles the AI can use (website, github, phone…).") + "\n\n")
	rows := parseContactRows(m.me)
	if len(rows) == 0 {
		b.WriteString(t.Help.Render("(none — press [e] to add)"))
	} else {
		for _, r := range rows {
			b.WriteString(row(t, r.Key, r.Value))
		}
	}
	return b.String()
}

func (m *model) viewSetIntegrations() string {
	t := m.theme
	var b strings.Builder
	b.WriteString(t.AgentName.Render("Integrations") + "  " +
		t.Help.Render("provider accounts (bot tokens) — [n]ew [e]dit [d]el · ↑↓ select") + "\n\n")
	if len(m.channels) == 0 {
		b.WriteString(t.Help.Render("No accounts yet. [n] add a Telegram bot (name + token).\nThen create a channel from it in the Channels tab."))
		return b.String()
	}
	if m.intSel >= len(m.channels) {
		m.intSel = len(m.channels) - 1
	}
	if m.intSel < 0 {
		m.intSel = 0
	}
	for i, it := range m.channels {
		agent := ""
		if id, _ := it.Config["channel_agent_id"].(string); id != "" {
			agent = " · agent set"
		}
		active := "○ off"
		if it.IsActive {
			active = lipgloss.NewStyle().Foreground(t.Good).Render("● on")
		}
		line := it.Name + "  " + t.Help.Render("("+it.IntegrationType+")") + "  " + active + t.Help.Render(agent)
		if i == m.intSel {
			b.WriteString(lipgloss.NewStyle().Foreground(t.Accent).Bold(true).Render("▸ "+line) + "\n")
		} else {
			b.WriteString("  " + line + "\n")
		}
	}
	return b.String()
}

func (m *model) viewSetDevices() string {
	t := m.theme
	var b strings.Builder
	b.WriteString(t.AgentName.Render("Linked devices") + "  " + t.Help.Render("[d] revoke one") + "\n\n")
	if len(m.devices) == 0 {
		b.WriteString(t.Help.Render("none linked") + "\n")
	}
	for _, d := range m.devices {
		seen := d.LastSeenAt
		if len(seen) > 16 {
			seen = seen[:16]
		}
		b.WriteString("  " + d.Name + "  " + t.Help.Render("("+orDefault(d.Platform, "?")+orDefault(" · "+seen, "")+")") + "\n")
	}
	b.WriteString("\n" + t.Help.Render("Pair a new device: run  nexora pair  (code from web Settings → Devices)."))
	return b.String()
}

func (m *model) viewSetVariables() string {
	t := m.theme
	var b strings.Builder
	b.WriteString(t.AgentName.Render("Environment Variables") + "  " +
		t.Help.Render("[n] add · [d] delete · [↑↓] select") + "\n\n")
	b.WriteString(t.Help.Render("API keys/secrets for tools — stored per org (shared) or personal. "+
		"Org value wins over personal. Values are write-only.") + "\n\n")
	if len(m.envVars) == 0 {
		b.WriteString(t.Help.Render("none set yet — press [n] to add one") + "\n")
		return b.String()
	}
	if m.envVarSel < 0 {
		m.envVarSel = 0
	}
	if m.envVarSel >= len(m.envVars) {
		m.envVarSel = len(m.envVars) - 1
	}
	for i, e := range m.envVars {
		cursor := "  "
		line := e.Key + "  " + t.Help.Render(e.Scope+" · "+e.Name)
		if i == m.envVarSel {
			cursor = t.AgentName.Render("▸ ")
			line = t.AgentName.Render(e.Key) + "  " + t.Help.Render(e.Scope+" · "+e.Name)
		}
		b.WriteString(cursor + line + "\n")
	}
	return b.String()
}

func (m *model) viewSetTelegram() string {
	t := m.theme
	var b strings.Builder
	b.WriteString(t.AgentName.Render("Telegram") + "\n\n")
	if m.me != nil && m.me.TelegramUserID != "" {
		b.WriteString(row(t, "Status", lipgloss.NewStyle().Foreground(t.Good).Render("linked")))
		b.WriteString(row(t, "User ID", m.me.TelegramUserID))
	} else {
		b.WriteString(row(t, "Status", lipgloss.NewStyle().Foreground(t.Subtle).Render("not linked")))
		b.WriteString("\n" + t.Help.Render("Send any message to the Nexora bot on Telegram —\nyour profile auto-connects. Then [r] refresh here."))
	}
	return b.String()
}

func (m *model) viewSetAccount() string {
	t := m.theme
	var b strings.Builder
	activeOrg := m.client.OrgID()
	b.WriteString(t.AgentName.Render("Organizations") + "  " + t.Help.Render("[o] switch") + "\n")
	for _, o := range m.orgs {
		marker := "  "
		name := o.Name
		if o.ID == activeOrg {
			marker = t.AgentName.Render("● ")
			name = t.AgentName.Render(o.Name) + t.Help.Render(" (active)")
		}
		tag := o.Role
		if o.IsPersonal {
			tag += " · personal"
		}
		b.WriteString(marker + name + "  " + t.Help.Render("("+tag+", "+itoa(o.MemberCount)+" members)") + "\n")
	}
	keyState := "not set"
	if m.mktKeySet {
		keyState = "configured"
	}
	b.WriteString("\n" + t.AgentName.Render("Marketplace") + "  " + t.Help.Render("[k] set API key") + "\n")
	b.WriteString("  api key: " + keyState + "  " + t.Help.Render("(needed for private packages)"))
	return b.String()
}

func (m *model) viewSetUsage() string {
	t := m.theme
	if m.usage == nil {
		return t.Help.Render("no usage data")
	}
	var b strings.Builder
	b.WriteString(t.AgentName.Render("Usage (30d)") + "\n\n")
	b.WriteString("  in " + itoa(m.usage.TotalInputTokens) + " · out " + itoa(m.usage.TotalOutputTokens) +
		" tokens · " + itoa(m.usage.TotalToolCalls) + " tool calls\n")
	for _, p := range m.usage.ByProvider {
		b.WriteString("    " + p.Provider + ": in " + itoa(p.InputTokens) + " / out " + itoa(p.OutputTokens) + "\n")
	}
	return b.String()
}

func (m *model) viewSetBackup() string {
	t := m.theme
	var b strings.Builder
	b.WriteString(t.AgentName.Render("Backup") + "\n\n")
	if m.me == nil || !m.me.IsSuperuser {
		return b.String() + t.Help.Render("Full-instance backup is superuser-only.")
	}
	b.WriteString(t.Help.Render("[b] export the whole instance → portable ZIP") + "\n")
	if m.backupStatus != "" {
		b.WriteString("  " + t.Help.Render("status: "+m.backupStatus) + "\n")
	}
	return b.String()
}

func row(t Theme, k, v string) string {
	return lipgloss.NewStyle().Foreground(t.Subtle).Render(padRight(k, 10)) + v + "\n"
}

// ── keys ──────────────────────────────────────────────────────────────────────

func (m *model) settingsKey(k tea.KeyMsg) (tea.Cmd, bool) {
	switch k.String() {
	case "]", "right":
		m.settingsSub = (m.settingsSub + 1) % len(settingsSubtabs)
		return m.settingsRefresh(), true
	case "[", "left":
		m.settingsSub = (m.settingsSub - 1 + len(settingsSubtabs)) % len(settingsSubtabs)
		return m.settingsRefresh(), true
	case "1", "2", "3", "4", "5", "6", "7", "8", "9":
		i := int(k.String()[0] - '1')
		if i < len(settingsSubtabs) {
			m.settingsSub = i
		}
		return m.settingsRefresh(), true
	case "r":
		return m.loadSettings(), true
	}

	switch m.settingsSub {
	case setProfile:
		if k.String() == "e" {
			m.openProfileForm()
			return nil, true
		}
	case setInterface:
		if k.String() == "space" || k.String() == " " || k.String() == "enter" {
			if m.uiMode == "simple" {
				m.uiMode = "advanced"
			} else {
				m.uiMode = "simple"
			}
			if m.saveUI != nil {
				m.saveUI(m.uiMode)
			}
			m.status = "interface → " + m.uiMode
			return nil, true
		}
	case setMemory:
		if k.String() == "e" {
			notes := ""
			if m.me != nil {
				notes = m.me.Notes
			}
			m.form = newForm("memory_edit", "AI Memory (single line — use web for long markdown)", []fieldSpec{
				{key: "notes", label: "Notes", placeholder: "role, timezone, preferences…", value: notes},
			})
			return nil, true
		}
	case setContact:
		if k.String() == "e" {
			var pairs []string
			for _, r := range parseContactRows(m.me) {
				pairs = append(pairs, r.Key+"="+r.Value)
			}
			m.form = newForm("contact_edit", "Contact (Label=Value, comma-separated)", []fieldSpec{
				{key: "rows", label: "Contacts", placeholder: "GitHub=user, Phone=+34…", value: strings.Join(pairs, ", ")},
			})
			return nil, true
		}
	case setIntegrations:
		switch k.String() {
		case "up", "k":
			if m.intSel > 0 {
				m.intSel--
			}
			return nil, true
		case "down", "j":
			if m.intSel < len(m.channels)-1 {
				m.intSel++
			}
			return nil, true
		case "n":
			m.form = newForm("integration_new", "New provider account", []fieldSpec{
				{key: "name", label: "Name", placeholder: "My Telegram bot"},
				{key: "type", label: "Type", placeholder: "telegram | slack | discord | whatsapp", value: "telegram"},
				{key: "bot_token", label: "Bot token", placeholder: "123456:ABC…", password: true},
			})
			return nil, true
		case "e":
			if m.intSel >= 0 && m.intSel < len(m.channels) {
				it := m.channels[m.intSel]
				m.form = newForm("integration_edit:"+it.ID, "Edit account", []fieldSpec{
					{key: "name", label: "Name", value: it.Name},
					{key: "bot_token", label: "Bot token (blank = keep)", placeholder: "leave blank to keep", password: true},
				})
			}
			return nil, true
		case "d":
			if m.intSel >= 0 && m.intSel < len(m.channels) {
				it := m.channels[m.intSel]
				m.openConfirm("integration_delete", it.ID, "", "Delete account \""+it.Name+"\"? (stops its bot)")
			}
			return nil, true
		}
	case setDevices:
		if k.String() == "d" {
			if len(m.devices) == 0 {
				m.status = "no devices to revoke"
				return nil, true
			}
			items := make([]list.Item, 0, len(m.devices))
			for _, d := range m.devices {
				items = append(items, devicePickItem{d})
			}
			m.picker.SetItems(items)
			m.picker.Title = "Revoke device (enter) · esc cancel"
			m.pickerKind = "device_revoke"
			m.pickerOpen = true
			return nil, true
		}
	case setVariables:
		switch k.String() {
		case "n":
			m.form = newForm("env_var_add", "Add environment variable", []fieldSpec{
				{key: "key", label: "KEY (e.g. STRIPE_SECRET_KEY)", placeholder: "STRIPE_SECRET_KEY"},
				{key: "name", label: "Name (blank = same as key)", placeholder: "for duplicates"},
				{key: "value", label: "Value (secret)", placeholder: "secret", password: true},
				{key: "scope", label: "Scope: user or org", placeholder: "user"},
			})
			return nil, true
		case "d":
			if m.envVarSel >= 0 && m.envVarSel < len(m.envVars) {
				e := m.envVars[m.envVarSel]
				m.openConfirm("env_var_delete", e.ID, "", "Delete "+e.Scope+" variable \""+e.Name+"\" ("+e.Key+")?")
			}
			return nil, true
		case "up", "k":
			if m.envVarSel > 0 {
				m.envVarSel--
				m.settingsVP.SetContent(m.settingsContent())
			}
			return nil, true
		case "down", "j":
			if m.envVarSel < len(m.envVars)-1 {
				m.envVarSel++
				m.settingsVP.SetContent(m.settingsContent())
			}
			return nil, true
		}
	case setAccount:
		switch k.String() {
		case "o":
			if len(m.orgs) == 0 {
				return m.loadSettings(), true
			}
			items := make([]list.Item, 0, len(m.orgs))
			for _, o := range m.orgs {
				items = append(items, orgPickItem{o})
			}
			m.picker.SetItems(items)
			m.picker.Title = "Switch organization"
			m.pickerKind = "org_select"
			m.pickerOpen = true
			return nil, true
		case "k":
			m.form = newForm("mkt_key", "Marketplace API key", []fieldSpec{
				{key: "key", label: "API key (nmk_…, blank to clear)", placeholder: "nmk_…", password: true},
			})
			return nil, true
		}
	case setBackup:
		if k.String() == "b" {
			if m.me == nil || !m.me.IsSuperuser {
				m.status = "backup is superuser-only"
				return nil, true
			}
			return m.startBackup(), true
		}
	}
	return nil, false
}

// settingsRefresh loads data the active subtab needs.
func (m *model) settingsRefresh() tea.Cmd {
	switch m.settingsSub {
	case setProfile, setMemory, setContact, setTelegram:
		return m.loadMe()
	case setDevices, setAccount, setUsage, setVariables:
		return m.loadSettings()
	case setIntegrations:
		return m.loadChannels()
	}
	return nil
}

func (m *model) openProfileForm() {
	name, avatar := "", ""
	if m.me != nil {
		name, avatar = m.me.FullName, m.me.AvatarEmoji
	}
	m.form = newForm("profile_edit", "Edit profile", []fieldSpec{
		{key: "full_name", label: "Display name", placeholder: "Your name", value: name},
		{key: "avatar_emoji", label: "Avatar emoji", placeholder: "🦊", value: avatar},
	})
}

// parseContactRows decodes the user's contact_info JSON into rows.
func parseContactRows(me *api.Me) []api.ContactRow {
	if me == nil || strings.TrimSpace(me.ContactInfo) == "" {
		return nil
	}
	var rows []api.ContactRow
	if json.Unmarshal([]byte(me.ContactInfo), &rows) == nil {
		return rows
	}
	return nil
}

type devicePickItem struct{ d api.Device }

func (i devicePickItem) Title() string       { return i.d.Name }
func (i devicePickItem) Description() string  { return i.d.Platform }
func (i devicePickItem) FilterValue() string { return i.d.Name }

// ── backup helpers (unchanged) ────────────────────────────────────────────────

func (m *model) startBackup() tea.Cmd {
	c := m.client
	m.backupStatus = "starting"
	m.status = "Starting full-instance backup…"
	return func() tea.Msg {
		id, err := c.StartBackup(ctxBg(), "instance", false)
		if err != nil {
			return errMsg{err}
		}
		m.backupJobID = id
		return chainCmd(okMsg{"Backup job " + id + " started"}, m.pollBackup())
	}
}

func (m *model) pollBackup() tea.Cmd {
	c := m.client
	id := m.backupJobID
	return tea.Tick(backupPoll, func(time.Time) tea.Msg {
		job, err := c.BackupStatus(ctxBg(), id)
		if err != nil {
			return errMsg{err}
		}
		return backupTickMsg{job}
	})
}

func (m *model) downloadBackup() tea.Cmd {
	c := m.client
	id := m.backupJobID
	dest := "nexora-backup-" + id[:8] + ".zip"
	m.status = "Downloading backup → " + dest + "…"
	return func() tea.Msg {
		if err := c.DownloadBackup(ctxBg(), id, dest); err != nil {
			return errMsg{err}
		}
		return okMsg{"Backup saved to " + dest}
	}
}

// org picker item
type orgPickItem struct{ o api.Org }

func (i orgPickItem) Title() string       { return i.o.Name }
func (i orgPickItem) Description() string  { return i.o.Role }
func (i orgPickItem) FilterValue() string { return i.o.Name }
