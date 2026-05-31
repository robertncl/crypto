package store_test

import (
	"testing"

	"cryptoex/internal/models"
	"cryptoex/internal/num"
	"cryptoex/internal/store"
)

func TestListAndGetPerpMarket(t *testing.T) {
	st := newStore(t)
	perps, err := st.ListPerpMarkets()
	if err != nil {
		t.Fatal(err)
	}
	if len(perps) < 3 {
		t.Errorf("perp markets = %d, want >= 3", len(perps))
	}
	m, err := st.GetPerpMarket("BTC-PERP")
	if err != nil {
		t.Fatal(err)
	}
	if m.Base != "BTC" || m.Settle != "USDT" || m.MaxLeverage != 100 {
		t.Errorf("BTC-PERP = %+v", m)
	}
}

func TestGetPositionDefaultFlat(t *testing.T) {
	st := newStore(t)
	p, err := st.GetPosition(1, "BTC-PERP")
	if err != nil {
		t.Fatal(err)
	}
	if p.Side != models.Flat || p.Size.Sign() != 0 {
		t.Errorf("default position = %+v, want flat/zero", p)
	}
}

func newPerpOrder(id string, user int64) *models.PerpOrder {
	return &models.PerpOrder{
		ID: id, UserID: user, Market: "BTC-PERP", Side: models.Buy, Type: models.TypeLimit,
		Price: num.MustParse("50000"), Quantity: num.MustParse("0.1"), Leverage: 10,
		Status: models.StatusOpen, CreatedAt: 100, UpdatedAt: 100,
	}
}

func TestInsertGetUpdatePerpOrder(t *testing.T) {
	st := newStore(t)
	o := newPerpOrder("p1", 1)
	o.ReduceOnly = true
	if err := st.InsertPerpOrder(o); err != nil {
		t.Fatal(err)
	}
	got, err := st.GetPerpOrder("p1")
	if err != nil {
		t.Fatal(err)
	}
	if got.Leverage != 10 || !got.ReduceOnly {
		t.Errorf("perp order = %+v", got)
	}
	o.Filled = num.MustParse("0.1")
	o.AvgPrice = num.MustParse("50000")
	o.Status = models.StatusFilled
	if err := st.UpdatePerpOrder(o); err != nil {
		t.Fatal(err)
	}
	got, _ = st.GetPerpOrder("p1")
	if got.Status != models.StatusFilled || got.AvgPrice.String() != "50000" {
		t.Errorf("after update = %+v", got)
	}
}

func TestListPerpOrders(t *testing.T) {
	st := newStore(t)
	st.InsertPerpOrder(newPerpOrder("po1", 1))
	filled := newPerpOrder("po2", 1)
	filled.Status = models.StatusFilled
	st.InsertPerpOrder(filled)

	openList, _ := st.ListOpenPerpOrders(1, "")
	if len(openList) != 1 || openList[0].ID != "po1" {
		t.Errorf("open perp orders = %+v", openList)
	}
	hist, _ := st.ListPerpOrderHistory(1, "", 10)
	if len(hist) != 2 {
		t.Errorf("perp history = %d, want 2", len(hist))
	}
	working, _ := st.ListWorkingPerpOrdersByMarket("BTC-PERP")
	if len(working) != 1 {
		t.Errorf("working perp = %d, want 1", len(working))
	}
}

func TestCommitPerpAndPositions(t *testing.T) {
	st := newStore(t)
	st.ApplyPostings("seed", 0, []store.Posting{
		{UserID: 1, Asset: "USDT", DeltaAvailable: num.MustParse("10000"), Reason: "seed"},
	})
	o := newPerpOrder("pc1", 1)
	st.InsertPerpOrder(o)
	o.Filled = num.MustParse("0.1")
	o.Status = models.StatusFilled

	pos := &models.Position{
		UserID: 1, Market: "BTC-PERP", Side: models.Long, Size: num.MustParse("0.1"),
		EntryPrice: num.MustParse("50000"), Margin: num.MustParse("500"), Leverage: 10, UpdatedAt: 100,
	}
	postings := []store.Posting{
		{UserID: 1, Asset: "USDT", DeltaAvailable: num.MustParse("-500"), DeltaLocked: num.MustParse("500"), Reason: "margin"},
	}
	tr := &models.Trade{
		Market: "BTC-PERP", Price: num.MustParse("50000"), Quantity: num.MustParse("0.1"),
		QuoteQty: num.MustParse("5000"), TakerSide: models.Buy,
		BuyOrderID: "pc1", SellOrderID: "x", BuyUserID: 1, SellUserID: 2, CreatedAt: 100,
	}
	if err := st.CommitPerp("commit1", 100, postings, tr, []*models.Position{pos}, []*models.PerpOrder{o}); err != nil {
		t.Fatalf("CommitPerp: %v", err)
	}
	got, _ := st.GetPosition(1, "BTC-PERP")
	if got.Side != models.Long || got.Size.String() != "0.1" || got.Margin.String() != "500" {
		t.Errorf("position = %+v", got)
	}
	if open, _ := st.ListPositions(1); len(open) != 1 {
		t.Errorf("open positions = %d, want 1", len(open))
	}
	if byMarket, _ := st.ListOpenPositionsByMarket("BTC-PERP"); len(byMarket) != 1 {
		t.Errorf("positions by market = %d, want 1", len(byMarket))
	}
	if gotOrder, _ := st.GetPerpOrder("pc1"); gotOrder.Status != models.StatusFilled {
		t.Errorf("perp order status = %s", gotOrder.Status)
	}
	if tr.ID == 0 {
		t.Error("trade ID not populated by CommitPerp")
	}
}

func TestCommitPerpNoTrade(t *testing.T) {
	st := newStore(t)
	pos := &models.Position{UserID: 1, Market: "BTC-PERP", Side: models.Flat, Size: num.Zero, UpdatedAt: 100}
	if err := st.CommitPerp("c2", 100, nil, nil, []*models.Position{pos}, nil); err != nil {
		t.Fatalf("CommitPerp no-trade: %v", err)
	}
	if open, _ := st.ListPositions(1); len(open) != 0 {
		t.Errorf("flat (size 0) position should not appear in open list: %d", len(open))
	}
}
