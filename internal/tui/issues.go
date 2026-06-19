package tui

import (
	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"gitlab.com/parendum/nexora/nexora-cli/internal/api"
)

type issueItem struct{ i api.Issue }

func (it issueItem) Title() string {
	return "[" + it.i.Priority + "] " + it.i.Title
}
func (it issueItem) Description() string {
	d := it.i.Status
	if it.i.ProjectName != "" {
		d += " · " + it.i.ProjectName
	}
	if it.i.AssignedAgent != "" {
		d += " · @" + it.i.AssignedAgent
	}
	if it.i.CommentCount > 0 {
		d += " · 💬" + itoa(it.i.CommentCount)
	}
	return d
}
func (it issueItem) FilterValue() string { return it.i.Title }

func toIssueItems(issues []api.Issue) []list.Item {
	items := make([]list.Item, 0, len(issues))
	for _, i := range issues {
		items = append(items, issueItem{i})
	}
	return items
}

func (m *model) selectedIssue() (api.Issue, bool) {
	it, ok := m.issueList.SelectedItem().(issueItem)
	return it.i, ok
}

func (m *model) issuesKey(k tea.KeyMsg) (tea.Cmd, bool) {
	if m.issueList.FilterState() == list.Filtering {
		return nil, false
	}
	switch k.String() {
	case "r":
		return tea.Batch(m.loadIssues(), m.loadProjects()), true
	case "n":
		m.openIssueForm()
		return nil, true
	case "e":
		if i, ok := m.selectedIssue(); ok {
			m.form = newForm("issue_edit:"+i.ID, "Edit issue", []fieldSpec{
				{key: "title", label: "Title", value: i.Title},
				{key: "description", label: "Description", value: i.Description},
				{key: "priority", label: "Priority", value: i.Priority},
				{key: "status", label: "Status (open/in_progress/review/closed)", value: i.Status},
			})
		}
		return nil, true
	case "d":
		if i, ok := m.selectedIssue(); ok {
			m.openConfirm("issue_delete", i.ID, "", "Delete issue \""+i.Title+"\"?")
		}
		return nil, true
	case "c":
		if i, ok := m.selectedIssue(); ok {
			return m.setIssueStatus(i.ID, "closed"), true
		}
	case "o":
		if i, ok := m.selectedIssue(); ok {
			return m.setIssueStatus(i.ID, "open"), true
		}
	}
	return nil, false
}

func (m *model) setIssueStatus(id, status string) tea.Cmd {
	c := m.client
	return func() tea.Msg {
		if _, err := c.UpdateIssue(ctxBg(), id, api.IssueInput{Status: status}); err != nil {
			return errMsg{err}
		}
		return chainCmd(okMsg{"Issue → " + status}, m.loadIssues())
	}
}

func (m *model) openIssueForm() {
	// default to the first project; user can edit the id field
	proj := ""
	if len(m.projects) > 0 {
		proj = m.projects[0].ID
	}
	m.form = newForm("issue_new", "New issue", []fieldSpec{
		{key: "title", label: "Title", placeholder: "Issue title"},
		{key: "description", label: "Description", placeholder: "(optional)"},
		{key: "priority", label: "Priority", placeholder: "medium", value: "medium"},
		{key: "project_id", label: "Project ID", placeholder: "required", value: proj},
	})
}

func (m *model) viewIssues() string {
	if len(m.issues) == 0 {
		return lipgloss.NewStyle().Padding(1, 2).Render("No issues.  [n] create one (needs a project).")
	}
	return m.issueList.View()
}
