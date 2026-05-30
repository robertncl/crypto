// Package market maintains live market-data derived from the trade stream:
// 24h rolling tickers and OHLCV candles for several intervals. It publishes
// updates to the WebSocket hub and is the source for the REST market-data
// endpoints' live fields.
package market

import (
	"context"
	"sync"
	"time"

	"cryptoex/internal/models"
	"cryptoex/internal/num"
	"cryptoex/internal/store"
	"cryptoex/internal/ws"
)

// Intervals (in seconds) for which live candles are maintained and published.
var Intervals = []int64{60, 300, 900, 3600, 14400, 86400}

type Service struct {
	store *store.Store
	hub   *ws.Hub

	mu       sync.RWMutex
	tickers  map[string]*models.Ticker
	candles  map[string]map[int64]*models.Candle // market -> intervalSec -> current bar
	bestBid  map[string]num.Dec
	bestAsk  map[string]num.Dec
	extra    []string // non-spot symbols (e.g. perps) registered for live data
}

func NewService(st *store.Store, hub *ws.Hub) *Service {
	return &Service{
		store:   st,
		hub:     hub,
		tickers: map[string]*models.Ticker{},
		candles: map[string]map[int64]*models.Candle{},
		bestBid: map[string]num.Dec{},
		bestAsk: map[string]num.Dec{},
	}
}

// Init loads initial tickers for all markets from historical trades.
func (s *Service) Init(markets []models.Market) {
	for _, m := range markets {
		t := &models.Ticker{Market: m.Symbol}
		s.mu.Lock()
		s.tickers[m.Symbol] = t
		s.candles[m.Symbol] = map[int64]*models.Candle{}
		s.bestBid[m.Symbol] = num.Zero
		s.bestAsk[m.Symbol] = num.Zero
		s.mu.Unlock()
		s.refresh(m.Symbol)
	}
}

// Register sets up live ticker/candle state for an additional (non-spot) symbol
// such as a perpetual market, so OnTrade and the refresh loop maintain it too.
func (s *Service) Register(symbol string) {
	s.mu.Lock()
	if _, exists := s.tickers[symbol]; !exists {
		s.tickers[symbol] = &models.Ticker{Market: symbol}
		s.candles[symbol] = map[int64]*models.Candle{}
		s.bestBid[symbol] = num.Zero
		s.bestAsk[symbol] = num.Zero
		s.extra = append(s.extra, symbol)
	}
	s.mu.Unlock()
	s.refresh(symbol)
}

// Start runs the periodic 24h ticker refresh until ctx is canceled, covering
// both the spot markets passed in and any registered extra symbols.
func (s *Service) Start(ctx context.Context, markets []models.Market) {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	publish := func(symbol string) {
		s.refresh(symbol)
		s.mu.RLock()
		t := *s.tickers[symbol]
		s.mu.RUnlock()
		s.hub.Publish("ticker:"+symbol, t)
	}
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			for _, m := range markets {
				publish(m.Symbol)
			}
			s.mu.RLock()
			extra := append([]string(nil), s.extra...)
			s.mu.RUnlock()
			for _, sym := range extra {
				publish(sym)
			}
		}
	}
}

// refresh recomputes 24h rolling statistics from the trades table.
func (s *Service) refresh(market string) {
	since := time.Now().Add(-24 * time.Hour).Unix()
	db := s.store.DB()

	var openRaw, lastRaw, highRaw, lowRaw, volRaw, qvolRaw int64
	_ = db.QueryRow(`SELECT price FROM trades WHERE market=? AND created_at>=? ORDER BY id ASC LIMIT 1`, market, since).Scan(&openRaw)
	_ = db.QueryRow(`SELECT price FROM trades WHERE market=? ORDER BY id DESC LIMIT 1`, market).Scan(&lastRaw)
	_ = db.QueryRow(`SELECT COALESCE(MAX(price),0),COALESCE(MIN(price),0),COALESCE(SUM(quantity),0),COALESCE(SUM(quote_qty),0)
	                 FROM trades WHERE market=? AND created_at>=?`, market, since).Scan(&highRaw, &lowRaw, &volRaw, &qvolRaw)

	last := num.FromRaw(lastRaw)
	open := num.FromRaw(openRaw)
	if open.IsZero() {
		open = last
	}
	change := last.Sub(open)
	var changePct num.Dec
	if !open.IsZero() {
		changePct = change.Div(open).MulRaw(100)
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	t := s.tickers[market]
	if t == nil {
		t = &models.Ticker{Market: market}
		s.tickers[market] = t
	}
	t.Last = last
	t.Open24h = open
	t.High24h = num.FromRaw(highRaw)
	t.Low24h = num.FromRaw(lowRaw)
	t.Volume24h = num.FromRaw(volRaw)
	t.QuoteVol24 = num.FromRaw(qvolRaw)
	t.Change = change
	t.ChangePct = changePct
	t.BestBid = s.bestBid[market]
	t.BestAsk = s.bestAsk[market]
	t.UpdatedAt = time.Now().Unix()
}

// OnTrade updates live candles and the last price, then publishes trade, candle
// and ticker updates. Called by the engine for every executed trade.
func (s *Service) OnTrade(t models.Trade) {
	s.mu.Lock()
	bars := s.candles[t.Market]
	if bars == nil {
		bars = map[int64]*models.Candle{}
		s.candles[t.Market] = bars
	}
	updated := make(map[int64]models.Candle, len(Intervals))
	for _, sec := range Intervals {
		bt := (t.CreatedAt / sec) * sec
		cur := bars[sec]
		if cur == nil || cur.Time != bt {
			cur = &models.Candle{Time: bt, Open: t.Price, High: t.Price, Low: t.Price, Close: t.Price, Volume: t.Quantity}
			bars[sec] = cur
		} else {
			cur.High = num.Max(cur.High, t.Price)
			cur.Low = num.Min(cur.Low, t.Price)
			cur.Close = t.Price
			cur.Volume = cur.Volume.Add(t.Quantity)
		}
		updated[sec] = *cur
	}
	tk := s.tickers[t.Market]
	if tk != nil {
		tk.Last = t.Price
	}
	s.mu.Unlock()

	s.hub.Publish("trades:"+t.Market, t)
	for sec, c := range updated {
		s.hub.Publish(klineTopic(t.Market, sec), c)
	}
}

// SetBook records the latest best bid/ask for ticker enrichment.
func (s *Service) SetBook(market string, bestBid, bestAsk num.Dec) {
	s.mu.Lock()
	s.bestBid[market] = bestBid
	s.bestAsk[market] = bestAsk
	if t := s.tickers[market]; t != nil {
		t.BestBid = bestBid
		t.BestAsk = bestAsk
	}
	s.mu.Unlock()
}

func (s *Service) GetTicker(market string) (models.Ticker, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	t, ok := s.tickers[market]
	if !ok {
		return models.Ticker{}, false
	}
	return *t, true
}

func (s *Service) AllTickers() []models.Ticker {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]models.Ticker, 0, len(s.tickers))
	for _, t := range s.tickers {
		out = append(out, *t)
	}
	return out
}

func klineTopic(market string, sec int64) string {
	return "kline:" + market + ":" + itoa(sec)
}

func itoa(n int64) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		b[i] = '-'
	}
	return string(b[i:])
}
