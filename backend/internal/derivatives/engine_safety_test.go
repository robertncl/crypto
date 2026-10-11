package derivatives_test

import (
	"errors"
	"testing"

	"cryptoex/internal/derivatives"
	"cryptoex/internal/models"
	"cryptoex/internal/num"
)

// TestPerpOverflowingNotionalRejected: price*quantity beyond the Dec range must
// be rejected, not wrapped into a tiny margin requirement.
func TestPerpOverflowingNotionalRejected(t *testing.T) {
	st, mgr := setup(t)
	eng, _ := mgr.Get("BTC-PERP")
	credit(t, st, 1, "10000")
	_, err := eng.Place(&models.PerpOrder{UserID: 1, Side: models.Buy, Type: models.TypeLimit,
		Price: num.MustParse("100000000"), Quantity: num.MustParse("1844.674"), Leverage: 100})
	if !errors.Is(err, derivatives.ErrBadOrder) {
		t.Fatalf("err = %v, want ErrBadOrder", err)
	}
	if av, lk := bal(t, st, 1); av != "10000" || lk != "0" {
		t.Errorf("balance = %s/%s, want 10000/0", av, lk)
	}
}

// TestPerpFailedFillRollsBack forces a settlement failure and checks the
// resting maker is restored in memory and the taker's margin is released.
func TestPerpFailedFillRollsBack(t *testing.T) {
	st, mgr := setup(t)
	eng, _ := mgr.Get("BTC-PERP")
	const maker, taker = int64(1), int64(2)
	credit(t, st, maker, "1000")
	credit(t, st, taker, "1000")
	ask, err := eng.Place(&models.PerpOrder{UserID: maker, Side: models.Sell, Type: models.TypeLimit,
		Price: num.MustParse("50000"), Quantity: num.MustParse("0.002"), Leverage: 10})
	if err != nil {
		t.Fatal(err)
	}
	// Drain the maker's balance behind the engine's back so the maker's fee
	// debit goes negative and the fill's commit fails.
	if _, err := st.DB().Exec(`UPDATE balances SET available=0, locked=0 WHERE user_id=? AND asset='USDT'`, maker); err != nil {
		t.Fatal(err)
	}
	if _, err := eng.Place(&models.PerpOrder{UserID: taker, Side: models.Buy, Type: models.TypeLimit,
		Price: num.MustParse("50000"), Quantity: num.MustParse("0.002"), Leverage: 10}); err == nil {
		t.Fatal("expected settlement failure")
	}
	if d := eng.Depth(5); len(d.Asks) != 1 || d.Asks[0].Qty.String() != "0.002" || len(d.Bids) != 0 {
		t.Fatalf("book after failed fill = %+v, want maker intact, no resting bid", d)
	}
	if o, _ := st.GetPerpOrder(ask.ID); !o.Filled.IsZero() || o.Status != models.StatusOpen {
		t.Errorf("maker row = filled %s status %s, want 0/open", o.Filled, o.Status)
	}
	if av, lk := bal(t, st, taker); av != "1000" || lk != "0" {
		t.Errorf("taker balance = %s/%s, want 1000/0", av, lk)
	}
	if pos, err := st.GetPosition(taker, "BTC-PERP"); err == nil && pos.Size.Sign() != 0 {
		t.Errorf("taker has a position after a failed fill: %+v", pos)
	}
}
