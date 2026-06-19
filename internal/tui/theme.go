package tui

import "github.com/charmbracelet/lipgloss"

// Theme is the single source of truth for colors/styles. Reskin the CLI by editing
// only this file (title + palette), per the project's "just change colors" goal.
type Theme struct {
	Accent    lipgloss.Color
	Accent2   lipgloss.Color
	Fg        lipgloss.Color
	Subtle    lipgloss.Color
	Bg        lipgloss.Color
	Good      lipgloss.Color
	Warn      lipgloss.Color
	Bad       lipgloss.Color
	Title     string

	TabActive   lipgloss.Style
	TabInactive lipgloss.Style
	Border      lipgloss.Style
	StatusBar   lipgloss.Style
	UserMsg     lipgloss.Style
	AgentName   lipgloss.Style
	ToolLine    lipgloss.Style
	Help        lipgloss.Style
	Spinner     lipgloss.Style
	UserBlock   lipgloss.Style // faint background card for user turns
	AsstBlock   lipgloss.Style // faint background card for assistant turns
}

// DefaultTheme — Nexora violet.
func DefaultTheme() Theme {
	accent := lipgloss.Color("#7C3AED")
	accent2 := lipgloss.Color("#A78BFA")
	fg := lipgloss.Color("#E5E7EB")
	subtle := lipgloss.Color("#6B7280")
	good := lipgloss.Color("#34D399")
	warn := lipgloss.Color("#FBBF24")
	bad := lipgloss.Color("#F87171")

	t := Theme{
		Accent: accent, Accent2: accent2, Fg: fg, Subtle: subtle,
		Good: good, Warn: warn, Bad: bad, Title: "Nexora",
	}
	t.TabActive = lipgloss.NewStyle().Foreground(lipgloss.Color("#0B0B10")).Background(accent).Bold(true).Padding(0, 1)
	t.TabInactive = lipgloss.NewStyle().Foreground(subtle).Padding(0, 1)
	t.Border = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(subtle)
	t.StatusBar = lipgloss.NewStyle().Foreground(subtle)
	t.UserMsg = lipgloss.NewStyle().Foreground(accent2).Bold(true)
	t.AgentName = lipgloss.NewStyle().Foreground(accent).Bold(true)
	t.ToolLine = lipgloss.NewStyle().Foreground(warn)
	t.Help = lipgloss.NewStyle().Foreground(subtle)
	t.Spinner = lipgloss.NewStyle().Foreground(accent)
	// Faint background cards differentiate user vs assistant (terminal "opacity").
	t.UserBlock = lipgloss.NewStyle().Background(lipgloss.Color("#241B33")).Padding(0, 1)
	t.AsstBlock = lipgloss.NewStyle().Background(lipgloss.Color("#15171E")).Padding(0, 1)
	return t
}

// statusColor maps a task/agent status to a color.
func (t Theme) statusColor(status string) lipgloss.Color {
	switch status {
	case "completed", "done", "ready":
		return t.Good
	case "failed", "dead", "error":
		return t.Bad
	case "in_progress", "running", "queued":
		return t.Warn
	default:
		return t.Subtle
	}
}
