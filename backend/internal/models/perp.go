package models

import "cryptoex/internal/num"

// PerpMarket is a linear, USDT-margined perpetual futures contract. Positions
// are collateralized and settled in `Settle` (USDT); `IndexSymbol` names the
// spot market used as the funding index.
type PerpMarket struct {
	Symbol      string  `json:"symbol"` // e.g. BTC-PERP
	Base        string  `json:"base"`   // BTC
	Settle      string  `json:"settle"` // USDT
	IndexSymbol string  `json:"indexSymbol"`
	PriceTick   num.Dec `json:"priceTick"`
	QtyStep     num.Dec `json:"qtyStep"`
	MinNotional num.Dec `json:"minNotional"`
	MakerFee    num.Dec `json:"makerFee"`
	TakerFee    num.Dec `json:"takerFee"`
	MaxLeverage int     `json:"maxLeverage"`
	MMR         num.Dec `json:"mmr"` // maintenance margin rate, e.g. 0.005 = 0.5%
	Status      string  `json:"status"`
}

// PositionSide is the direction of a netted position.
type PositionSide string

const (
	Long  PositionSide = "long"
	Short PositionSide = "short"
	Flat  PositionSide = "flat"
)

// Position is a user's netted (one-way) position in a perp market. Size is the
// absolute base quantity; direction is carried by Side. Margin is the isolated
// USDT collateral held in the user's locked balance and attributed to this
// position.
type Position struct {
	ID          int64        `json:"id"`
	UserID      int64        `json:"userId"`
	Market      string       `json:"market"`
	Side        PositionSide `json:"side"`
	Size        num.Dec      `json:"size"`
	EntryPrice  num.Dec      `json:"entryPrice"`
	Margin      num.Dec      `json:"margin"`
	Leverage    int          `json:"leverage"`
	RealizedPnL num.Dec      `json:"realizedPnl"`
	FundingPaid num.Dec      `json:"fundingPaid"`
	UpdatedAt   int64        `json:"updatedAt"`

	// Computed, not persisted — populated for API responses against live mark price.
	MarkPrice     num.Dec `json:"markPrice"`
	LiqPrice      num.Dec `json:"liqPrice"`
	UnrealizedPnL num.Dec `json:"unrealizedPnl"`
	Notional      num.Dec `json:"notional"`
	MarginRatio   num.Dec `json:"marginRatio"`
}

// PerpOrder is an order against a perpetual market. Quantity is always in base
// contracts (unlike spot market buys, which use a quote budget).
type PerpOrder struct {
	ID         string      `json:"id"`
	UserID     int64       `json:"userId"`
	Market     string      `json:"market"`
	Side       Side        `json:"side"` // buy = long, sell = short
	Type       OrderType   `json:"type"`
	Price      num.Dec     `json:"price"`
	Quantity   num.Dec     `json:"quantity"`
	Filled     num.Dec     `json:"filled"`
	AvgPrice   num.Dec     `json:"avgPrice"`
	Leverage   int         `json:"leverage"`
	ReduceOnly bool        `json:"reduceOnly"`
	Status     OrderStatus `json:"status"`
	CreatedAt  int64       `json:"createdAt"`
	UpdatedAt  int64       `json:"updatedAt"`
}

// FundingInfo is the current funding state of a perp market, published on the
// ticker stream and exposed via REST.
type FundingInfo struct {
	Market          string  `json:"market"`
	Rate            num.Dec `json:"rate"` // applied per interval
	IndexPrice      num.Dec `json:"indexPrice"`
	MarkPrice       num.Dec `json:"markPrice"`
	IntervalSec     int64   `json:"intervalSec"`
	NextFundingTime int64   `json:"nextFundingTime"`
}
