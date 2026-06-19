package tui

import (
	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"gitlab.com/parendum/nexora/nexora-cli/internal/api"
)

type scheduleItem struct{ s api.Schedule }

func (it scheduleItem) Title() string {
	dot := "○"
	if it.s.IsActive {
		dot = "●"
	}
	return dot + " " + it.s.Name
}
func (it scheduleItem) Description() string {
	trig := it.s.CronExpr
	if trig == "" && it.s.IntervalMinutes > 0 {
		trig = "every " + itoa(it.s.IntervalMinutes) + "m"
	}
	d := trig
	if it.s.NextRunAt != "" && it.s.IsActive {
		d += " · next " + it.s.NextRunAt
	}
	return d
}
func (it scheduleItem) FilterValue() string { return it.s.Name }

func toScheduleItems(scheds []api.Schedule) []list.Item {
	items := make([]list.Item, 0, len(scheds))
	for _, s := range scheds {
		items = append(items, scheduleItem{s})
	}
	return items
}

func (m *model) selectedSchedule() (api.Schedule, bool) {
	it, ok := m.scheduleList.SelectedItem().(scheduleItem)
	return it.s, ok
}

func (m *model) schedulesKey(k tea.KeyMsg) (tea.Cmd, bool) {
	if m.scheduleList.FilterState() == list.Filtering {
		return nil, false
	}
	switch k.String() {
	case "r":
		return m.loadSchedules(), true
	case "n":
		m.form = newForm("schedule_new", "New schedule", []fieldSpec{
			{key: "name", label: "Name", placeholder: "Nightly report"},
			{key: "prompt", label: "Prompt", placeholder: "What the agent should do"},
			{key: "cron_expr", label: "Cron (or leave blank)", placeholder: "0 9 * * *"},
			{key: "interval_minutes", label: "Interval minutes (if no cron)", placeholder: "60"},
			{key: "agent_id", label: "Agent ID", placeholder: "(optional)"},
		})
		return nil, true
	case "e":
		if s, ok := m.selectedSchedule(); ok {
			iv := ""
			if s.IntervalMinutes > 0 {
				iv = itoa(s.IntervalMinutes)
			}
			m.form = newForm("schedule_edit:"+s.ID, "Edit schedule", []fieldSpec{
				{key: "name", label: "Name", value: s.Name},
				{key: "prompt", label: "Prompt", value: s.Prompt},
				{key: "cron_expr", label: "Cron (or blank)", value: s.CronExpr},
				{key: "interval_minutes", label: "Interval minutes", value: iv},
				{key: "agent_id", label: "Agent ID", value: s.AgentID},
			})
		}
		return nil, true
	case " ":
		if s, ok := m.selectedSchedule(); ok {
			return m.toggleSchedule(s), true
		}
	case "t":
		if s, ok := m.selectedSchedule(); ok {
			c := m.client
			return func() tea.Msg {
				if err := c.TriggerSchedule(ctxBg(), s.ID); err != nil {
					return errMsg{err}
				}
				return okMsg{"Triggered " + s.Name}
			}, true
		}
	case "d":
		if s, ok := m.selectedSchedule(); ok {
			m.openConfirm("schedule_delete", s.ID, "", "Delete schedule \""+s.Name+"\"?")
		}
		return nil, true
	}
	return nil, false
}

func (m *model) toggleSchedule(s api.Schedule) tea.Cmd {
	c := m.client
	want := !s.IsActive
	return func() tea.Msg {
		if err := c.ActivateSchedule(ctxBg(), s.ID, want); err != nil {
			return errMsg{err}
		}
		state := "deactivated"
		if want {
			state = "activated"
		}
		return chainCmd(okMsg{s.Name + " " + state}, m.loadSchedules())
	}
}

func (m *model) viewSchedules() string {
	if len(m.schedules) == 0 {
		return lipgloss.NewStyle().Padding(1, 2).Render("No schedules.  [n] create one.")
	}
	return m.scheduleList.View()
}
