// Package api exposes the REST + WebSocket HTTP surface and wires together the
// store, auth, matching engine, market-data and wallet services.
package api

import (
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"cryptoex/internal/auth"
	"cryptoex/internal/config"
	"cryptoex/internal/derivatives"
	"cryptoex/internal/earn"
	"cryptoex/internal/engine"
	"cryptoex/internal/market"
	"cryptoex/internal/models"
	"cryptoex/internal/store"
	"cryptoex/internal/wallet"
	"cryptoex/internal/ws"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"
	"github.com/go-chi/httprate"
)

// maxBodyBytes caps request bodies to bound memory/CPU (paired with the
// length-bounded decimal parser) against oversized-payload DoS.
const maxBodyBytes int64 = 64 << 10 // 64 KiB

type Server struct {
	cfg    config.Config
	st     *store.Store
	auth   *auth.Manager
	mgr    *engine.Manager
	perp   *derivatives.Manager
	md     *market.Service
	wallet *wallet.Service
	earn   *earn.Service
	hub    *ws.Hub
	Router http.Handler
}

func NewServer(cfg config.Config, st *store.Store, am *auth.Manager, mgr *engine.Manager,
	perp *derivatives.Manager, md *market.Service, wal *wallet.Service, ern *earn.Service, hub *ws.Hub) *Server {
	s := &Server{cfg: cfg, st: st, auth: am, mgr: mgr, perp: perp, md: md, wallet: wal, earn: ern, hub: hub}
	s.Router = s.routes()
	return s
}

func (s *Server) routes() http.Handler {
	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	r.Use(middleware.Recoverer)
	r.Use(securityHeaders)
	r.Use(cors.Handler(cors.Options{
		AllowedOrigins:   []string{s.cfg.CORSOrigin, "http://localhost:5173", "http://127.0.0.1:5173"},
		AllowedMethods:   []string{"GET", "POST", "DELETE", "OPTIONS"},
		AllowedHeaders:   []string{"Authorization", "Content-Type"},
		AllowCredentials: false,
		MaxAge:           300,
	}))

	r.Route("/api", func(r chi.Router) {
		r.Use(maxBody(maxBodyBytes))                // bound request bodies (DoS)
		r.Use(httprate.LimitByIP(600, time.Minute)) // generous per-IP API ceiling

		// Auth endpoints get a much tighter per-IP limit to blunt brute-force
		// and credential-stuffing attacks.
		r.Group(func(r chi.Router) {
			r.Use(httprate.LimitByIP(20, time.Minute))
			r.Post("/auth/register", s.handleRegister)
			r.Post("/auth/login", s.handleLogin)
		})

		// Public market data.
		r.Get("/assets", s.handleAssets)
		r.Get("/markets", s.handleMarkets)
		r.Get("/tickers", s.handleTickers)
		r.Get("/markets/{symbol}", s.handleMarket)
		r.Get("/markets/{symbol}/depth", s.handleDepth)
		r.Get("/markets/{symbol}/trades", s.handleMarketTrades)
		r.Get("/markets/{symbol}/candles", s.handleCandles)

		// Public derivatives market data. Trades/candles/ticker reuse the spot
		// endpoints above (the trade tape is shared, keyed by symbol).
		r.Get("/perp/markets", s.handlePerpMarkets)
		r.Get("/perp/markets/{symbol}", s.handlePerpMarket)
		r.Get("/perp/markets/{symbol}/depth", s.handlePerpDepth)
		r.Get("/perp/markets/{symbol}/funding", s.handlePerpFunding)

		// Public earn product catalog.
		r.Get("/earn/products", s.handleEarnProducts)

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

			// Derivatives trading.
			r.Post("/perp/orders", s.handlePlacePerpOrder)
			r.Delete("/perp/orders/{id}", s.handleCancelPerpOrder)
			r.Get("/perp/orders", s.handleOpenPerpOrders)
			r.Get("/perp/orders/history", s.handlePerpOrderHistory)
			r.Get("/perp/positions", s.handlePositions)
			r.Post("/perp/positions/{symbol}/close", s.handleClosePosition)

			// Earn subscriptions.
			r.Get("/earn/positions", s.handleEarnPositions)
			r.Post("/earn/subscribe", s.handleEarnSubscribe)
			r.Post("/earn/positions/{id}/redeem", s.handleEarnRedeem)
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
	serve := func(w http.ResponseWriter, req *http.Request) {
		path := filepath.Join(dist, filepath.Clean(req.URL.Path))
		if info, err := os.Stat(path); err == nil && !info.IsDir() {
			// Vite emits content-hashed files under /assets, so they're safe to
			// cache forever; everything else (and the HTML shell) must revalidate
			// so a new deploy's asset references are picked up immediately.
			if strings.HasPrefix(req.URL.Path, "/assets/") {
				w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
			} else {
				w.Header().Set("Cache-Control", "no-cache")
			}
			fs.ServeHTTP(w, req)
			return
		}
		w.Header().Set("Cache-Control", "no-cache")
		http.ServeFile(w, req, filepath.Join(dist, "index.html"))
	}
	r.Get("/*", serve)
	r.Head("/*", serve) // CDNs / health checks may probe with HEAD
}

// ---------- security middleware ----------

// contentSecurityPolicy is strict: the built SPA loads only same-origin external
// JS/CSS (no inline scripts), needs inline style *attributes* (React/charts), and
// talks to same-origin REST + WebSocket — so 'self' covers connect-src.
const contentSecurityPolicy = "default-src 'self'; " +
	"base-uri 'self'; " +
	"object-src 'none'; " +
	"frame-ancestors 'none'; " +
	"script-src 'self'; " +
	"style-src 'self' 'unsafe-inline'; " +
	"img-src 'self' data:; " +
	"font-src 'self' data:; " +
	"connect-src 'self'"

func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Referrer-Policy", "strict-origin-when-cross-origin")
		h.Set("Content-Security-Policy", contentSecurityPolicy)
		// Honored only over HTTPS (ignored on plain HTTP), so safe to always set.
		h.Set("Strict-Transport-Security", "max-age=63072000; includeSubDomains")
		next.ServeHTTP(w, r)
	})
}

// maxBody caps the request body size to defend against oversized-payload DoS.
func maxBody(n int64) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			r.Body = http.MaxBytesReader(w, r.Body, n)
			next.ServeHTTP(w, r)
		})
	}
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
	case errors.Is(err, derivatives.ErrBadOrder), errors.Is(err, derivatives.ErrReduceOnly),
		errors.Is(err, derivatives.ErrNoLiquidity):
		writeErr(w, http.StatusBadRequest, err.Error())
	case errors.Is(err, derivatives.ErrMarketHalted):
		writeErr(w, http.StatusConflict, err.Error())
	case errors.Is(err, derivatives.ErrOrderNotFound):
		writeErr(w, http.StatusNotFound, err.Error())
	case errors.Is(err, derivatives.ErrNotOwner):
		writeErr(w, http.StatusForbidden, "not your order")
	case errors.Is(err, wallet.ErrKYCRequired):
		writeErr(w, http.StatusForbidden, err.Error())
	case errors.Is(err, wallet.ErrBelowMin), errors.Is(err, wallet.ErrInvalidAmount),
		errors.Is(err, wallet.ErrInvalidAddress), errors.Is(err, wallet.ErrUnknownAsset):
		writeErr(w, http.StatusBadRequest, err.Error())
	case errors.Is(err, earn.ErrUnknownProduct):
		writeErr(w, http.StatusNotFound, err.Error())
	case errors.Is(err, earn.ErrProductInactive), errors.Is(err, earn.ErrInvalidAmount),
		errors.Is(err, earn.ErrBelowMin), errors.Is(err, earn.ErrAboveMax),
		errors.Is(err, earn.ErrPositionClosed), errors.Is(err, earn.ErrNotMatured):
		writeErr(w, http.StatusBadRequest, err.Error())
	case errors.Is(err, earn.ErrNotOwner):
		writeErr(w, http.StatusForbidden, err.Error())
	case errors.Is(err, store.ErrNotFound):
		writeErr(w, http.StatusNotFound, "not found")
	default:
		// Unmapped error: log the detail server-side, return a generic message so
		// internal details (DB errors, etc.) are never disclosed to clients.
		log.Printf("unhandled error: %v", err)
		writeErr(w, http.StatusInternalServerError, "internal error")
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

// parseSideType validates and normalizes the side/type strings common to spot
// and perp order-placement requests. On error it writes the response itself
// and returns ok=false.
func parseSideType(w http.ResponseWriter, sideStr, typeStr string) (side models.Side, typ models.OrderType, ok bool) {
	side = models.Side(strings.ToLower(sideStr))
	if side != models.Buy && side != models.Sell {
		writeErr(w, http.StatusBadRequest, "side must be 'buy' or 'sell'")
		return "", "", false
	}
	typ = models.OrderType(strings.ToLower(typeStr))
	if typ != models.TypeLimit && typ != models.TypeMarket {
		writeErr(w, http.StatusBadRequest, "type must be 'limit' or 'market'")
		return "", "", false
	}
	return side, typ, true
}
