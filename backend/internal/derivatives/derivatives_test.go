package derivatives_test

import (
	"path/filepath"
	"testing"

	"cryptoex/internal/db"
	"cryptoex/internal/derivatives"
	"cryptoex/internal/market"
	"cryptoex/internal/models"
	"cryptoex/internal/num"
	"cryptoex/internal/store"
	"cryptoex/internal/ws"
)

func setup(t *testing.T) (*store.Store, *derivatives.Manager) {
	t.Helper()
	conn, err := db.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { conn.Close() })
	st := store.New(conn)
	hub := ws.NewHub()
	md := market.NewService(st, hub)
	spot, _ := st.ListMarkets()
	md.Init(spot)
	perps, _ := st.ListPerpMarkets()
	mgr := derivatives.NewManager(st, md, hub, 60)
	if err := mgr.Init(perps); err != nil {
		t.Fatalf("init perp: %v", err)
	}
	return st, mgr
}

func credit(t *testing.T, st *store.Store, user int64, amount string) {
	t.Helper()
	if err := st.ApplyPostings("seed", 0, []store.Posting{{
		UserID: user, Asset: "USDT", DeltaAvailable: num.MustParse(amount), Reason: "seed",
	}}); err != nil {
		t.Fatalf("seed: %v", err)
	}
}

func bal(t *testing.T, st *store.Store, user int64) (avail, locked string) {
	t.Helper()
	b, err := st.GetBalance(user, "USDT")
	if err != nil {
		t.Fatalf("balance: %v", err)
	}
	return b.Available.String(), b.Locked.String()
}

// TestPerpOpenSettlement opens offsetting long/short positions via a crossing
// limit order and verifies position state, isolated margin, and fees.
func TestPerpOpenSettlement(t *testing.T) {
	st, mgr := setup(t)
	eng, ok := mgr.Get("BTC-PERP")
	if !ok {
		t.Fatal("no BTC-PERP engine")
	}
	const a, b = int64(1), int64(2)
	credit(t, st, a, "100000")
	credit(t, st, b, "100000")

	// A rests a long (maker). margin = 50000*0.1/10 = 500.
	if _, err := eng.Place(&models.PerpOrder{UserID: a, Side: models.Buy, Type: models.TypeLimit, Price: num.MustParse("50000"), Quantity: num.MustParse("0.1"), Leverage: 10}); err != nil {
		t.Fatalf("place A: %v", err)
	}
	if av, lk := bal(t, st, a); av != "99500" || lk != "500" {
		t.Fatalf("A after rest: avail=%s locked=%s, want 99500/500", av, lk)
	}

	// B crosses with a short (taker), filling at 50000.
	if _, err := eng.Place(&models.PerpOrder{UserID: b, Side: models.Sell, Type: models.TypeLimit, Price: num.MustParse("50000"), Quantity: num.MustParse("0.1"), Leverage: 10}); err != nil {
		t.Fatalf("place B: %v", err)
	}

	pa, _ := st.GetPosition(a, "BTC-PERP")
	pb, _ := st.GetPosition(b, "BTC-PERP")
	if pa.Side != models.Long || pa.Size.String() != "0.1" || pa.EntryPrice.String() != "50000" || pa.Margin.String() != "500" {
		t.Errorf("A position = %+v", pa)
	}
	if pb.Side != models.Short || pb.Size.String() != "0.1" || pb.Margin.String() != "500" {
		t.Errorf("B position = %+v", pb)
	}
	// Maker fee 0.02% of 5000 = 1; taker fee 0.06% = 3.
	if av, lk := bal(t, st, a); av != "99499" || lk != "500" {
		t.Errorf("A balance: avail=%s locked=%s, want 99499/500", av, lk)
	}
	if av, lk := bal(t, st, b); av != "99497" || lk != "500" {
		t.Errorf("B balance: avail=%s locked=%s, want 99497/500", av, lk)
	}
}

// TestPerpLiquidation opens a 50x long and force-liquidates it when the mark
// price falls below the liquidation price, wiping the isolated margin.
func TestPerpLiquidation(t *testing.T) {
	st, mgr := setup(t)
	eng, _ := mgr.Get("BTC-PERP")
	const a, m = int64(1), int64(2)
	credit(t, st, a, "100000")
	credit(t, st, m, "100000")

	// Maker posts an ask; A market-buys into a 50x long. margin = 5000/50 = 100.
	if _, err := eng.Place(&models.PerpOrder{UserID: m, Side: models.Sell, Type: models.TypeLimit, Price: num.MustParse("50000"), Quantity: num.MustParse("0.1"), Leverage: 10}); err != nil {
		t.Fatalf("maker: %v", err)
	}
	if _, err := eng.Place(&models.PerpOrder{UserID: a, Side: models.Buy, Type: models.TypeMarket, Quantity: num.MustParse("0.1"), Leverage: 50}); err != nil {
		t.Fatalf("taker: %v", err)
	}
	pa, _ := st.GetPosition(a, "BTC-PERP")
	if pa.Side != models.Long || pa.Margin.String() != "100" {
		t.Fatalf("A position = %+v, want long margin 100", pa)
	}
	availBefore, _ := bal(t, st, a) // 100000 - 100 margin - 3 fee = 99897
	if availBefore != "99897" {
		t.Fatalf("A avail before liq = %s, want 99897", availBefore)
	}

	// Mark below liq price (~49250) triggers liquidation; loss = full margin.
	eng.CheckLiquidations(num.MustParse("49000"))

	pa, _ = st.GetPosition(a, "BTC-PERP")
	if pa.Side != models.Flat || pa.Size.Sign() != 0 {
		t.Errorf("A position not flat after liquidation: %+v", pa)
	}
	if av, lk := bal(t, st, a); av != "99897" || lk != "0" {
		t.Errorf("A after liq: avail=%s locked=%s, want 99897/0 (margin wiped)", av, lk)
	}
}
