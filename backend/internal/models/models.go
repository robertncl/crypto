// Package models holds the core domain types shared across the exchange. They
// double as the JSON wire format (camelCase tags) and the in-memory shape of DB
// rows. Monetary values use num.Dec, never float64.
package models

import "cryptoex/internal/num"

// User is an exchange account holder.
type User struct {
	ID           int64  `json:"id"`
	Email        string `json:"email"`
	PasswordHash string `json:"-"`
	KYCStatus    string `json:"kycStatus"` // none | pending | verified
	Role         string `json:"role"`      // user | admin | bot
	CreatedAt    int64  `json:"createdAt"`
}

// Asset is a tradable/holdable currency (crypto or fiat).
type Asset struct {
	Symbol        string  `json:"symbol"`
	Name          string  `json:"name"`
	Kind          string  `json:"kind"` // crypto | fiat
	Decimals      int     `json:"decimals"`
	Network       string  `json:"network"`
	WithdrawFee   num.Dec `json:"withdrawFee"`
	MinWithdraw   num.Dec `json:"minWithdraw"`
	Confirmations int     `json:"confirmations"` // simulated confirmations before credit
}

// Market is a trading pair such as BTC-USDT.
type Market struct {
	Symbol      string  `json:"symbol"`
	Base        string  `json:"base"`
	Quote       string  `json:"quote"`
	PriceTick   num.Dec `json:"priceTick"`   // minimum price increment
	QtyStep     num.Dec `json:"qtyStep"`     // minimum quantity increment
	MinNotional num.Dec `json:"minNotional"` // minimum order value in quote
	MakerFee    num.Dec `json:"makerFee"`    // fraction, e.g. 0.001 = 0.1%
	TakerFee    num.Dec `json:"takerFee"`
	Status      string  `json:"status"` // trading | halted
}

// Balance is a user's holding of a single asset.
type Balance struct {
	Asset     string  `json:"asset"`
	Available num.Dec `json:"available"`
	Locked    num.Dec `json:"locked"`
}

// Side is the direction of an order.
type Side string

const (
	Buy  Side = "buy"
	Sell Side = "sell"
)

// OrderType distinguishes limit and market orders.
type OrderType string

const (
	Limit  OrderType = "limit"
	Market OrderType = "market"
)

// OrderStatus tracks an order's lifecycle.
type OrderStatus string

const (
	StatusOpen     OrderStatus = "open"
	StatusPartial  OrderStatus = "partial"
	StatusFilled   OrderStatus = "filled"
	StatusCanceled OrderStatus = "canceled"
	StatusRejected OrderStatus = "rejected"
)

// Order is a request to trade. For a market BUY, Quantity carries the quote
// budget to spend rather than a base quantity; for all other orders Quantity is
// the base quantity.
type Order struct {
	ID          string      `json:"id"`
	UserID      int64       `json:"userId"`
	Market      string      `json:"market"`
	Side        Side        `json:"side"`
	Type        OrderType   `json:"type"`
	Price       num.Dec     `json:"price"`
	Quantity    num.Dec     `json:"quantity"`
	Filled      num.Dec     `json:"filled"`      // base quantity matched so far
	QuoteFilled num.Dec     `json:"quoteFilled"` // quote value matched so far
	FeePaid     num.Dec     `json:"feePaid"`
	Status      OrderStatus `json:"status"`
	CreatedAt   int64       `json:"createdAt"`
	UpdatedAt   int64       `json:"updatedAt"`
}

// Remaining returns the unfilled base quantity for limit orders.
func (o *Order) Remaining() num.Dec { return o.Quantity.Sub(o.Filled) }

// Trade is a single match between a resting (maker) and incoming (taker) order.
type Trade struct {
	ID          int64   `json:"id"`
	Market      string  `json:"market"`
	Price       num.Dec `json:"price"`
	Quantity    num.Dec `json:"quantity"`
	QuoteQty    num.Dec `json:"quoteQty"`
	TakerSide   Side    `json:"takerSide"`
	BuyOrderID  string  `json:"buyOrderId"`
	SellOrderID string  `json:"sellOrderId"`
	BuyUserID   int64   `json:"-"`
	SellUserID  int64   `json:"-"`
	CreatedAt   int64   `json:"createdAt"`
}

// WalletAddress is a (simulated) deposit address for a user + asset.
type WalletAddress struct {
	Asset   string `json:"asset"`
	Address string `json:"address"`
	Network string `json:"network"`
}

// TxnType and TxnStatus describe wallet movements.
type TxnType string

const (
	TxnDeposit    TxnType = "deposit"
	TxnWithdrawal TxnType = "withdrawal"
)

type TxnStatus string

const (
	TxnPending   TxnStatus = "pending"
	TxnConfirmed TxnStatus = "confirmed"
	TxnCompleted TxnStatus = "completed"
	TxnFailed    TxnStatus = "failed"
)

// WalletTxn is a deposit or withdrawal record.
type WalletTxn struct {
	ID            string    `json:"id"`
	UserID        int64     `json:"userId"`
	Asset         string    `json:"asset"`
	Type          TxnType   `json:"type"`
	Amount        num.Dec   `json:"amount"`
	Fee           num.Dec   `json:"fee"`
	Address       string    `json:"address"`
	TxID          string    `json:"txid"`
	Status        TxnStatus `json:"status"`
	Confirmations int       `json:"confirmations"`
	CreatedAt     int64     `json:"createdAt"`
	UpdatedAt     int64     `json:"updatedAt"`
}

// Candle is an OHLCV bar for charting.
type Candle struct {
	Time   int64   `json:"time"` // bucket start, unix seconds
	Open   num.Dec `json:"open"`
	High   num.Dec `json:"high"`
	Low    num.Dec `json:"low"`
	Close  num.Dec `json:"close"`
	Volume num.Dec `json:"volume"` // base volume
}

// Ticker is a 24h rolling market summary.
type Ticker struct {
	Market     string  `json:"market"`
	Last       num.Dec `json:"last"`
	Open24h    num.Dec `json:"open24h"`
	High24h    num.Dec `json:"high24h"`
	Low24h     num.Dec `json:"low24h"`
	Volume24h  num.Dec `json:"volume24h"`  // base volume
	QuoteVol24 num.Dec `json:"quoteVol24h"` // quote volume
	Change     num.Dec `json:"change"`      // absolute price change
	ChangePct  num.Dec `json:"changePct"`   // percent change
	BestBid    num.Dec `json:"bestBid"`
	BestAsk    num.Dec `json:"bestAsk"`
	UpdatedAt  int64   `json:"updatedAt"`
}
