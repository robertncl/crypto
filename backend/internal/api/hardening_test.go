package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"cryptoex/internal/auth"
	"cryptoex/internal/models"

	"github.com/gorilla/websocket"
)

// TestBotAccountCannotLogIn: bot accounts are funded and KYC-verified, so even
// a correct password must not yield a session (older builds derived the bot
// password from its public email).
func TestBotAccountCannotLogIn(t *testing.T) {
	srv := newTestServer(t)
	hash, _ := auth.HashPassword("correct-horse-battery")
	if _, err := srv.st.CreateUser("bot@exchange.local", hash, models.RoleBot, time.Now().Unix()); err != nil {
		t.Fatal(err)
	}
	w := do(t, srv, "POST", "/api/auth/login", "", `{"email":"bot@exchange.local","password":"correct-horse-battery"}`)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("bot login status = %d, want 401", w.Code)
	}
	if strings.Contains(w.Body.String(), "token") {
		t.Fatalf("bot login leaked a token: %s", w.Body.String())
	}
}

func TestPasswordPolicyBounds(t *testing.T) {
	srv := newTestServer(t)
	for _, tc := range []struct {
		pw   string
		want int
	}{
		{"seven77", http.StatusBadRequest},
		{strings.Repeat("x", 73), http.StatusBadRequest}, // beyond bcrypt's 72-byte limit
		{strings.Repeat("x", 72), http.StatusOK},
	} {
		w := do(t, srv, "POST", "/api/auth/register", "", `{"email":"p`+tc.pw[:3]+`@t.com","password":"`+tc.pw+`"}`)
		if w.Code != tc.want {
			t.Errorf("register with %d-char password = %d, want %d", len(tc.pw), w.Code, tc.want)
		}
	}
}

func TestUnknownEmailLoginIs401(t *testing.T) {
	srv := newTestServer(t)
	w := do(t, srv, "POST", "/api/auth/login", "", `{"email":"ghost@t.com","password":"whatever123"}`)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", w.Code)
	}
}

func TestAPIResponsesAreNotCacheable(t *testing.T) {
	srv := newTestServer(t)
	token, _ := register(t, srv, "cache@t.com")
	w := do(t, srv, "GET", "/api/account/balances", token, "")
	if got := w.Header().Get("Cache-Control"); got != "no-store" {
		t.Fatalf("Cache-Control = %q, want no-store", got)
	}
}

func TestQueryTokenRejectedForREST(t *testing.T) {
	srv := newTestServer(t)
	token, _ := register(t, srv, "q@t.com")
	if w := do(t, srv, "GET", "/api/me?token="+token, "", ""); w.Code != http.StatusUnauthorized {
		t.Fatalf("query-string token status = %d, want 401", w.Code)
	}
}

func TestWebSocketOriginCheck(t *testing.T) {
	srv := newTestServer(t)
	srv.origins = []string{"https://app.example"}
	hs := httptest.NewServer(srv.Router)
	defer hs.Close()
	url := "ws" + strings.TrimPrefix(hs.URL, "http") + "/ws"
	for _, tc := range []struct {
		origin string
		ok     bool
	}{
		{"", true},                    // non-browser client
		{hs.URL, true},                // same origin
		{"https://app.example", true}, // configured SPA origin
		{"https://evil.example", false},
	} {
		h := http.Header{}
		if tc.origin != "" {
			h.Set("Origin", tc.origin)
		}
		conn, resp, err := websocket.DefaultDialer.Dial(url, h)
		if conn != nil {
			conn.Close()
		}
		if ok := err == nil; ok != tc.ok {
			status := 0
			if resp != nil {
				status = resp.StatusCode
			}
			t.Errorf("origin %q: connected=%v (status %d), want %v", tc.origin, ok, status, tc.ok)
		}
	}
}
