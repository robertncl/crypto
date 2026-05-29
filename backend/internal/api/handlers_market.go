package api

import (
	"net/http"

	"cryptoex/internal/models"
)

func (s *Server) handleAssets(w http.ResponseWriter, _ *http.Request) {
	assets, err := s.st.ListAssets()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "could not load assets")
		return
	}
	writeJSON(w, http.StatusOK, assets)
}

func (s *Server) handleMarkets(w http.ResponseWriter, _ *http.Request) {
	markets, err := s.st.ListMarkets()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "could not load markets")
		return
	}
	writeJSON(w, http.StatusOK, markets)
}

func (s *Server) handleMarket(w http.ResponseWriter, r *http.Request) {
	m, err := s.st.GetMarket(symbolParam(r))
	if err != nil {
		writeErr(w, http.StatusNotFound, "market not found")
		return
	}
	writeJSON(w, http.StatusOK, m)
}

func (s *Server) handleTickers(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, s.md.AllTickers())
}

func (s *Server) handleDepth(w http.ResponseWriter, r *http.Request) {
	sym := symbolParam(r)
	eng, ok := s.mgr.Get(sym)
	if !ok {
		writeErr(w, http.StatusNotFound, "market not found")
		return
	}
	limit := queryInt(r, "limit", 50, 200)
	writeJSON(w, http.StatusOK, eng.Depth(limit))
}

func (s *Server) handleMarketTrades(w http.ResponseWriter, r *http.Request) {
	sym := symbolParam(r)
	limit := queryInt(r, "limit", 50, 500)
	trades, err := s.st.ListTradesByMarket(sym, limit)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "could not load trades")
		return
	}
	if trades == nil {
		trades = []models.Trade{}
	}
	writeJSON(w, http.StatusOK, trades)
}

// intervalSeconds maps a human interval label to seconds.
var intervalSeconds = map[string]int64{
	"1m": 60, "5m": 300, "15m": 900, "30m": 1800,
	"1h": 3600, "4h": 14400, "1d": 86400,
}

func (s *Server) handleCandles(w http.ResponseWriter, r *http.Request) {
	sym := symbolParam(r)
	label := r.URL.Query().Get("interval")
	sec, ok := intervalSeconds[label]
	if !ok {
		sec = 60
	}
	limit := queryInt(r, "limit", 300, 1000)
	candles, err := s.st.Candles(sym, sec, limit)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "could not load candles")
		return
	}
	if candles == nil {
		candles = []models.Candle{}
	}
	writeJSON(w, http.StatusOK, candles)
}
