package tui

import (
	"strconv"
	"strings"

	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"gitlab.com/parendum/nexora/nexora-cli/internal/api"
)

type marketItem struct{ it api.MarketItem }

func (i marketItem) Title() string {
	mark := ""
	if i.it.Installed {
		mark += " ✓"
	}
	if i.it.Liked {
		mark += " ♥"
	}
	icon := i.it.Icon
	if icon != "" {
		icon += " "
	}
	return icon + i.it.Name + mark
}
func (i marketItem) Description() string {
	d := i.it.Type
	if i.it.Visibility == "private" {
		d += " · 🔒private"
	}
	if i.it.Author != "" {
		d += " · @" + i.it.Author
	}
	d += " · ⬇" + itoa(i.it.InstallCount)
	if i.it.Description != "" {
		d += " · " + i.it.Description
	}
	return d
}
func (i marketItem) FilterValue() string { return i.it.Name + " " + i.it.Description }

func toMarketItems(items []api.MarketItem) []list.Item {
	out := make([]list.Item, 0, len(items))
	for _, it := range items {
		out = append(out, marketItem{it})
	}
	return out
}

// seedItem is an installed seed row (Installed view).
type seedItem struct{ s api.Seed }

func (i seedItem) Title() string {
	t := i.s.Name
	if i.s.IsBuiltin {
		t += "  (builtin)"
	}
	return t
}
func (i seedItem) Description() string  { return i.s.Kind() }
func (i seedItem) FilterValue() string { return i.s.Name }

func toSeedItems(seeds []api.Seed) []list.Item {
	out := make([]list.Item, 0, len(seeds))
	for _, s := range seeds {
		out = append(out, seedItem{s})
	}
	return out
}

var marketTypes = []string{"", "skill", "tool", "agent", "persona"}
var marketViews = []string{"browse", "installed", "liked"}

func (m *model) marketKey(k tea.KeyMsg) (tea.Cmd, bool) {
	if m.marketList.FilterState() == list.Filtering {
		return nil, false
	}
	switch k.String() {
	case "b":
		m.marketView = "browse"
		return m.loadMarket(m.marketQuery), true
	case "I":
		m.marketView = "installed"
		return m.loadInstalled(), true
	case "L":
		m.marketView = "liked"
		m.applyMarketFilter()
		return nil, true
	case "1", "2", "3", "4", "5":
		idx := int(k.String()[0] - '1')
		if idx >= 0 && idx < len(marketTypes) {
			m.marketType = marketTypes[idx]
			m.marketTag = ""
			if m.marketView == "installed" {
				m.applyMarketFilter()
				return nil, true
			}
			return m.loadMarket(m.marketQuery), true
		}
	case "r":
		m.marketTag = ""
		if m.marketView == "installed" {
			return m.loadInstalled(), true
		}
		return m.loadMarket(m.marketQuery), true
	case "/":
		m.form = newForm("market_search", "Search marketplace", []fieldSpec{
			{key: "q", label: "Query", placeholder: "name or description", value: m.marketQuery},
		})
		return nil, true
	case "t":
		m.form = newForm("market_tag", "Filter by tag", []fieldSpec{
			{key: "tag", label: "Tag", placeholder: "e.g. devops (blank = clear)", value: m.marketTag},
		})
		return nil, true
	case "l":
		if it, ok := m.marketList.SelectedItem().(marketItem); ok && it.it.Slug != "" {
			return m.likeMarket(it.it), true
		}
	case "u", "d":
		// uninstall (Installed view)
		if it, ok := m.marketList.SelectedItem().(seedItem); ok {
			if it.s.IsBuiltin {
				m.status = "builtin seeds can't be uninstalled"
				return nil, true
			}
			m.openConfirm("seed_uninstall", it.s.ID, it.s.Kind(), "Uninstall "+it.s.Kind()+" \""+it.s.Name+"\"?")
			return nil, true
		}
	case "enter":
		if it, ok := m.marketList.SelectedItem().(marketItem); ok {
			if it.it.Installed {
				m.status = it.it.Name + " already installed"
				return nil, true
			}
			return m.installMarket(it.it), true
		}
	case "i":
		m.form = newForm("market_import", "Import from URL", []fieldSpec{
			{key: "url", label: "Package URL", placeholder: "https://marketplace…/api/packages/<slug>"},
		})
		return nil, true
	}
	return nil, false
}

func (m *model) likeMarket(it api.MarketItem) tea.Cmd {
	c := m.client
	q := m.marketQuery
	return func() tea.Msg {
		if err := c.LikeRegistry(ctxBg(), it.Slug); err != nil {
			return errMsg{err}
		}
		return chainCmd(okMsg{"♥ " + it.Name}, m.loadMarket(q))
	}
}

// applyMarketFilter populates the list based on the current view. Browse/Liked use the
// registry results (server-filtered by type/query/tag); Installed uses the local seeds;
// Liked keeps only liked registry items.
func (m *model) applyMarketFilter() {
	switch m.marketView {
	case "installed":
		// Installed = custom (marketplace-installed) seeds only; never builtins.
		seeds := m.installed[:0:0]
		for _, s := range m.installed {
			if s.IsBuiltin {
				continue
			}
			if m.marketType != "" && s.Kind() != m.marketType {
				continue
			}
			seeds = append(seeds, s)
		}
		m.marketList.SetItems(toSeedItems(seeds))
	case "liked":
		liked := m.market[:0:0]
		for _, it := range m.market {
			if it.Liked {
				liked = append(liked, it)
			}
		}
		m.marketList.SetItems(toMarketItems(liked))
	default:
		m.marketList.SetItems(toMarketItems(m.market))
	}
}

func (m *model) installMarket(it api.MarketItem) tea.Cmd {
	m.status = "Installing " + it.Name + "…"
	if it.ImportURL != "" {
		// registry item → import by URL (uses the stored key for private packages).
		return m.importMarketURL(it.ImportURL, it.Name, false)
	}
	c := m.client
	q := m.marketQuery
	name := it.Name
	slug := it.Slug
	return func() tea.Msg {
		if err := c.InstallMarketItem(ctxBg(), slug); err != nil {
			if api.IsConflict(err) {
				return okMsg{err.Error()} // "Skill already installed" — info, not an error
			}
			return errMsg{err}
		}
		return chainCmd(okMsg{"Installed " + name}, m.loadMarket(q))
	}
}

// importMarketURL runs a marketplace import-by-URL through the risk-acknowledgment
// gate. Both install entry points (registry list + the import-from-URL form) route
// here so neither bypasses the gate. On a low-reputation 409 it returns a
// riskAckMsg (→ confirm overlay); on success it provisions venvs, hints missing
// credentials, and adds a terse third-party note when a disclaimer is present.
func (m *model) importMarketURL(url, name string, acknowledgeRisk bool) tea.Cmd {
	c := m.client
	q := m.marketQuery
	return func() tea.Msg {
		res, err := c.ImportMarketURLFull(ctxBg(), url, acknowledgeRisk)
		if err != nil {
			if ra, ok := api.AsRiskAck(err); ok {
				// Low-reputation package → ask the user to acknowledge, then retry.
				dispName := name
				if dispName == "" {
					dispName = ra.Slug
				}
				return riskAckMsg{importURL: url, name: dispName, detail: ra}
			}
			if api.IsConflict(err) {
				return okMsg{err.Error()} // "already installed" — info, not an error (separate 409)
			}
			return errMsg{err}
		}
		dispName := name
		if dispName == "" && res.Name != "" {
			dispName = res.Name
		}
		if dispName == "" {
			dispName = "package"
		}
		extra := ""
		if len(res.Requirements) > 0 {
			if msg := provisionToolEnvs(c, res.Requirements); msg != "" {
				extra += " · " + msg
			}
		}
		if hint := envHint(c, res.RequiredEnv); hint != "" {
			extra += " · " + hint
		}
		if res.Disclaimer != "" {
			extra += " · ⚠ third-party — at your own risk"
		}
		return chainCmd(okMsg{"Installed " + dispName + extra}, m.loadMarket(q))
	}
}

// riskAckText builds the styled body for the risk-acknowledgment confirm overlay:
// package name/slug, warning level (amber=elevated, red=high), trust tier, which
// reputation thresholds tripped, the disclaimer, and the server message.
func (m *model) riskAckText(name string, d *api.RiskAckRequired) string {
	t := m.theme
	level := d.WarningLevel
	if level == "" {
		level = "elevated"
	}
	levelColor := t.Warn // elevated → amber
	if level == "high" {
		levelColor = t.Bad // high → red
	}
	levelStyle := lipgloss.NewStyle().Foreground(levelColor).Bold(true)

	var b strings.Builder
	hdr := name
	if d.Slug != "" {
		hdr += "  (" + d.Slug + ")"
	}
	b.WriteString(t.AgentName.Render(hdr) + "\n")
	meta := levelStyle.Render(strings.ToUpper(level)+" risk")
	if d.TrustTier != "" {
		meta += t.Help.Render("  ·  trust tier: " + d.TrustTier)
	}
	if d.Type != "" {
		meta += t.Help.Render("  ·  " + d.Type)
	}
	b.WriteString(meta + "\n")

	var reasons []string
	if d.BelowLikeThreshold {
		reasons = append(reasons, "few likes")
	}
	if d.BelowDownloadThreshold {
		reasons = append(reasons, "few downloads")
	}
	if d.TrustTier == "new" {
		reasons = append(reasons, "new publisher")
	}
	if len(reasons) > 0 {
		b.WriteString(t.Help.Render("Flags: "+strings.Join(reasons, ", ")) + "\n")
	}
	if d.Disclaimer != "" {
		b.WriteString("\n" + lipgloss.NewStyle().Foreground(levelColor).Render(d.Disclaimer) + "\n")
	}
	if d.Message != "" {
		b.WriteString("\n" + d.Message)
	}
	return b.String()
}

// envHint checks which declared env vars (credentials) are not yet configured at
// org/user scope and returns a short toast hint pointing to Settings → Variables.
func envHint(c *api.Client, need []api.RequiredEnvVar) string {
	if len(need) == 0 {
		return ""
	}
	keys := make([]string, 0, len(need))
	for _, n := range need {
		keys = append(keys, n.Key)
	}
	res, err := c.ResolveEnvVars(ctxBg(), keys)
	if err != nil {
		return ""
	}
	missing := 0
	for _, r := range res {
		if len(r.Configured) == 0 {
			missing++
		}
	}
	if missing == 0 {
		return ""
	}
	return itoa(missing) + " credential(s) needed — set in Settings → Variables"
}

// provisionToolEnvs installs any not-yet-provisioned Python requirements a just-
// imported pack reported, into isolated per-pack venvs. Returns a short status
// string for the toast (empty if there was nothing to do).
func provisionToolEnvs(c *api.Client, reqs []api.ToolEnvStatus) string {
	pending := 0
	for _, r := range reqs {
		if r.EnvHash != "" && !r.Provisioned {
			pending++
		}
	}
	if pending == 0 {
		return ""
	}
	ok, failed := 0, 0
	var lastErr string
	for _, r := range reqs {
		if r.EnvHash == "" || r.Provisioned {
			continue
		}
		good, errMsg, err := c.ProvisionToolEnv(ctxBg(), r.Requirements)
		if err != nil {
			failed++
			lastErr = err.Error()
		} else if good {
			ok++
		} else {
			failed++
			lastErr = errMsg
		}
	}
	if failed == 0 {
		return "installed deps for " + strconv.Itoa(ok) + " tool env(s)"
	}
	msg := "deps: " + strconv.Itoa(ok) + " ok, " + strconv.Itoa(failed) + " failed"
	if lastErr != "" {
		msg += " (" + lastErr + ")"
	}
	return msg
}

func (m *model) viewMarket() string {
	t := m.theme
	if m.marketView == "" {
		m.marketView = "browse"
	}
	// view row: Browse / Installed / Liked
	vlabels := map[string]string{"browse": "Browse [b]", "installed": "Installed [I]", "liked": "Liked [L]"}
	var views []string
	for _, v := range marketViews {
		if v == m.marketView {
			views = append(views, t.TabActive.Render(vlabels[v]))
		} else {
			views = append(views, t.TabInactive.Render(vlabels[v]))
		}
	}
	// type row
	labels := []string{"All", "Skills", "Tools", "Agents", "Personas"}
	var subtabs []string
	for i, lbl := range labels {
		if marketTypes[i] == m.marketType {
			subtabs = append(subtabs, t.TabActive.Render(lbl))
		} else {
			subtabs = append(subtabs, t.TabInactive.Render(lbl))
		}
	}
	header := strings.Join(views, " ") + "\n" + strings.Join(subtabs, " ")
	var crumbs []string
	if m.marketQuery != "" {
		crumbs = append(crumbs, "search: "+m.marketQuery)
	}
	if m.marketTag != "" {
		crumbs = append(crumbs, "tag: "+m.marketTag)
	}
	if len(crumbs) > 0 {
		header += "   " + t.Help.Render(strings.Join(crumbs, " · "))
	}

	cap := lipgloss.NewStyle().MaxHeight(max(1, m.height-4)) // never push the tab bar off-screen
	if len(m.marketList.Items()) == 0 {
		hint := "No items.  [b]rowse [I]nstalled [L]iked · [1-5] type · [/] search · [t]ag · [i] import"
		if m.marketView == "browse" {
			hint += "\n\n" + t.Help.Render("Private packages need a marketplace API key — set it in settings ([k]).")
		}
		return cap.Render(header + "\n" + lipgloss.NewStyle().Padding(1, 2).Render(hint))
	}
	return cap.Render(header + "\n" + m.marketList.View())
}
