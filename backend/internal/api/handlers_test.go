package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"cryptoex/internal/models"
	"cryptoex/internal/num"
	"cryptoex/internal/store"
)

// ---------- helpers ----------

// register creates a user and returns its token and id. Each test server has
// its own rate limiter (20 auth requests/min), so keep registrations per test
// well under that ceiling.
func register(t *testing.T, srv *Server, email string) (token string, userID int64) {
	t.Helper()
	w := do(t, srv, "POST", "/api/auth/register", "", `{"email":"`+email+`","password":"secret123"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("register %s: status %d, body %s", email, w.Code, w.Body.String())
	}
	var ar authResponse
	if err := json.Unmarshal(w.Body.Bytes(), &ar); err != nil {
		t.Fatalf("register %s: %v", email, err)
	}
	return ar.Token, ar.User.ID
}

// credit funds an account directly through the ledger, for assets the welcome
// bonus does not provide (e.g. BTC to sell).
func credit(t *testing.T, srv *Server, userID int64, asset, amount string) {
	t.Helper()
	err := srv.st.ApplyPostings("test-credit:"+asset+num.MustParse(amount).String(), 0, []store.Posting{{
		UserID: userID, Asset: asset, DeltaAvailable: num.MustParse(amount), Reason: "test_seed",
	}})
	if err != nil {
		t.Fatalf("credit %s %s: %v", amount, asset, err)
	}
}

func mustJSON(t *testing.T, w interface{ Bytes() []byte }, v any) {
	t.Helper()
	if err := json.Unmarshal(w.Bytes(), v); err != nil {
		t.Fatalf("unmarshal: %v (body %s)", err, string(w.Bytes()))
	}
}

// ---------- spot order handlers ----------

func TestPlaceAndCancelOrder(t *testing.T) {
	srv := newTestServer(t)
	token, _ := register(t, srv, "trader@test.com")

	// Limit buy: 0.02 BTC @ 1000 = 20 USDT notional (min notional is 5).
	body := `{"market":"BTC-USDT","side":"buy","type":"limit","price":"1000","quantity":"0.02"}`
	w := do(t, srv, "POST", "/api/orders", token, body)
	if w.Code != http.StatusCreated {
		t.Fatalf("place order status = %d, body %s", w.Code, w.Body.String())
	}
	var o models.Order
	mustJSON(t, w.Body, &o)
	if o.ID == "" || o.Status != models.StatusOpen || o.Side != models.Buy {
		t.Fatalf("unexpected order: %+v", o)
	}

	// It should now appear in open orders.
	w = do(t, srv, "GET", "/api/orders?market=BTC-USDT", token, "")
	if w.Code != http.StatusOK {
		t.Fatalf("open orders status = %d", w.Code)
	}
	var open []models.Order
	mustJSON(t, w.Body, &open)
	if len(open) != 1 || open[0].ID != o.ID {
		t.Fatalf("open orders = %+v, want the placed order", open)
	}

	// Cancel it.
	w = do(t, srv, "DELETE", "/api/orders/"+o.ID, token, "")
	if w.Code != http.StatusOK {
		t.Fatalf("cancel status = %d, body %s", w.Code, w.Body.String())
	}
	var canceled models.Order
	mustJSON(t, w.Body, &canceled)
	if canceled.Status != models.StatusCanceled {
		t.Errorf("status after cancel = %s, want canceled", canceled.Status)
	}

	// Open orders is now empty (and encodes as [] rather than null).
	w = do(t, srv, "GET", "/api/orders", token, "")
	if body := w.Body.String(); body != "[]\n" {
		t.Errorf("open orders after cancel = %q, want []", body)
	}
}

func TestPlaceOrderValidation(t *testing.T) {
	srv := newTestServer(t)
	token, _ := register(t, srv, "val@test.com")
	cases := []struct {
		name string
		body string
		want int
	}{
		{"bad side", `{"market":"BTC-USDT","side":"sideways","type":"limit","price":"1000","quantity":"0.02"}`, http.StatusBadRequest},
		{"bad type", `{"market":"BTC-USDT","side":"buy","type":"stop","price":"1000","quantity":"0.02"}`, http.StatusBadRequest},
		{"unknown market", `{"market":"NOPE-USDT","side":"buy","type":"limit","price":"1000","quantity":"0.02"}`, http.StatusNotFound},
		{"below min notional", `{"market":"BTC-USDT","side":"buy","type":"limit","price":"1","quantity":"0.00001"}`, http.StatusBadRequest},
		{"malformed json", `not json`, http.StatusBadRequest},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if w := do(t, srv, "POST", "/api/orders", token, c.body); w.Code != c.want {
				t.Errorf("status = %d, want %d (body %s)", w.Code, c.want, w.Body.String())
			}
		})
	}
}

func TestCancelOrderNotFoundAndNotOwner(t *testing.T) {
	srv := newTestServer(t)
	owner, _ := register(t, srv, "owner@test.com")
	other, _ := register(t, srv, "other@test.com")

	if w := do(t, srv, "DELETE", "/api/orders/does-not-exist", owner, ""); w.Code != http.StatusNotFound {
		t.Errorf("cancel unknown order = %d, want 404", w.Code)
	}

	w := do(t, srv, "POST", "/api/orders", owner,
		`{"market":"BTC-USDT","side":"buy","type":"limit","price":"1000","quantity":"0.02"}`)
	var o models.Order
	mustJSON(t, w.Body, &o)

	// A different user must not be able to cancel it.
	if w := do(t, srv, "DELETE", "/api/orders/"+o.ID, other, ""); w.Code != http.StatusForbidden {
		t.Errorf("cancel as non-owner = %d, want 403", w.Code)
	}
}

func TestOrderHistoryAndMyTrades(t *testing.T) {
	srv := newTestServer(t)
	buyer, _ := register(t, srv, "buyer@test.com")
	seller, sellerID := register(t, srv, "seller@test.com")
	credit(t, srv, sellerID, "BTC", "1") // seller needs BTC to sell

	// Seller rests an ask, buyer crosses it -> a trade for both.
	if w := do(t, srv, "POST", "/api/orders", seller,
		`{"market":"BTC-USDT","side":"sell","type":"limit","price":"1000","quantity":"0.02"}`); w.Code != http.StatusCreated {
		t.Fatalf("seller order: %d %s", w.Code, w.Body.String())
	}
	if w := do(t, srv, "POST", "/api/orders", buyer,
		`{"market":"BTC-USDT","side":"buy","type":"limit","price":"1000","quantity":"0.02"}`); w.Code != http.StatusCreated {
		t.Fatalf("buyer order: %d %s", w.Code, w.Body.String())
	}

	// History includes the filled order.
	w := do(t, srv, "GET", "/api/orders/history?market=BTC-USDT&limit=10", buyer, "")
	if w.Code != http.StatusOK {
		t.Fatalf("history status = %d", w.Code)
	}
	var hist []models.Order
	mustJSON(t, w.Body, &hist)
	if len(hist) != 1 || hist[0].Status != models.StatusFilled {
		t.Errorf("buyer history = %+v, want one filled order", hist)
	}

	// Both sides see the trade.
	for name, tok := range map[string]string{"buyer": buyer, "seller": seller} {
		w := do(t, srv, "GET", "/api/trades?market=BTC-USDT", tok, "")
		if w.Code != http.StatusOK {
			t.Fatalf("%s trades status = %d", name, w.Code)
		}
		var trades []models.Trade
		mustJSON(t, w.Body, &trades)
		if len(trades) != 1 || trades[0].Quantity.String() != "0.02" {
			t.Errorf("%s trades = %+v, want one 0.02 trade", name, trades)
		}
	}
}

// ---------- market data handlers ----------

func TestMarketDataHandlers(t *testing.T) {
	srv := newTestServer(t)
	token, _ := register(t, srv, "md@test.com")
	// Rest an order so depth is non-empty.
	do(t, srv, "POST", "/api/orders", token,
		`{"market":"BTC-USDT","side":"buy","type":"limit","price":"1000","quantity":"0.02"}`)

	w := do(t, srv, "GET", "/api/markets/BTC-USDT/depth?limit=10", "", "")
	if w.Code != http.StatusOK {
		t.Fatalf("depth status = %d", w.Code)
	}
	var depth struct {
		Market string `json:"market"`
		Bids   []struct {
			Price string `json:"price"`
			Qty   string `json:"qty"`
		} `json:"bids"`
	}
	mustJSON(t, w.Body, &depth)
	if depth.Market != "BTC-USDT" || len(depth.Bids) != 1 || depth.Bids[0].Price != "1000" {
		t.Errorf("depth = %+v, want one bid at 1000", depth)
	}

	if w := do(t, srv, "GET", "/api/markets/BTC-USDT/trades?limit=5", "", ""); w.Code != http.StatusOK {
		t.Errorf("market trades status = %d", w.Code)
	}
	if w := do(t, srv, "GET", "/api/markets/BTC-USDT/candles?interval=1m&limit=10", "", ""); w.Code != http.StatusOK {
		t.Errorf("candles status = %d", w.Code)
	}
	// Unknown interval falls back to 60s rather than erroring.
	if w := do(t, srv, "GET", "/api/markets/BTC-USDT/candles?interval=bogus", "", ""); w.Code != http.StatusOK {
		t.Errorf("candles with bad interval status = %d, want 200", w.Code)
	}
	// Unknown market.
	if w := do(t, srv, "GET", "/api/markets/NOPE-USDT/depth", "", ""); w.Code != http.StatusNotFound {
		t.Errorf("depth unknown market = %d, want 404", w.Code)
	}
	if w := do(t, srv, "GET", "/api/markets/NOPE-USDT", "", ""); w.Code != http.StatusNotFound {
		t.Errorf("unknown market = %d, want 404", w.Code)
	}
}

// ---------- perp handlers ----------

func TestPerpMarketDataHandlers(t *testing.T) {
	srv := newTestServer(t)
	if w := do(t, srv, "GET", "/api/perp/markets/BTC-PERP", "", ""); w.Code != http.StatusOK {
		t.Errorf("perp market status = %d", w.Code)
	}
	if w := do(t, srv, "GET", "/api/perp/markets/NOPE-PERP", "", ""); w.Code != http.StatusNotFound {
		t.Errorf("unknown perp market = %d, want 404", w.Code)
	}
	if w := do(t, srv, "GET", "/api/perp/markets/BTC-PERP/depth?limit=5", "", ""); w.Code != http.StatusOK {
		t.Errorf("perp depth status = %d", w.Code)
	}
	if w := do(t, srv, "GET", "/api/perp/markets/NOPE-PERP/depth", "", ""); w.Code != http.StatusNotFound {
		t.Errorf("perp depth unknown = %d, want 404", w.Code)
	}
	// Funding returns a zero-rate snapshot before the first funding tick.
	w := do(t, srv, "GET", "/api/perp/markets/BTC-PERP/funding", "", "")
	if w.Code != http.StatusOK {
		t.Fatalf("funding status = %d", w.Code)
	}
	var fi models.FundingInfo
	mustJSON(t, w.Body, &fi)
	if fi.Market != "BTC-PERP" {
		t.Errorf("funding market = %q, want BTC-PERP", fi.Market)
	}
}

func TestPerpOrderLifecycle(t *testing.T) {
	srv := newTestServer(t)
	token, _ := register(t, srv, "perp@test.com")

	// Resting long: 0.001 BTC @ 50000 = 50 notional, 10x -> 5 USDT margin.
	body := `{"market":"BTC-PERP","side":"buy","type":"limit","price":"50000","quantity":"0.001","leverage":10}`
	w := do(t, srv, "POST", "/api/perp/orders", token, body)
	if w.Code != http.StatusCreated {
		t.Fatalf("place perp order = %d, body %s", w.Code, w.Body.String())
	}
	var po models.PerpOrder
	mustJSON(t, w.Body, &po)
	if po.Leverage != 10 || po.Status != models.StatusOpen {
		t.Fatalf("unexpected perp order: %+v", po)
	}

	w = do(t, srv, "GET", "/api/perp/orders?market=BTC-PERP", token, "")
	var open []models.PerpOrder
	mustJSON(t, w.Body, &open)
	if len(open) != 1 {
		t.Fatalf("open perp orders = %d, want 1", len(open))
	}

	if w := do(t, srv, "DELETE", "/api/perp/orders/"+po.ID, token, ""); w.Code != http.StatusOK {
		t.Fatalf("cancel perp order = %d, body %s", w.Code, w.Body.String())
	}

	w = do(t, srv, "GET", "/api/perp/orders/history?market=BTC-PERP&limit=10", token, "")
	if w.Code != http.StatusOK {
		t.Fatalf("perp history status = %d", w.Code)
	}
	var hist []models.PerpOrder
	mustJSON(t, w.Body, &hist)
	if len(hist) != 1 || hist[0].Status != models.StatusCanceled {
		t.Errorf("perp history = %+v, want one canceled order", hist)
	}
}

func TestPerpOrderValidation(t *testing.T) {
	srv := newTestServer(t)
	token, _ := register(t, srv, "perpval@test.com")
	cases := []struct {
		name string
		body string
		want int
	}{
		{"bad side", `{"market":"BTC-PERP","side":"up","type":"limit","price":"50000","quantity":"0.001"}`, http.StatusBadRequest},
		{"bad type", `{"market":"BTC-PERP","side":"buy","type":"stop","price":"50000","quantity":"0.001"}`, http.StatusBadRequest},
		{"unknown market", `{"market":"NOPE-PERP","side":"buy","type":"limit","price":"50000","quantity":"0.001"}`, http.StatusNotFound},
		{"malformed json", `{`, http.StatusBadRequest},
		{"market order with no liquidity", `{"market":"BTC-PERP","side":"buy","type":"market","quantity":"0.001"}`, http.StatusBadRequest},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if w := do(t, srv, "POST", "/api/perp/orders", token, c.body); w.Code != c.want {
				t.Errorf("status = %d, want %d (body %s)", w.Code, c.want, w.Body.String())
			}
		})
	}
	if w := do(t, srv, "DELETE", "/api/perp/orders/nope", token, ""); w.Code != http.StatusNotFound {
		t.Errorf("cancel unknown perp order = %d, want 404", w.Code)
	}
}

// TestPositionsAndClose opens a long, lists it, then closes it through the
// close-position handler (a reduce-only market order into a resting bid).
func TestPositionsAndClose(t *testing.T) {
	srv := newTestServer(t)
	maker, _ := register(t, srv, "pmaker@test.com")
	taker, _ := register(t, srv, "ptaker@test.com")

	// Maker rests an ask; taker buys into it -> taker is long 0.001.
	if w := do(t, srv, "POST", "/api/perp/orders", maker,
		`{"market":"BTC-PERP","side":"sell","type":"limit","price":"50000","quantity":"0.001","leverage":10}`); w.Code != http.StatusCreated {
		t.Fatalf("maker ask: %d %s", w.Code, w.Body.String())
	}
	if w := do(t, srv, "POST", "/api/perp/orders", taker,
		`{"market":"BTC-PERP","side":"buy","type":"limit","price":"50000","quantity":"0.001","leverage":10}`); w.Code != http.StatusCreated {
		t.Fatalf("taker bid: %d %s", w.Code, w.Body.String())
	}

	w := do(t, srv, "GET", "/api/perp/positions", taker, "")
	if w.Code != http.StatusOK {
		t.Fatalf("positions status = %d", w.Code)
	}
	var positions []models.Position
	mustJSON(t, w.Body, &positions)
	if len(positions) != 1 || positions[0].Side != models.Long {
		t.Fatalf("taker positions = %+v, want one long", positions)
	}
	// Enriched (computed) fields are populated for the API view.
	if positions[0].MarkPrice.Sign() <= 0 || positions[0].Notional.Sign() <= 0 {
		t.Errorf("position not enriched: %+v", positions[0])
	}

	// Maker rests a bid so the close (market sell) has something to hit.
	if w := do(t, srv, "POST", "/api/perp/orders", maker,
		`{"market":"BTC-PERP","side":"buy","type":"limit","price":"49900","quantity":"0.001","leverage":10}`); w.Code != http.StatusCreated {
		t.Fatalf("maker bid: %d %s", w.Code, w.Body.String())
	}
	if w := do(t, srv, "POST", "/api/perp/positions/BTC-PERP/close", taker, ""); w.Code != http.StatusOK {
		t.Fatalf("close position = %d, body %s", w.Code, w.Body.String())
	}

	pos, err := srv.st.GetPosition(positions[0].UserID, "BTC-PERP")
	if err != nil {
		t.Fatal(err)
	}
	if pos.Side != models.Flat || pos.Size.Sign() != 0 {
		t.Errorf("position after close = %+v, want flat", pos)
	}
}

func TestClosePositionWithoutPosition(t *testing.T) {
	srv := newTestServer(t)
	token, _ := register(t, srv, "noposition@test.com")
	if w := do(t, srv, "POST", "/api/perp/positions/BTC-PERP/close", token, ""); w.Code != http.StatusBadRequest {
		t.Errorf("close with no position = %d, want 400", w.Code)
	}
}

// ---------- wallet handlers ----------

func TestWalletAddressDepositAndTxns(t *testing.T) {
	srv := newTestServer(t)
	token, _ := register(t, srv, "wallet@test.com")

	w := do(t, srv, "GET", "/api/wallet/address?asset=BTC", token, "")
	if w.Code != http.StatusOK {
		t.Fatalf("address status = %d, body %s", w.Code, w.Body.String())
	}
	var addr models.WalletAddress
	mustJSON(t, w.Body, &addr)
	if addr.Asset != "BTC" || addr.Address == "" {
		t.Fatalf("address = %+v", addr)
	}
	// Stable across calls.
	w2 := do(t, srv, "GET", "/api/wallet/address?asset=BTC", token, "")
	var addr2 models.WalletAddress
	mustJSON(t, w2.Body, &addr2)
	if addr2.Address != addr.Address {
		t.Errorf("address not stable: %s vs %s", addr.Address, addr2.Address)
	}
	if w := do(t, srv, "GET", "/api/wallet/address?asset=NOPE", token, ""); w.Code != http.StatusBadRequest {
		t.Errorf("unknown asset address = %d, want 400", w.Code)
	}

	// Deposit is accepted immediately and confirms asynchronously.
	if w := do(t, srv, "POST", "/api/wallet/deposit", token, `{"asset":"BTC","amount":"1"}`); w.Code != http.StatusAccepted {
		t.Fatalf("deposit = %d, body %s", w.Code, w.Body.String())
	}
	if w := do(t, srv, "POST", "/api/wallet/deposit", token, `{"asset":"BTC","amount":"0"}`); w.Code != http.StatusBadRequest {
		t.Errorf("zero deposit = %d, want 400", w.Code)
	}
	if w := do(t, srv, "POST", "/api/wallet/deposit", token, `bad json`); w.Code != http.StatusBadRequest {
		t.Errorf("malformed deposit = %d, want 400", w.Code)
	}

	w = do(t, srv, "GET", "/api/wallet/transactions?limit=10", token, "")
	if w.Code != http.StatusOK {
		t.Fatalf("txns status = %d", w.Code)
	}
	var txns []models.WalletTxn
	mustJSON(t, w.Body, &txns)
	if len(txns) != 1 || txns[0].Type != models.TxnDeposit {
		t.Errorf("txns = %+v, want one deposit", txns)
	}
}

func TestWithdrawRequiresKYCThenSucceeds(t *testing.T) {
	srv := newTestServer(t)
	token, userID := register(t, srv, "withdraw@test.com")

	// Withdrawals are blocked until identity is verified.
	body := `{"asset":"USDT","address":"TXYZdestination","amount":"100"}`
	if w := do(t, srv, "POST", "/api/wallet/withdraw", token, body); w.Code != http.StatusForbidden {
		t.Fatalf("withdraw pre-KYC = %d, want 403", w.Code)
	}

	if w := do(t, srv, "POST", "/api/kyc/verify", token, ""); w.Code != http.StatusOK {
		t.Fatalf("kyc verify = %d", w.Code)
	}

	// Below the 10 USDT minimum.
	if w := do(t, srv, "POST", "/api/wallet/withdraw", token,
		`{"asset":"USDT","address":"TXYZ","amount":"1"}`); w.Code != http.StatusBadRequest {
		t.Errorf("below-min withdraw = %d, want 400", w.Code)
	}
	// Empty destination address.
	if w := do(t, srv, "POST", "/api/wallet/withdraw", token,
		`{"asset":"USDT","address":"","amount":"100"}`); w.Code != http.StatusBadRequest {
		t.Errorf("empty-address withdraw = %d, want 400", w.Code)
	}
	// More than the balance (10000 welcome bonus).
	if w := do(t, srv, "POST", "/api/wallet/withdraw", token,
		`{"asset":"USDT","address":"TXYZ","amount":"99999"}`); w.Code != http.StatusBadRequest {
		t.Errorf("overdraw withdraw = %d, want 400", w.Code)
	}

	if w := do(t, srv, "POST", "/api/wallet/withdraw", token, body); w.Code != http.StatusAccepted {
		t.Fatalf("withdraw = %d, body %s", w.Code, w.Body.String())
	}
	// 100 withdrawn + 1 network fee debited from the 10000 welcome bonus.
	b, err := srv.st.GetBalance(userID, "USDT")
	if err != nil {
		t.Fatal(err)
	}
	if b.Available.String() != "9899" {
		t.Errorf("balance after withdraw = %s, want 9899", b.Available)
	}
}

// ---------- earn handlers ----------

func TestEarnProductsSubscribeAndRedeem(t *testing.T) {
	srv := newTestServer(t)
	token, _ := register(t, srv, "earn@test.com")

	w := do(t, srv, "GET", "/api/earn/products", "", "")
	if w.Code != http.StatusOK {
		t.Fatalf("earn products status = %d", w.Code)
	}
	var products []models.EarnProduct
	mustJSON(t, w.Body, &products)
	if len(products) == 0 {
		t.Fatal("expected seeded earn products")
	}

	// No positions yet (encoded as [] not null).
	if w := do(t, srv, "GET", "/api/earn/positions", token, ""); w.Body.String() != "[]\n" {
		t.Errorf("initial earn positions = %q, want []", w.Body.String())
	}

	w = do(t, srv, "POST", "/api/earn/subscribe", token, `{"productId":"USDT-FLEX","amount":"100"}`)
	if w.Code != http.StatusCreated {
		t.Fatalf("subscribe = %d, body %s", w.Code, w.Body.String())
	}
	var pos models.EarnPosition
	mustJSON(t, w.Body, &pos)
	if pos.Status != models.EarnActive || pos.Principal.String() != "100" {
		t.Fatalf("subscribed position = %+v", pos)
	}

	w = do(t, srv, "GET", "/api/earn/positions", token, "")
	var positions []models.EarnPosition
	mustJSON(t, w.Body, &positions)
	if len(positions) != 1 {
		t.Fatalf("earn positions = %d, want 1", len(positions))
	}

	if w := do(t, srv, "POST", "/api/earn/positions/"+pos.ID+"/redeem", token, ""); w.Code != http.StatusOK {
		t.Fatalf("redeem = %d, body %s", w.Code, w.Body.String())
	}
	// Redeeming twice must fail (guarded transition).
	if w := do(t, srv, "POST", "/api/earn/positions/"+pos.ID+"/redeem", token, ""); w.Code != http.StatusBadRequest {
		t.Errorf("second redeem = %d, want 400", w.Code)
	}
}

func TestEarnSubscribeValidation(t *testing.T) {
	srv := newTestServer(t)
	token, _ := register(t, srv, "earnval@test.com")
	cases := []struct {
		name string
		body string
		want int
	}{
		{"unknown product", `{"productId":"NOPE","amount":"100"}`, http.StatusNotFound},
		{"below minimum", `{"productId":"USDT-FLEX","amount":"1"}`, http.StatusBadRequest},
		{"zero amount", `{"productId":"USDT-FLEX","amount":"0"}`, http.StatusBadRequest},
		{"insufficient funds", `{"productId":"USDT-FLEX","amount":"999999"}`, http.StatusBadRequest},
		{"malformed json", `{`, http.StatusBadRequest},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if w := do(t, srv, "POST", "/api/earn/subscribe", token, c.body); w.Code != c.want {
				t.Errorf("status = %d, want %d (body %s)", w.Code, c.want, w.Body.String())
			}
		})
	}
	if w := do(t, srv, "POST", "/api/earn/positions/nope/redeem", token, ""); w.Code != http.StatusNotFound {
		t.Errorf("redeem unknown position = %d, want 404", w.Code)
	}
}

func TestEarnRedeemNotOwner(t *testing.T) {
	srv := newTestServer(t)
	owner, _ := register(t, srv, "eowner@test.com")
	other, _ := register(t, srv, "eother@test.com")

	w := do(t, srv, "POST", "/api/earn/subscribe", owner, `{"productId":"USDT-FLEX","amount":"100"}`)
	var pos models.EarnPosition
	mustJSON(t, w.Body, &pos)

	if w := do(t, srv, "POST", "/api/earn/positions/"+pos.ID+"/redeem", other, ""); w.Code != http.StatusForbidden {
		t.Errorf("redeem as non-owner = %d, want 403", w.Code)
	}
}

// ---------- small helpers ----------

func TestParseSideType(t *testing.T) {
	cases := []struct {
		side, typ string
		ok        bool
		wantSide  models.Side
		wantType  models.OrderType
	}{
		{"buy", "limit", true, models.Buy, models.TypeLimit},
		{"SELL", "MARKET", true, models.Sell, models.TypeMarket}, // case-insensitive
		{"buy", "stop", false, "", ""},
		{"hold", "limit", false, "", ""},
		{"", "", false, "", ""},
	}
	for _, c := range cases {
		w := httptest.NewRecorder()
		side, typ, ok := parseSideType(w, c.side, c.typ)
		if ok != c.ok {
			t.Errorf("parseSideType(%q,%q) ok = %v, want %v", c.side, c.typ, ok, c.ok)
			continue
		}
		if ok && (side != c.wantSide || typ != c.wantType) {
			t.Errorf("parseSideType(%q,%q) = %q,%q; want %q,%q", c.side, c.typ, side, typ, c.wantSide, c.wantType)
		}
		if !ok && w.Code != http.StatusBadRequest {
			t.Errorf("parseSideType(%q,%q) wrote status %d, want 400", c.side, c.typ, w.Code)
		}
	}
}

func TestNonNilOrders(t *testing.T) {
	if got := nonNilOrders(nil); got == nil || len(got) != 0 {
		t.Errorf("nonNilOrders(nil) = %v, want empty non-nil slice", got)
	}
	in := []models.Order{{ID: "a"}}
	if got := nonNilOrders(in); len(got) != 1 || got[0].ID != "a" {
		t.Errorf("nonNilOrders passed through incorrectly: %v", got)
	}
}
