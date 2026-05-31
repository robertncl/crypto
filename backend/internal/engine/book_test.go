package engine

import (
	"testing"

	"cryptoex/internal/models"
	"cryptoex/internal/num"
)

func ro(id string, side models.Side, price, remaining string) *restingOrder {
	return &restingOrder{
		id:        id,
		side:      side,
		typ:       models.TypeLimit,
		price:     num.MustParse(price),
		remaining: num.MustParse(remaining),
	}
}

func TestSideBestBidIsHighest(t *testing.T) {
	s := newSide(true) // bids
	s.add(ro("1", models.Buy, "100", "1"))
	s.add(ro("2", models.Buy, "102", "1"))
	s.add(ro("3", models.Buy, "101", "1"))
	if best := s.best(); best == nil || best.price.String() != "102" {
		t.Fatalf("best bid = %v, want 102", best)
	}
}

func TestSideBestAskIsLowest(t *testing.T) {
	s := newSide(false) // asks
	s.add(ro("1", models.Sell, "100", "1"))
	s.add(ro("2", models.Sell, "102", "1"))
	s.add(ro("3", models.Sell, "101", "1"))
	if best := s.best(); best == nil || best.price.String() != "100" {
		t.Fatalf("best ask = %v, want 100", best)
	}
}

func TestSideBestEmpty(t *testing.T) {
	if newSide(true).best() != nil {
		t.Error("empty side best() should be nil")
	}
}

func TestSideRemoveLastOrderClearsLevel(t *testing.T) {
	s := newSide(false)
	o := ro("1", models.Sell, "100", "1")
	s.add(o)
	s.remove(o)
	if s.best() != nil {
		t.Error("side should be empty after removing only order")
	}
	if len(s.prices) != 0 {
		t.Errorf("price keys not cleaned up: %v", s.prices)
	}
}

func TestSideRemoveKeepsLevelWithRemaining(t *testing.T) {
	s := newSide(false)
	o1 := ro("1", models.Sell, "100", "1")
	o2 := ro("2", models.Sell, "100", "2")
	s.add(o1)
	s.add(o2)
	s.remove(o1)
	best := s.best()
	if best == nil || levelQty(best).String() != "2" {
		t.Errorf("level qty after removing one order = %v, want 2", best)
	}
}

func TestSideRemoveDetachedOrderIsNoop(t *testing.T) {
	s := newSide(false)
	o := ro("1", models.Sell, "100", "1") // never added
	s.remove(o)                           // must not panic
}

func TestLevelQtySumsRemaining(t *testing.T) {
	s := newSide(false)
	s.add(ro("1", models.Sell, "100", "1.5"))
	s.add(ro("2", models.Sell, "100", "2.5"))
	if got := levelQty(s.best()).String(); got != "4" {
		t.Errorf("levelQty = %s, want 4", got)
	}
}

func TestBookAddGetRemove(t *testing.T) {
	b := newBook()
	o := ro("abc", models.Buy, "100", "1")
	b.add(o)
	if b.get("abc") != o {
		t.Error("get() should return the added order")
	}
	b.remove(o)
	if b.get("abc") != nil {
		t.Error("get() should return nil after remove")
	}
}

func TestBookBestBidAsk(t *testing.T) {
	b := newBook()
	if b.bestBid().Sign() != 0 || b.bestAsk().Sign() != 0 {
		t.Error("empty book best bid/ask should be zero")
	}
	b.add(ro("1", models.Buy, "99", "1"))
	b.add(ro("2", models.Sell, "101", "1"))
	if b.bestBid().String() != "99" {
		t.Errorf("bestBid = %s, want 99", b.bestBid())
	}
	if b.bestAsk().String() != "101" {
		t.Errorf("bestAsk = %s, want 101", b.bestAsk())
	}
}

func TestBookSnapshotOrdering(t *testing.T) {
	b := newBook()
	b.add(ro("1", models.Buy, "99", "1"))
	b.add(ro("2", models.Buy, "98", "2"))
	b.add(ro("3", models.Buy, "100", "3"))
	b.add(ro("4", models.Sell, "101", "1"))
	b.add(ro("5", models.Sell, "103", "2"))
	b.add(ro("6", models.Sell, "102", "3"))

	ds := b.snapshot("BTC-USDT", 10)
	if ds.Market != "BTC-USDT" {
		t.Errorf("market = %s", ds.Market)
	}
	// Bids: highest price first.
	if len(ds.Bids) != 3 || ds.Bids[0].Price.String() != "100" || ds.Bids[2].Price.String() != "98" {
		t.Errorf("bids ordering wrong: %+v", ds.Bids)
	}
	// Asks: lowest price first.
	if len(ds.Asks) != 3 || ds.Asks[0].Price.String() != "101" || ds.Asks[2].Price.String() != "103" {
		t.Errorf("asks ordering wrong: %+v", ds.Asks)
	}
}

func TestBookSnapshotRespectsLimit(t *testing.T) {
	b := newBook()
	b.add(ro("1", models.Buy, "100", "1"))
	b.add(ro("2", models.Buy, "99", "1"))
	b.add(ro("3", models.Buy, "98", "1"))
	ds := b.snapshot("BTC-USDT", 2)
	if len(ds.Bids) != 2 {
		t.Fatalf("expected 2 bids with limit 2, got %d", len(ds.Bids))
	}
	if ds.Bids[0].Price.String() != "100" || ds.Bids[1].Price.String() != "99" {
		t.Errorf("limited snapshot took wrong levels: %+v", ds.Bids)
	}
}

func TestBookSnapshotEmpty(t *testing.T) {
	b := newBook()
	ds := b.snapshot("BTC-USDT", 5)
	if len(ds.Bids) != 0 || len(ds.Asks) != 0 {
		t.Errorf("empty book snapshot should have no levels: %+v", ds)
	}
}
