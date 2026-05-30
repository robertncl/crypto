package derivatives

import (
	"context"
	"sync"
	"time"

	"cryptoex/internal/market"
	"cryptoex/internal/models"
	"cryptoex/internal/num"
	"cryptoex/internal/store"
	"cryptoex/internal/ws"
)

// fundingCap bounds the per-interval funding rate (0.05%).
var fundingCap = num.MustParse("0.0005")

// insuranceTarget is the USDT the insurance fund is topped up to on boot.
var insuranceTarget = num.MustParse("10000000")

// Manager owns the perp engines and runs the mark-price-driven funding and
// liquidation schedulers.
type Manager struct {
	engines     map[string]*Engine
	markets     []models.PerpMarket
	st          *store.Store
	md          *market.Service
	hub         *ws.Hub
	fundingEach time.Duration

	mu          sync.RWMutex
	funding     map[string]models.FundingInfo
	nextFunding int64
}

func NewManager(st *store.Store, md *market.Service, hub *ws.Hub, fundingSec int64) *Manager {
	if fundingSec <= 0 {
		fundingSec = 60
	}
	return &Manager{
		engines: map[string]*Engine{}, st: st, md: md, hub: hub,
		fundingEach: time.Duration(fundingSec) * time.Second,
		funding:     map[string]models.FundingInfo{},
	}
}

// Init seeds the insurance fund, registers perp symbols for market data, and
// builds an engine (with rebuilt book + positions) per market.
func (m *Manager) Init(markets []models.PerpMarket) error {
	m.markets = markets
	m.seedInsurance()
	for _, mk := range markets {
		m.md.Register(mk.Symbol)
		e := newEngine(mk, m.st, m.md, m.hub)
		if err := m.rebuild(e); err != nil {
			return err
		}
		e.start()
		m.engines[mk.Symbol] = e
		m.md.SetBook(mk.Symbol, e.book.bestBid(), e.book.bestAsk())
	}
	m.nextFunding = time.Now().Add(m.fundingEach).Unix()
	return nil
}

func (m *Manager) seedInsurance() {
	bal, err := m.st.GetBalance(store.InsuranceFundID, settle)
	if err != nil {
		return
	}
	if bal.Available.Lt(insuranceTarget) {
		topUp := insuranceTarget.Sub(bal.Available)
		_ = m.st.ApplyPostings("insurance_seed", time.Now().Unix(), []store.Posting{{
			UserID: store.InsuranceFundID, Asset: settle, DeltaAvailable: topUp,
			Reason: "insurance_seed", Ref: "boot",
		}})
	}
}

func (m *Manager) rebuild(e *Engine) error {
	positions, err := m.st.ListOpenPositionsByMarket(e.mkt.Symbol)
	if err != nil {
		return err
	}
	for i := range positions {
		p := positions[i]
		e.positions[p.UserID] = &p
	}
	orders, err := m.st.ListWorkingPerpOrdersByMarket(e.mkt.Symbol)
	if err != nil {
		return err
	}
	for i := range orders {
		o := orders[i]
		mo := &o
		remaining := mo.Quantity.Sub(mo.Filled)
		if remaining.Sign() <= 0 {
			continue
		}
		locked := num.Zero
		if !mo.ReduceOnly {
			locked = mo.Price.Mul(remaining).Div(num.FromInt(int64(mo.Leverage)))
		}
		e.book.add(&restingOrder{
			id: mo.ID, userID: mo.UserID, side: mo.Side, typ: mo.Type, price: mo.Price,
			remaining: remaining, lockedMargin: locked, leverage: mo.Leverage,
			reduceOnly: mo.ReduceOnly, createdAt: mo.CreatedAt, mo: mo,
		})
	}
	return nil
}

func (m *Manager) Get(symbol string) (*Engine, bool) {
	e, ok := m.engines[symbol]
	return e, ok
}

func (m *Manager) Markets() []models.PerpMarket { return m.markets }

// MarkPrice returns the current mark (last perp trade price).
func (m *Manager) MarkPrice(symbol string) num.Dec {
	if t, ok := m.md.GetTicker(symbol); ok {
		return t.Last
	}
	return num.Zero
}

// IndexPrice returns the spot index price for a perp market.
func (m *Manager) IndexPrice(symbol string) num.Dec {
	e, ok := m.engines[symbol]
	if !ok {
		return num.Zero
	}
	if t, ok := m.md.GetTicker(e.mkt.IndexSymbol); ok {
		return t.Last
	}
	return num.Zero
}

// Funding returns the latest funding info for a market.
func (m *Manager) Funding(symbol string) (models.FundingInfo, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	fi, ok := m.funding[symbol]
	return fi, ok
}

// Start runs the funding and liquidation schedulers until ctx is canceled.
func (m *Manager) Start(ctx context.Context) {
	fundingTick := time.NewTicker(m.fundingEach)
	liqTick := time.NewTicker(2 * time.Second)
	defer fundingTick.Stop()
	defer liqTick.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-fundingTick.C:
			m.runFunding()
		case <-liqTick.C:
			for _, e := range m.engines {
				e.CheckLiquidations(m.MarkPrice(e.mkt.Symbol))
			}
		}
	}
}

func (m *Manager) runFunding() {
	now := time.Now()
	m.mu.Lock()
	m.nextFunding = now.Add(m.fundingEach).Unix()
	next := m.nextFunding
	m.mu.Unlock()

	for _, e := range m.engines {
		mark := m.MarkPrice(e.mkt.Symbol)
		index := m.IndexPrice(e.mkt.Symbol)
		rate := num.Zero
		if index.Sign() > 0 && mark.Sign() > 0 {
			premium := mark.Sub(index).Div(index)
			rate = num.Max(fundingCap.Neg(), num.Min(fundingCap, premium))
		}
		e.ApplyFunding(rate, mark)
		fi := models.FundingInfo{
			Market: e.mkt.Symbol, Rate: rate, IndexPrice: index, MarkPrice: mark,
			IntervalSec: int64(m.fundingEach / time.Second), NextFundingTime: next,
		}
		m.mu.Lock()
		m.funding[e.mkt.Symbol] = fi
		m.mu.Unlock()
		m.hub.Publish("funding:"+e.mkt.Symbol, fi)
	}
}
