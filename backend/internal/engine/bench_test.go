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

// BenchmarkPlaceAndCross measures end-to-end matching throughput: each
// iteration rests a maker sell and crosses it with a taker buy (two placements,
// one fill, full settlement and persistence).
func BenchmarkPlaceAndCross(b *testing.B) {
	conn, err := db.Open(filepath.Join(b.TempDir(), "bench.db"))
	if err != nil {
		b.Fatal(err)
	}
	b.Cleanup(func() { conn.Close() })
	st := store.New(conn)
	markets, _ := st.ListMarkets()
	hub := ws.NewHub()
	md := market.NewService(st, hub)
	md.Init(markets)
	mgr := engine.NewManager(st, md, hub)
	if err := mgr.Init(markets); err != nil {
		b.Fatal(err)
	}
	eng, _ := mgr.Get("BTC-USDT")
	const seller, buyer = int64(1), int64(2)
	_ = st.ApplyPostings("seed", 0, []store.Posting{
		{UserID: seller, Asset: "BTC", DeltaAvailable: num.FromInt(1_000_000)},
		{UserID: buyer, Asset: "USDT", DeltaAvailable: num.FromInt(10_000_000_000)},
	})
	price, qty := num.MustParse("50000"), num.MustParse("0.01")
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := eng.Place(&models.Order{UserID: seller, Side: models.Sell, Type: models.TypeLimit, Price: price, Quantity: qty}); err != nil {
			b.Fatal(err)
		}
		if _, err := eng.Place(&models.Order{UserID: buyer, Side: models.Buy, Type: models.TypeLimit, Price: price, Quantity: qty}); err != nil {
			b.Fatal(err)
		}
	}
}
