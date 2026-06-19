package tui

import (
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// formField is one labeled input in a form overlay.
type formField struct {
	key   string
	label string
	input textinput.Model
}

// formModel is a generic multi-field form rendered as a centered overlay.
// It emits formSubmitMsg{kind, values} on submit and formCancelMsg on esc.
type formModel struct {
	kind   string
	title  string
	fields []formField
	focus  int
}

type formSubmitMsg struct {
	kind   string
	values map[string]string
}
type formCancelMsg struct{}

// field spec used to build a form.
type fieldSpec struct {
	key, label, placeholder, value string
	password                       bool
}

func newForm(kind, title string, specs []fieldSpec) *formModel {
	f := &formModel{kind: kind, title: title}
	for i, s := range specs {
		ti := textinput.New()
		ti.Placeholder = s.placeholder
		ti.SetValue(s.value)
		ti.CharLimit = 4000
		ti.Prompt = "› "
		if s.password {
			ti.EchoMode = textinput.EchoPassword
		}
		if i == 0 {
			ti.Focus()
		}
		f.fields = append(f.fields, formField{key: s.key, label: s.label, input: ti})
	}
	return f
}

func (f *formModel) values() map[string]string {
	v := map[string]string{}
	for _, fld := range f.fields {
		v[fld.key] = strings.TrimSpace(fld.input.Value())
	}
	return v
}

func (f *formModel) focusOnly(i int) {
	for j := range f.fields {
		if j == i {
			f.fields[j].input.Focus()
		} else {
			f.fields[j].input.Blur()
		}
	}
}

// update handles a key event; returns a msg to bubble up (submit/cancel) or nil.
func (f *formModel) update(msg tea.KeyMsg) tea.Msg {
	switch msg.String() {
	case "esc":
		return formCancelMsg{}
	case "enter", "ctrl+s":
		// enter on last field (or ctrl+s anywhere) submits; else advance
		if msg.String() == "enter" && f.focus < len(f.fields)-1 {
			f.focus++
			f.focusOnly(f.focus)
			return nil
		}
		return formSubmitMsg{kind: f.kind, values: f.values()}
	case "tab", "down":
		f.focus = (f.focus + 1) % len(f.fields)
		f.focusOnly(f.focus)
		return nil
	case "shift+tab", "up":
		f.focus = (f.focus - 1 + len(f.fields)) % len(f.fields)
		f.focusOnly(f.focus)
		return nil
	}
	var cmd tea.Cmd
	f.fields[f.focus].input, cmd = f.fields[f.focus].input.Update(msg)
	_ = cmd
	return nil
}

func (f *formModel) view(th Theme, width int) string {
	var b strings.Builder
	b.WriteString(th.AgentName.Render(f.title) + "\n\n")
	for i, fld := range f.fields {
		label := th.StatusBar.Render(fld.label)
		if i == f.focus {
			label = lipgloss.NewStyle().Foreground(th.Accent).Bold(true).Render(fld.label)
		}
		b.WriteString(label + "\n" + fld.input.View() + "\n\n")
	}
	b.WriteString(th.Help.Render("[tab] next  [enter] next/submit  [ctrl+s] submit  [esc] cancel"))
	return lipgloss.NewStyle().Width(width).Render(b.String())
}
