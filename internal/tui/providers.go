package tui

import (
	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"gitlab.com/parendum/nexora/nexora-cli/internal/api"
)

// ── provider list items ─────────────────────────────────────────────────────────

type providerItem struct{ p api.Provider }

func (i providerItem) Title() string {
	mark := ""
	if !i.p.IsActive {
		mark = " (inactive)"
	}
	return i.p.Name + mark
}
func (i providerItem) Description() string {
	d := i.p.ProviderType
	if i.p.ModelName != "" {
		d += " · " + i.p.ModelName
	}
	if i.p.LastError != "" {
		d += " · ⚠ " + i.p.LastError
	}
	return d
}
func (i providerItem) FilterValue() string { return i.p.Name }

func toProviderItems(ps []api.Provider) []list.Item {
	items := make([]list.Item, 0, len(ps))
	for _, p := range ps {
		items = append(items, providerItem{p})
	}
	return items
}

// ── provider-type picker items ───────────────────────────────────────────────────

type ptItem struct{ pt api.ProviderType }

func (i ptItem) Title() string       { return i.pt.Name }
func (i ptItem) Description() string { return i.pt.Category + " · " + i.pt.AuthType }
func (i ptItem) FilterValue() string { return i.pt.Name + " " + i.pt.Key }

func (m *model) providersKey(k tea.KeyMsg) (tea.Cmd, bool) {
	if m.providerList.FilterState() == list.Filtering {
		return nil, false
	}
	switch k.String() {
	case "n":
		return m.openProviderTypePicker(), true
	case "e":
		if it, ok := m.providerList.SelectedItem().(providerItem); ok {
			m.form = newForm("provider_edit:"+it.p.ID, "Edit "+it.p.Name, []fieldSpec{
				{key: "name", label: "Name", value: it.p.Name},
				{key: "api_key", label: "API key", placeholder: "(leave blank to keep)", password: true},
				{key: "base_url", label: "Base URL", value: it.p.BaseURL},
				{key: "model", label: "Model", value: it.p.ModelName},
			})
		}
		return nil, true
	case "d":
		if it, ok := m.providerList.SelectedItem().(providerItem); ok {
			m.openConfirm("provider_delete", it.p.ID, "", "Remove provider \""+it.p.Name+"\"?")
		}
		return nil, true
	}
	return nil, false
}

func (m *model) openProviderTypePicker() tea.Cmd {
	items := make([]list.Item, 0, len(m.providerTypes))
	for _, pt := range m.providerTypes {
		items = append(items, ptItem{pt})
	}
	if len(items) == 0 {
		m.status = "Provider types still loading — press n again in a moment"
		return m.loadProviderTypes()
	}
	m.picker.SetItems(items)
	m.picker.Title = "Select provider type"
	m.pickerKind = "provider_type"
	m.pickerOpen = true
	return nil
}

func (m *model) openProviderForm(pt api.ProviderType) {
	fields := []fieldSpec{
		{key: "name", label: "Name", placeholder: pt.Name, value: pt.Name},
	}
	if pt.AuthType != "none" {
		fields = append(fields, fieldSpec{key: "api_key", label: "API key", placeholder: "secret", password: true})
	}
	fields = append(fields,
		fieldSpec{key: "base_url", label: "Base URL", placeholder: "https://…", value: pt.BaseURL},
		fieldSpec{key: "model", label: "Model", placeholder: pt.DefaultModel, value: pt.DefaultModel},
	)
	m.form = newForm("provider_new:"+pt.Key, "Add "+pt.Name, fields)
}

func (m *model) viewProviders() string {
	if len(m.providers) == 0 {
		hint := "No providers.  [n] add one.\n\n" +
			m.theme.Help.Render("Providers supply the LLM credentials your agents use.")
		return lipgloss.NewStyle().Padding(1, 2).Render(hint)
	}
	body := m.providerList.View()
	if len(m.chains) > 0 {
		body += "\n" + m.theme.StatusBar.Render("fallback chains: ")
		for _, ch := range m.chains {
			if isInternalChain(ch.Name) {
				continue
			}
			tag := ch.Name
			if ch.IsDefault {
				tag += "*"
			}
			body += m.theme.AgentName.Render(" "+tag) + m.theme.StatusBar.Render("("+itoa(len(ch.Steps))+") ")
		}
	}
	return body
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		b[i] = '-'
	}
	return string(b[i:])
}
