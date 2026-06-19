package tui

import (
	"context"
	"strings"
	"time"

	"github.com/atotto/clipboard"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"gitlab.com/parendum/nexora/nexora-cli/internal/api"
	"gitlab.com/parendum/nexora/nexora-cli/internal/ws"
)

func (m *model) updateChat(msg tea.Msg) (tea.Model, tea.Cmd) {
	if k, ok := msg.(tea.KeyMsg); ok {
		// message-pick (yank) mode captures keys first
		if m.pickMode {
			return m.updatePick(k)
		}
		// slash-autocomplete popup navigation (when visible)
		if m.slashOpen {
			switch k.String() {
			case "up", "ctrl+p":
				if m.slashIdx > 0 {
					m.slashIdx--
				}
				return m, nil
			case "down", "ctrl+n":
				if m.slashIdx < len(m.slashHits)-1 {
					m.slashIdx++
				}
				return m, nil
			case "tab":
				if m.slashIdx < len(m.slashHits) {
					c := m.slashHits[m.slashIdx]
					m.input.SetValue(c.name + " ")
					m.input.CursorEnd()
					m.refreshSlash()
				}
				return m, nil
			case "esc":
				m.slashOpen = false
				return m, nil
			case "enter":
				// run the highlighted command (or complete if it takes an arg)
				if m.slashIdx < len(m.slashHits) {
					c := m.slashHits[m.slashIdx]
					if c.arg {
						m.input.SetValue(c.name + " ")
						m.input.CursorEnd()
						m.refreshSlash()
						return m, nil
					}
					m.slashOpen = false
					m.input.Reset()
					return m.runSlash(c.name)
				}
			}
		}
		switch k.String() {
		case "ctrl+b":
			m.sidebarOpen = !m.sidebarOpen
			if m.sidebarOpen {
				m.clampPanel() // ensure the active panel is visible in the current UI mode
			}
			m.layout()
			m.rebuildBlocks()
			if m.sidebarOpen {
				return m, m.sidebarRefresh()
			}
			return m, nil
		case "ctrl+o":
			if m.sidebarOpen {
				m.cyclePanel(1)
				return m, m.sidebarRefresh()
			}
			return m, nil
		case "ctrl+g":
			// sub-chat navigator: open a spawned sub-agent's chat (read/talk) or go back
			return m, m.openSubChatNav()
		case "up", "ctrl+up":
			// in the Agents (tree) panel ↑↓ move the selection (textinput ignores ↑↓)
			if m.sidebarOpen && m.sidebarPanel == panTree {
				if m.treeSel > 0 {
					m.treeSel--
				}
				m.renderTranscript()
				return m, nil
			}
		case "down", "ctrl+down":
			if m.sidebarOpen && m.sidebarPanel == panTree {
				if m.treeSel < len(m.treeRows())-1 {
					m.treeSel++
				}
				m.renderTranscript()
				return m, nil
			}
		case "ctrl+p":
			if len(m.renderedBlocks) > 0 {
				m.pickMode = true
				m.pickIdx = len(m.renderedBlocks) - 1
				m.input.Blur()
				m.status = "pick a message · ↑↓ move · y/enter copy · esc cancel"
				m.renderTranscript()
			}
			return m, nil
		case "ctrl+l":
			// toggle verbose detail: expands collapsed thinking blocks + raw local output
			m.localExpand = !m.localExpand
			m.rebuildBlocks()
			return m, nil
		case "enter":
			content := strings.TrimSpace(m.input.Value())
			// Empty input + the Agents panel → enter loads the highlighted chat.
			if content == "" && m.sidebarOpen && m.sidebarPanel == panTree {
				return m, m.openTreeSel()
			}
			if content == "" || m.streaming {
				return m, nil
			}
			// slash command?
			if strings.HasPrefix(content, "/") {
				m.slashOpen = false
				m.input.Reset()
				return m.runSlash(content)
			}
			m.input.Reset()
			// no open chat → start a general (agentless or active-agent) chat, queue the message
			if m.currentChat == nil {
				m.pendingSend = content
				m.status = "Starting chat…"
				return m, m.newChat(m.activeAgentID, firstTitle(content))
			}
			m.appendMessage(api.Message{Role: "user", Content: content, UserName: "you"})
			m.startTurn()
			m.renderTranscript()
			return m, m.sendTurn(content)
		case "pgup", "pgdown", "ctrl+u", "ctrl+d":
			var cmd tea.Cmd
			m.transcript, cmd = m.transcript.Update(msg)
			return m, cmd
		case "ctrl+home":
			m.transcript.GotoTop()
			return m, nil
		case "ctrl+end":
			m.transcript.GotoBottom()
			return m, nil
		case "ctrl+y":
			return m, m.copyLastAssistant()
		}
	}
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	m.refreshSlash()
	return m, cmd
}

// refreshSlash recomputes the slash-autocomplete popup from the current input.
func (m *model) refreshSlash() {
	m.slashHits = slashMatches(m.input.Value())
	m.slashOpen = len(m.slashHits) > 0
	if m.slashIdx >= len(m.slashHits) {
		m.slashIdx = 0
	}
}

// updatePick handles keys while in message-pick (yank) mode.
func (m *model) updatePick(k tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch k.String() {
	case "up", "k":
		if m.pickIdx > 0 {
			m.pickIdx--
		}
		m.renderTranscript()
	case "down", "j":
		if m.pickIdx < len(m.renderedBlocks)-1 {
			m.pickIdx++
		}
		m.renderTranscript()
	case "g", "home":
		m.pickIdx = 0
		m.renderTranscript()
	case "G", "end":
		m.pickIdx = len(m.renderedBlocks) - 1
		m.renderTranscript()
	case "y", "enter", "c":
		m.copyBlock(m.pickIdx)
		m.exitPick()
	case "esc", "ctrl+p", "q":
		m.exitPick()
	}
	return m, nil
}

func (m *model) exitPick() {
	m.pickMode = false
	m.input.Focus()
	m.renderTranscript()
}

func (m *model) copyBlock(i int) {
	if i < 0 || i >= len(m.blockTexts) {
		return
	}
	text := m.blockTexts[i]
	if strings.TrimSpace(text) == "" {
		m.status = "nothing to copy"
		return
	}
	if err := clipboard.WriteAll(text); err != nil {
		m.status = "copy failed: " + err.Error()
		return
	}
	m.status = "copied message (" + itoa(len(text)) + " chars)"
}

// copyLastAssistant copies the most recent assistant reply (plain text) to the clipboard.
func (m *model) copyLastAssistant() tea.Cmd {
	text := m.lastAssistantText()
	if text == "" {
		m.status = "nothing to copy"
		return nil
	}
	if err := clipboard.WriteAll(text); err != nil {
		m.status = "copy failed: " + err.Error()
		return nil
	}
	n := len(text)
	m.status = "copied last reply (" + itoa(n) + " chars)"
	return nil
}

// lastAssistantText returns the cleaned text of the last displayable assistant message.
func (m *model) lastAssistantText() string {
	for i := len(m.messages) - 1; i >= 0; i-- {
		if m.messages[i].Role == "assistant" {
			if t, _ := cleanContent(m.messages[i].Content); strings.TrimSpace(t) != "" {
				return t
			}
		}
	}
	return ""
}

func firstTitle(s string) string {
	s = strings.TrimSpace(s)
	if len(s) > 48 {
		s = string([]rune(s)[:48])
	}
	return s
}

// startTurn resets per-turn streaming + activity state for a new user message.
func (m *model) startTurn() {
	m.streaming = true
	m.assistantBuf = ""
	m.toolActions = nil
	m.subAgents = nil
}

// displayable filters out messages the UI should not show (mirrors the web):
// empty assistant turns (tool-only) and <system_observation> tool-result injections.
func displayable(msg api.Message) bool {
	// Internal/injected messages (system_observation, watchdog nudges, tool reminders,
	// child-task injections) are all stored excluded=True — never show them.
	if msg.Excluded {
		return false
	}
	switch msg.Role {
	case "assistant":
		text, _ := cleanContent(msg.Content)
		return strings.TrimSpace(text) != ""
	case "user":
		// content-prefix fallback in case an internal msg wasn't flagged excluded
		c := strings.TrimSpace(msg.Content)
		if strings.HasPrefix(c, "<system_observation") || strings.HasPrefix(c, "[Tool results") ||
			strings.HasPrefix(c, "[WATCHDOG") || strings.HasPrefix(c, "[Resumed") {
			return false
		}
		return true
	default:
		return false // system/tool rows hidden
	}
}

// appendMessage stores a message and regroups blocks. Consecutive assistant turns
// (the model often replies in many small steps) merge under ONE header.
func (m *model) appendMessage(msg api.Message) {
	m.messages = append(m.messages, msg)
	m.rebuildBlocks()
}

// rebuildBlocks turns the message list into display blocks. A user message is one card;
// a run of consecutive visible assistant turns (ignoring hidden system/empty messages
// between them) collapses into a single "● <agent>" block with its bodies stacked.
func (m *model) rebuildBlocks() {
	t := m.theme
	// Sync the markdown renderer to the CURRENT transcript width first. ensureRenderer
	// busts the per-message render cache when the width changed (e.g. the side panel just
	// opened/closed), so cached glamour renders aren't reused at the wrong width and tables
	// stop breaking.
	m.ensureRenderer()
	m.renderedBlocks = m.renderedBlocks[:0]
	m.blockTexts = m.blockTexts[:0]
	i := 0
	for i < len(m.messages) {
		msg := m.messages[i]
		if displayable(msg) && msg.Role == "user" {
			m.renderedBlocks = append(m.renderedBlocks, m.renderUserBlock(msg))
			m.blockTexts = append(m.blockTexts, strings.TrimSpace(msg.Content))
			i++
			continue
		}
		if msg.Role == "assistant" {
			// gather this assistant run until the next real user message
			label := orDefault(msg.AgentName, "assistant")
			type seg struct{ body, raw string }
			var segs []seg
			for i < len(m.messages) {
				mm := m.messages[i]
				if displayable(mm) && mm.Role == "user" {
					break // a real user turn ends the assistant group
				}
				if mm.Role == "assistant" {
					if mm.AgentName != "" {
						label = mm.AgentName
					}
					if b := m.assistantBody(mm); b != "" {
						txt, _ := cleanContent(mm.Content)
						segs = append(segs, seg{body: b, raw: strings.TrimSpace(txt)})
					}
				}
				i++ // skip hidden (system_observation / empty) without breaking the group
			}
			if len(segs) > 0 {
				head := t.AgentName.Render("● " + label)
				// Interim segments (the model's running narration before its final answer)
				// merge into ONE continuous dim-italic blockquote with an unbroken left
				// rail; the LAST segment is the real reply, rendered normally.
				raws := make([]string, 0, len(segs))
				for _, s := range segs {
					raws = append(raws, s.raw)
				}
				var parts []string
				if len(segs) > 1 {
					interim := make([]string, 0, len(segs)-1)
					for _, s := range segs[:len(segs)-1] {
						interim = append(interim, s.raw)
					}
					// finished turn → collapse thinking to a one-liner (ctrl+l expands)
					if m.localExpand {
						parts = append(parts, m.thinkBlock(strings.Join(interim, "\n\n")))
					} else {
						parts = append(parts, m.thinkCollapsed(len(interim)))
					}
				}
				parts = append(parts, segs[len(segs)-1].body)
				m.renderedBlocks = append(m.renderedBlocks, head+"\n"+strings.Join(parts, "\n\n"))
				m.blockTexts = append(m.blockTexts, strings.Join(raws, "\n\n"))
			}
			continue
		}
		i++ // hidden / system message
	}
	if m.pickIdx >= len(m.renderedBlocks) {
		m.pickIdx = len(m.renderedBlocks) - 1
	}
	if m.pickIdx < 0 {
		m.pickIdx = 0
	}
	m.renderTranscript()
}

func (m *model) handleFrame(f ws.Frame) (tea.Model, tea.Cmd) {
	rearm := waitForFrame(m.events)
	switch f.Type {
	case "user_message":
		// A message posted to this chat — show it live. Skip ONLY our own echo (matched by
		// the client_message_id we sent); a message from another client (web, another CLI,
		// telegram), even the same user, must still appear in real time.
		if f.ClientMessageID != "" && m.sentClientIDs[f.ClientMessageID] {
			break
		}
		if strings.TrimSpace(f.Content) == "" {
			break
		}
		dup := false
		for _, mm := range m.messages {
			if f.MessageID != "" && mm.ID == f.MessageID {
				dup = true
				break
			}
		}
		if !dup {
			m.appendMessage(api.Message{ID: f.MessageID, Role: "user", Content: f.Content, UserName: orDefault(f.UserName, "user")})
		}

	case "stream_start":
		m.streaming = true
		m.assistantBuf = ""
		// poll usage live while the turn runs (mirrors web's periodic refetch)
		return m, tea.Batch(rearm, usagePollTick())

	case "chunk":
		m.assistantBuf += f.Content
		m.renderTranscript()

	case "tool_call", "agent_action_start":
		m.upsertTool(f)
		m.renderTranscript()
	case "agent_action_done":
		m.finishTool(f)
		m.renderTranscript()
	case "activity_status":
		if f.Tool != "" {
			m.status = "⚙ " + f.Tool
		} else if f.Status != "" && f.Status != "idle" {
			m.status = f.Status
		} else if f.Status == "idle" {
			m.status = "" // don't leave a stale "idle" pinned in the footer
		}

	case "sub_agent_start":
		m.subAgents = append(m.subAgents, subAgentAct{
			taskID: f.TaskID, subChatID: f.SubChatID,
			name:  orDefault(f.AgentName, "sub-agent"),
			title: f.TaskTitle, status: "running",
		})
		// Auto-open the lateral panel on the Sub-agents tab so the user sees + can open
		// the spawned sub-chats (mirrors the web). Only when it's currently closed.
		if !m.sidebarOpen {
			m.sidebarOpen = true
			m.sidebarPanel = panTree
			m.layout()
		}
		m.renderTranscript()
		if m.currentChat != nil {
			return m, tea.Batch(rearm, m.loadHierarchy(m.currentChat.ID))
		}
	case "sub_agent_step_start":
		m.subStep(f.TaskID, subStep{id: f.StepID, label: orDefault(f.StepLabel, f.StepName), status: "running"})
		m.renderTranscript()
	case "sub_agent_step_done":
		m.subStepDone(f.TaskID, f.StepID, orDefault(f.Status, "success"))
		m.renderTranscript()
	case "sub_agent_done":
		m.subDone(f.TaskID, orDefault(f.Status, "completed"), f.Result)
		m.renderTranscript()

	case "stream_end":
		final := f.Content
		if final == "" {
			final = m.assistantBuf
		}
		name := ""
		if m.currentChat != nil {
			name = m.currentChat.AgentName
		}
		// Only emit a message block when there's real text (tool-only turns clean to empty).
		text, _ := cleanContent(final)
		if strings.TrimSpace(text) != "" {
			m.appendMessage(api.Message{Role: "assistant", Content: final, AgentName: name})
		}
		m.assistantBuf = ""
		// Keep tool/sub-agent activity visible — sub-agents run async past this stream_end.
		m.streaming = false
		m.renderTranscript()
		if m.currentChat != nil {
			cmds := []tea.Cmd{rearm, m.loadTasks(m.currentChat.ID)}
			if m.sidebarOpen {
				cmds = append(cmds, m.sidebarRefresh()) // auto-refresh usage/plan/logs/etc.
			}
			return m, tea.Batch(cmds...)
		}

	case "task_created", "task_updated":
		if m.currentChat != nil {
			return m, tea.Batch(rearm, m.loadTasks(m.currentChat.ID))
		}

	case "tool_exec_request":
		// Server wants a filesystem/shell tool run on THIS host (CLI local-exec).
		if cmd := m.onToolExecRequest(f); cmd != nil {
			return m, tea.Batch(rearm, cmd)
		}
		return m, rearm

	case "error", "tool_parse_error":
		m.status = "error: " + f.Message
		m.streaming = false
		m.renderTranscript()
	}
	return m, rearm
}

// usagePollMsg drives a live refresh of the Usage sidebar panel while a turn streams.
type usagePollMsg struct{}

func usagePollTick() tea.Cmd {
	return tea.Tick(3*time.Second, func(time.Time) tea.Msg { return usagePollMsg{} })
}

// onUsagePoll re-fetches usage and reschedules itself until the stream ends.
func (m *model) onUsagePoll() (tea.Model, tea.Cmd) {
	if !m.streaming || m.currentChat == nil {
		return m, nil // stream done — stop polling
	}
	cmds := []tea.Cmd{usagePollTick()}
	if m.sidebarOpen && m.sidebarPanel == panUsage {
		cmds = append(cmds, m.loadChatUsage(m.currentChat.ID))
	}
	return m, tea.Batch(cmds...)
}

// ── activity mutators ───────────────────────────────────────────────────────────

func (m *model) upsertTool(f ws.Frame) {
	for i := range m.toolActions {
		if m.toolActions[i].groupID != "" && m.toolActions[i].groupID == f.GroupID {
			m.toolActions[i].status = "running"
			return
		}
	}
	m.toolActions = append(m.toolActions, toolAct{groupID: f.GroupID, tool: f.Tool, label: f.Label, status: "running"})
}

func (m *model) finishTool(f ws.Frame) {
	for i := range m.toolActions {
		if m.toolActions[i].groupID == f.GroupID {
			m.toolActions[i].status = orDefault(f.Status, "success")
			return
		}
	}
}

func (m *model) subStep(taskID string, st subStep) {
	for i := range m.subAgents {
		if m.subAgents[i].taskID == taskID {
			m.subAgents[i].steps = append(m.subAgents[i].steps, st)
			return
		}
	}
}

func (m *model) subStepDone(taskID, stepID, status string) {
	for i := range m.subAgents {
		if m.subAgents[i].taskID == taskID {
			for j := range m.subAgents[i].steps {
				if m.subAgents[i].steps[j].id == stepID {
					m.subAgents[i].steps[j].status = status
					return
				}
			}
		}
	}
}

func (m *model) subDone(taskID, status, result string) {
	for i := range m.subAgents {
		if m.subAgents[i].taskID == taskID {
			m.subAgents[i].status = status
			m.subAgents[i].result = result
			return
		}
	}
}

// ── transcript ──────────────────────────────────────────────────────────────────

func (m *model) renderTranscript() {
	t := m.theme

	// Pick mode: render blocks plainly with the selected one highlighted (left accent bar).
	if m.pickMode {
		sel := lipgloss.NewStyle().
			Border(lipgloss.Border{Left: "▌"}, false, false, false, true).
			BorderForeground(t.Accent).PaddingLeft(1)
		var pb strings.Builder
		for i, blk := range m.renderedBlocks {
			if i == m.pickIdx {
				pb.WriteString(sel.Render(blk))
			} else {
				pb.WriteString(lipgloss.NewStyle().PaddingLeft(2).Render(blk))
			}
			if i < len(m.renderedBlocks)-1 {
				pb.WriteString("\n\n")
			}
		}
		m.transcript.SetContent(pb.String())
		// keep the selection roughly in view
		frac := 0.0
		if len(m.renderedBlocks) > 1 {
			frac = float64(m.pickIdx) / float64(len(m.renderedBlocks)-1)
		}
		m.transcript.SetYOffset(int(frac * float64(max(0, m.transcript.TotalLineCount()-m.transcript.Height))))
		return
	}

	act := m.renderActivity()
	blocks := m.renderedBlocks
	var b strings.Builder

	// When the turn is finished and produced a final assistant answer, place the activity
	// panel BEFORE that answer (the sub-agents ran, then the agent replied) — like the web.
	if !m.streaming && act != "" && len(blocks) > 0 && m.lastBlockAssistant() {
		b.WriteString(strings.Join(blocks[:len(blocks)-1], "\n\n"))
		if len(blocks) > 1 {
			b.WriteString("\n\n")
		}
		b.WriteString(act + "\n\n")
		b.WriteString(blocks[len(blocks)-1])
		m.transcript.SetContent(b.String())
		m.transcript.GotoBottom()
		return
	}

	b.WriteString(strings.Join(blocks, "\n\n"))

	if m.streaming {
		if len(blocks) > 0 {
			b.WriteString("\n\n")
		}
		name := "assistant"
		if m.currentChat != nil && m.currentChat.AgentName != "" {
			name = m.currentChat.AgentName
		}
		b.WriteString(t.AgentName.Render("● "+name) + " " + m.spinner.View() + "\n")
		if live := liveDisplay(m.assistantBuf); live != "" {
			b.WriteString(wrap(live, m.transcript.Width) + "\n")
		}
	}
	if act != "" {
		if !m.streaming && len(blocks) > 0 {
			b.WriteString("\n\n")
		}
		b.WriteString(act)
	}

	m.transcript.SetContent(b.String())
	m.transcript.GotoBottom()
}

// lastBlockAssistant reports whether the last displayed message is an assistant turn.
func (m *model) lastBlockAssistant() bool {
	for i := len(m.messages) - 1; i >= 0; i-- {
		if displayable(m.messages[i]) {
			return m.messages[i].Role == "assistant"
		}
	}
	return false
}

// liveDisplay returns the streaming text to show, hiding in-progress tool-call JSON
// (a buffer that is building a bare array / json fence) so it doesn't flash then vanish.
func liveDisplay(buf string) string {
	s, _ := cleanContent(buf)
	trimmed := strings.TrimSpace(s)
	if trimmed == "" {
		return ""
	}
	// Looks like the model is emitting a (still-incomplete) tool-call / decompose payload.
	if strings.HasPrefix(trimmed, "[{") || strings.HasPrefix(trimmed, "[ {") ||
		strings.HasPrefix(trimmed, `{"name"`) || strings.HasPrefix(trimmed, `{ "name"`) ||
		strings.HasPrefix(trimmed, "```json") || strings.HasPrefix(trimmed, "```tool") ||
		strings.HasPrefix(trimmed, "```shell_run") || strings.HasPrefix(trimmed, "<tool_calls") {
		return ""
	}
	return s
}

func (m *model) viewChat() string {
	t := m.theme
	var top string
	if m.currentChat == nil {
		agent := "general chat (no agent)"
		if m.activeAgentID != "" {
			agent = "agent: " + m.agentNameByID(m.activeAgentID)
		}
		top = lipgloss.NewStyle().Padding(1, 2).Render(
			t.AgentName.Render("Start chatting") + " — just type below and press enter.\n\n" +
				t.Help.Render("Routing: ") + agent + "\n" +
				t.Help.Render("Type / for commands · ctrl+b for the side panel") + "\n" +
				t.Help.Render("Or open a past chat from the ") + "sessions" + t.Help.Render(" tab."))
		fill := max(0, m.transcript.Height-lipgloss.Height(top))
		top += strings.Repeat("\n", fill)
	} else {
		top = m.transcript.View()
	}

	// Slash-command autocomplete popup, floated just above the input.
	if m.slashOpen {
		top = m.overlaySlash(top)
	}

	inputW := m.transcript.Width - 2
	main := lipgloss.JoinVertical(lipgloss.Left, top, t.Border.Width(inputW).Render(m.input.View()))

	if m.sidebarOpen {
		return lipgloss.JoinHorizontal(lipgloss.Top, main, m.renderSidebar())
	}
	return main
}

// sidebarWidth is the lateral panel width when open.
func (m *model) sidebarWidth() int {
	if !m.sidebarOpen {
		return 0
	}
	return min(60, max(40, m.width/3))
}

// sidebar panel order — mirrors the web's advanced-mode chat panels.
const (
	panTree = iota // "Agents" — the chat's agent hierarchy tree
	panTasks
	panPlan
	panNotes
	panFiles
	panLogs
	panUsage
	panInfo
)

var sidebarPanels = []string{"Agents", "Tasks", "Plan", "Notes", "Files", "Logs", "Usage", "Info"}

// simplePanels is the reduced sidebar set in "simple" UI mode (mirrors the web: keep the
// agent tree + task tree, drop the advanced-only panels).
var simplePanels = []int{panTree, panTasks, panInfo}

// visiblePanels returns the sidebar panel indices available for the current UI mode.
func (m *model) visiblePanels() []int {
	if m.uiMode == "simple" {
		return simplePanels
	}
	all := make([]int, len(sidebarPanels))
	for i := range sidebarPanels {
		all[i] = i
	}
	return all
}

// clampPanel snaps the active panel to the first visible one if it's hidden in this mode.
func (m *model) clampPanel() {
	vis := m.visiblePanels()
	for _, p := range vis {
		if p == m.sidebarPanel {
			return
		}
	}
	if len(vis) > 0 {
		m.sidebarPanel = vis[0]
	}
}

// cyclePanel moves to the next/prev visible panel.
func (m *model) cyclePanel(dir int) {
	vis := m.visiblePanels()
	idx := 0
	for i, p := range vis {
		if p == m.sidebarPanel {
			idx = i
			break
		}
	}
	m.sidebarPanel = vis[(idx+dir+len(vis))%len(vis)]
}

// sidebarRefresh loads data for the active sidebar panel.
func (m *model) sidebarRefresh() tea.Cmd {
	if m.currentChat == nil {
		return nil
	}
	id := m.currentChat.ID
	switch m.sidebarPanel {
	case panTree, panInfo:
		return m.loadHierarchy(id)
	case panTasks:
		return m.loadTasks(id)
	case panPlan:
		return m.loadPlans(id)
	case panLogs:
		return m.loadLogs(id)
	case panNotes:
		return m.loadNotes(id)
	case panFiles:
		return m.loadChatFiles(id)
	case panUsage:
		return m.loadChatUsage(id)
	}
	return nil
}

func (m *model) loadChatUsage(id string) tea.Cmd {
	c := m.client
	return func() tea.Msg {
		u, err := c.ChatUsage(context.Background(), id)
		if err != nil {
			return errMsg{err}
		}
		return chatUsageMsg{u}
	}
}
func (m *model) loadPlans(id string) tea.Cmd {
	c := m.client
	return func() tea.Msg {
		p, err := c.Plans(context.Background(), id)
		if err != nil {
			return errMsg{err}
		}
		return plansMsg{p}
	}
}
func (m *model) loadLogs(id string) tea.Cmd {
	c := m.client
	return func() tea.Msg {
		l, err := c.Logs(context.Background(), id)
		if err != nil {
			return errMsg{err}
		}
		return logsMsg{l}
	}
}
func (m *model) loadNotes(id string) tea.Cmd {
	c := m.client
	return func() tea.Msg {
		n, err := c.Notes(context.Background(), id)
		if err != nil {
			return errMsg{err}
		}
		return notesMsg{n}
	}
}
func (m *model) loadChatFiles(id string) tea.Cmd {
	c := m.client
	return func() tea.Msg {
		f, err := c.ChatFiles(context.Background(), id)
		if err != nil {
			return errMsg{err}
		}
		return chatFilesMsg{f}
	}
}

// renderSidebar draws the navigable chat lateral panel (Sub-agents / Tasks / Usage),
// mirroring the web's advanced-mode chat panels. ctrl+o cycles panels.
func (m *model) renderSidebar() string {
	t := m.theme
	w := m.sidebarWidth()
	var b strings.Builder

	// compact single-line panel selector: ‹ Panel ›  (n/N within the visible set)
	vis := m.visiblePanels()
	pos := 1
	for i, p := range vis {
		if p == m.sidebarPanel {
			pos = i + 1
			break
		}
	}
	cur := sidebarPanels[m.sidebarPanel]
	sel := lipgloss.NewStyle().Foreground(t.Subtle).Render("‹ ") +
		lipgloss.NewStyle().Foreground(t.Accent).Bold(true).Render(cur) +
		lipgloss.NewStyle().Foreground(t.Subtle).Render(" ›") +
		t.Help.Render("  "+itoa(pos)+"/"+itoa(len(vis))+"  ctrl+o ↹")
	b.WriteString(sel + "\n" + lipgloss.NewStyle().Foreground(t.Subtle).Render(strings.Repeat("─", w-2)) + "\n\n")

	switch m.sidebarPanel {
	case panTree:
		b.WriteString(m.renderTreePanel(w))
	case panInfo:
		b.WriteString(m.renderInfoPanel(w))
	case panTasks:
		if len(m.tasks) == 0 {
			b.WriteString(t.Help.Render("No tasks in this chat."))
		}
		for _, tk := range m.tasks {
			indent := ""
			if tk.ParentID != "" {
				indent = "  "
			}
			line := indent + statusIcon(tk.Status) + " " + truncate(tk.Title, w-6)
			b.WriteString(lipgloss.NewStyle().Foreground(t.statusColor(tk.Status)).Render(line) + "\n")
		}
	case panPlan:
		if len(m.plans) == 0 {
			b.WriteString(t.Help.Render("No plan for this chat."))
		}
		for _, pl := range m.plans {
			b.WriteString(t.AgentName.Render(truncate(pl.Title, w-2)) + " " + t.Help.Render("("+pl.Status+")") + "\n")
			for _, st := range pl.Steps {
				b.WriteString(lipgloss.NewStyle().Foreground(t.statusColor(st.Status)).
					Render("  "+statusIcon(st.Status)+" "+truncate(st.Title, w-6)) + "\n")
			}
			b.WriteString("\n")
		}
	case panLogs:
		if len(m.logs) == 0 {
			b.WriteString(t.Help.Render("No logs."))
		}
		start := 0
		if len(m.logs) > 30 { // show the most recent 30
			start = len(m.logs) - 30
		}
		for _, lg := range m.logs[start:] {
			c := t.Subtle
			switch lg.Level {
			case "error":
				c = t.Bad
			case "warn":
				c = t.Warn
			}
			who := lg.AgentName
			if who != "" {
				who = "[" + who + "] "
			}
			b.WriteString(lipgloss.NewStyle().Foreground(c).Render(truncate(who+lg.Message, w-2)) + "\n")
		}
	case panNotes:
		if len(m.notes) == 0 {
			b.WriteString(t.Help.Render("No notes."))
		}
		for _, n := range m.notes {
			b.WriteString("• " + wrap(n.Content, w-3) + "\n")
			if n.Author != "" {
				b.WriteString(t.Help.Render("  — "+n.Author) + "\n")
			}
		}
	case panFiles:
		if len(m.chatFiles) == 0 {
			b.WriteString(t.Help.Render("No attachments."))
		}
		for _, f := range m.chatFiles {
			b.WriteString("📎 " + truncate(f.Name, w-4) + "\n" +
				t.Help.Render("   "+humanBytes(f.SizeBytes)) + "\n")
		}
	case panUsage:
		u := m.chatUsage
		if u == nil {
			b.WriteString(t.Help.Render("Loading usage…"))
		} else {
			b.WriteString(t.AgentName.Render("Tokens") + "\n")
			b.WriteString("  in:  " + itoa(u.InputTokens) + "\n")
			b.WriteString("  out: " + itoa(u.OutputTokens) + "\n")
			b.WriteString("  tools: " + itoa(u.ToolCalls) + "\n\n")
			b.WriteString(t.AgentName.Render("Routing") + "\n")
			agent := "general"
			if m.activeAgentID != "" {
				agent = m.agentNameByID(m.activeAgentID)
			}
			b.WriteString("  agent: " + truncate(agent, w-9) + "\n")
			b.WriteString("  model: " + truncate(orDefault(m.activeModel, "default"), w-9) + "\n\n")
			if len(u.ByProvider) > 0 {
				b.WriteString(t.AgentName.Render("Providers") + "\n")
				for _, p := range u.ByProvider {
					b.WriteString("  " + truncate(p.Provider, w-14) + " " + itoa(p.InputTokens) + "/" + itoa(p.OutputTokens) + "\n")
				}
			}
		}
	}

	box := lipgloss.NewStyle().
		Border(lipgloss.NormalBorder(), false, false, false, true).
		BorderForeground(t.Subtle).
		Width(w).Height(m.transcript.Height + 2).PaddingLeft(1)
	return box.Render(b.String())
}

// overlaySlash renders the autocomplete popup over the bottom of the transcript area.
func (m *model) overlaySlash(background string) string {
	t := m.theme
	var rows []string
	for i, c := range m.slashHits {
		name := lipgloss.NewStyle().Foreground(t.Accent).Bold(true).Render(c.name)
		line := name + "  " + t.Help.Render(c.desc)
		if i == m.slashIdx {
			line = lipgloss.NewStyle().Foreground(t.Accent2).Render("▸ ") + line
		} else {
			line = "  " + line
		}
		rows = append(rows, line)
	}
	popup := t.Border.BorderForeground(t.Accent).Render(strings.Join(rows, "\n"))
	// place the popup near the bottom of the transcript region
	lines := strings.Split(background, "\n")
	ph := lipgloss.Height(popup)
	if len(lines) > ph+1 {
		// overwrite the last ph lines of the background with the popup
		keep := lines[:len(lines)-ph]
		return strings.Join(keep, "\n") + "\n" + popup
	}
	return background + "\n" + popup
}

func (m *model) agentNameByID(id string) string {
	for _, a := range m.agents {
		if a.ID == id {
			return a.Name
		}
	}
	return id
}

// wrap hard-wraps text to width (ANSI-safe via lipgloss).
func wrap(s string, width int) string {
	if width <= 0 {
		return s
	}
	return lipgloss.NewStyle().Width(width).Render(s)
}

func humanBytes(n int) string {
	switch {
	case n >= 1<<20:
		return itoa(n>>20) + " MB"
	case n >= 1<<10:
		return itoa(n>>10) + " KB"
	default:
		return itoa(n) + " B"
	}
}

func orDefault(s, def string) string {
	if strings.TrimSpace(s) == "" {
		return def
	}
	return s
}

var _ = textinput.Blink
