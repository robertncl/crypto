package store_test

import (
	"path/filepath"
	"strconv"
	"testing"

	"cryptoex/internal/db"
	"cryptoex/internal/num"
	"cryptoex/internal/store"
)

func benchStore(b *testing.B) *store.Store {
	b.Helper()
	conn, err := db.Open(filepath.Join(b.TempDir(), "bench.db"))
	if err != nil {
		b.Fatal(err)
	}
	b.Cleanup(func() { conn.Close() })
	return store.New(conn)
}

// seedTrades bulk-inserts n trades for market, one every `every` seconds
// ending at `end`, inside a single transaction.
func seedTrades(b *testing.B, st *store.Store, market string, n int, end, every int64) {
	b.Helper()
	tx, err := st.DB().Begin()
	if err != nil {
		b.Fatal(err)
	}
	stmt, err := tx.Prepare(`INSERT INTO trades(market,price,quantity,quote_qty,taker_side,buy_order_id,sell_order_id,buy_user_id,sell_user_id,created_at)
		VALUES(?,?,?,?,'buy','b','s',1,2,?)`)
	if err != nil {
		b.Fatal(err)
	}
	for i := 0; i < n; i++ {
		p := num.FromInt(int64(60000 + i%500)).Raw()
		if _, err := stmt.Exec(market, p, num.MustParse("0.01").Raw(), p/100, end-int64(n-i)*every); err != nil {
			b.Fatal(err)
		}
	}
	if err := tx.Commit(); err != nil {
		b.Fatal(err)
	}
}

// BenchmarkApplyPostings measures one two-party ledger transaction, the
// primitive every balance change goes through.
func BenchmarkApplyPostings(b *testing.B) {
	st := benchStore(b)
	one := num.FromInt(1)
	if err := st.ApplyPostings("seed", 0, []store.Posting{{UserID: 1, Asset: "USDT", DeltaAvailable: num.FromInt(1_000_000_000)}}); err != nil {
		b.Fatal(err)
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := st.ApplyPostings("t"+strconv.Itoa(i), 0, []store.Posting{
			{UserID: 1, Asset: "USDT", DeltaAvailable: one.Neg(), DeltaLocked: one, Reason: "lock"},
			{UserID: 2, Asset: "USDT", DeltaAvailable: one, Reason: "credit"},
		}); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkCandles serves a 300-bar 1m chart from a market with a long trade
// history (200k trades, one every 3s ≈ 7 days).
func BenchmarkCandles(b *testing.B) {
	st := benchStore(b)
	seedTrades(b, st, "BTC-USDT", 200_000, 1_800_000_000, 3)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		c, err := st.Candles("BTC-USDT", 60, 300)
		if err != nil || len(c) != 300 {
			b.Fatalf("candles=%d err=%v", len(c), err)
		}
	}
}
