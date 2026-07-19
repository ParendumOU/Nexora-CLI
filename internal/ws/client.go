// Package ws is the chat streaming client. It connects to /ws/chat/{chat_id} with the
// auth token in the Authorization header (core #159 — keeps it out of URL/proxy logs),
// falling back to the legacy ?token= query param for cores older than v1.10.0, and
// exposes a frame channel the Bubble Tea loop drains (see the bubbletea-tui skill).
package ws

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

// wsProbeTimeout bounds how long Dial waits for the server's first frame while probing
// for an auth rejection. A healthy server sends "connected" within milliseconds; the
// deadline only trips on a broken/silent socket.
const wsProbeTimeout = 8 * time.Second

// Frame is a server→client event. Covers the full turn event catalog: chunk/stream,
// direct tool actions, and sub-agent / sub-chat activity.
type Frame struct {
	Type      string          `json:"type"`
	Content   string          `json:"content"`
	Tool      string          `json:"tool"`
	Args      json.RawMessage `json:"args"`
	MessageID string          `json:"message_id"`
	Metadata  map[string]any  `json:"metadata"`
	Task      json.RawMessage `json:"task"`
	Message   string          `json:"message"` // error / busy / status text

	// local execution proxy (tool_exec_request): run a tool on the CLI host
	RequestID string `json:"request_id"`

	// user_message (another source posted to this chat — e.g. telegram, another tab)
	UserID          string `json:"user_id"`
	UserName        string `json:"user_name"`
	ClientMessageID string `json:"client_message_id"`

	// direct tool actions (agent_action_start/done)
	GroupID string `json:"group_id"`
	Label   string `json:"label"`
	Status  string `json:"status"`
	Error   string `json:"error"`

	// sub-agent / sub-chat (sub_agent_*)
	AgentName string `json:"agent_name"`
	TaskID    string `json:"task_id"`
	TaskTitle string `json:"task_title"`
	SubChatID string `json:"sub_chat_id"`
	StepID    string `json:"step_id"`
	StepName  string `json:"step_name"`
	StepLabel string `json:"step_label"`
	Result    string `json:"result"`

	// HasContent distinguishes an explicit "content":"" (authoritative empty turn)
	// from the field being absent. Set by UnmarshalJSON; not a wire field.
	HasContent bool `json:"-"`
}

// UnmarshalJSON decodes a Frame and records whether the "content" key was present,
// so stream_end can trust an explicit empty content instead of falling back to the
// raw streamed buffer.
func (f *Frame) UnmarshalJSON(data []byte) error {
	type alias Frame // avoid recursion
	if err := json.Unmarshal(data, (*alias)(f)); err != nil {
		return err
	}
	var probe map[string]json.RawMessage
	if err := json.Unmarshal(data, &probe); err == nil {
		_, f.HasContent = probe["content"]
	}
	return nil
}

// outbound is a client→server message.
type outbound struct {
	Type            string `json:"type"`
	Content         string `json:"content,omitempty"`
	AgentID         string `json:"agent_id,omitempty"`
	ModelName       string `json:"model_name,omitempty"`
	ProviderChainID string `json:"provider_chain_id,omitempty"`
	EnableAgent     bool   `json:"enable_agent"`
	ClientMessageID string `json:"client_message_id,omitempty"`
	// reasoning mode: flash (default) | think | deep — drives provider-native
	// reasoning server-side (see core providers/reasoning.py).
	Mode string `json:"mode,omitempty"`
	// local execution: when true the backend proxies filesystem/shell tools to this host.
	LocalExec bool   `json:"local_exec,omitempty"`
	Cwd       string `json:"cwd,omitempty"`
	ClientOS  string `json:"client_os,omitempty"`
}

// SendOpts carries the per-turn routing overrides.
type SendOpts struct {
	AgentID         string
	ModelName       string
	ChainID         string
	EnableAgent     bool
	ClientMessageID string
	Mode            string
	LocalExec       bool
	Cwd             string
	ClientOS        string
}

// Client wraps a single chat websocket connection.
type Client struct {
	conn  *websocket.Conn
	mu    sync.Mutex // guards writes
	first *Frame     // first frame read during Dial's auth probe; emitted before the read loop
}

// errWSAuthRejected means the server accepted the handshake but rejected our auth
// (sent an "Unauthorized" error / closed 4001). It triggers the legacy-query-param
// fallback for cores older than v1.10.0, which only read ?token= and ignore the header.
var errWSAuthRejected = errors.New("ws auth rejected")

// Dial opens the chat socket. baseURL is the instance root (http/https); it is converted
// to ws/wss. token is the raw JWT or nxr_ key (no Bearer prefix).
//
// Auth is sent in the Authorization header first (core #159 — keeps the token out of the
// URL and proxy access logs; supported by core >= v1.10.0). If that instance rejects the
// handshake auth (an older core that reads only the legacy ?token= query param), Dial
// transparently retries once with ?token= so a current CLI still talks to an older
// instance. On a modern instance the query param is never emitted.
func Dial(ctx context.Context, baseURL, chatID, token string) (*Client, error) {
	c, err := dialOnce(ctx, baseURL, chatID, token, false)
	if errors.Is(err, errWSAuthRejected) {
		return dialOnce(ctx, baseURL, chatID, token, true)
	}
	return c, err
}

// dialOnce performs a single handshake. When legacy is true the token also rides the
// ?token= query param (for pre-v1.10.0 cores); otherwise only the Authorization header
// is sent. Core accepts the socket before authenticating, so an auth failure surfaces as
// the first frame (an "error"/"Unauthorized") or a 4001 close, not a dial error — that is
// mapped to errWSAuthRejected so Dial can fall back. The probed first frame is buffered
// on the returned Client and re-emitted by ReadInto so no server event is lost.
func dialOnce(ctx context.Context, baseURL, chatID, token string, legacy bool) (*Client, error) {
	wsURL := toWS(baseURL) + "/ws/chat/" + url.PathEscape(chatID)
	if legacy {
		wsURL += "?token=" + url.QueryEscape(token)
	}
	hdr := http.Header{}
	hdr.Set("Authorization", "Bearer "+token)
	conn, _, err := websocket.DefaultDialer.DialContext(ctx, wsURL, hdr)
	if err != nil {
		return nil, err
	}

	_ = conn.SetReadDeadline(time.Now().Add(wsProbeTimeout))
	var f Frame
	rerr := conn.ReadJSON(&f)
	_ = conn.SetReadDeadline(time.Time{})

	if rerr != nil {
		conn.Close()
		if websocket.IsCloseError(rerr, 4001) {
			return nil, errWSAuthRejected
		}
		return nil, rerr
	}
	if f.Type == "error" && f.Message == "Unauthorized" {
		conn.Close()
		return nil, errWSAuthRejected
	}
	return &Client{conn: conn, first: &f}, nil
}

// ReadInto pumps frames into ch until the connection closes, then closes ch.
// Run it in a goroutine. Replies to server pings automatically.
func (c *Client) ReadInto(ch chan<- Frame) {
	defer close(ch)
	// Emit the frame already consumed by Dial's auth probe before resuming the socket read.
	if c.first != nil {
		f := *c.first
		c.first = nil
		if f.Type == "ping" {
			c.write(map[string]string{"type": "pong"})
		} else {
			ch <- f
		}
	}
	for {
		var f Frame
		if err := c.conn.ReadJSON(&f); err != nil {
			return
		}
		if f.Type == "ping" {
			c.write(map[string]string{"type": "pong"})
			continue
		}
		ch <- f
	}
}

// Send posts a user turn with optional routing overrides.
func (c *Client) Send(content string, o SendOpts) error {
	return c.write(outbound{
		Type: "message", Content: content,
		AgentID: o.AgentID, ModelName: o.ModelName, ProviderChainID: o.ChainID,
		EnableAgent: o.EnableAgent, ClientMessageID: o.ClientMessageID,
		Mode:      o.Mode,
		LocalExec: o.LocalExec, Cwd: o.Cwd, ClientOS: o.ClientOS,
	})
}

// SendToolResult replies to a tool_exec_request with the host execution result.
func (c *Client) SendToolResult(requestID string, result map[string]any) error {
	return c.write(map[string]any{
		"type":       "tool_exec_result",
		"request_id": requestID,
		"result":     result,
	})
}

func (c *Client) write(v any) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.conn.WriteJSON(v)
}

// Close tears down the connection.
func (c *Client) Close() error {
	if c.conn == nil {
		return nil
	}
	return c.conn.Close()
}

func toWS(base string) string {
	base = strings.TrimRight(base, "/")
	switch {
	case strings.HasPrefix(base, "https://"):
		return "wss://" + strings.TrimPrefix(base, "https://")
	case strings.HasPrefix(base, "http://"):
		return "ws://" + strings.TrimPrefix(base, "http://")
	default:
		return "ws://" + base
	}
}
