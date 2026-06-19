package tui

import (
	"encoding/json"
	"regexp"
	"strings"

	"github.com/charmbracelet/glamour"
	"github.com/charmbracelet/glamour/styles"
	"github.com/charmbracelet/lipgloss"
	"gitlab.com/parendum/nexora/nexora-cli/internal/api"
)

// ── marker stripping (mirrors the web frontend's message cleaning) ───────────────

var (
	reThinking    = regexp.MustCompile(`(?is)<(thinking|think)>(.*?)</(thinking|think)>`)
	reProposal    = regexp.MustCompile(`(?is)<proposal>.*?</proposal>`)
	reInternal    = regexp.MustCompile(`(?is)<\s*(analysis_thought|internal_thought|scratchpad)\s*>.*?<\s*/\s*(analysis_thought|internal_thought|scratchpad)\s*>`)
	reFinal       = regexp.MustCompile(`(?i)<\s*final\s*/?\s*>`)
	reToolFence   = regexp.MustCompile("(?is)```[ \t]*(tool_calls|tools|json)[ \t]*\n.*?```")
	reToolXML     = regexp.MustCompile(`(?is)<tool_calls>.*?</tool_calls>`)
	// A message whose entire body is a JSON tool-call array — the model emitted tool calls as
	// bare JSON instead of using the protocol. Two shapes seen in the wild:
	//   [{"name":"task_create","args":{…}}, …]      ← canonical tool-call list
	//   [{"title":…,"task":…,"skills":[…]}, …]      ← task-decompose list
	reBareToolArr = regexp.MustCompile(`(?s)^\s*\[\s*\{.*?("name"\s*:\s*"[\w_]+"\s*,\s*"args"|"task"|"tool_name"|"skills").*\}\s*\]\s*$`)
	// A single bare tool-call object {"name":"…","args":{…}}.
	reBareToolObj = regexp.MustCompile(`(?s)^\s*\{\s*"name"\s*:\s*"[\w_]+"\s*,\s*"args"\s*:.*\}\s*$`)
	// A fenced tool block for any builtin tool, e.g. ```shell_run … ``` / ```task_create … ```.
	reToolNameFence = regexp.MustCompile("(?is)```[ \t]*(shell_run|task_create|task_update|log_entry|memory_manage|agent_[a-z_]+|plan_[a-z_]+|platform_[a-z_]+)[ \t]*\\n.*?```")
	reEchoHeader  = regexp.MustCompile(`(?m)^\*\*[\w_]+\*\*:[ \t]*$`)
	reOrphanFence = regexp.MustCompile("(?m)^```\\s*$")
	reSysObs      = regexp.MustCompile(`(?is)<system_observation>.*?</system_observation>`)
	reLeakPrefix  = regexp.MustCompile(`(?m)^\s*\[(Tool results|Resumed)[^\]]*\]\s*$`)
)

// cleanContent strips protocol markers/scaffolding from an assistant message and
// returns the display text plus any extracted reasoning blocks.
func cleanContent(s string) (text string, reasoning []string) {
	for _, mtch := range reThinking.FindAllStringSubmatch(s, -1) {
		if len(mtch) >= 3 {
			r := strings.TrimSpace(mtch[2])
			if r != "" {
				reasoning = append(reasoning, r)
			}
		}
	}
	s = reThinking.ReplaceAllString(s, "")
	s = reProposal.ReplaceAllString(s, "")
	s = reInternal.ReplaceAllString(s, "")
	s = reSysObs.ReplaceAllString(s, "")
	s = reToolXML.ReplaceAllString(s, "")
	s = reToolFence.ReplaceAllString(s, "")
	s = reToolNameFence.ReplaceAllString(s, "")
	s = reFinal.ReplaceAllString(s, "")
	s = reEchoHeader.ReplaceAllString(s, "")
	s = reOrphanFence.ReplaceAllString(s, "")
	s = reLeakPrefix.ReplaceAllString(s, "")
	// Remove bare tool-call JSON the model emitted instead of using the protocol fence —
	// ANYWHERE in the message, not just whole-message (it often interleaves several
	// tool-call arrays with its reasoning prose).
	s = stripBareToolCalls(s)
	s = dedentProse(s)
	// collapse any run of blank lines to a single blank line (tight paragraphs)
	s = regexp.MustCompile(`\n[ \t]*\n[ \t\n]*`).ReplaceAllString(s, "\n\n")
	return strings.TrimSpace(s), reasoning
}

// dedentProse strips accidental leading indentation the model sometimes emits (e.g.
// "  Hecho…") so text sits flush at column 0. Lines inside fenced code blocks are left
// untouched.
func dedentProse(s string) string {
	lines := strings.Split(s, "\n")
	inFence := false
	for i, ln := range lines {
		trimmed := strings.TrimSpace(ln)
		if strings.HasPrefix(trimmed, "```") {
			inFence = !inFence
			lines[i] = trimmed
			continue
		}
		if inFence {
			continue
		}
		lines[i] = strings.TrimLeft(ln, " \t")
	}
	return strings.Join(lines, "\n")
}

// reToolCallSig matches the tool-call signature `"name":"<tool>","args":` anywhere — used
// for the tolerant structural check (the payload often contains unescaped Windows paths
// like C:\Users which make it INVALID JSON, so a strict parse fails).
var reToolCallSig = regexp.MustCompile(`"name"\s*:\s*"[\w_]+"\s*,\s*"args"\s*:`)

// stripBareToolCalls removes every bare tool-call JSON array/object embedded in the text
// (the model sometimes emits multiple tool calls interleaved with its prose instead of
// using the protocol fence). Bracket-matching respects JSON string literals.
func stripBareToolCalls(s string) string {
	var b strings.Builder
	i := 0
	for i < len(s) {
		c := s[i]
		if c == '[' || c == '{' {
			if end := matchJSONSpan(s, i); end > i {
				cand := strings.TrimSpace(s[i:end])
				if isBareToolCallJSON(cand) {
					i = end
					// also swallow a trailing newline left behind so we don't leave a gap
					if i < len(s) && s[i] == '\n' {
						i++
					}
					continue
				}
			}
		}
		b.WriteByte(c)
		i++
	}
	return b.String()
}

// matchJSONSpan returns the index just past the bracket that matches the one at start
// (handles nested [] {} and ignores brackets inside JSON strings). -1 if unbalanced.
func matchJSONSpan(s string, start int) int {
	depth, inStr, esc := 0, false, false
	for i := start; i < len(s); i++ {
		c := s[i]
		if inStr {
			switch {
			case esc:
				esc = false
			case c == '\\':
				esc = true
			case c == '"':
				inStr = false
			}
			continue
		}
		switch c {
		case '"':
			inStr = true
		case '[', '{':
			depth++
		case ']', '}':
			depth--
			if depth == 0 {
				return i + 1
			}
		}
	}
	return -1
}

// isBareToolCallJSON reports whether the whole message is a tool-call list/object the model
// emitted instead of using the protocol fence. Matches:
//   [{"name":"task_create","args":{…}}, …]   ·   {"name":"…","args":{…}}
//   [{"title":…,"task":…}, …]                 (task-decompose list)
// Tolerant of invalid JSON (unescaped backslashes in Windows paths) via a structural fallback.
func isBareToolCallJSON(s string) bool {
	if len(s) < 2 || (s[0] != '[' && s[0] != '{') {
		return false
	}
	if !strings.HasSuffix(s, "]") && !strings.HasSuffix(s, "}") {
		return false
	}
	looksLikeCall := func(m map[string]any) bool {
		if _, ok := m["name"]; ok {
			if _, ok2 := m["args"]; ok2 {
				return true
			}
		}
		if _, ok := m["tool_name"]; ok {
			return true
		}
		if _, ok := m["task"]; ok {
			return true
		}
		return false
	}
	// strict parse first (handles valid JSON precisely)
	if s[0] == '[' {
		var arr []map[string]any
		if err := json.Unmarshal([]byte(s), &arr); err == nil && len(arr) > 0 {
			for _, m := range arr {
				if !looksLikeCall(m) {
					return false
				}
			}
			return true
		}
	} else {
		var obj map[string]any
		if err := json.Unmarshal([]byte(s), &obj); err == nil {
			return looksLikeCall(obj)
		}
	}
	// tolerant fallback: invalid JSON but clearly a tool-call payload (e.g. unescaped paths)
	return reToolCallSig.MatchString(s)
}

// ── activity (tool calls + sub-agents) ──────────────────────────────────────────

type toolAct struct{ groupID, tool, label, status string }

type subStep struct{ id, label, status string }

type subAgentAct struct {
	taskID, subChatID, name, title, status, result string
	steps                                          []subStep
}

func statusIcon(status string) string {
	switch status {
	case "success", "completed", "done", "ready", "ok":
		return "✓"
	case "failed", "error", "dead":
		return "✗"
	case "running", "in_progress", "executing_tool", "pending", "":
		return "⟳"
	default:
		return "•"
	}
}

// compactLines trims leading/trailing blank lines and collapses interior blank runs to
// a single blank — PowerShell Format-Table output is mostly padding, which otherwise
// renders as huge gaps in the feed.
var reWideGap = regexp.MustCompile(`[ \t]{3,}`)

func compactLines(lines []string) []string {
	out := make([]string, 0, len(lines))
	prevBlank := false
	for _, ln := range lines {
		blank := strings.TrimSpace(ln) == ""
		if blank && (prevBlank || len(out) == 0) {
			continue // skip leading + consecutive blanks
		}
		// PowerShell Format-Table / Out-String -Width pads columns to thousands of cols;
		// collapse long gaps to 2 spaces so rows fit on one feed line.
		clean := reWideGap.ReplaceAllString(strings.TrimRight(ln, " \t"), "  ")
		out = append(out, clean)
		prevBlank = blank
	}
	for len(out) > 0 && strings.TrimSpace(out[len(out)-1]) == "" {
		out = out[:len(out)-1]
	}
	return out
}

// ── markdown ────────────────────────────────────────────────────────────────────

func (m *model) ensureRenderer() {
	w := m.transcript.Width
	if w <= 0 {
		w = 80
	}
	if m.md != nil && m.mdWidth == w {
		return
	}
	// Use the dark style but with ZERO document margin so markdown content sits flush at
	// column 0 — same left edge as plain-prose (tokenWrap) replies. The default 2-col
	// margin made backtick-containing replies look indented vs plain ones.
	st := styles.DarkStyleConfig
	zero := uint(0)
	st.Document.Margin = &zero
	r, err := glamour.NewTermRenderer(glamour.WithStyles(st), glamour.WithWordWrap(w))
	if err == nil {
		m.md = r
		m.mdWidth = w
		m.mdCache = map[string]string{} // width changed → cached renders are stale
	}
}

func (m *model) renderMarkdown(s string) string {
	w := m.transcript.Width
	// Plain prose (no markdown structure) → token-safe wrap so long filenames/paths are
	// never split mid-word (glamour's word-wrap breaks at '.' inside "name.ext").
	if !looksLikeMarkdown(s) {
		return tokenWrap(s, w)
	}
	m.ensureRenderer()
	if m.md == nil {
		return wrap(s, w)
	}
	out, err := m.md.Render(s)
	if err != nil {
		return wrap(s, w)
	}
	return strings.TrimSpace(out)
}

var reMarkdownSig = regexp.MustCompile("(?m)(^```|^\\s*[-*+] |^\\s*\\d+\\. |^#{1,6} |^\\s*\\||\\*\\*|`[^`]+`|\\[.+\\]\\(.+\\))")

// looksLikeMarkdown reports whether text has structure worth handing to glamour.
func looksLikeMarkdown(s string) bool { return reMarkdownSig.MatchString(s) }

// tokenWrap wraps on spaces, never splitting a single token (filename/path/URL). Existing
// newlines are preserved (each line wrapped independently).
func tokenWrap(s string, width int) string {
	if width <= 0 {
		width = 80
	}
	var out []string
	for _, line := range strings.Split(s, "\n") {
		words := strings.Fields(line)
		if len(words) == 0 {
			out = append(out, "")
			continue
		}
		cur := ""
		for _, wd := range words {
			switch {
			case cur == "":
				cur = wd
			case len(cur)+1+len(wd) <= width:
				cur += " " + wd
			default:
				out = append(out, cur)
				cur = wd
			}
		}
		if cur != "" {
			out = append(out, cur)
		}
	}
	return strings.Join(out, "\n")
}

// thinkBlock renders an interim narration segment as a dim italic blockquote (a left bar +
// gray italic text), distinct from the final answer. raw is the cleaned plain text.
func (m *model) thinkBlock(raw string) string {
	t := m.theme
	if strings.TrimSpace(raw) == "" {
		return ""
	}
	bar := lipgloss.NewStyle().Foreground(t.Subtle)
	txt := lipgloss.NewStyle().Foreground(t.Subtle).Italic(true)
	w := max(20, m.transcript.Width-2)
	var b strings.Builder
	for _, ln := range strings.Split(tokenWrap(raw, w), "\n") {
		if strings.TrimSpace(ln) == "" {
			b.WriteString(bar.Render("▎") + "\n") // unbroken left rail across blank lines
		} else {
			b.WriteString(bar.Render("▎ ") + txt.Render(ln) + "\n")
		}
	}
	return strings.TrimRight(b.String(), "\n")
}

// thinkCollapsed renders the folded one-line summary of a finished thinking block.
func (m *model) thinkCollapsed(steps int) string {
	t := m.theme
	bar := lipgloss.NewStyle().Foreground(t.Subtle)
	label := "thinking"
	if steps > 1 {
		label = "thinking · " + itoa(steps) + " steps"
	}
	return bar.Render("▎ ") + lipgloss.NewStyle().Foreground(t.Subtle).Italic(true).Render(label) +
		t.Help.Render("  (ctrl+l)")
}

// renderUserBlock renders a user turn as a faint background card.
func (m *model) renderUserBlock(msg api.Message) string {
	t := m.theme
	w := max(20, m.transcript.Width)
	name := m.userName
	if name == "" {
		name = orDefault(msg.UserName, "you")
	}
	head := t.UserMsg.Render(name)
	body := wrap(msg.Content, w-2)
	return t.UserBlock.Width(w).Render(head + "\n" + body)
}

// assistantBody returns the cleaned, markdown-rendered body for one assistant message
// (no header, no background) — empty when the turn was tool-only. Reasoning count is
// appended as a faint note. Cached per content for fast regrouping.
func (m *model) assistantBody(msg api.Message) string {
	text, reasoning := cleanContent(msg.Content)
	if strings.TrimSpace(text) == "" {
		return ""
	}
	key := text
	body, ok := m.mdCache[key]
	if !ok {
		body = m.renderMarkdown(text)
		m.mdCache[key] = body
	}
	if len(reasoning) > 0 {
		body = m.theme.Help.Render("("+itoa(len(reasoning))+" reasoning hidden)") + "\n" + body
	}
	return body
}

// renderActivity renders the tool/sub-agent panel. Persists across stream_ends within a
// turn (sub-agents run async), cleared when the next user message starts.
func (m *model) renderActivity() string {
	if len(m.toolActions) == 0 && len(m.subAgents) == 0 && len(m.localLog) == 0 {
		return ""
	}
	t := m.theme
	w := m.transcript.Width
	var b strings.Builder
	b.WriteString(t.Help.Render("─ activity ──────────") + "\n")
	// local-execution feed: the actual commands run on THIS host + their raw output
	// (ground truth — the agent's prose can hallucinate, the raw output can't).
	for _, lc := range m.localLog {
		icon := "⟳"
		if lc.state == "ok" {
			icon = "✓"
		} else if lc.state == "err" {
			icon = "✗"
		}
		// One line per command. The command text is the whole row; raw output stays
		// hidden until ctrl+l (the agent's reply already presents the result cleanly).
		out := compactLines(lc.output)
		suffix := ""
		if len(out) > 0 && !m.localExpand {
			suffix = t.Help.Render("  (" + itoa(len(out)) + " lines · ctrl+l)")
		}
		b.WriteString(lipgloss.NewStyle().Foreground(t.Accent2).Render("  ⚡ "+icon+"  "+truncate(lc.text, max(10, w-24))) + suffix + "\n")
		if m.localExpand && len(out) > 0 {
			shown := out
			if len(shown) > 400 {
				shown = shown[:400]
			}
			for _, ln := range shown {
				b.WriteString(t.Help.Render("       │ "+truncate(ln, max(10, w-12))) + "\n")
			}
			if len(out) > 400 {
				b.WriteString(t.Help.Render("       │ … "+itoa(len(out)-400)+" more") + "\n")
			}
		}
	}
	for _, a := range m.toolActions {
		lbl := a.label
		if lbl == "" {
			lbl = a.tool
		}
		b.WriteString(t.ToolLine.Render("  ⚙  "+statusIcon(a.status)+"  "+lbl) + "\n")
	}
	for _, sa := range m.subAgents {
		// Ad-hoc sub-agents are named "<base> · <task-title>", but the agent may be
		// reused across tasks so that embedded title is stale. Show the base name +
		// the CURRENT task title instead.
		base := sa.name
		if idx := strings.Index(base, " · "); idx >= 0 {
			base = base[:idx]
		}
		label := base
		if sa.title != "" && sa.title != base {
			label = base + " · " + sa.title
		}
		head := lipgloss.NewStyle().Foreground(t.Accent2).Render("  🤖  " + statusIcon(sa.status) + "  " + label)
		b.WriteString(head + "\n")
		// Collapse steps once the sub-agent is done (mirrors the web/Telegram) — show result only.
		if isTerminal(sa.status) {
			if sa.result != "" {
				res, _ := cleanContent(sa.result)
				if res != "" {
					b.WriteString(t.StatusBar.Render("        ↳  "+truncate(oneLine(res), max(10, w-12))) + "\n")
				}
			}
			continue
		}
		for _, st := range sa.steps {
			b.WriteString(t.StatusBar.Render("        "+statusIcon(st.status)+"  "+st.label) + "\n")
		}
	}
	return strings.TrimRight(b.String(), "\n")
}

func isTerminal(status string) bool {
	switch status {
	case "completed", "done", "failed", "error", "dead":
		return true
	}
	return false
}

func oneLine(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

