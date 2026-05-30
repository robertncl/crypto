// Package api exposes the REST + WebSocket HTTP surface and wires together the
// store, auth, matching engine, market-data and wallet services.
package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"cryptoex/internal/auth"
	"cryptoex/internal/config"
	"cryptoex/internal/engine"
	"cryptoex/internal/market"
	"cryptoex/internal/store"
	"cryptoex/internal/wallet"
	"cryptoex/internal/ws"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"
)

type Server struct {
	cfg    config.Config
	st     *store.Store
	auth   *auth.Manager
	mgr    *engine.Manager
	md     *market.Service
	wallet *wallet.Service
	hub    *ws.Hub
	Router http.Handler
}

func NewServer(cfg config.Config, st *store.Store, am *auth.Manager, mgr *engine.Manager,
	md *market.Service, wal *wallet.Service, hub *ws.Hub) *Server {
	s := &Server{cfg: cfg, st: st, auth: am, mgr: mgr, md: md, wallet: wal, hub: hub}
	s.Router = s.routes()
	return s
}

func (s *Server) routes() http.Handler {
	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	r.Use(middleware.Recoverer)
	r.Use(cors.Handler(cors.Options{
		AllowedOrigins:   []string{s.cfg.CORSOrigin, "http://localhost:5173", "http://127.0.0.1:5173"},
		AllowedMethods:   []string{"GET", "POST", "DELETE", "OPTIONS"},
		AllowedHeaders:   []string{"Authorization", "Content-Type"},
		AllowCredentials: false,
		MaxAge:           300,
	}))

	r.Route("/api", func(r chi.Router) {
		// Public market data & auth.
		r.Post("/auth/register", s.handleRegister)
		r.Post("/auth/login", s.handleLogin)
		r.Get("/assets", s.handleAssets)
		r.Get("/markets", s.handleMarkets)
		r.Get("/tickers", s.handleTickers)
		r.Get("/markets/{symbol}", s.handleMarket)
		r.Get("/markets/{symbol}/depth", s.handleDepth)
		r.Get("/markets/{symbol}/trades", s.handleMarketTrades)
		r.Get("/markets/{symbol}/candles", s.handleCandles)

		// Authenticated.
		r.Group(func(r chi.Router) {
			r.Use(s.auth.Middleware)
			r.Get("/me", s.handleMe)
			r.Post("/kyc/verify", s.handleKYCVerify)
			r.Get("/account/balances", s.handleBalances)
			r.Post("/orders", s.handlePlaceOrder)
			r.Delete("/orders/{id}", s.handleCancelOrder)
			r.Get("/orders", s.handleOpenOrders)
			r.Get("/orders/history", s.handleOrderHistory)
			r.Get("/trades", s.handleMyTrades)
			r.Get("/wallet/address", s.handleWalletAddress)
			r.Post("/wallet/deposit", s.handleDeposit)
			r.Post("/wallet/withdraw", s.handleWithdraw)
			r.Get("/wallet/transactions", s.handleWalletTxns)
		})
	})

	r.Get("/ws", s.handleWS)
	r.Get("/health", func(w http.ResponseWriter, _ *http.Request) { writeJSON(w, 200, map[string]string{"status": "ok"}) })

	s.mountStatic(r)
	return r
}

// mountStatic serves the built SPA from ./web/dist when present, with history
// API fallback so client-side routes resolve to index.html. In dev the SPA runs
// under Vite, so a missing dist directory is fine.
func (s *Server) mountStatic(r chi.Router) {
	dist := s.cfg.WebDir
	if dist == "" {
		return
	}
	if _, err := os.Stat(filepath.Join(dist, "index.html")); err != nil {
		return
	}
	fs := http.FileServer(http.Dir(dist))
	r.Get("/*", func(w http.ResponseWriter, req *http.Request) {
		path := filepath.Join(dist, filepath.Clean(req.URL.Path))
		if info, err := os.Stat(path); err == nil && !info.IsDir() {
			fs.ServeHTTP(w, req)
			return
		}
		http.ServeFile(w, req, filepath.Join(dist, "index.html"))
	})
}

// ---------- shared helpers ----------

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

// errStatus maps domain errors to HTTP status codes.
func writeDomainErr(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, store.ErrInsufficientFunds):
		writeErr(w, http.StatusBadRequest, "insufficient funds")
	case errors.Is(err, engine.ErrBadOrder):
		writeErr(w, http.StatusBadRequest, err.Error())
	case errors.Is(err, engine.ErrMarketHalted):
		writeErr(w, http.StatusConflict, err.Error())
	case errors.Is(err, engine.ErrOrderNotFound):
		writeErr(w, http.StatusNotFound, err.Error())
	case errors.Is(err, engine.ErrNotOwner):
		writeErr(w, http.StatusForbidden, "not your order")
	case errors.Is(err, wallet.ErrKYCRequired):
		writeErr(w, http.StatusForbidden, err.Error())
	case errors.Is(err, wallet.ErrBelowMin), errors.Is(err, wallet.ErrInvalidAmount),
		errors.Is(err, wallet.ErrInvalidAddress), errors.Is(err, wallet.ErrUnknownAsset):
		writeErr(w, http.StatusBadRequest, err.Error())
	case errors.Is(err, store.ErrNotFound):
		writeErr(w, http.StatusNotFound, "not found")
	default:
		writeErr(w, http.StatusInternalServerError, err.Error())
	}
}

func decode(r *http.Request, v any) error {
	defer r.Body.Close()
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	return dec.Decode(v)
}

func queryInt(r *http.Request, key string, def, max int) int {
	v := r.URL.Query().Get(key)
	if v == "" {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil || n <= 0 {
		return def
	}
	if n > max {
		return max
	}
	return n
}

func symbolParam(r *http.Request) string {
	return strings.ToUpper(chi.URLParam(r, "symbol"))
}
