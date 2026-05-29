package engine

import (
	"cryptoex/internal/market"
	"cryptoex/internal/models"
	"cryptoex/internal/num"
	"cryptoex/internal/store"
	"cryptoex/internal/ws"
)

// Manager owns one Engine per market and routes commands to them.
type Manager struct {
	engines map[string]*Engine
	st      *store.Store
	md      *market.Service
	hub     *ws.Hub
}

func NewManager(st *store.Store, md *market.Service, hub *ws.Hub) *Manager {
	return &Manager{engines: map[string]*Engine{}, st: st, md: md, hub: hub}
}

// Init creates an engine for each market, rebuilds its in-memory book from the
// persisted working orders, and starts the matching goroutine.
func (m *Manager) Init(markets []models.Market) error {
	for _, mk := range markets {
		e := newEngine(mk, m.st, m.md, m.hub)
		if err := m.rebuild(e); err != nil {
			return err
		}
		e.start()
		m.engines[mk.Symbol] = e
		m.md.SetBook(mk.Symbol, e.book.bestBid(), e.book.bestAsk())
	}
	return nil
}

// rebuild loads open/partial orders into the book without re-reserving funds
// (they were already locked when originally placed).
func (m *Manager) rebuild(e *Engine) error {
	orders, err := m.st.ListWorkingOrdersByMarket(e.mkt.Symbol)
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
		var locked num.Dec
		if mo.Side == models.Buy {
			locked = mo.Price.Mul(remaining)
		} else {
			locked = remaining
		}
		ro := &restingOrder{
			id: mo.ID, userID: mo.UserID, side: mo.Side, typ: mo.Type,
			price: mo.Price, remaining: remaining, locked: locked,
			createdAt: mo.CreatedAt, mo: mo,
		}
		e.book.add(ro)
	}
	return nil
}

func (m *Manager) Get(symbol string) (*Engine, bool) {
	e, ok := m.engines[symbol]
	return e, ok
}
