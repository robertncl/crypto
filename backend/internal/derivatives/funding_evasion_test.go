package derivatives_test

import (
	"testing"

	"cryptoex/internal/models"
	"cryptoex/internal/num"
)

// TestFundingCannotBeEvadedWithEmptyAvailable: funding used to be charged to
// the available balance only, and a payer without free balance was silently
// skipped. A trader could keep available at zero (e.g. locked in a far-away
// resting order) and never pay funding while still receiving it. The payment
// must instead come out of the position's isolated margin.
func TestFundingCannotBeEvadedWithEmptyAvailable(t *testing.T) {
	st, mgr := setup(t)
	eng, _ := mgr.Get("BTC-PERP")
	const m, a = int64(1), int64(2)
	credit(t, st, m, "1000000")
	credit(t, st, a, "1000")

	// A: long 1 BTC @ 50000, 100x -> margin 500, taker fee 30 -> 470 free.
	openLong(t, st, eng, m, a, "50000", "1", 100)
	// Park the remaining 470 in a resting bid far below the market.
	if _, err := eng.Place(&models.PerpOrder{UserID: a, Side: models.Buy, Type: models.TypeLimit,
		Price: num.MustParse("1000"), Quantity: num.MustParse("0.47"), Leverage: 1}); err != nil {
		t.Fatal(err)
	}
	if av, _ := bal(t, st, a); av != "0" {
		t.Fatalf("setup: available = %s, want 0", av)
	}
	_, lockedBefore := bal(t, st, a)

	// Positive rate: longs pay. 1 * 50000 * 0.0005 = 25 USDT.
	eng.ApplyFunding(num.MustParse("0.0005"), num.MustParse("50000"))

	pos, err := st.GetPosition(a, "BTC-PERP")
	if err != nil {
		t.Fatal(err)
	}
	if pos.FundingPaid.String() != "25" {
		t.Fatalf("funding paid = %s, want 25 (payment was skipped)", pos.FundingPaid)
	}
	if pos.Margin.String() != "475" {
		t.Errorf("position margin = %s, want 475 (funding taken from margin)", pos.Margin)
	}
	_, lockedAfter := bal(t, st, a)
	if got := num.MustParse(lockedBefore).Sub(num.MustParse(lockedAfter)).String(); got != "25" {
		t.Errorf("locked balance fell by %s, want 25", got)
	}
}
