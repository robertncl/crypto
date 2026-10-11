package engine_test

import (
	"errors"
	"testing"

	"cryptoex/internal/engine"
	"cryptoex/internal/models"
	"cryptoex/internal/num"
)

// TestOverflowingNotionalRejected pins the fix for a price*quantity overflow:
// the product used to wrap to ~262.9 USDT, so a 1.8e11 USDT bid locked pennies,
// then failed mid-settlement and corrupted the resting maker's state.
func TestOverflowingNotionalRejected(t *testing.T) {
	st, mgr := setup(t)
	eng, _ := mgr.Get("BTC-USDT")
	const seller, attacker = int64(1), int64(2)
	credit(t, st, seller, "BTC", "1")
	credit(t, st, attacker, "USDT", "10000")
	sell, err := eng.Place(&models.Order{UserID: seller, Side: models.Sell, Type: models.TypeLimit, Price: num.MustParse("50000"), Quantity: num.MustParse("0.01")})
	if err != nil {
		t.Fatal(err)
	}
	_, err = eng.Place(&models.Order{UserID: attacker, Side: models.Buy, Type: models.TypeLimit,
		Price: num.MustParse("100000000"), Quantity: num.MustParse("1844.67441")})
	if !errors.Is(err, engine.ErrBadOrder) {
		t.Fatalf("overflowing order err = %v, want ErrBadOrder", err)
	}
	if got := avail(t, st, attacker, "USDT"); got != "10000" {
		t.Errorf("attacker USDT available = %s, want 10000 (nothing locked)", got)
	}
	if d := eng.Depth(5); len(d.Asks) != 1 || d.Asks[0].Qty.String() != "0.01" {
		t.Fatalf("maker level disturbed: %+v", d.Asks)
	}
	if err := eng.Cancel(sell.ID, seller); err != nil {
		t.Fatal(err)
	}
	if b, _ := st.GetBalance(seller, "BTC"); b.Available.String() != "1" || !b.Locked.IsZero() {
		t.Errorf("seller BTC after cancel = %s/%s, want 1/0", b.Available, b.Locked)
	}
}

// TestFailedFillRollsBack forces a settlement failure and checks that the
// engine's in-memory book is restored and the taker's reservation released,
// so the book never drifts from the database and no funds are stranded.
func TestFailedFillRollsBack(t *testing.T) {
	st, mgr := setup(t)
	eng, _ := mgr.Get("BTC-USDT")
	const seller, buyer = int64(1), int64(2)
	credit(t, st, seller, "BTC", "1")
	credit(t, st, buyer, "USDT", "1000")
	sell, err := eng.Place(&models.Order{UserID: seller, Side: models.Sell, Type: models.TypeLimit, Price: num.MustParse("50000"), Quantity: num.MustParse("0.01")})
	if err != nil {
		t.Fatal(err)
	}
	// Simulate drift: the seller's locked BTC disappears, so settling a fill
	// against the maker would drive it negative and the commit must fail.
	if _, err := st.DB().Exec(`UPDATE balances SET locked=0 WHERE user_id=? AND asset='BTC'`, seller); err != nil {
		t.Fatal(err)
	}
	buy, err := eng.Place(&models.Order{UserID: buyer, Side: models.Buy, Type: models.TypeLimit, Price: num.MustParse("50000"), Quantity: num.MustParse("0.01")})
	if err == nil {
		t.Fatalf("expected settlement failure, got order %+v", buy)
	}
	// Maker is untouched in memory (full quantity still resting).
	if d := eng.Depth(5); len(d.Asks) != 1 || d.Asks[0].Qty.String() != "0.01" || len(d.Bids) != 0 {
		t.Fatalf("book after failed fill = %+v, want maker intact and no resting bid", d)
	}
	if o, _ := st.GetOrder(sell.ID); !o.Filled.IsZero() || o.Status != models.StatusOpen {
		t.Errorf("maker row = filled %s status %s, want 0/open", o.Filled, o.Status)
	}
	// Taker's reservation released and the order closed — nothing stranded.
	if b, _ := st.GetBalance(buyer, "USDT"); b.Available.String() != "1000" || !b.Locked.IsZero() {
		t.Errorf("buyer USDT = %s/%s, want 1000/0", b.Available, b.Locked)
	}
	if open, _ := st.ListOpenOrders(buyer, ""); len(open) != 0 {
		t.Errorf("buyer has %d open orders, want 0", len(open))
	}
}

// TestDepthTotalsTrackFills checks the per-level running totals through
// partial fills, self-trade cancels and full fills.
func TestDepthTotalsTrackFills(t *testing.T) {
	st, mgr := setup(t)
	eng, _ := mgr.Get("BTC-USDT")
	const a, b, c = int64(1), int64(2), int64(3)
	credit(t, st, a, "BTC", "1")
	credit(t, st, b, "BTC", "1")
	credit(t, st, c, "USDT", "100000")
	credit(t, st, a, "USDT", "100000")
	place := func(u int64, side models.Side, qty string) {
		t.Helper()
		if _, err := eng.Place(&models.Order{UserID: u, Side: side, Type: models.TypeLimit, Price: num.MustParse("50000"), Quantity: num.MustParse(qty)}); err != nil {
			t.Fatal(err)
		}
	}
	askQty := func() string {
		d := eng.Depth(5)
		if len(d.Asks) == 0 {
			return "0"
		}
		return d.Asks[0].Qty.String()
	}
	place(a, models.Sell, "0.01")
	place(b, models.Sell, "0.02")
	if got := askQty(); got != "0.03" {
		t.Fatalf("level = %s, want 0.03", got)
	}
	place(c, models.Buy, "0.005") // partial fill of a's order
	if got := askQty(); got != "0.025" {
		t.Fatalf("after partial = %s, want 0.025", got)
	}
	place(a, models.Buy, "0.001") // self-trade: cancels a's 0.005 remainder, then fills b
	if got := askQty(); got != "0.019" {
		t.Fatalf("after self-trade cancel = %s, want 0.019", got)
	}
	place(c, models.Buy, "0.019")
	if got := askQty(); got != "0" {
		t.Fatalf("after full fill = %s, want empty", got)
	}
}
