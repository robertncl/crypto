package store_test

import (
	"errors"
	"path/filepath"
	"testing"

	"cryptoex/internal/db"
	"cryptoex/internal/models"
	"cryptoex/internal/num"
	"cryptoex/internal/store"
)

func newStore(t *testing.T) *store.Store {
	t.Helper()
	conn, err := db.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { conn.Close() })
	return store.New(conn)
}

// ---------- users ----------

func TestCreateAndGetUser(t *testing.T) {
	st := newStore(t)
	u, err := st.CreateUser("a@b.com", "hash", "user", 1000)
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	if u.ID == 0 {
		t.Error("expected non-zero user id")
	}
	if u.KYCStatus != "none" {
		t.Errorf("kyc = %q, want none", u.KYCStatus)
	}

	byEmail, err := st.GetUserByEmail("a@b.com")
	if err != nil {
		t.Fatalf("GetUserByEmail: %v", err)
	}
	if byEmail.ID != u.ID || byEmail.PasswordHash != "hash" {
		t.Errorf("GetUserByEmail mismatch: %+v", byEmail)
	}

	byID, err := st.GetUserByID(u.ID)
	if err != nil || byID.Email != "a@b.com" {
		t.Errorf("GetUserByID: %+v %v", byID, err)
	}
}

func TestGetUserNotFound(t *testing.T) {
	st := newStore(t)
	if _, err := st.GetUserByID(999); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("GetUserByID err = %v, want ErrNotFound", err)
	}
	if _, err := st.GetUserByEmail("nope@x.com"); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("GetUserByEmail err = %v, want ErrNotFound", err)
	}
}

func TestCreateUserDuplicateEmail(t *testing.T) {
	st := newStore(t)
	if _, err := st.CreateUser("dup@b.com", "h", "user", 1); err != nil {
		t.Fatal(err)
	}
	if _, err := st.CreateUser("dup@b.com", "h", "user", 2); err == nil {
		t.Error("duplicate email should fail (UNIQUE constraint)")
	}
}

func TestSetKYCStatus(t *testing.T) {
	st := newStore(t)
	u, _ := st.CreateUser("k@b.com", "h", "user", 1)
	if err := st.SetKYCStatus(u.ID, "verified"); err != nil {
		t.Fatal(err)
	}
	got, _ := st.GetUserByID(u.ID)
	if got.KYCStatus != "verified" {
		t.Errorf("kyc = %q, want verified", got.KYCStatus)
	}
}

// ---------- assets & markets ----------

func TestListAndGetAsset(t *testing.T) {
	st := newStore(t)
	assets, err := st.ListAssets()
	if err != nil {
		t.Fatal(err)
	}
	if len(assets) < 5 {
		t.Errorf("assets = %d, want >= 5", len(assets))
	}
	btc, err := st.GetAsset("BTC")
	if err != nil {
		t.Fatal(err)
	}
	if btc.Symbol != "BTC" || btc.Network != "Bitcoin" {
		t.Errorf("BTC asset = %+v", btc)
	}
	if _, err := st.GetAsset("NOPE"); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("GetAsset(NOPE) err = %v, want ErrNotFound", err)
	}
}

func TestListAndGetMarket(t *testing.T) {
	st := newStore(t)
	markets, err := st.ListMarkets()
	if err != nil {
		t.Fatal(err)
	}
	if len(markets) < 4 {
		t.Errorf("markets = %d, want >= 4", len(markets))
	}
	m, err := st.GetMarket("BTC-USDT")
	if err != nil {
		t.Fatal(err)
	}
	if m.Base != "BTC" || m.Quote != "USDT" || m.Status != "trading" {
		t.Errorf("BTC-USDT = %+v", m)
	}
	if _, err := st.GetMarket("NOPE"); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("GetMarket(NOPE) err = %v", err)
	}
}

// ---------- balances & postings ----------

func TestGetBalanceDefaultZero(t *testing.T) {
	st := newStore(t)
	b, err := st.GetBalance(1, "BTC")
	if err != nil {
		t.Fatal(err)
	}
	if !b.Available.IsZero() || !b.Locked.IsZero() {
		t.Errorf("default balance should be zero: %+v", b)
	}
}

func TestApplyPostingsCreditAndLedger(t *testing.T) {
	st := newStore(t)
	err := st.ApplyPostings("txn1", 100, []store.Posting{{
		UserID: 1, Asset: "USDT", DeltaAvailable: num.MustParse("500"), Reason: "deposit", Ref: "r1",
	}})
	if err != nil {
		t.Fatalf("ApplyPostings: %v", err)
	}
	b, _ := st.GetBalance(1, "USDT")
	if b.Available.String() != "500" {
		t.Errorf("available = %s, want 500", b.Available)
	}
	var n int
	if err := st.DB().QueryRow(`SELECT COUNT(*) FROM ledger_entries WHERE txn_id=?`, "txn1").Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Errorf("ledger entries = %d, want 1", n)
	}
}

func TestApplyPostingsInsufficientFunds(t *testing.T) {
	st := newStore(t)
	err := st.ApplyPostings("txn1", 100, []store.Posting{{
		UserID: 1, Asset: "USDT", DeltaAvailable: num.MustParse("-10"), Reason: "debit",
	}})
	if !errors.Is(err, store.ErrInsufficientFunds) {
		t.Errorf("err = %v, want ErrInsufficientFunds", err)
	}
	b, _ := st.GetBalance(1, "USDT")
	if !b.Available.IsZero() {
		t.Errorf("balance should be zero after rollback, got %s", b.Available)
	}
}

func TestApplyPostingsMultiPartyAtomic(t *testing.T) {
	st := newStore(t)
	st.ApplyPostings("seed", 0, []store.Posting{{UserID: 1, Asset: "USDT", DeltaAvailable: num.MustParse("100"), Reason: "seed"}})
	err := st.ApplyPostings("xfer", 1, []store.Posting{
		{UserID: 1, Asset: "USDT", DeltaAvailable: num.MustParse("-30"), Reason: "xfer"},
		{UserID: 2, Asset: "USDT", DeltaAvailable: num.MustParse("30"), Reason: "xfer"},
	})
	if err != nil {
		t.Fatal(err)
	}
	b1, _ := st.GetBalance(1, "USDT")
	b2, _ := st.GetBalance(2, "USDT")
	if b1.Available.String() != "70" || b2.Available.String() != "30" {
		t.Errorf("balances after xfer: %s / %s, want 70/30", b1.Available, b2.Available)
	}
}

func TestApplyPostingsRollbackOnSecondFailure(t *testing.T) {
	st := newStore(t)
	st.ApplyPostings("seed", 0, []store.Posting{{UserID: 1, Asset: "USDT", DeltaAvailable: num.MustParse("100"), Reason: "seed"}})
	// First posting ok; second drives user 2 negative → whole tx rolls back.
	err := st.ApplyPostings("bad", 1, []store.Posting{
		{UserID: 1, Asset: "USDT", DeltaAvailable: num.MustParse("-50"), Reason: "x"},
		{UserID: 2, Asset: "USDT", DeltaAvailable: num.MustParse("-1"), Reason: "x"},
	})
	if !errors.Is(err, store.ErrInsufficientFunds) {
		t.Fatalf("err = %v, want ErrInsufficientFunds", err)
	}
	b1, _ := st.GetBalance(1, "USDT")
	if b1.Available.String() != "100" {
		t.Errorf("user1 balance = %s, want 100 (rolled back)", b1.Available)
	}
}

func TestApplyPostingsEmptyNoop(t *testing.T) {
	st := newStore(t)
	if err := st.ApplyPostings("empty", 0, nil); err != nil {
		t.Errorf("empty postings should be a no-op, got %v", err)
	}
}

func TestApplyPostingsLockedBalance(t *testing.T) {
	st := newStore(t)
	err := st.ApplyPostings("lock", 0, []store.Posting{{
		UserID: 1, Asset: "BTC", DeltaAvailable: num.MustParse("1"), DeltaLocked: num.MustParse("0.5"), Reason: "x",
	}})
	if err != nil {
		t.Fatal(err)
	}
	b, _ := st.GetBalance(1, "BTC")
	if b.Available.String() != "1" || b.Locked.String() != "0.5" {
		t.Errorf("balance = %+v, want avail 1 locked 0.5", b)
	}
}

func TestListBalances(t *testing.T) {
	st := newStore(t)
	st.ApplyPostings("s", 0, []store.Posting{
		{UserID: 1, Asset: "BTC", DeltaAvailable: num.MustParse("1"), Reason: "x"},
		{UserID: 1, Asset: "USDT", DeltaAvailable: num.MustParse("100"), Reason: "x"},
	})
	bals, err := st.ListBalances(1)
	if err != nil {
		t.Fatal(err)
	}
	if len(bals) != 2 {
		t.Fatalf("balances = %d, want 2", len(bals))
	}
	if bals[0].Asset != "BTC" || bals[1].Asset != "USDT" { // ordered by asset
		t.Errorf("balances order = %s,%s", bals[0].Asset, bals[1].Asset)
	}
}

// ---------- orders ----------

func newOrder(id string, user int64) *models.Order {
	return &models.Order{
		ID: id, UserID: user, Market: "BTC-USDT", Side: models.Buy, Type: models.TypeLimit,
		Price: num.MustParse("50000"), Quantity: num.MustParse("0.1"),
		Status: models.StatusOpen, CreatedAt: 100, UpdatedAt: 100,
	}
}

func TestInsertGetUpdateOrder(t *testing.T) {
	st := newStore(t)
	o := newOrder("o1", 1)
	if err := st.InsertOrder(o); err != nil {
		t.Fatal(err)
	}
	got, err := st.GetOrder("o1")
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != "o1" || got.Price.String() != "50000" {
		t.Errorf("GetOrder = %+v", got)
	}
	o.Filled = num.MustParse("0.1")
	o.Status = models.StatusFilled
	o.UpdatedAt = 200
	if err := st.UpdateOrder(o); err != nil {
		t.Fatal(err)
	}
	got, _ = st.GetOrder("o1")
	if got.Status != models.StatusFilled || got.Filled.String() != "0.1" {
		t.Errorf("after update = %+v", got)
	}
}

func TestGetOrderNotFound(t *testing.T) {
	st := newStore(t)
	if _, err := st.GetOrder("nope"); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("err = %v, want ErrNotFound", err)
	}
}

func TestListOpenOrdersAndHistory(t *testing.T) {
	st := newStore(t)
	st.InsertOrder(newOrder("open1", 1))
	filled := newOrder("filled1", 1)
	filled.Status = models.StatusFilled
	st.InsertOrder(filled)

	openList, err := st.ListOpenOrders(1, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(openList) != 1 || openList[0].ID != "open1" {
		t.Errorf("open orders = %+v", openList)
	}
	if got, _ := st.ListOpenOrders(1, "ETH-USDT"); len(got) != 0 {
		t.Errorf("open orders for other market = %d, want 0", len(got))
	}

	hist, err := st.ListOrderHistory(1, "", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(hist) != 2 {
		t.Errorf("history = %d, want 2", len(hist))
	}
}

func TestListWorkingOrdersByMarketTimePriority(t *testing.T) {
	st := newStore(t)
	o1 := newOrder("w1", 1)
	o1.CreatedAt = 100
	o2 := newOrder("w2", 2)
	o2.CreatedAt = 50
	st.InsertOrder(o1)
	st.InsertOrder(o2)
	working, err := st.ListWorkingOrdersByMarket("BTC-USDT")
	if err != nil {
		t.Fatal(err)
	}
	if len(working) != 2 {
		t.Fatalf("working = %d, want 2", len(working))
	}
	if working[0].ID != "w2" { // oldest first
		t.Errorf("expected older order w2 first, got %s", working[0].ID)
	}
}

// ---------- trades ----------

func TestTrades(t *testing.T) {
	st := newStore(t)
	tr := &models.Trade{
		Market: "BTC-USDT", Price: num.MustParse("50000"), Quantity: num.MustParse("0.1"),
		QuoteQty: num.MustParse("5000"), TakerSide: models.Buy,
		BuyOrderID: "b1", SellOrderID: "s1", BuyUserID: 1, SellUserID: 2, CreatedAt: 100,
	}
	id, err := st.InsertTrade(tr)
	if err != nil || id == 0 {
		t.Fatalf("InsertTrade: id=%d err=%v", id, err)
	}
	byMarket, err := st.ListTradesByMarket("BTC-USDT", 10)
	if err != nil || len(byMarket) != 1 {
		t.Fatalf("ListTradesByMarket = %d, %v", len(byMarket), err)
	}
	if byMarket[0].QuoteQty.String() != "5000" {
		t.Errorf("trade quote = %s", byMarket[0].QuoteQty)
	}
	if buyer, _ := st.ListTradesByUser(1, "", 10); len(buyer) != 1 {
		t.Errorf("buyer trades = %d, want 1", len(buyer))
	}
	if seller, _ := st.ListTradesByUser(2, "BTC-USDT", 10); len(seller) != 1 {
		t.Errorf("seller trades = %d, want 1", len(seller))
	}
	if other, _ := st.ListTradesByUser(3, "", 10); len(other) != 0 {
		t.Errorf("uninvolved user trades = %d, want 0", len(other))
	}
}

func TestCommitFill(t *testing.T) {
	st := newStore(t)
	st.ApplyPostings("seed", 0, []store.Posting{
		{UserID: 1, Asset: "USDT", DeltaAvailable: num.MustParse("10000"), Reason: "seed"},
		{UserID: 2, Asset: "BTC", DeltaAvailable: num.MustParse("1"), Reason: "seed"},
	})
	buy := newOrder("buy1", 1)
	sell := newOrder("sell1", 2)
	sell.Side = models.Sell
	st.InsertOrder(buy)
	st.InsertOrder(sell)

	tr := &models.Trade{
		Market: "BTC-USDT", Price: num.MustParse("50000"), Quantity: num.MustParse("0.1"),
		QuoteQty: num.MustParse("5000"), TakerSide: models.Buy,
		BuyOrderID: "buy1", SellOrderID: "sell1", BuyUserID: 1, SellUserID: 2, CreatedAt: 100,
	}
	buy.Filled = num.MustParse("0.1")
	buy.Status = models.StatusFilled
	postings := []store.Posting{
		{UserID: 1, Asset: "USDT", DeltaAvailable: num.MustParse("-5000"), Reason: "buy"},
		{UserID: 1, Asset: "BTC", DeltaAvailable: num.MustParse("0.1"), Reason: "buy"},
		{UserID: 2, Asset: "BTC", DeltaAvailable: num.MustParse("-0.1"), Reason: "sell"},
		{UserID: 2, Asset: "USDT", DeltaAvailable: num.MustParse("5000"), Reason: "sell"},
	}
	if err := st.CommitFill("fill1", 100, postings, tr, buy); err != nil {
		t.Fatalf("CommitFill: %v", err)
	}
	if tr.ID == 0 {
		t.Error("trade ID not populated")
	}
	b1, _ := st.GetBalance(1, "BTC")
	if b1.Available.String() != "0.1" {
		t.Errorf("buyer BTC = %s, want 0.1", b1.Available)
	}
	got, _ := st.GetOrder("buy1")
	if got.Status != models.StatusFilled {
		t.Errorf("order status = %s, want filled", got.Status)
	}
}

// ---------- candles ----------

func TestCandles(t *testing.T) {
	st := newStore(t)
	trades := []struct {
		price string
		ts    int64
	}{{"100", 0}, {"110", 30}, {"105", 59}, {"120", 60}}
	for i, tr := range trades {
		_, err := st.InsertTrade(&models.Trade{
			Market: "BTC-USDT", Price: num.MustParse(tr.price), Quantity: num.MustParse("1"),
			QuoteQty: num.MustParse(tr.price), TakerSide: models.Buy,
			BuyOrderID: "b", SellOrderID: "s", BuyUserID: 1, SellUserID: 2, CreatedAt: tr.ts,
		})
		if err != nil {
			t.Fatalf("trade %d: %v", i, err)
		}
	}
	candles, err := st.Candles("BTC-USDT", 60, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(candles) != 2 {
		t.Fatalf("candles = %d, want 2 buckets", len(candles))
	}
	first := candles[0]
	if first.Open.String() != "100" || first.High.String() != "110" || first.Low.String() != "100" || first.Close.String() != "105" {
		t.Errorf("first candle OHLC = %s/%s/%s/%s", first.Open, first.High, first.Low, first.Close)
	}
	if first.Volume.String() != "3" {
		t.Errorf("first candle volume = %s, want 3", first.Volume)
	}
}

func TestCandlesRespectsLimit(t *testing.T) {
	st := newStore(t)
	for i := int64(0); i < 5; i++ {
		st.InsertTrade(&models.Trade{
			Market: "BTC-USDT", Price: num.MustParse("100"), Quantity: num.MustParse("1"),
			QuoteQty: num.MustParse("100"), TakerSide: models.Buy,
			BuyOrderID: "b", SellOrderID: "s", BuyUserID: 1, SellUserID: 2, CreatedAt: i * 60,
		})
	}
	candles, _ := st.Candles("BTC-USDT", 60, 2)
	if len(candles) != 2 {
		t.Errorf("candles with limit 2 = %d", len(candles))
	}
}

// ---------- wallet ----------

func TestGetOrCreateAddress(t *testing.T) {
	st := newStore(t)
	calls := 0
	gen := func() string { calls++; return "addr-generated" }
	a, err := st.GetOrCreateAddress(1, "BTC", "Bitcoin", gen)
	if err != nil {
		t.Fatal(err)
	}
	if a.Address != "addr-generated" || a.Network != "Bitcoin" {
		t.Errorf("address = %+v", a)
	}
	a2, _ := st.GetOrCreateAddress(1, "BTC", "Bitcoin", gen)
	if a2.Address != "addr-generated" {
		t.Errorf("second address = %+v", a2)
	}
	if calls != 1 {
		t.Errorf("gen called %d times, want 1 (cached on second call)", calls)
	}
}

func TestWalletTxns(t *testing.T) {
	st := newStore(t)
	txn := &models.WalletTxn{
		ID: "tx1", UserID: 1, Asset: "BTC", Type: models.TxnDeposit,
		Amount: num.MustParse("0.5"), Fee: num.Zero, Status: models.TxnPending,
		Confirmations: 0, CreatedAt: 100, UpdatedAt: 100,
	}
	if err := st.InsertTxn(txn); err != nil {
		t.Fatal(err)
	}
	txn.Status = models.TxnCompleted
	txn.Confirmations = 3
	txn.TxID = "0xabc"
	txn.UpdatedAt = 200
	if err := st.UpdateTxn(txn); err != nil {
		t.Fatal(err)
	}
	list, err := st.ListTxns(1, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 {
		t.Fatalf("txns = %d, want 1", len(list))
	}
	if list[0].Status != models.TxnCompleted || list[0].Confirmations != 3 || list[0].TxID != "0xabc" {
		t.Errorf("txn after update = %+v", list[0])
	}
}
