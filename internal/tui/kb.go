package tui

import (
	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/bubbles/table"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"gitlab.com/parendum/nexora/nexora-cli/internal/api"
)

type kbItem struct{ k api.KB }

func (i kbItem) Title() string       { return i.k.Name }
func (i kbItem) Description() string { return itoa(i.k.FileCount) + " files · " + i.k.Description }
func (i kbItem) FilterValue() string { return i.k.Name }

func toKBItems(kbs []api.KB) []list.Item {
	items := make([]list.Item, 0, len(kbs))
	for _, k := range kbs {
		items = append(items, kbItem{k})
	}
	return items
}

func toKBFileRows(files []api.KBFile, th Theme) []table.Row {
	rows := make([]table.Row, 0, len(files))
	for _, f := range files {
		status := lipgloss.NewStyle().Foreground(th.statusColor(f.Status)).Render(f.Status)
		rows = append(rows, table.Row{status, f.Filename, itoa(f.ChunkCount)})
	}
	return rows
}

func (m *model) kbKey(k tea.KeyMsg) (tea.Cmd, bool) {
	// files mode
	if m.currentKB != nil {
		switch k.String() {
		case "esc":
			m.currentKB = nil
			return m.loadKBs(), true
		case "u":
			m.form = newForm("kb_upload:"+m.currentKB.ID, "Upload file", []fieldSpec{
				{key: "path", label: "Local file path", placeholder: "/path/to/file.pdf"},
			})
			return nil, true
		case "i":
			m.form = newForm("kb_url:"+m.currentKB.ID, "Ingest URL", []fieldSpec{
				{key: "url", label: "URL", placeholder: "https://…"},
			})
			return nil, true
		case "r":
			return m.loadKBFiles(m.currentKB.ID), true
		case "d":
			idx := m.kbFileTable.Cursor()
			if idx >= 0 && idx < len(m.kbFiles) {
				f := m.kbFiles[idx]
				m.openConfirm("kbfile_delete", m.currentKB.ID, f.ID, "Delete file \""+f.Filename+"\"?")
			}
			return nil, true
		}
		return nil, false
	}

	// list mode
	if m.kbList.FilterState() == list.Filtering {
		return nil, false
	}
	switch k.String() {
	case "enter":
		if it, ok := m.kbList.SelectedItem().(kbItem); ok {
			kb := it.k
			m.currentKB = &kb
			m.status = "KB: " + kb.Name
			return m.loadKBFiles(kb.ID), true
		}
		return nil, true
	case "n":
		m.form = newForm("kb_new", "New knowledge base", []fieldSpec{
			{key: "name", label: "Name", placeholder: "Docs"},
			{key: "description", label: "Description", placeholder: "(optional)"},
		})
		return nil, true
	case "e":
		if it, ok := m.kbList.SelectedItem().(kbItem); ok {
			m.form = newForm("kb_edit:"+it.k.ID, "Edit "+it.k.Name, []fieldSpec{
				{key: "name", label: "Name", value: it.k.Name},
				{key: "description", label: "Description", value: it.k.Description},
			})
		}
		return nil, true
	case "d":
		if it, ok := m.kbList.SelectedItem().(kbItem); ok {
			m.openConfirm("kb_delete", it.k.ID, "", "Delete knowledge base \""+it.k.Name+"\" and all its files?")
		}
		return nil, true
	}
	return nil, false
}

func (m *model) viewKB() string {
	if m.currentKB != nil {
		header := m.theme.AgentName.Render("📚 "+m.currentKB.Name) + "  " +
			m.theme.Help.Render("[u]pload [i]ngest-url [d]elete [esc]back")
		if len(m.kbFiles) == 0 {
			return header + "\n\n" + lipgloss.NewStyle().Padding(1, 2).Render("No files yet. [u] upload or [i] ingest a URL.")
		}
		return header + "\n" + m.kbFileTable.View()
	}
	if len(m.kbs) == 0 {
		return lipgloss.NewStyle().Padding(1, 2).Render("No knowledge bases.  [n] create one.")
	}
	return m.kbList.View()
}
