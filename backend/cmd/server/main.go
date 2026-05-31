// Command server boots the spot exchange: it opens the database, rebuilds the
// matching engine's order books, starts market-data and (optionally) the seed
// market-maker bot, and serves the REST + WebSocket API.
package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"cryptoex/internal/api"
	"cryptoex/internal/auth"
	"cryptoex/internal/bot"
	"cryptoex/internal/config"
	"cryptoex/internal/db"
	"cryptoex/internal/derivatives"
	"cryptoex/internal/engine"
	"cryptoex/internal/market"
	"cryptoex/internal/store"
	"cryptoex/internal/wallet"
	"cryptoex/internal/ws"
)

func main() {
	cfg := config.Load()
	log.SetFlags(log.LstdFlags | log.Lmsgprefix)
	log.SetPrefix("[exchange] ")

	// Refuse to run with the insecure default signing secret outside dev mode —
	// it would let anyone forge tokens for any user. Set a strong JWT_SECRET, or
	// DEV=true for local development.
	if cfg.JWTSecret == config.DefaultJWTSecret && !cfg.Dev {
		log.Fatal("refusing to start: JWT_SECRET is unset/default. Set a strong JWT_SECRET (32+ random bytes), or DEV=true for local development.")
	}
	if !cfg.Dev && len(cfg.JWTSecret) < 32 {
		log.Fatal("refusing to start: JWT_SECRET must be at least 32 bytes in production (or set DEV=true).")
	}

	database, err := db.Open(cfg.DBPath)
	if err != nil {
		log.Fatalf("open db: %v", err)
	}
	defer database.Close()
	st := store.New(database)

	markets, err := st.ListMarkets()
	if err != nil {
		log.Fatalf("load markets: %v", err)
	}
	perpMarkets, err := st.ListPerpMarkets()
	if err != nil {
		log.Fatalf("load perp markets: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	hub := ws.NewHub()
	md := market.NewService(st, hub)
	md.Init(markets)

	mgr := engine.NewManager(st, md, hub)
	if err := mgr.Init(markets); err != nil {
		log.Fatalf("init engines: %v", err)
	}

	perpMgr := derivatives.NewManager(st, md, hub, int64(cfg.PerpFunding))
	if err := perpMgr.Init(perpMarkets); err != nil {
		log.Fatalf("init perp engines: %v", err)
	}

	go md.Start(ctx, markets)
	go perpMgr.Start(ctx)

	walletSvc := wallet.NewService(st, hub)
	authMgr := auth.NewManager(cfg.JWTSecret, cfg.JWTTTLHours)

	if cfg.EnableBot {
		b := bot.New(st, mgr, markets, perpMgr, perpMarkets)
		if err := b.Start(ctx); err != nil {
			log.Printf("bot start failed (continuing without it): %v", err)
		} else {
			log.Printf("seed market-maker bot running across %d spot + %d perp markets", len(markets), len(perpMarkets))
		}
	}

	srv := api.NewServer(cfg, st, authMgr, mgr, perpMgr, md, walletSvc, hub)
	httpServer := &http.Server{
		Addr:              cfg.Addr,
		Handler:           srv.Router,
		ReadHeaderTimeout: 10 * time.Second,
	}

	go func() {
		log.Printf("listening on %s (CORS origin %s)", cfg.Addr, cfg.CORSOrigin)
		if err := httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("http server: %v", err)
		}
	}()

	// Graceful shutdown.
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	<-stop
	log.Println("shutting down...")
	cancel()
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer shutdownCancel()
	_ = httpServer.Shutdown(shutdownCtx)
}
