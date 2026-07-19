package ws

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

var testUpgrader = websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}

// drainFirst runs ReadInto and returns the first non-nil frame (or fails on timeout).
func drainFirst(t *testing.T, c *Client) Frame {
	t.Helper()
	ch := make(chan Frame, 8)
	go c.ReadInto(ch)
	select {
	case f, ok := <-ch:
		if !ok {
			t.Fatal("frame channel closed before any frame")
		}
		return f
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for first frame")
		return Frame{}
	}
}

// TestDialModernHeader: a core that authenticates via the Authorization header must
// receive the token in the header and NEVER in the query string, and the client must
// surface the server's first frame.
func TestDialModernHeader(t *testing.T) {
	var gotAuth, gotQueryToken string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotQueryToken = r.URL.Query().Get("token")
		conn, err := testUpgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()
		_ = conn.WriteJSON(Frame{Type: "connected"})
		for { // keep the socket open until the client closes it
			if _, _, err := conn.ReadMessage(); err != nil {
				return
			}
		}
	}))
	defer srv.Close()

	c, err := Dial(context.Background(), srv.URL, "chat-1", "jwt-123")
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer c.Close()

	if gotAuth != "Bearer jwt-123" {
		t.Errorf("Authorization header = %q, want %q", gotAuth, "Bearer jwt-123")
	}
	if gotQueryToken != "" {
		t.Errorf("token leaked into query string: %q (must be header-only on a modern core)", gotQueryToken)
	}
	if f := drainFirst(t, c); f.Type != "connected" {
		t.Errorf("first frame type = %q, want connected", f.Type)
	}
}

// TestDialLegacyFallback: an older core that only reads ?token= accepts the handshake,
// then closes 4001 when the token is absent (header-only first attempt). The client must
// transparently retry with the query param and connect.
func TestDialLegacyFallback(t *testing.T) {
	attempts := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		conn, err := testUpgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()
		if r.URL.Query().Get("token") == "" {
			// Accept-then-reject, mirroring core: no error frame, just a 4001 close.
			_ = conn.WriteControl(websocket.CloseMessage,
				websocket.FormatCloseMessage(4001, "unauthorized"),
				time.Now().Add(time.Second))
			return
		}
		_ = conn.WriteJSON(Frame{Type: "connected"})
		for {
			if _, _, err := conn.ReadMessage(); err != nil {
				return
			}
		}
	}))
	defer srv.Close()

	c, err := Dial(context.Background(), srv.URL, "chat-1", "jwt-xyz")
	if err != nil {
		t.Fatalf("Dial with legacy fallback: %v", err)
	}
	defer c.Close()

	if attempts < 2 {
		t.Errorf("expected a header attempt then a query-param retry, got %d attempt(s)", attempts)
	}
	if f := drainFirst(t, c); f.Type != "connected" {
		t.Errorf("first frame type = %q, want connected", f.Type)
	}
}

// TestDialLegacyFallbackUnauthorizedFrame: some cores send an {"error","Unauthorized"}
// frame before closing. That must also trigger the fallback.
func TestDialLegacyFallbackUnauthorizedFrame(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := testUpgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()
		if r.URL.Query().Get("token") == "" {
			_ = conn.WriteJSON(Frame{Type: "error", Message: "Unauthorized"})
			return
		}
		_ = conn.WriteJSON(Frame{Type: "connected"})
		for {
			if _, _, err := conn.ReadMessage(); err != nil {
				return
			}
		}
	}))
	defer srv.Close()

	c, err := Dial(context.Background(), srv.URL, "chat-1", "jwt-xyz")
	if err != nil {
		t.Fatalf("Dial with unauthorized-frame fallback: %v", err)
	}
	defer c.Close()
	if f := drainFirst(t, c); f.Type != "connected" {
		t.Errorf("first frame type = %q, want connected", f.Type)
	}
}
