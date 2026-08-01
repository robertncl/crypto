package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

// dialWS starts the router on a real listener and opens a WebSocket to /ws,
// optionally authenticated with token.
func dialWS(t *testing.T, srv *Server, token string) (*websocket.Conn, func()) {
	t.Helper()
	hs := httptest.NewServer(srv.Router)
	url := "ws" + strings.TrimPrefix(hs.URL, "http") + "/ws"
	if token != "" {
		url += "?token=" + token
	}
	conn, _, err := websocket.DefaultDialer.Dial(url, nil)
	if err != nil {
		hs.Close()
		t.Fatalf("dial %s: %v", url, err)
	}
	return conn, func() { conn.Close(); hs.Close() }
}

// readEnvelope waits briefly for a frame on the given channel, returning
// ("", false) if nothing arrives before the deadline.
func readEnvelope(t *testing.T, conn *websocket.Conn, want string, wait time.Duration) (string, bool) {
	t.Helper()
	deadline := time.Now().Add(wait)
	_ = conn.SetReadDeadline(deadline)
	for time.Now().Before(deadline) {
		_, raw, err := conn.ReadMessage()
		if err != nil {
			return "", false
		}
		var env struct {
			Channel string          `json:"channel"`
			Data    json.RawMessage `json:"data"`
		}
		if err := json.Unmarshal(raw, &env); err != nil {
			continue
		}
		if env.Channel == want {
			return string(raw), true
		}
	}
	return "", false
}

// TestWSPublicChannelUnauthenticated covers handleWS for an anonymous client
// and confirms public market-data channels still stream.
func TestWSPublicChannelUnauthenticated(t *testing.T) {
	srv := newTestServer(t)
	token, _ := register(t, srv, "wspub@test.com")

	conn, cleanup := dialWS(t, srv, "")
	defer cleanup()
	if err := conn.WriteJSON(map[string]any{"op": "subscribe", "channels": []string{"depth:BTC-USDT"}}); err != nil {
		t.Fatal(err)
	}
	time.Sleep(150 * time.Millisecond)

	// Placing an order publishes a depth update to the public channel.
	do(t, srv, "POST", "/api/orders", token,
		`{"market":"BTC-USDT","side":"buy","type":"limit","price":"1000","quantity":"0.02"}`)

	if _, ok := readEnvelope(t, conn, "depth:BTC-USDT", 2*time.Second); !ok {
		t.Error("expected a public depth update on the anonymous connection")
	}
}

// TestWSPrivateChannelScopedToOwnUser covers the authenticated handleWS path:
// subscribing by bare name delivers this user's own private stream.
func TestWSPrivateChannelScopedToOwnUser(t *testing.T) {
	srv := newTestServer(t)
	token, userID := register(t, srv, "wspriv@test.com")

	conn, cleanup := dialWS(t, srv, token)
	defer cleanup()
	if err := conn.WriteJSON(map[string]any{"op": "subscribe", "channels": []string{"balances", "orders"}}); err != nil {
		t.Fatal(err)
	}
	time.Sleep(150 * time.Millisecond)

	do(t, srv, "POST", "/api/orders", token,
		`{"market":"BTC-USDT","side":"buy","type":"limit","price":"1000","quantity":"0.02"}`)

	// The hub topic is suffixed with the authenticated user's id.
	want := "balances:" + strconv.FormatInt(userID, 10)
	if _, ok := readEnvelope(t, conn, want, 2*time.Second); !ok {
		t.Errorf("expected own private balance update on %s", want)
	}
}

// TestWSCannotSubscribeToAnotherUsersPrivateTopic is the end-to-end regression
// guard for the WebSocket authorization bypass: naming a user-scoped private
// topic directly must deliver nothing, even for an authenticated attacker.
func TestWSCannotSubscribeToAnotherUsersPrivateTopic(t *testing.T) {
	srv := newTestServer(t)
	victimToken, victimID := register(t, srv, "wsvictim@test.com")
	attackerToken, _ := register(t, srv, "wsattacker@test.com")

	victimTopic := "balances:" + strconv.FormatInt(victimID, 10)

	for _, tc := range []struct {
		name  string
		token string
	}{
		{"unauthenticated attacker", ""},
		{"authenticated attacker", attackerToken},
	} {
		t.Run(tc.name, func(t *testing.T) {
			conn, cleanup := dialWS(t, srv, tc.token)
			defer cleanup()
			if err := conn.WriteJSON(map[string]any{
				"op": "subscribe", "channels": []string{victimTopic, "orders:" + strconv.FormatInt(victimID, 10)},
			}); err != nil {
				t.Fatal(err)
			}
			time.Sleep(150 * time.Millisecond)

			// Victim generates private events.
			do(t, srv, "POST", "/api/orders", victimToken,
				`{"market":"BTC-USDT","side":"buy","type":"limit","price":"1000","quantity":"0.02"}`)

			if raw, ok := readEnvelope(t, conn, victimTopic, 1500*time.Millisecond); ok {
				t.Errorf("attacker received victim's private stream: %s", raw)
			}
		})
	}
}

// TestWSUnsubscribeStopsDelivery exercises the unsubscribe op.
func TestWSUnsubscribeStopsDelivery(t *testing.T) {
	srv := newTestServer(t)
	token, _ := register(t, srv, "wsunsub@test.com")

	conn, cleanup := dialWS(t, srv, token)
	defer cleanup()
	conn.WriteJSON(map[string]any{"op": "subscribe", "channels": []string{"depth:BTC-USDT"}})
	time.Sleep(100 * time.Millisecond)
	conn.WriteJSON(map[string]any{"op": "unsubscribe", "channels": []string{"depth:BTC-USDT"}})
	time.Sleep(100 * time.Millisecond)

	do(t, srv, "POST", "/api/orders", token,
		`{"market":"BTC-USDT","side":"buy","type":"limit","price":"1000","quantity":"0.02"}`)

	if raw, ok := readEnvelope(t, conn, "depth:BTC-USDT", 800*time.Millisecond); ok {
		t.Errorf("received depth after unsubscribe: %s", raw)
	}
}

// TestWSIgnoresMalformedFrames confirms a junk frame does not kill the
// connection: a later valid subscribe still works.
func TestWSIgnoresMalformedFrames(t *testing.T) {
	srv := newTestServer(t)
	token, _ := register(t, srv, "wsjunk@test.com")

	conn, cleanup := dialWS(t, srv, token)
	defer cleanup()
	if err := conn.WriteMessage(websocket.TextMessage, []byte("not json at all")); err != nil {
		t.Fatal(err)
	}
	time.Sleep(100 * time.Millisecond)
	if err := conn.WriteJSON(map[string]any{"op": "subscribe", "channels": []string{"depth:BTC-USDT"}}); err != nil {
		t.Fatalf("connection died after malformed frame: %v", err)
	}
	time.Sleep(100 * time.Millisecond)

	do(t, srv, "POST", "/api/orders", token,
		`{"market":"BTC-USDT","side":"buy","type":"limit","price":"1000","quantity":"0.02"}`)

	if _, ok := readEnvelope(t, conn, "depth:BTC-USDT", 2*time.Second); !ok {
		t.Error("expected depth update after recovering from a malformed frame")
	}
}

// TestWSInvalidTokenConnectsAnonymously: a bad token must not be treated as
// authenticated, so private channels stay unavailable.
func TestWSInvalidTokenConnectsAnonymously(t *testing.T) {
	srv := newTestServer(t)
	token, _ := register(t, srv, "wsbadtok@test.com")

	conn, cleanup := dialWS(t, srv, "garbage-token")
	defer cleanup()
	conn.WriteJSON(map[string]any{"op": "subscribe", "channels": []string{"balances"}})
	time.Sleep(150 * time.Millisecond)

	do(t, srv, "POST", "/api/orders", token,
		`{"market":"BTC-USDT","side":"buy","type":"limit","price":"1000","quantity":"0.02"}`)

	// userID stays 0, so the private subscribe was refused outright.
	if raw, ok := readEnvelope(t, conn, "balances:1", time.Second); ok {
		t.Errorf("bad-token connection received private data: %s", raw)
	}
}

func TestHealthEndpoint(t *testing.T) {
	w := do(t, newTestServer(t), "GET", "/health", "", "")
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "ok") {
		t.Errorf("health = %d %s", w.Code, w.Body.String())
	}
}
