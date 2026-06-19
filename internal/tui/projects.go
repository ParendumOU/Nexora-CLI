package tui

import (
	"sort"
	"strings"

	"github.com/alecthomas/chroma/v2"
	"github.com/alecthomas/chroma/v2/formatters"
	"github.com/alecthomas/chroma/v2/lexers"
	"github.com/alecthomas/chroma/v2/styles"
	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"gitlab.com/parendum/nexora/nexora-cli/internal/api"
)

// project detail subtabs (mirror the web: Overview/Agents/Resources/Tasks/Logs/Repository)
const (
	subOverview = iota
	subAgents
	subResources
	subTasks
	subLogs
	subRepo
)

var projSubtabs = []string{"Overview", "Agents", "Resources", "Tasks", "Logs", "Repository"}

type projectItem struct{ p api.Project }

func (i projectItem) Title() string { return i.p.Name }
func (i projectItem) Description() string {
	d := i.p.Status
	if i.p.Description != "" {
		d += " · " + i.p.Description
	}
	return d
}
func (i projectItem) FilterValue() string { return i.p.Name }

func toProjectItems(projects []api.Project) []list.Item {
	out := make([]list.Item, 0, len(projects))
	for _, p := range projects {
		if p.Status == "deleted" {
			continue
		}
		out = append(out, projectItem{p})
	}
	return out
}

// openProjectDetail resets detail state and loads the project.
func (m *model) openProjectDetail(id string) tea.Cmd {
	m.projSubtab = subOverview
	m.projScroll = 0
	m.repoTree = nil
	m.repoFile = nil
	m.repoCursor = 0
	m.repoFileOff = 0
	m.repoStatus = ""
	m.repoExpanded = map[string]bool{}
	// also load catalogs (tools) + full MCP servers for the Resources editors
	return tea.Batch(m.loadProjectDetail(id), m.loadCatalogs(), m.loadMcpServers())
}

func (m *model) projectsKey(k tea.KeyMsg) (tea.Cmd, bool) {
	// ── detail mode ──────────────────────────────────────────────────────────
	if m.currentProject != nil {
		switch k.String() {
		case "esc":
			if m.projSubtab == subRepo && m.repoFile != nil {
				m.repoFile = nil // close file viewer, stay in tree
				return nil, true
			}
			m.currentProject = nil
			return nil, true
		case "r":
			cmds := []tea.Cmd{m.loadProjectDetail(m.currentProject.ID)}
			if m.projSubtab == subRepo {
				m.repoTree = nil
				cmds = append(cmds, m.loadRepoTree(*m.currentProject))
			}
			return tea.Batch(cmds...), true
		case "]", "right":
			if m.projSubtab == subRepo && k.String() == "right" {
				return m.repoKey(k), true // arrows drive the tree
			}
			return m.setSubtab((m.projSubtab + 1) % len(projSubtabs)), true
		case "[", "left":
			if m.projSubtab == subRepo && k.String() == "left" {
				return m.repoKey(k), true
			}
			return m.setSubtab((m.projSubtab - 1 + len(projSubtabs)) % len(projSubtabs)), true
		case "1", "2", "3", "4", "5", "6":
			return m.setSubtab(int(k.String()[0] - '1')), true
		}
		if m.projSubtab == subRepo {
			return m.repoKey(k), true
		}
		if m.projSubtab == subResources {
			switch k.String() {
			case "t": // edit tools
				return m.editProjTools(), true
			case "M": // edit MCP servers (pick a server → choose its tools)
				return m.editProjMcp(), true
			case "e": // edit env vars
				m.editProjEnv()
				return nil, true
			}
		}
		// tasks/logs/agents scroll
		switch k.String() {
		case "up", "k":
			if m.projScroll > 0 {
				m.projScroll--
			}
			return nil, true
		case "down", "j":
			m.projScroll++
			return nil, true
		}
		return nil, false
	}

	// ── list mode ────────────────────────────────────────────────────────────
	if m.projectList.FilterState() == list.Filtering {
		return nil, false
	}
	switch k.String() {
	case "r":
		return m.loadProjects(), true
	case "enter":
		if it, ok := m.projectList.SelectedItem().(projectItem); ok {
			m.status = "Opening " + it.p.Name + "…"
			return m.openProjectDetail(it.p.ID), true
		}
	case "n":
		m.form = newForm("project_new", "New project", []fieldSpec{
			{key: "name", label: "Name", placeholder: "My project"},
			{key: "description", label: "Description", placeholder: "(optional)"},
			{key: "repo_url", label: "Git repo URL", placeholder: "https://github.com/org/repo (optional)"},
			{key: "repo_type", label: "Repo type", placeholder: "github | gitlab (optional)"},
		})
		return nil, true
	}
	return nil, false
}

// ── resource editors ────────────────────────────────────────────────────────────

// editProjTools opens a tools multi-select preselected with the project's tools.
func (m *model) editProjTools() tea.Cmd {
	if m.currentProject == nil {
		return nil
	}
	if len(m.toolCat) == 0 {
		m.status = "tools catalog still loading — try again"
		return m.loadCatalogs()
	}
	cur := map[string]bool{}
	for _, t := range m.currentProject.Tools {
		cur[t] = true
	}
	items := make([]msItem, 0, len(m.toolCat))
	for _, ci := range m.toolCat {
		label := orDefault(ci.Name, ci.Key)
		items = append(items, msItem{key: ci.Key, label: label + "  (" + ci.Key + ")", selected: cur[ci.Key]})
	}
	m.ms = newMultiSelect("proj_tools:"+m.currentProject.ID, "Project tools", items)
	return nil
}

// editProjMcp opens a picker of MCP servers; choosing one opens its tool selection.
func (m *model) editProjMcp() tea.Cmd {
	if m.currentProject == nil {
		return nil
	}
	if len(m.mcpServers) == 0 {
		m.status = "no MCP servers configured (add one in the web app)"
		return m.loadMcpServers()
	}
	items := make([]list.Item, 0, len(m.mcpServers))
	for _, s := range m.mcpServers {
		items = append(items, mcpPickItem{s})
	}
	m.picker.SetItems(items)
	m.picker.Title = "MCP server → choose tools"
	m.pickerKind = "proj_mcp_pick:" + m.currentProject.ID
	m.pickerOpen = true
	return nil
}

// editProjEnv opens a single-field form with all env vars as "K=V, K2=V2".
func (m *model) editProjEnv() {
	if m.currentProject == nil {
		return
	}
	var pairs []string
	keys := make([]string, 0, len(m.currentProject.EnvVars))
	for k := range m.currentProject.EnvVars {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		pairs = append(pairs, k+"="+m.currentProject.EnvVars[k])
	}
	m.form = newForm("proj_env:"+m.currentProject.ID, "Env vars (KEY=VALUE, comma-separated)", []fieldSpec{
		{key: "vars", label: "Vars", placeholder: "KEY=VALUE, OTHER=123", value: strings.Join(pairs, ", ")},
	})
}

type mcpPickItem struct{ s api.McpServer }

func (i mcpPickItem) Title() string { return i.s.Name }
func (i mcpPickItem) Description() string {
	return itoa(len(i.s.KnownTools)) + " tools · " + i.s.URL
}
func (i mcpPickItem) FilterValue() string { return i.s.Name }

// parseEnvVars turns "K=V, K2=V2" (comma or newline separated) into a map.
func parseEnvVars(s string) map[string]string {
	out := map[string]string{}
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
		out[k] = strings.TrimSpace(v)
	}
	return out
}

// setSubtab switches subtab and lazily loads the repo tree the first time.
func (m *model) setSubtab(i int) tea.Cmd {
	m.projSubtab = i
	m.projScroll = 0
	if i == subRepo && m.currentProject != nil && len(m.repoTree) == 0 && m.repoStatus == "" {
		p := m.currentProject
		if p.RepoURL == "" {
			m.repoStatus = "no repo configured (set repo URL in the web app)"
			return nil
		}
		if p.RepoCredID == "" {
			m.repoStatus = "no git credential bound (set one in the web app)"
			return nil
		}
		m.repoStatus = "loading tree…"
		return m.loadRepoTree(*p)
	}
	return nil
}

// repoKey drives the repository file-tree navigation.
func (m *model) repoKey(k tea.KeyMsg) tea.Cmd {
	rows := m.buildRepoRows()
	switch k.String() {
	case "up", "k":
		if m.repoFile != nil {
			if m.repoFileOff > 0 {
				m.repoFileOff--
			}
			return nil
		}
		if m.repoCursor > 0 {
			m.repoCursor--
		}
	case "down", "j":
		if m.repoFile != nil {
			m.repoFileOff++
			return nil
		}
		if m.repoCursor < len(rows)-1 {
			m.repoCursor++
		}
	case "pgup", "ctrl+u":
		m.repoFileOff = max(0, m.repoFileOff-10)
	case "pgdown", "ctrl+d":
		m.repoFileOff += 10
	case "left", "h":
		if m.repoCursor < len(rows) {
			r := rows[m.repoCursor]
			if r.isDir && m.repoExpanded[r.path] {
				m.repoExpanded[r.path] = false
			}
		}
	case "right", "l", "enter":
		if m.repoCursor >= len(rows) {
			return nil
		}
		r := rows[m.repoCursor]
		if r.isDir {
			m.repoExpanded[r.path] = !m.repoExpanded[r.path]
			return nil
		}
		// file → load content
		m.repoStatus = "loading " + r.path + "…"
		return m.loadRepoFile(*m.currentProject, r.path)
	}
	return nil
}

// ── repo tree building ─────────────────────────────────────────────────────────

type repoRow struct {
	path  string
	name  string
	depth int
	isDir bool
}

// buildRepoRows flattens the tree into the currently-visible rows (respecting expanded dirs).
func (m *model) buildRepoRows() []repoRow {
	if len(m.repoTree) == 0 {
		return nil
	}
	isDir := map[string]bool{}
	children := map[string][]string{}
	seen := map[string]bool{}
	add := func(path string, dir bool) {
		if dir {
			isDir[path] = true
		}
		if seen[path] {
			return
		}
		seen[path] = true
		parent := ""
		if i := strings.LastIndex(path, "/"); i >= 0 {
			parent = path[:i]
		}
		children[parent] = append(children[parent], path)
	}
	for _, it := range m.repoTree {
		parts := strings.Split(it.Path, "/")
		for i := 1; i < len(parts); i++ {
			add(strings.Join(parts[:i], "/"), true)
		}
		add(it.Path, it.Type == "dir")
	}
	// sort each level: dirs first, then alpha by basename
	base := func(p string) string {
		if i := strings.LastIndex(p, "/"); i >= 0 {
			return p[i+1:]
		}
		return p
	}
	for k := range children {
		ch := children[k]
		sort.Slice(ch, func(a, b int) bool {
			da, db := isDir[ch[a]], isDir[ch[b]]
			if da != db {
				return da
			}
			return strings.ToLower(base(ch[a])) < strings.ToLower(base(ch[b]))
		})
		children[k] = ch
	}
	var rows []repoRow
	var walk func(parent string, depth int)
	walk = func(parent string, depth int) {
		for _, p := range children[parent] {
			rows = append(rows, repoRow{path: p, name: base(p), depth: depth, isDir: isDir[p]})
			if isDir[p] && m.repoExpanded[p] {
				walk(p, depth+1)
			}
		}
	}
	walk("", 0)
	return rows
}

// ── views ──────────────────────────────────────────────────────────────────────

func (m *model) viewProjects() string {
	if m.currentProject == nil {
		if len(m.projects) == 0 {
			return lipgloss.NewStyle().Padding(1, 2).Render("No projects.  [n] create one (optionally import a Git repo).")
		}
		return m.projectList.View()
	}
	t := m.theme
	p := m.currentProject
	bodyH := max(4, m.height-4)

	// header: name + subtab bar
	var head strings.Builder
	head.WriteString(t.AgentName.Render("📁 "+p.Name) + "  " + t.Help.Render("[esc] back · [1-6]/[ ] subtab · [r]efresh") + "\n")
	var tabs []string
	for i, name := range projSubtabs {
		label := itoa(i+1) + " " + name
		if i == m.projSubtab {
			tabs = append(tabs, lipgloss.NewStyle().Foreground(t.Accent).Bold(true).Underline(true).Render(label))
		} else {
			tabs = append(tabs, t.Help.Render(label))
		}
	}
	head.WriteString(strings.Join(tabs, t.Help.Render("  ·  ")) + "\n")
	header := head.String()
	// body budget = bodyH minus: 2 lines subtab header + 1 blank separator + 2 lines
	// vertical padding. Without this the content overflows and scrolls the global tab
	// bar off the top of the terminal.
	contentH := max(3, bodyH-5)

	var body string
	switch m.projSubtab {
	case subOverview:
		body = m.viewProjOverview(p)
	case subAgents:
		body = m.scrollBox(m.viewProjAgents(), contentH)
	case subResources:
		body = m.scrollBox(m.viewProjResources(p), contentH)
	case subTasks:
		body = m.scrollBox(m.viewProjTasks(), contentH)
	case subLogs:
		body = m.scrollBox(m.viewProjLogs(), contentH)
	case subRepo:
		body = m.viewProjRepo(contentH)
	}
	// hard-cap total height so the body can never push the header/footer off-screen
	return lipgloss.NewStyle().Padding(1, 2).MaxHeight(max(1, bodyH)).Render(header + "\n" + body)
}

func (m *model) viewProjOverview(p *api.Project) string {
	t := m.theme
	var b strings.Builder
	if p.Description != "" {
		b.WriteString(wrap(p.Description, m.width-6) + "\n\n")
	}
	if p.RepoURL != "" {
		meta := orDefault(p.RepoType, "git")
		if p.RepoBranch != "" {
			meta += " · " + p.RepoBranch
		}
		b.WriteString(t.Help.Render("repo: ") + p.RepoURL + "  " + t.Help.Render("("+meta+")") + "\n")
	}
	if p.PMAgentName != "" {
		b.WriteString(t.Help.Render("PM agent: ") + p.PMAgentName + "\n")
	}
	b.WriteString(t.Help.Render("status: ") + p.Status + "\n")
	b.WriteString(t.Help.Render("counts: ") +
		itoa(len(p.Tools)) + " tools · " + itoa(len(p.Mcps)) + " MCP · " + itoa(len(p.EnvVars)) + " env vars · " +
		itoa(len(m.projTasks)) + " tasks · " + itoa(len(m.projIssues)) + " issues · " + itoa(len(m.projAgents)) + " agents\n")
	return b.String()
}

func (m *model) viewProjAgents() string {
	t := m.theme
	if len(m.projAgents) == 0 {
		return t.Help.Render("No agents on this project.")
	}
	var b strings.Builder
	b.WriteString(t.AgentName.Render("Agents ("+itoa(len(m.projAgents))+")") + "\n")
	for _, a := range m.projAgents {
		badge := ""
		if a.IsPM {
			badge = lipgloss.NewStyle().Foreground(t.Accent).Render(" [PM]")
		}
		b.WriteString("  " + a.Name + badge + t.Help.Render("  "+a.AgentType+" · "+itoa(a.TaskCount)+" tasks") + "\n")
		if len(a.Skills) > 0 {
			b.WriteString(t.Help.Render("    skills: "+truncate(strings.Join(a.Skills, ", "), m.width-12)) + "\n")
		}
	}
	return b.String()
}

func (m *model) viewProjResources(p *api.Project) string {
	t := m.theme
	var b strings.Builder
	b.WriteString(t.Help.Render("[t] edit tools · [M] edit MCP servers · [e] edit env vars") + "\n\n")
	b.WriteString(t.AgentName.Render("Tools ("+itoa(len(p.Tools))+")") + "\n")
	if len(p.Tools) == 0 {
		b.WriteString(t.Help.Render("  (none)") + "\n")
	} else {
		b.WriteString("  " + wrap(strings.Join(p.Tools, ", "), m.width-8) + "\n")
	}
	b.WriteString("\n" + t.AgentName.Render("MCP Servers ("+itoa(len(p.Mcps))+")") + "\n")
	if len(p.Mcps) == 0 {
		b.WriteString(t.Help.Render("  (none)") + "\n")
	} else {
		for _, mc := range p.Mcps {
			name := mcpName(mc)
			tools := mcpAllowed(mc)
			if len(tools) == 0 {
				b.WriteString("  • " + name + t.Help.Render("  (all tools)") + "\n")
			} else {
				b.WriteString("  • " + name + t.Help.Render("  "+itoa(len(tools))+" tools") + "\n")
				b.WriteString(t.Help.Render("      "+truncate(strings.Join(tools, ", "), m.width-12)) + "\n")
			}
		}
	}
	b.WriteString("\n" + t.AgentName.Render("Env Vars ("+itoa(len(p.EnvVars))+")") + "\n")
	if len(p.EnvVars) == 0 {
		b.WriteString(t.Help.Render("  (none)") + "\n")
	} else {
		keys := make([]string, 0, len(p.EnvVars))
		for k := range p.EnvVars {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			b.WriteString("  " + k + t.Help.Render(" = ") + truncate(p.EnvVars[k], m.width-len(k)-10) + "\n")
		}
	}
	return b.String()
}

func (m *model) viewProjTasks() string {
	t := m.theme
	if len(m.projTasks) == 0 {
		return t.Help.Render("No tasks.")
	}
	done := 0
	for _, tk := range m.projTasks {
		if tk.Status == "completed" {
			done++
		}
	}
	var b strings.Builder
	b.WriteString(t.AgentName.Render("Tasks ("+itoa(done)+"/"+itoa(len(m.projTasks))+" done)") + "\n")
	for _, tk := range m.projTasks {
		line := "  " + statusIcon(tk.Status) + " " + truncate(tk.Title, m.width-12)
		b.WriteString(lipgloss.NewStyle().Foreground(t.statusColor(tk.Status)).Render(line) + "\n")
	}
	return b.String()
}

func (m *model) viewProjLogs() string {
	t := m.theme
	if len(m.projLogs) == 0 {
		return t.Help.Render("No logs.")
	}
	var b strings.Builder
	b.WriteString(t.AgentName.Render("Logs ("+itoa(len(m.projLogs))+")") + "\n")
	for _, lg := range m.projLogs {
		ts := lg.CreatedAt
		if len(ts) > 19 {
			ts = ts[:19]
		}
		who := orDefault(lg.AgentName, "system")
		lvl := lipgloss.NewStyle().Foreground(t.statusColor(lg.Level)).Render(strings.ToUpper(orDefault(lg.Level, "info")))
		b.WriteString(t.Help.Render(ts+" ") + lvl + " " + t.Help.Render(who+": ") + truncate(lg.Message, m.width-12) + "\n")
	}
	return b.String()
}

func (m *model) viewProjRepo(h int) string {
	t := m.theme
	rows := m.buildRepoRows()
	if len(rows) == 0 {
		msg := orDefault(m.repoStatus, "no files")
		return t.Help.Render(msg)
	}
	if m.repoCursor >= len(rows) {
		m.repoCursor = len(rows) - 1
	}
	treeW := min(46, max(28, m.width/2))

	// tree pane (windowed around cursor)
	start := 0
	if m.repoCursor >= h {
		start = m.repoCursor - h + 1
	}
	end := min(len(rows), start+h)
	var tb strings.Builder
	for i := start; i < end; i++ {
		r := rows[i]
		icon := fileIcon(r.name) + " "
		if r.isDir {
			if m.repoExpanded[r.path] {
				icon = "▾ 📁 "
			} else {
				icon = "▸ 📁 "
			}
		}
		line := strings.Repeat("  ", r.depth) + icon + r.name
		line = truncate(line, treeW-1)
		if i == m.repoCursor && m.repoFile == nil {
			line = lipgloss.NewStyle().Foreground(t.Accent).Bold(true).Render(line)
		} else {
			// explicit default-fg + reset so a bled color from the viewer pane can't tint the tree
			line = "\x1b[39m" + line + "\x1b[0m"
		}
		tb.WriteString(line + "\n")
	}
	tree := lipgloss.NewStyle().Width(treeW).Render(tb.String())

	// file viewer pane
	var vb strings.Builder
	if m.repoFile == nil {
		vb.WriteString(t.Help.Render("select a file · ↑↓ move · →/enter open dir/file · ← collapse"))
	} else {
		vb.WriteString(t.AgentName.Render(m.repoFile.Path))
		if m.repoFile.Truncated {
			vb.WriteString(t.Help.Render("  (truncated)"))
		}
		vb.WriteString("  " + t.Help.Render("[esc] close · pgup/pgdn scroll") + "\n")
		lines := m.repoFileLines
		if len(lines) == 0 {
			lines = strings.Split(strings.ReplaceAll(m.repoFile.Content, "\t", "    "), "\n")
		}
		if m.repoFileOff > len(lines)-1 {
			m.repoFileOff = max(0, len(lines)-1)
		}
		viewW := max(20, m.width-treeW-4)
		gutter := lipgloss.NewStyle().Foreground(t.Subtle)
		clip := lipgloss.NewStyle().MaxWidth(max(10, viewW-5)) // ANSI-aware truncation (minus gutter)
		vend := min(len(lines), m.repoFileOff+h-1)
		for i := m.repoFileOff; i < vend; i++ {
			ln := gutter.Render(padLeft(itoa(i+1), 4) + " ")
			vb.WriteString(ln + clip.Render(lines[i]) + "\n")
		}
		if vend < len(lines) {
			vb.WriteString(t.Help.Render("… " + itoa(len(lines)-vend) + " more lines"))
		}
	}
	viewer := lipgloss.NewStyle().Width(max(20, m.width-treeW-4)).Render(vb.String())
	return lipgloss.JoinHorizontal(lipgloss.Top, tree, t.Help.Render(" │ "), viewer)
}

// ── helpers ──────────────────────────────────────────────────────────────────

// scrollBox windows a multi-line body to h lines using m.projScroll (clamped) and
// appends a scroll indicator when the content overflows.
func (m *model) scrollBox(content string, h int) string {
	lines := strings.Split(strings.TrimRight(content, "\n"), "\n")
	if len(lines) <= h {
		m.projScroll = 0
		return content
	}
	visible := max(1, h-1) // reserve one line for the indicator
	maxOff := len(lines) - visible
	if m.projScroll > maxOff {
		m.projScroll = maxOff
	}
	if m.projScroll < 0 {
		m.projScroll = 0
	}
	off := m.projScroll
	seg := lines[off:min(len(lines), off+visible)]
	ind := m.theme.Help.Render("[" + itoa(off+1) + "-" + itoa(off+len(seg)) + "/" + itoa(len(lines)) + "]  ↑↓ scroll")
	return strings.Join(seg, "\n") + "\n" + ind
}

// highlightCode syntax-highlights file content (by filename, then content sniff) into
// ANSI-colored lines via chroma. Falls back to plain lines on any failure.
func highlightCode(path, content string) []string {
	content = strings.ReplaceAll(content, "\t", "    ")
	plain := strings.Split(strings.TrimRight(content, "\n"), "\n")
	lexer := lexers.Match(path)
	if lexer == nil {
		lexer = lexers.Analyse(content)
	}
	if lexer == nil {
		lexer = lexers.Fallback
	}
	lexer = chroma.Coalesce(lexer)
	style := styles.Get("monokai")
	if style == nil {
		style = styles.Fallback
	}
	formatter := formatters.Get("terminal256")
	if formatter == nil {
		formatter = formatters.Fallback
	}
	it, err := lexer.Tokenise(nil, content)
	if err != nil {
		return plain
	}
	var buf strings.Builder
	if err := formatter.Format(&buf, style, it); err != nil {
		return plain
	}
	out := strings.Split(strings.TrimRight(buf.String(), "\n"), "\n")
	if len(out) == 0 {
		return plain
	}
	// Force a reset at every line end. chroma emits multi-line tokens (docstrings,
	// block strings) as a single token, so interior lines carry no reset and the
	// color bleeds across the newline into the next row (e.g. the file tree).
	for i := range out {
		out[i] += "\x1b[0m"
	}
	return out
}

// fileIcon picks an emoji per file extension (2-cell wide for stable alignment).
func fileIcon(name string) string {
	lower := strings.ToLower(name)
	if lower == "dockerfile" || strings.HasPrefix(lower, "dockerfile") {
		return "🐳"
	}
	if strings.Contains(lower, "lock") {
		return "🔒"
	}
	ext := ""
	if i := strings.LastIndex(lower, "."); i >= 0 {
		ext = lower[i+1:]
	}
	switch ext {
	case "py":
		return "🐍"
	case "go":
		return "🐹"
	case "ts", "tsx":
		return "🟦"
	case "js", "jsx", "mjs", "cjs":
		return "🟨"
	case "json":
		return "📋"
	case "md", "mdx", "rst", "txt":
		return "📝"
	case "rs":
		return "🦀"
	case "css", "scss", "sass", "less":
		return "🎨"
	case "html", "htm", "xml":
		return "🌐"
	case "yml", "yaml", "toml", "ini", "conf", "cfg", "env":
		return "🔧"
	case "sh", "bash", "zsh", "ps1", "bat":
		return "📜"
	case "png", "jpg", "jpeg", "gif", "svg", "ico", "webp":
		return "🖼"
	case "pem", "key", "crt", "cert":
		return "🔑"
	}
	return "📄"
}

// padLeft right-aligns s within width w (spaces on the left).
func padLeft(s string, w int) string {
	if len(s) >= w {
		return s
	}
	return strings.Repeat(" ", w-len(s)) + s
}

func mcpName(v any) string {
	if mm, ok := v.(map[string]any); ok {
		if s, ok := mm["name"].(string); ok && s != "" {
			return s
		}
		if s, ok := mm["url"].(string); ok {
			return s
		}
	}
	return "mcp"
}

// mcpAllowed extracts the allowed_tools (callable functions) from a project MCP entry.
func mcpAllowed(v any) []string {
	mm, ok := v.(map[string]any)
	if !ok {
		return nil
	}
	raw, ok := mm["allowed_tools"].([]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(raw))
	for _, x := range raw {
		if s, ok := x.(string); ok {
			out = append(out, s)
		}
	}
	return out
}
