package tui

import (
	"encoding/json"

	tea "github.com/charmbracelet/bubbletea"
	"gitlab.com/parendum/nexora/nexora-cli/internal/localexec"
	"gitlab.com/parendum/nexora/nexora-cli/internal/ws"
)

// localReq is a pending tool_exec_request from the server.
type localReq struct {
	id, tool string
	args     json.RawMessage
}

// localCmd is one entry in the local-execution feed shown in the activity panel.
type localCmd struct {
	text   string   // e.g. "shell_run: ls -la"
	state  string   // "run" | "ok" | "err"
	output []string // raw output lines (ground truth — agent prose can't be trusted)
}

// localDoneMsg marks a feed entry done after the host finished running it.
type localDoneMsg struct {
	idx    int
	note   string
	ok     bool
	output []string
}

// pushLocal appends a command to the feed (capped) and returns its index.
func (m *model) pushLocal(text string) int {
	m.localLog = append(m.localLog, localCmd{text: text, state: "run"})
	if len(m.localLog) > 30 {
		m.localLog = m.localLog[len(m.localLog)-30:]
	}
	m.renderTranscript()
	return len(m.localLog) - 1
}

// onToolExecRequest decides how to handle a server request to run a tool on this host:
// refuse if local-exec is off (the CLI is the gatekeeper), auto-run if yolo or the op is
// read-only (allowlist), otherwise prompt for confirmation.
func (m *model) onToolExecRequest(f ws.Frame) tea.Cmd {
	req := localReq{id: f.RequestID, tool: f.Tool, args: f.Args}
	if !m.localExec {
		return m.sendToolResultCmd(req.id, map[string]any{
			"error": "local execution is disabled on this CLI client",
		})
	}
	summary := localexec.Summary(req.tool, req.args)
	if m.localYolo || localexec.ReadOnly(req.tool, req.args) {
		idx := m.pushLocal(summary)
		m.status = "⚡ " + summary
		return m.runLocalTool(req, idx)
	}
	// needs explicit approval
	m.pendingLocal = &req
	m.openConfirm("local_exec", req.id, "",
		"Run this on YOUR machine?\n\n  "+summary+"\n\n  cwd: "+m.localCwd)
	return nil
}

// runLocalTool executes the request on the host and replies to the server.
func (m *model) runLocalTool(req localReq, idx int) tea.Cmd {
	wsc := m.wsClient
	cwd := m.localCwd
	return func() tea.Msg {
		res := localexec.Run(req.tool, req.args, cwd)
		if wsc != nil {
			_ = wsc.SendToolResult(req.id, res)
		}
		note := "ran " + req.tool + " ✓"
		ok := true
		if e, isErr := res["error"].(string); isErr && e != "" {
			note = req.tool + " failed: " + e
			ok = false
		}
		return localDoneMsg{idx: idx, note: note, ok: ok, output: localexec.OutputLines(res)}
	}
}

// sendToolResultCmd replies to the server with a result (e.g. a denial) without running.
func (m *model) sendToolResultCmd(reqID string, res map[string]any) tea.Cmd {
	wsc := m.wsClient
	return func() tea.Msg {
		if wsc != nil {
			_ = wsc.SendToolResult(reqID, res)
		}
		return nil
	}
}
