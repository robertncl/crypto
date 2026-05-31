package market

import (
	"path/filepath"
	"testing"

	"cryptoex/internal/db"
	"cryptoex/internal/models"
	"cryptoex/internal/num"
	"cryptoex/internal/store"
	"cryptoex/internal/ws"
)

func newService(t *testing.T) (*Service, *store.Store) {
	t.Helper()
	conn, err := db.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { conn.Close() })
	st := store.New(conn)
	return NewService(st, ws.NewHub()), st
}

func TestItoa(t *testing.T) {
	cases := map[int64]string{0: "0", 5: "5", 60: "60", 3600: "3600", -42: "-42", 86400: "86400"}
	for in, want := range cases {
		if got := itoa(in); got != want {
			t.Errorf("itoa(%d) = %q, want %q", in, got, want)
		}
	}
}

func TestKlineTopic(t *testing.T) {
	if got := klineTopic("BTC-USDT", 60); got != "kline:BTC-USDT:60" {
		t.Errorf("klineTopic = %q, want kline:BTC-USDT:60", got)
	}
}

func TestInitAndGetTicker(t *testing.T) {
	s, st := newService(t)
	markets, _ := st.ListMarkets()
	s.Init(markets)
	if _, ok := s.GetTicker("BTC-USDT"); !ok {
		t.Error("expected BTC-USDT ticker after Init")
	}
	if _, ok := s.GetTicker("NOPE-USDT"); ok {
		t.Error("unexpected ticker for unknown market")
	}
}

func TestAllTickers(t *testing.T) {
	s, st := newService(t)
	markets, _ := st.ListMarkets()
	s.Init(markets)
	if got := len(s.AllTickers()); got != len(markets) {
		t.Errorf("AllTickers = %d, want %d", got, len(markets))
	}
}

func TestRegisterIsIdempotent(t *testing.T) {
	s, _ := newService(t)
	s.Register("BTC-PERP")
	if _, ok := s.GetTicker("BTC-PERP"); !ok {
		t.Error("expected ticker after Register")
	}
	s.Register("BTC-PERP")
	count := 0
	for _, tk := range s.AllTickers() {
		if tk.Market == "BTC-PERP" {
			count++
		}
	}
	if count != 1 {
		t.Errorf("BTC-PERP registered %d times, want 1", count)
	}
}

func TestSetBook(t *testing.T) {
	s, st := newService(t)
	markets, _ := st.ListMarkets()
	s.Init(markets)
	s.SetBook("BTC-USDT", num.MustParse("49999"), num.MustParse("50001"))
	tk, _ := s.GetTicker("BTC-USDT")
	if tk.BestBid.String() != "49999" || tk.BestAsk.String() != "50001" {
		t.Errorf("ticker book = %s/%s, want 49999/50001", tk.BestBid, tk.BestAsk)
	}
}

func TestOnTradeUpdatesLastPrice(t *testing.T) {
	s, st := newService(t)
	markets, _ := st.ListMarkets()
	s.Init(markets)
	s.OnTrade(models.Trade{
		Market: "BTC-USDT", Price: num.MustParse("51000"), Quantity: num.MustParse("0.5"),
		QuoteQty: num.MustParse("25500"), TakerSide: models.Buy, CreatedAt: 100,
	})
	tk, _ := s.GetTicker("BTC-USDT")
	if tk.Last.String() != "51000" {
		t.Errorf("ticker last = %s, want 51000", tk.Last)
	}
}

func TestOnTradeBuildsCandle(t *testing.T) {
	s, _ := newService(t)
	s.Register("BTC-PERP")
	s.OnTrade(models.Trade{
		Market: "BTC-PERP", Price: num.MustParse("100"), Quantity: num.MustParse("1"),
		QuoteQty: num.MustParse("100"), TakerSide: models.Buy, CreatedAt: 0,
	})
	s.OnTrade(models.Trade{
		Market: "BTC-PERP", Price: num.MustParse("110"), Quantity: num.MustParse("2"),
		QuoteQty: num.MustParse("220"), TakerSide: models.Buy, CreatedAt: 30,
	})
	// Inspect the live 60s candle (same bucket for ts 0 and 30).
	s.mu.RLock()
	defer s.mu.RUnlock()
	c := s.candles["BTC-PERP"][60]
	if c == nil {
		t.Fatal("no 60s candle built")
	}
	if c.Open.String() != "100" || c.High.String() != "110" || c.Close.String() != "110" || c.Volume.String() != "3" {
		t.Errorf("candle = open %s high %s close %s vol %s", c.Open, c.High, c.Close, c.Volume)
	}
}
