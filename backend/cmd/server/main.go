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

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	hub := ws.NewHub()
	md := market.NewService(st, hub)
	md.Init(markets)
	go md.Start(ctx, markets)

	mgr := engine.NewManager(st, md, hub)
	if err := mgr.Init(markets); err != nil {
		log.Fatalf("init engines: %v", err)
	}

	walletSvc := wallet.NewService(st, hub)
	authMgr := auth.NewManager(cfg.JWTSecret, cfg.JWTTTLHours)

	if cfg.EnableBot {
		b := bot.New(st, mgr, markets)
		if err := b.Start(ctx); err != nil {
			log.Printf("bot start failed (continuing without it): %v", err)
		} else {
			log.Printf("seed market-maker bot running across %d markets", len(markets))
		}
	}

	srv := api.NewServer(cfg, st, authMgr, mgr, md, walletSvc, hub)
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
