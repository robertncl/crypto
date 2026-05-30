package engine_test

import (
	"path/filepath"
	"testing"

	"cryptoex/internal/db"
	"cryptoex/internal/engine"
	"cryptoex/internal/market"
	"cryptoex/internal/models"
	"cryptoex/internal/num"
	"cryptoex/internal/store"
	"cryptoex/internal/ws"
)

// setup spins up a real (temp-file) store + engine manager for BTC-USDT.
func setup(t *testing.T) (*store.Store, *engine.Manager) {
	t.Helper()
	conn, err := db.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { conn.Close() })
	st := store.New(conn)
	markets, err := st.ListMarkets()
	if err != nil {
		t.Fatalf("markets: %v", err)
	}
	hub := ws.NewHub()
	md := market.NewService(st, hub)
	md.Init(markets)
	mgr := engine.NewManager(st, md, hub)
	if err := mgr.Init(markets); err != nil {
		t.Fatalf("init engines: %v", err)
	}
	return st, mgr
}

func credit(t *testing.T, st *store.Store, user int64, asset, amount string) {
	t.Helper()
	if err := st.ApplyPostings("seed", 0, []store.Posting{{
		UserID: user, Asset: asset, DeltaAvailable: num.MustParse(amount), Reason: "seed",
	}}); err != nil {
		t.Fatalf("seed %s: %v", asset, err)
	}
}

func avail(t *testing.T, st *store.Store, user int64, asset string) string {
	t.Helper()
	b, err := st.GetBalance(user, asset)
	if err != nil {
		t.Fatalf("balance: %v", err)
	}
	return b.Available.String()
}

// TestLimitCrossSettlement verifies a maker/taker fill settles both sides with
// correct fees and conserves funds via the double-entry ledger.
func TestLimitCrossSettlement(t *testing.T) {
	st, mgr := setup(t)
	eng, ok := mgr.Get("BTC-USDT")
	if !ok {
		t.Fatal("no BTC-USDT engine")
	}
	const seller, buyer = int64(1), int64(2)
	credit(t, st, seller, "BTC", "1")
	credit(t, st, buyer, "USDT", "100000")

	// Maker: sell 0.01 BTC @ 50000 (rests).
	sell, err := eng.Place(&models.Order{UserID: seller, Side: models.Sell, Type: models.TypeLimit, Price: num.MustParse("50000"), Quantity: num.MustParse("0.01")})
	if err != nil {
		t.Fatalf("place sell: %v", err)
	}
	if sell.Status != models.StatusOpen {
		t.Fatalf("maker status = %s, want open", sell.Status)
	}
	// Seller's BTC should now be locked (0.99 available).
	if got := avail(t, st, seller, "BTC"); got != "0.99" {
		t.Fatalf("seller BTC available = %s, want 0.99", got)
	}

	// Taker: buy 0.01 BTC @ 50000 (crosses → full fill).
	buy, err := eng.Place(&models.Order{UserID: buyer, Side: models.Buy, Type: models.TypeLimit, Price: num.MustParse("50000"), Quantity: num.MustParse("0.01")})
	if err != nil {
		t.Fatalf("place buy: %v", err)
	}
	if buy.Status != models.StatusFilled {
		t.Fatalf("taker status = %s, want filled", buy.Status)
	}

	// Notional = 500 USDT. Fees are 0.1% on each side, charged in the asset received.
	// Seller receives 500 USDT - 0.5 fee = 499.5. Buyer receives 0.01 BTC - 0.00001 fee.
	checks := []struct {
		who   string
		user  int64
		asset string
		want  string
	}{
		{"seller BTC", seller, "BTC", "0.99"},
		{"seller USDT", seller, "USDT", "499.5"},
		{"buyer BTC", buyer, "BTC", "0.00999"},
		{"buyer USDT", buyer, "USDT", "99500"},
		{"exchange BTC fee", store.ExchangeUserID, "BTC", "0.00001"},
		{"exchange USDT fee", store.ExchangeUserID, "USDT", "0.5"},
	}
	for _, c := range checks {
		if got := avail(t, st, c.user, c.asset); got != c.want {
			t.Errorf("%s available = %s, want %s", c.who, got, c.want)
		}
	}

	// A trade should be recorded.
	trades, err := st.ListTradesByMarket("BTC-USDT", 10)
	if err != nil || len(trades) != 1 {
		t.Fatalf("expected 1 trade, got %d (err=%v)", len(trades), err)
	}
	if trades[0].QuoteQty.String() != "500" {
		t.Errorf("trade quote = %s, want 500", trades[0].QuoteQty)
	}
}

// TestCancelReleasesFunds verifies canceling a resting order returns locked funds.
func TestCancelReleasesFunds(t *testing.T) {
	st, mgr := setup(t)
	eng, _ := mgr.Get("BTC-USDT")
	const u = int64(7)
	credit(t, st, u, "USDT", "1000")

	o, err := eng.Place(&models.Order{UserID: u, Side: models.Buy, Type: models.TypeLimit, Price: num.MustParse("50000"), Quantity: num.MustParse("0.01")})
	if err != nil {
		t.Fatalf("place: %v", err)
	}
	if got := avail(t, st, u, "USDT"); got != "500" { // 500 locked
		t.Fatalf("after place available = %s, want 500", got)
	}
	if err := eng.Cancel(o.ID, u); err != nil {
		t.Fatalf("cancel: %v", err)
	}
	if got := avail(t, st, u, "USDT"); got != "1000" {
		t.Fatalf("after cancel available = %s, want 1000", got)
	}
}
