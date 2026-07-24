package api

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"cryptoex/internal/store"
	"cryptoex/internal/wallet"

	"github.com/go-chi/chi/v5"
)

func TestWriteJSON(t *testing.T) {
	w := httptest.NewRecorder()
	writeJSON(w, http.StatusCreated, map[string]string{"k": "v"})
	if w.Code != http.StatusCreated {
		t.Errorf("status = %d, want 201", w.Code)
	}
	if ct := w.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("content-type = %q", ct)
	}
	if !strings.Contains(w.Body.String(), `"k":"v"`) {
		t.Errorf("body = %s", w.Body.String())
	}
}

func TestWriteErr(t *testing.T) {
	w := httptest.NewRecorder()
	writeErr(w, http.StatusBadRequest, "bad thing")
	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", w.Code)
	}
	if !strings.Contains(w.Body.String(), `"error":"bad thing"`) {
		t.Errorf("body = %s", w.Body.String())
	}
}

func TestWriteDomainErr(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want int
	}{
		{"insufficient funds", store.ErrInsufficientFunds, http.StatusBadRequest},
		{"not found", store.ErrNotFound, http.StatusNotFound},
		{"kyc required", wallet.ErrKYCRequired, http.StatusForbidden},
		{"unknown asset", wallet.ErrUnknownAsset, http.StatusBadRequest},
		{"below min", wallet.ErrBelowMin, http.StatusBadRequest},
		{"unmapped → 500", errors.New("some unexpected error"), http.StatusInternalServerError},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			writeDomainErr(w, c.err)
			if w.Code != c.want {
				t.Errorf("writeDomainErr(%v) = %d, want %d", c.err, w.Code, c.want)
			}
		})
	}
}

func TestDecode(t *testing.T) {
	type payload struct {
		Name string `json:"name"`
	}
	t.Run("valid", func(t *testing.T) {
		r := httptest.NewRequest("POST", "/", strings.NewReader(`{"name":"x"}`))
		var p payload
		if err := decode(r, &p); err != nil {
			t.Fatal(err)
		}
		if p.Name != "x" {
			t.Errorf("name = %q", p.Name)
		}
	})
	t.Run("unknown field rejected", func(t *testing.T) {
		r := httptest.NewRequest("POST", "/", strings.NewReader(`{"name":"x","extra":1}`))
		var p payload
		if err := decode(r, &p); err == nil {
			t.Error("expected error for unknown field (DisallowUnknownFields)")
		}
	})
	t.Run("malformed json", func(t *testing.T) {
		r := httptest.NewRequest("POST", "/", strings.NewReader(`{bad`))
		var p payload
		if err := decode(r, &p); err == nil {
			t.Error("expected error for malformed JSON")
		}
	})
}

func TestQueryInt(t *testing.T) {
	mk := func(qs string) *http.Request { return httptest.NewRequest("GET", "/?"+qs, nil) }
	cases := []struct {
		qs             string
		def, max, want int
	}{
		{"limit=10", 5, 100, 10},
		{"", 5, 100, 5},            // missing → default
		{"limit=500", 5, 100, 100}, // over max → clamp
		{"limit=abc", 5, 100, 5},   // invalid → default
		{"limit=-3", 5, 100, 5},    // negative → default
		{"limit=0", 5, 100, 5},     // zero → default
	}
	for _, c := range cases {
		if got := queryInt(mk(c.qs), "limit", c.def, c.max); got != c.want {
			t.Errorf("queryInt(%q) = %d, want %d", c.qs, got, c.want)
		}
	}
}

func TestSymbolParam(t *testing.T) {
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("symbol", "btc-usdt")
	r := httptest.NewRequest("GET", "/", nil)
	r = r.WithContext(context.WithValue(r.Context(), chi.RouteCtxKey, rctx))
	if got := symbolParam(r); got != "BTC-USDT" {
		t.Errorf("symbolParam = %q, want BTC-USDT (uppercased)", got)
	}
}
