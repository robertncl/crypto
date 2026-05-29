package engine

import (
	"container/list"
	"sort"

	"cryptoex/internal/models"
	"cryptoex/internal/num"
)

// restingOrder is the in-memory state of a working order living in the book.
// It is also used to represent the incoming taker order while it is matched.
type restingOrder struct {
	id        string
	userID    int64
	side      models.Side
	typ       models.OrderType
	price     num.Dec // limit price (Zero for market orders)
	remaining num.Dec // base quantity still to fill

	// budget is the remaining quote to spend, used only for market BUY orders
	// where the order is sized in quote rather than base.
	budget num.Dec

	// locked is the amount of this order's own funds still held in the locked
	// balance (quote for buys, base for sells). Released on completion/cancel.
	locked num.Dec

	createdAt int64

	// bookkeeping for O(1) removal once resting
	elem  *list.Element
	level *priceLevel
}

func (o *restingOrder) isMarket() bool { return o.typ == models.TypeMarket }

// priceLevel is a FIFO queue of orders sharing a price (time priority).
type priceLevel struct {
	price  num.Dec
	orders *list.List // of *restingOrder
}

// side is one half of the book (all bids or all asks).
type side struct {
	isBid  bool
	levels map[int64]*priceLevel
	prices []int64 // raw prices kept sorted ascending
}

func newSide(isBid bool) *side {
	return &side{isBid: isBid, levels: map[int64]*priceLevel{}}
}

func (s *side) add(o *restingOrder) {
	key := o.price.Raw()
	lvl := s.levels[key]
	if lvl == nil {
		lvl = &priceLevel{price: o.price, orders: list.New()}
		s.levels[key] = lvl
		// insert key into sorted slice
		i := sort.Search(len(s.prices), func(i int) bool { return s.prices[i] >= key })
		s.prices = append(s.prices, 0)
		copy(s.prices[i+1:], s.prices[i:])
		s.prices[i] = key
	}
	o.elem = lvl.orders.PushBack(o)
	o.level = lvl
}

// remove detaches a resting order from its level.
func (s *side) remove(o *restingOrder) {
	if o.level == nil || o.elem == nil {
		return
	}
	o.level.orders.Remove(o.elem)
	o.elem = nil
	if o.level.orders.Len() == 0 {
		key := o.level.price.Raw()
		delete(s.levels, key)
		i := sort.Search(len(s.prices), func(i int) bool { return s.prices[i] >= key })
		if i < len(s.prices) && s.prices[i] == key {
			s.prices = append(s.prices[:i], s.prices[i+1:]...)
		}
	}
	o.level = nil
}

// best returns the most aggressive price level (highest bid / lowest ask) or nil.
func (s *side) best() *priceLevel {
	if len(s.prices) == 0 {
		return nil
	}
	if s.isBid {
		return s.levels[s.prices[len(s.prices)-1]]
	}
	return s.levels[s.prices[0]]
}

// book holds both sides plus an id index for cancellation.
type book struct {
	bids  *side
	asks  *side
	index map[string]*restingOrder
}

func newBook() *book {
	return &book{bids: newSide(true), asks: newSide(false), index: map[string]*restingOrder{}}
}

func (b *book) add(o *restingOrder) {
	if o.side == models.Buy {
		b.bids.add(o)
	} else {
		b.asks.add(o)
	}
	b.index[o.id] = o
}

func (b *book) get(id string) *restingOrder { return b.index[id] }

func (b *book) remove(o *restingOrder) {
	if o.side == models.Buy {
		b.bids.remove(o)
	} else {
		b.asks.remove(o)
	}
	delete(b.index, o.id)
}

// DepthLevel is an aggregated price level for the depth snapshot.
type DepthLevel struct {
	Price num.Dec `json:"price"`
	Qty   num.Dec `json:"qty"`
}

// DepthSnapshot is the top-of-book view published to clients.
type DepthSnapshot struct {
	Market string       `json:"market"`
	Bids   []DepthLevel `json:"bids"` // highest price first
	Asks   []DepthLevel `json:"asks"` // lowest price first
}

// snapshot builds an aggregated depth view of up to `limit` levels per side.
func (b *book) snapshot(market string, limit int) DepthSnapshot {
	ds := DepthSnapshot{Market: market, Bids: []DepthLevel{}, Asks: []DepthLevel{}}
	// bids: iterate from highest price downward
	for i := len(b.bids.prices) - 1; i >= 0 && len(ds.Bids) < limit; i-- {
		lvl := b.bids.levels[b.bids.prices[i]]
		ds.Bids = append(ds.Bids, DepthLevel{Price: lvl.price, Qty: levelQty(lvl)})
	}
	// asks: iterate from lowest price upward
	for i := 0; i < len(b.asks.prices) && len(ds.Asks) < limit; i++ {
		lvl := b.asks.levels[b.asks.prices[i]]
		ds.Asks = append(ds.Asks, DepthLevel{Price: lvl.price, Qty: levelQty(lvl)})
	}
	return ds
}

func levelQty(lvl *priceLevel) num.Dec {
	sum := num.Zero
	for e := lvl.orders.Front(); e != nil; e = e.Next() {
		sum = sum.Add(e.Value.(*restingOrder).remaining)
	}
	return sum
}

func (b *book) bestBid() num.Dec {
	if l := b.bids.best(); l != nil {
		return l.price
	}
	return num.Zero
}

func (b *book) bestAsk() num.Dec {
	if l := b.asks.best(); l != nil {
		return l.price
	}
	return num.Zero
}
