package market

import (
	"testing"
	"time"

	"cryptoex/internal/num"
)

// BenchmarkRefresh measures the periodic 24h-ticker recompute for one market
// holding a busy day of trades (one every second).
func BenchmarkRefresh(b *testing.B) {
	s, st := newService(b)
	tx, _ := st.DB().Begin()
	stmt, _ := tx.Prepare(`INSERT INTO trades(market,price,quantity,quote_qty,taker_side,buy_order_id,sell_order_id,buy_user_id,sell_user_id,created_at)
		VALUES('BTC-USDT',?,?,?,'buy','b','s',1,2,?)`)
	now := time.Now().Unix()
	for i := 0; i < 86_400; i++ {
		p := num.FromInt(int64(60000 + i%500)).Raw()
		_, _ = stmt.Exec(p, num.MustParse("0.01").Raw(), p/100, now-86_400+int64(i))
	}
	_ = tx.Commit()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		s.refresh("BTC-USDT")
	}
}
