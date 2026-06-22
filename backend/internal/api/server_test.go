package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"cryptoex/internal/auth"
	"cryptoex/internal/config"
	"cryptoex/internal/db"
	"cryptoex/internal/derivatives"
	"cryptoex/internal/earn"
	"cryptoex/internal/engine"
	"cryptoex/internal/market"
	"cryptoex/internal/store"
	"cryptoex/internal/wallet"
	"cryptoex/internal/ws"
)

// newTestServer wires a full server backed by a temp SQLite DB, mirroring the
// production wiring in cmd/server.
func newTestServer(t *testing.T) *Server {
	t.Helper()
	conn, err := db.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { conn.Close() })
	st := store.New(conn)
	hub := ws.NewHub()
	md := market.NewService(st, hub)
	markets, _ := st.ListMarkets()
	md.Init(markets)
	mgr := engine.NewManager(st, md, hub)
	if err := mgr.Init(markets); err != nil {
		t.Fatalf("engine init: %v", err)
	}
	perps, _ := st.ListPerpMarkets()
	perp := derivatives.NewManager(st, md, hub, 60)
	if err := perp.Init(perps); err != nil {
		t.Fatalf("perp init: %v", err)
	}
	wal := wallet.NewService(st, hub)
	ern := earn.NewService(st, hub, 60)
	if err := ern.Init(); err != nil {
		t.Fatalf("earn init: %v", err)
	}
	am := auth.NewManager("test-secret", 24)
	cfg := config.Config{JWTSecret: "test-secret", JWTTTLHours: 24, WebDir: ""}
	return NewServer(cfg, st, am, mgr, perp, md, wal, ern, hub)
}

func do(t *testing.T, srv *Server, method, path, token, body string) *httptest.ResponseRecorder {
	t.Helper()
	var r *http.Request
	if body != "" {
		r = httptest.NewRequest(method, path, strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
	} else {
		r = httptest.NewRequest(method, path, nil)
	}
	if token != "" {
		r.Header.Set("Authorization", "Bearer "+token)
	}
	w := httptest.NewRecorder()
	srv.Router.ServeHTTP(w, r)
	return w
}

func TestAuthFlow(t *testing.T) {
	srv := newTestServer(t)
	body := `{"email":"user@test.com","password":"secret123"}`

	w := do(t, srv, "POST", "/api/auth/register", "", body)
	if w.Code != http.StatusOK {
		t.Fatalf("register status = %d, body = %s", w.Code, w.Body.String())
	}
	var ar authResponse
	if err := json.Unmarshal(w.Body.Bytes(), &ar); err != nil {
		t.Fatal(err)
	}
	if ar.Token == "" || ar.User == nil {
		t.Fatalf("register response missing token/user: %+v", ar)
	}

	if w := do(t, srv, "POST", "/api/auth/login", "", body); w.Code != http.StatusOK {
		t.Fatalf("login status = %d", w.Code)
	}

	if w := do(t, srv, "GET", "/api/me", ar.Token, ""); w.Code != http.StatusOK {
		t.Fatalf("/me status = %d", w.Code)
	}

	w = do(t, srv, "GET", "/api/account/balances", ar.Token, "")
	if w.Code != http.StatusOK {
		t.Fatalf("balances status = %d", w.Code)
	}
	if !strings.Contains(w.Body.String(), "USDT") {
		t.Errorf("expected USDT welcome balance, got %s", w.Body.String())
	}
}

func TestRegisterValidation(t *testing.T) {
	srv := newTestServer(t)
	cases := []struct {
		name string
		body string
		want int
	}{
		{"invalid email", `{"email":"bad","password":"secret123"}`, http.StatusBadRequest},
		{"short password", `{"email":"a@b.com","password":"short"}`, http.StatusBadRequest},
		{"malformed json", `not json`, http.StatusBadRequest},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if w := do(t, srv, "POST", "/api/auth/register", "", c.body); w.Code != c.want {
				t.Errorf("status = %d, want %d (body %s)", w.Code, c.want, w.Body.String())
			}
		})
	}
}

func TestRegisterDuplicateEmail(t *testing.T) {
	srv := newTestServer(t)
	body := `{"email":"dup@test.com","password":"secret123"}`
	if w := do(t, srv, "POST", "/api/auth/register", "", body); w.Code != http.StatusOK {
		t.Fatalf("first register status = %d", w.Code)
	}
	if w := do(t, srv, "POST", "/api/auth/register", "", body); w.Code != http.StatusConflict {
		t.Errorf("duplicate register status = %d, want 409", w.Code)
	}
}

func TestLoginWrongPassword(t *testing.T) {
	srv := newTestServer(t)
	do(t, srv, "POST", "/api/auth/register", "", `{"email":"u@test.com","password":"secret123"}`)
	w := do(t, srv, "POST", "/api/auth/login", "", `{"email":"u@test.com","password":"wrongpass"}`)
	if w.Code != http.StatusUnauthorized {
		t.Errorf("wrong password status = %d, want 401", w.Code)
	}
}

func TestProtectedEndpointRequiresAuth(t *testing.T) {
	srv := newTestServer(t)
	if w := do(t, srv, "GET", "/api/account/balances", "", ""); w.Code != http.StatusUnauthorized {
		t.Errorf("no-token status = %d, want 401", w.Code)
	}
	if w := do(t, srv, "GET", "/api/account/balances", "garbage-token", ""); w.Code != http.StatusUnauthorized {
		t.Errorf("bad-token status = %d, want 401", w.Code)
	}
}

func TestPublicEndpoints(t *testing.T) {
	srv := newTestServer(t)
	for _, path := range []string{
		"/api/assets", "/api/markets", "/api/tickers",
		"/api/markets/BTC-USDT", "/api/perp/markets", "/health",
	} {
		if w := do(t, srv, "GET", path, "", ""); w.Code != http.StatusOK {
			t.Errorf("GET %s status = %d, want 200", path, w.Code)
		}
	}
}

func TestKYCVerifyFlow(t *testing.T) {
	srv := newTestServer(t)
	w := do(t, srv, "POST", "/api/auth/register", "", `{"email":"kyc@test.com","password":"secret123"}`)
	var ar authResponse
	json.Unmarshal(w.Body.Bytes(), &ar)

	w = do(t, srv, "POST", "/api/kyc/verify", ar.Token, "")
	if w.Code != http.StatusOK {
		t.Fatalf("kyc verify status = %d", w.Code)
	}
	if !strings.Contains(w.Body.String(), "verified") {
		t.Errorf("expected verified status in response: %s", w.Body.String())
	}
}
