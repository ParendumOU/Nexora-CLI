// Package localexec runs a proxied tool call on the CLI host machine.
//
// When a chat opts into local execution, the backend sends `tool_exec_request` frames
// instead of running filesystem/shell builtins in its container. This package executes
// them here (in the CLI's working directory) and produces a result dict whose shape
// matches the backend's own builtin executors, so the agent sees identical output.
package localexec

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"
)

const (
	shellTimeout = 30 * time.Second
	maxShellOut  = 200000 // ~200 KB inline; overflow spills to a temp file (never silently cut)
	maxFileBytes = 2 << 20 // 2 MiB cap on read/write payloads
)

// Tools is the set this client can execute. The backend proxies exactly these names.
var Tools = map[string]bool{
	"shell_run": true, "file_read": true, "file_write": true, "file_list": true,
}

// Summary returns a short, human-readable one-line preview of a request (for the
// confirmation prompt). e.g. `shell_run: ls -la` or `file_write: ./notes.txt`.
func Summary(tool string, args json.RawMessage) string {
	m := parse(args)
	switch tool {
	case "shell_run":
		return "shell_run: " + str(m, "command")
	case "file_read":
		return "file_read: " + str(m, "path")
	case "file_write":
		return "file_write: " + str(m, "path")
	case "file_list":
		return "file_list: " + orDef(str(m, "path"), ".")
	}
	return tool
}

// ReadOnly reports whether a request only reads (never mutates the host) — used by the
// allowlist so safe ops can auto-run even in confirm mode.
func ReadOnly(tool string, args json.RawMessage) bool {
	switch tool {
	case "file_read", "file_list":
		return true
	case "shell_run":
		return shellIsReadOnly(str(parse(args), "command"))
	}
	return false
}

// safeShellPrefixes are commands considered read-only/non-destructive.
var safeShellPrefixes = []string{
	"ls", "dir", "pwd", "cat", "type", "echo", "whoami", "hostname", "date",
	"head", "tail", "wc", "find", "tree", "stat", "file", "which", "where",
	"env", "printenv", "uname", "ver", "id", "df", "du", "ps", "git status",
	"git log", "git diff", "git branch", "git show",
}

func shellIsReadOnly(cmd string) bool {
	c := strings.TrimSpace(strings.ToLower(cmd))
	if c == "" {
		return false
	}
	// Reject anything with chaining/redirection/pipes that could hide a write.
	if strings.ContainsAny(c, ">&|;`") || strings.Contains(c, "$(") || strings.Contains(c, "&&") {
		return false
	}
	for _, p := range safeShellPrefixes {
		if c == p || strings.HasPrefix(c, p+" ") {
			return true
		}
	}
	return false
}

// OutputLines extracts human-readable output lines from a result dict for the TUI feed
// (ground truth the user can trust, regardless of how the agent later paraphrases it).
func OutputLines(res map[string]any) []string {
	if e, ok := res["error"].(string); ok && e != "" {
		return []string{"error: " + e}
	}
	d, ok := res["data"].(map[string]any)
	if !ok {
		return nil
	}
	switch d["type"] {
	case "directory":
		if entries, ok := d["entries"].([]any); ok {
			out := make([]string, 0, len(entries))
			for _, e := range entries {
				if s, ok := e.(string); ok {
					out = append(out, s)
				}
			}
			return out
		}
	case "file":
		if c, ok := d["content"].(string); ok {
			return strings.Split(c, "\n")
		}
	}
	// shell_run: {"output": "...", "exit_code": N}
	if o, ok := d["output"].(string); ok {
		if o == "" {
			return []string{"(no output)"}
		}
		return strings.Split(o, "\n")
	}
	return nil
}

// Run executes the tool and returns a result dict (`{"data":{...}}` or `{"error":"..."}`).
func Run(tool string, args json.RawMessage, cwd string) map[string]any {
	m := parse(args)
	switch tool {
	case "shell_run":
		return runShell(str(m, "command"), cwd)
	case "file_read":
		return runFileRead(m, cwd)
	case "file_write":
		return runFileWrite(m, cwd)
	case "file_list":
		return runFileList(orDef(str(m, "path"), "."), cwd)
	}
	return errf("unsupported local tool: " + tool)
}

func runShell(command, cwd string) map[string]any {
	if strings.TrimSpace(command) == "" {
		return errf("command is required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), shellTimeout)
	defer cancel()
	var cmd *exec.Cmd
	if runtime.GOOS == "windows" {
		// Headless PowerShell has no console width, so it truncates table output (e.g.
		// Get-ChildItem cuts long Name values to ~80 cols with "…"). Wrap the command so
		// its output is rendered at a wide fixed width and streamed line-by-line, so the
		// agent receives every full filename instead of a clipped first row.
		// -Width 512 is wide enough for long paths/filenames without truncation; trailing
		// padding is stripped afterward so it doesn't bloat the payload.
		wrapped := "$FormatEnumerationLimit=-1; & {\n" + command + "\n} | Out-String -Stream -Width 512"
		cmd = exec.CommandContext(ctx, "powershell", "-NoProfile", "-NonInteractive", "-Command", wrapped)
	} else {
		cmd = exec.CommandContext(ctx, "bash", "-c", command)
	}
	cmd.Dir = cwd
	var out, errb bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errb
	runErr := cmd.Run()
	if ctx.Err() == context.DeadlineExceeded {
		return errf("command timed out after 30s")
	}
	exitCode := 0
	if ee, ok := runErr.(*exec.ExitError); ok {
		exitCode = ee.ExitCode()
	} else if runErr != nil {
		return errf(runErr.Error())
	}
	// Strip per-line trailing whitespace FIRST. PowerShell's `Out-String -Width 4096`
	// right-pads every row to 4096 cols; without trimming, a few padded lines blow the
	// size budget and the rest of the listing is lost ("only 1 file"). Trimming makes the
	// real content compact so the whole listing fits.
	combined := trimTrailingPerLine(out.String())
	if e := trimTrailingPerLine(errb.String()); e != "" {
		combined += "\n--- stderr ---\n" + e
	}

	// Never silently cut a listing. If the (already-compacted) output still exceeds the
	// cap, write the FULL output to a temp file in the working dir and tell the agent the
	// path so nothing is lost; include as much as fits inline.
	overflow := false
	var savedPath string
	if len(combined) > maxShellOut {
		overflow = true
		savedPath = filepath.Join(cwd, ".nexora-output.txt")
		_ = os.WriteFile(savedPath, []byte(combined), 0o644)
		combined = combined[:maxShellOut]
	}
	res := map[string]any{"exit_code": exitCode, "output": combined, "truncated": overflow}
	if overflow {
		res["full_output_path"] = savedPath
		res["note"] = "output exceeded the inline limit; the COMPLETE output was written to " + savedPath + " — read that file for the full result."
	}
	return data(res)
}

// trimTrailingPerLine removes trailing whitespace from each line and trailing blank lines.
func trimTrailingPerLine(s string) string {
	lines := strings.Split(s, "\n")
	for i := range lines {
		lines[i] = strings.TrimRight(lines[i], " \t\r")
	}
	out := strings.Join(lines, "\n")
	return strings.TrimRight(out, "\n")
}

func runFileRead(m map[string]any, cwd string) map[string]any {
	p := resolve(str(m, "path"), cwd)
	if p == "" {
		return errf("path is required")
	}
	info, err := os.Stat(p)
	if err != nil {
		return errf("Path not found: " + str(m, "path"))
	}
	if info.IsDir() {
		return listDir(p)
	}
	if info.Size() > maxFileBytes {
		return errf("file too large to read over local exec (>2 MiB)")
	}
	raw, err := os.ReadFile(p)
	if err != nil {
		return errf(err.Error())
	}
	lines := strings.Split(string(raw), "\n")
	offset := intOr(m, "offset", 1)
	if offset < 1 {
		offset = 1
	}
	limit := intOr(m, "limit", 200)
	if limit < 1 {
		limit = 200
	}
	start := offset - 1
	if start > len(lines) {
		start = len(lines)
	}
	end := start + limit
	if end > len(lines) {
		end = len(lines)
	}
	chunk := lines[start:end]
	return data(map[string]any{
		"type": "file", "path": p, "total_lines": len(lines),
		"offset": offset, "returned_lines": len(chunk), "content": strings.Join(chunk, "\n"),
	})
}

func runFileWrite(m map[string]any, cwd string) map[string]any {
	p := resolve(str(m, "path"), cwd)
	if p == "" {
		return errf("path is required")
	}
	content := str(m, "content")
	if len(content) > maxFileBytes {
		return errf("content too large to write over local exec (>2 MiB)")
	}
	if dir := filepath.Dir(p); dir != "" {
		_ = os.MkdirAll(dir, 0o755)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		return errf(err.Error())
	}
	return data(map[string]any{"path": p, "bytes_written": len(content)})
}

func runFileList(path, cwd string) map[string]any {
	p := resolve(path, cwd)
	info, err := os.Stat(p)
	if err != nil {
		return errf("Path not found: " + path)
	}
	if !info.IsDir() {
		return errf("not a directory: " + path)
	}
	return listDir(p)
}

func listDir(p string) map[string]any {
	entries, err := os.ReadDir(p)
	if err != nil {
		return errf(err.Error())
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		n := e.Name()
		if e.IsDir() {
			n += "/"
		}
		names = append(names, n)
	}
	sort.SliceStable(names, func(i, j int) bool {
		di, dj := strings.HasSuffix(names[i], "/"), strings.HasSuffix(names[j], "/")
		if di != dj {
			return di
		}
		return names[i] < names[j]
	})
	truncated := false
	if len(names) > 500 {
		names = names[:500]
		truncated = true
	}
	return data(map[string]any{"type": "directory", "path": p, "entries": names, "truncated": truncated})
}

// ── helpers ──────────────────────────────────────────────────────────────────

func parse(raw json.RawMessage) map[string]any {
	m := map[string]any{}
	if len(raw) > 0 {
		_ = json.Unmarshal(raw, &m)
	}
	return m
}

func str(m map[string]any, k string) string {
	if v, ok := m[k].(string); ok {
		return v
	}
	return ""
}

func intOr(m map[string]any, k string, def int) int {
	switch v := m[k].(type) {
	case float64:
		return int(v)
	case int:
		return v
	}
	return def
}

func resolve(path, cwd string) string {
	if path == "" {
		return ""
	}
	if filepath.IsAbs(path) {
		return filepath.Clean(path)
	}
	return filepath.Clean(filepath.Join(cwd, path))
}

func data(d map[string]any) map[string]any { return map[string]any{"data": d} }
func errf(msg string) map[string]any       { return map[string]any{"error": msg} }
func orDef(s, def string) string {
	if s == "" {
		return def
	}
	return s
}
