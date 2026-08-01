package derivatives_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"cryptoex/internal/derivatives"
	"cryptoex/internal/models"
	"cryptoex/internal/num"
	"cryptoex/internal/store"
)

// openLong gives `taker` a long of qty at price by resting an ask from `maker`
// and crossing it with a market buy. Returns the resulting position.
func openLong(t *testing.T, st *store.Store, eng *derivatives.Engine, maker, taker int64, price, qty string, lev int) *models.Position {
	t.Helper()
	if _, err := eng.Place(&models.PerpOrder{
		UserID: maker, Side: models.Sell, Type: models.TypeLimit,
		Price: num.MustParse(price), Quantity: num.MustParse(qty), Leverage: lev,
	}); err != nil {
		t.Fatalf("maker ask: %v", err)
	}
	if _, err := eng.Place(&models.PerpOrder{
		UserID: taker, Side: models.Buy, Type: models.TypeMarket,
		Quantity: num.MustParse(qty), Leverage: lev,
	}); err != nil {
		t.Fatalf("taker buy: %v", err)
	}
	pos, err := st.GetPosition(taker, "BTC-PERP")
	if err != nil {
		t.Fatalf("position: %v", err)
	}
	return pos
}

// TestPerpPartialCloseRealizesPnL reduces half a long at a higher price and
// checks the released margin, realized PnL, and untouched entry price.
func TestPerpPartialCloseRealizesPnL(t *testing.T) {
	st, mgr := setup(t)
	eng, _ := mgr.Get("BTC-PERP")
	const m, a = int64(1), int64(2)
	credit(t, st, m, "100000")
	credit(t, st, a, "100000")

	// A goes long 0.002 @ 50000, 10x -> notional 100, margin 10.
	pos := openLong(t, st, eng, m, a, "50000", "0.002", 10)
	if pos.Side != models.Long || pos.Size.String() != "0.002" || pos.Margin.String() != "10" {
		t.Fatalf("opened position = %+v, want long 0.002 margin 10", pos)
	}

	// Maker rests a bid at 51000; A sells half into it.
	if _, err := eng.Place(&models.PerpOrder{
		UserID: m, Side: models.Buy, Type: models.TypeLimit,
		Price: num.MustParse("51000"), Quantity: num.MustParse("0.001"), Leverage: 10,
	}); err != nil {
		t.Fatalf("maker bid: %v", err)
	}
	if _, err := eng.Place(&models.PerpOrder{
		UserID: a, Side: models.Sell, Type: models.TypeLimit,
		Price: num.MustParse("51000"), Quantity: num.MustParse("0.001"), Leverage: 10,
	}); err != nil {
		t.Fatalf("A partial close: %v", err)
	}

	pos, _ = st.GetPosition(a, "BTC-PERP")
	// Half closed: size 0.002->0.001, margin 10->5 (pro-rata release).
	// PnL = (51000-50000) * 0.001 = +1. Entry price is unchanged by a reduce.
	if pos.Side != models.Long {
		t.Errorf("side = %s, want long (still open)", pos.Side)
	}
	if pos.Size.String() != "0.001" {
		t.Errorf("size = %s, want 0.001", pos.Size)
	}
	if pos.Margin.String() != "5" {
		t.Errorf("margin = %s, want 5 (pro-rata release)", pos.Margin)
	}
	if pos.EntryPrice.String() != "50000" {
		t.Errorf("entry = %s, want 50000 (unchanged by reduce)", pos.EntryPrice)
	}
	if pos.RealizedPnL.String() != "1" {
		t.Errorf("realized pnl = %s, want 1", pos.RealizedPnL)
	}
}

// TestPerpFullCloseWithLoss closes an entire long at a lower price and checks
// the position goes flat with a negative realized PnL and all margin released.
func TestPerpFullCloseWithLoss(t *testing.T) {
	st, mgr := setup(t)
	eng, _ := mgr.Get("BTC-PERP")
	const m, a = int64(1), int64(2)
	credit(t, st, m, "100000")
	credit(t, st, a, "100000")

	// Long 0.001 @ 50000, 10x -> notional 50, margin 5.
	pos := openLong(t, st, eng, m, a, "50000", "0.001", 10)
	if pos.Margin.String() != "5" {
		t.Fatalf("margin = %s, want 5", pos.Margin)
	}
	_, lockedBefore := bal(t, st, a)
	if lockedBefore != "5" {
		t.Fatalf("locked = %s, want 5", lockedBefore)
	}

	// Close the whole position at 49000 -> loss of 1.
	if _, err := eng.Place(&models.PerpOrder{
		UserID: m, Side: models.Buy, Type: models.TypeLimit,
		Price: num.MustParse("49000"), Quantity: num.MustParse("0.001"), Leverage: 10,
	}); err != nil {
		t.Fatalf("maker bid: %v", err)
	}
	if _, err := eng.Place(&models.PerpOrder{
		UserID: a, Side: models.Sell, Type: models.TypeLimit,
		Price: num.MustParse("49000"), Quantity: num.MustParse("0.001"), Leverage: 10,
	}); err != nil {
		t.Fatalf("A close: %v", err)
	}

	pos, _ = st.GetPosition(a, "BTC-PERP")
	if pos.Side != models.Flat || pos.Size.Sign() != 0 {
		t.Errorf("position = %+v, want flat", pos)
	}
	if pos.EntryPrice.Sign() != 0 || pos.Margin.Sign() != 0 {
		t.Errorf("entry/margin = %s/%s, want 0/0 after full close", pos.EntryPrice, pos.Margin)
	}
	if pos.RealizedPnL.String() != "-1" {
		t.Errorf("realized pnl = %s, want -1", pos.RealizedPnL)
	}
	// All isolated margin is released back out of `locked`.
	if _, locked := bal(t, st, a); locked != "0" {
		t.Errorf("locked after full close = %s, want 0", locked)
	}
}

// TestPerpFlipLongToShort sells more than the open long so the remainder opens
// a short on the other side (the flip branch of closeOrFlip).
func TestPerpFlipLongToShort(t *testing.T) {
	st, mgr := setup(t)
	eng, _ := mgr.Get("BTC-PERP")
	const m, a = int64(1), int64(2)
	credit(t, st, m, "100000")
	credit(t, st, a, "100000")

	openLong(t, st, eng, m, a, "50000", "0.001", 10)

	// Maker bids 0.002 @ 50000; A sells 0.002 -> closes 0.001, flips short 0.001.
	if _, err := eng.Place(&models.PerpOrder{
		UserID: m, Side: models.Buy, Type: models.TypeLimit,
		Price: num.MustParse("50000"), Quantity: num.MustParse("0.002"), Leverage: 10,
	}); err != nil {
		t.Fatalf("maker bid: %v", err)
	}
	if _, err := eng.Place(&models.PerpOrder{
		UserID: a, Side: models.Sell, Type: models.TypeLimit,
		Price: num.MustParse("50000"), Quantity: num.MustParse("0.002"), Leverage: 10,
	}); err != nil {
		t.Fatalf("A flip: %v", err)
	}

	pos, _ := st.GetPosition(a, "BTC-PERP")
	if pos.Side != models.Short {
		t.Fatalf("side = %s, want short after flip", pos.Side)
	}
	if pos.Size.String() != "0.001" {
		t.Errorf("size = %s, want 0.001 (the flipped remainder)", pos.Size)
	}
	if pos.EntryPrice.String() != "50000" {
		t.Errorf("entry = %s, want 50000 (re-entered at fill price)", pos.EntryPrice)
	}
	// Closed at the entry price, so PnL is flat; new short margin = 50/10 = 5.
	if pos.RealizedPnL.Sign() != 0 {
		t.Errorf("realized pnl = %s, want 0 (closed at entry)", pos.RealizedPnL)
	}
	if pos.Margin.String() != "5" {
		t.Errorf("margin = %s, want 5 for the new short", pos.Margin)
	}
}

// TestPerpReduceOnlyRejectedWithoutPosition covers reduceOnlyBlocked.
func TestPerpReduceOnlyRejectedWithoutPosition(t *testing.T) {
	st, mgr := setup(t)
	eng, _ := mgr.Get("BTC-PERP")
	const a = int64(1)
	credit(t, st, a, "100000")

	_, err := eng.Place(&models.PerpOrder{
		UserID: a, Side: models.Sell, Type: models.TypeLimit,
		Price: num.MustParse("50000"), Quantity: num.MustParse("0.001"), Leverage: 10, ReduceOnly: true,
	})
	if !errors.Is(err, derivatives.ErrReduceOnly) {
		t.Errorf("reduce-only with no position: err = %v, want ErrReduceOnly", err)
	}
}

// TestPerpReduceOnlyCannotFlip caps a reduce-only order at the position size.
func TestPerpReduceOnlyCannotFlip(t *testing.T) {
	st, mgr := setup(t)
	eng, _ := mgr.Get("BTC-PERP")
	const m, a = int64(1), int64(2)
	credit(t, st, m, "100000")
	credit(t, st, a, "100000")

	openLong(t, st, eng, m, a, "50000", "0.001", 10)

	// Bid for more than the position; a reduce-only sell must only close 0.001.
	if _, err := eng.Place(&models.PerpOrder{
		UserID: m, Side: models.Buy, Type: models.TypeLimit,
		Price: num.MustParse("50000"), Quantity: num.MustParse("0.003"), Leverage: 10,
	}); err != nil {
		t.Fatalf("maker bid: %v", err)
	}
	if _, err := eng.Place(&models.PerpOrder{
		UserID: a, Side: models.Sell, Type: models.TypeLimit,
		Price: num.MustParse("50000"), Quantity: num.MustParse("0.003"), Leverage: 10, ReduceOnly: true,
	}); err != nil {
		t.Fatalf("reduce-only close: %v", err)
	}

	pos, _ := st.GetPosition(a, "BTC-PERP")
	if pos.Side != models.Flat || pos.Size.Sign() != 0 {
		t.Errorf("position = %+v, want flat (reduce-only must not flip)", pos)
	}
}

// TestPerpCancelReleasesMargin covers cancel/cancelResting/unlockLeftover.
func TestPerpCancelReleasesMargin(t *testing.T) {
	st, mgr := setup(t)
	eng, _ := mgr.Get("BTC-PERP")
	const a = int64(1)
	credit(t, st, a, "100000")

	o, err := eng.Place(&models.PerpOrder{
		UserID: a, Side: models.Buy, Type: models.TypeLimit,
		Price: num.MustParse("50000"), Quantity: num.MustParse("0.001"), Leverage: 10,
	})
	if err != nil {
		t.Fatalf("place: %v", err)
	}
	if av, lk := bal(t, st, a); av != "99995" || lk != "5" {
		t.Fatalf("after rest: avail=%s locked=%s, want 99995/5", av, lk)
	}

	if err := eng.Cancel(o.ID, a); err != nil {
		t.Fatalf("cancel: %v", err)
	}
	if av, lk := bal(t, st, a); av != "100000" || lk != "0" {
		t.Errorf("after cancel: avail=%s locked=%s, want 100000/0", av, lk)
	}
	got, _ := st.GetPerpOrder(o.ID)
	if got.Status != models.StatusCanceled {
		t.Errorf("status = %s, want canceled", got.Status)
	}
}

func TestPerpCancelErrors(t *testing.T) {
	st, mgr := setup(t)
	eng, _ := mgr.Get("BTC-PERP")
	const a, b = int64(1), int64(2)
	credit(t, st, a, "100000")

	if err := eng.Cancel("no-such-order", a); !errors.Is(err, derivatives.ErrOrderNotFound) {
		t.Errorf("cancel unknown: err = %v, want ErrOrderNotFound", err)
	}
	o, err := eng.Place(&models.PerpOrder{
		UserID: a, Side: models.Buy, Type: models.TypeLimit,
		Price: num.MustParse("50000"), Quantity: num.MustParse("0.001"), Leverage: 10,
	})
	if err != nil {
		t.Fatalf("place: %v", err)
	}
	if err := eng.Cancel(o.ID, b); !errors.Is(err, derivatives.ErrNotOwner) {
		t.Errorf("cancel as non-owner: err = %v, want ErrNotOwner", err)
	}
}

// TestPerpApplyFunding moves funding from the long to the short via the
// insurance fund and records it on both positions.
func TestPerpApplyFunding(t *testing.T) {
	st, mgr := setup(t)
	eng, _ := mgr.Get("BTC-PERP")
	const m, a = int64(1), int64(2)
	credit(t, st, m, "100000")
	credit(t, st, a, "100000")

	// A long 0.001, M short 0.001 (the maker took the other side).
	openLong(t, st, eng, m, a, "50000", "0.001", 10)
	longAvail, _ := bal(t, st, a)
	shortAvail, _ := bal(t, st, m)

	// rate 0.0001 at mark 50000 on size 0.001 -> payment 0.005, long pays short.
	eng.ApplyFunding(num.MustParse("0.0001"), num.MustParse("50000"))

	gotLong, _ := bal(t, st, a)
	gotShort, _ := bal(t, st, m)
	wantLong := num.MustParse(longAvail).Sub(num.MustParse("0.005")).String()
	wantShort := num.MustParse(shortAvail).Add(num.MustParse("0.005")).String()
	if gotLong != wantLong {
		t.Errorf("long avail = %s, want %s (paid funding)", gotLong, wantLong)
	}
	if gotShort != wantShort {
		t.Errorf("short avail = %s, want %s (received funding)", gotShort, wantShort)
	}

	pa, _ := st.GetPosition(a, "BTC-PERP")
	if pa.FundingPaid.String() != "0.005" {
		t.Errorf("long funding paid = %s, want 0.005", pa.FundingPaid)
	}
}

// TestPerpApplyFundingNoopCases covers the early returns.
func TestPerpApplyFundingNoopCases(t *testing.T) {
	st, mgr := setup(t)
	eng, _ := mgr.Get("BTC-PERP")
	const m, a = int64(1), int64(2)
	credit(t, st, m, "100000")
	credit(t, st, a, "100000")
	openLong(t, st, eng, m, a, "50000", "0.001", 10)
	before, _ := bal(t, st, a)

	eng.ApplyFunding(num.Zero, num.MustParse("50000"))  // zero rate
	eng.ApplyFunding(num.MustParse("0.0001"), num.Zero) // no mark price

	if after, _ := bal(t, st, a); after != before {
		t.Errorf("balance moved on a no-op funding call: %s -> %s", before, after)
	}
}

// TestPerpDepthAndAccessors covers Depth, Market, Enrich and the manager's
// Markets/MarkPrice/IndexPrice/Funding accessors.
func TestPerpDepthAndAccessors(t *testing.T) {
	st, mgr := setup(t)
	eng, _ := mgr.Get("BTC-PERP")
	const m, a = int64(1), int64(2)
	credit(t, st, m, "100000")
	credit(t, st, a, "100000")

	if mkt := eng.Market(); mkt.Symbol != "BTC-PERP" || mkt.MaxLeverage != 100 {
		t.Errorf("Market() = %+v", mkt)
	}
	if perps := mgr.Markets(); len(perps) != 3 {
		t.Errorf("Markets() = %d, want 3", len(perps))
	}
	if _, ok := mgr.Get("NOPE-PERP"); ok {
		t.Error("Get(NOPE-PERP) should not resolve")
	}
	// No funding tick has run yet.
	if _, ok := mgr.Funding("BTC-PERP"); ok {
		t.Error("Funding() should be unset before the first tick")
	}

	// Rest both sides and check the depth snapshot.
	eng.Place(&models.PerpOrder{UserID: m, Side: models.Buy, Type: models.TypeLimit,
		Price: num.MustParse("49000"), Quantity: num.MustParse("0.001"), Leverage: 10})
	eng.Place(&models.PerpOrder{UserID: m, Side: models.Sell, Type: models.TypeLimit,
		Price: num.MustParse("51000"), Quantity: num.MustParse("0.001"), Leverage: 10})

	d := eng.Depth(10)
	if d.Market != "BTC-PERP" || len(d.Bids) != 1 || len(d.Asks) != 1 {
		t.Fatalf("depth = %+v, want one bid and one ask", d)
	}
	if d.Bids[0].Price.String() != "49000" || d.Asks[0].Price.String() != "51000" {
		t.Errorf("depth prices = %s/%s, want 49000/51000", d.Bids[0].Price, d.Asks[0].Price)
	}

	// A trade sets the perp mark price; the spot index has no trades yet.
	openLong(t, st, eng, m, a, "50000", "0.001", 10)
	if mp := mgr.MarkPrice("BTC-PERP"); mp.Sign() <= 0 {
		t.Errorf("MarkPrice = %s, want > 0 after a trade", mp)
	}
	if ip := mgr.IndexPrice("BTC-PERP"); ip.Sign() != 0 {
		t.Errorf("IndexPrice = %s, want 0 (no spot trades)", ip)
	}
	if ip := mgr.IndexPrice("NOPE-PERP"); ip.Sign() != 0 {
		t.Errorf("IndexPrice(unknown) = %s, want 0", ip)
	}

	// Enrich fills the computed view fields.
	pos, _ := st.GetPosition(a, "BTC-PERP")
	e := eng.Enrich(*pos, num.MustParse("52000"))
	if e.MarkPrice.String() != "52000" || e.Notional.Sign() <= 0 || e.UnrealizedPnL.Sign() <= 0 {
		t.Errorf("Enrich = %+v, want mark 52000 with positive notional/uPnL", e)
	}
	// A flat position is returned untouched.
	flat := models.Position{Side: models.Flat}
	if got := eng.Enrich(flat, num.MustParse("52000")); got.Notional.Sign() != 0 {
		t.Errorf("Enrich(flat) = %+v, want untouched", got)
	}
}

// TestManagerStartRunsFunding drives the scheduler loop so a funding tick is
// computed and published (covers Start and runFunding).
func TestManagerStartRunsFunding(t *testing.T) {
	st, mgr := setupWithFunding(t, 1) // 1s funding interval
	eng, _ := mgr.Get("BTC-PERP")
	const m, a = int64(1), int64(2)
	credit(t, st, m, "100000")
	credit(t, st, a, "100000")
	openLong(t, st, eng, m, a, "50000", "0.001", 10)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go mgr.Start(ctx)

	deadline := time.Now().Add(4 * time.Second)
	for time.Now().Before(deadline) {
		if fi, ok := mgr.Funding("BTC-PERP"); ok {
			if fi.Market != "BTC-PERP" || fi.IntervalSec != 1 {
				t.Errorf("funding info = %+v", fi)
			}
			if fi.NextFundingTime == 0 {
				t.Error("next funding time not set")
			}
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatal("funding tick did not run within the deadline")
}
