package models

import "cryptoex/internal/num"

// Earn product kinds. Flexible products can be redeemed at any time; fixed
// products lock the principal until maturity.
const (
	EarnFlexible = "flexible"
	EarnFixed    = "fixed"
)

// Earn position lifecycle states.
const (
	EarnActive   = "active"
	EarnRedeemed = "redeemed"
)

// EarnProduct is a yield-bearing savings product for a single asset. Subscribing
// moves an amount of the asset into the product, which then accrues interest at
// the annualized rate APR.
type EarnProduct struct {
	ID        string  `json:"id"`        // e.g. BTC-FLEX, USDT-30D
	Asset     string  `json:"asset"`     // asset symbol held by the product
	Kind      string  `json:"kind"`      // flexible | fixed
	APR       num.Dec `json:"apr"`       // annualized rate as a fraction (0.05 = 5%)
	TermDays  int     `json:"termDays"`  // lock period in days; 0 for flexible
	MinAmount num.Dec `json:"minAmount"` // minimum subscription amount
	MaxAmount num.Dec `json:"maxAmount"` // per-subscription cap; 0 = uncapped
	Status    string  `json:"status"`    // active | paused
}

// EarnPosition is a user's subscription to an EarnProduct. Interest accrues
// continuously and is paid into the user's spendable balance by the accrual
// scheduler; AccruedTotal is the lifetime interest earned so far. The principal
// is returned on redemption.
type EarnPosition struct {
	ID            string  `json:"id"`
	UserID        int64   `json:"userId"`
	ProductID     string  `json:"productId"`
	Asset         string  `json:"asset"`
	Kind          string  `json:"kind"`          // flexible | fixed (snapshot)
	Principal     num.Dec `json:"principal"`     // amount subscribed
	APR           num.Dec `json:"apr"`           // rate snapshot at subscription
	AccruedTotal  num.Dec `json:"accruedTotal"`  // lifetime interest paid out
	Status        string  `json:"status"`        // active | redeemed
	StartAt       int64   `json:"startAt"`       // subscription time (unix sec)
	MaturityAt    int64   `json:"maturityAt"`    // unlock time; 0 for flexible
	LastAccrualAt int64   `json:"lastAccrualAt"` // last interest accrual time
	RedeemedAt    int64   `json:"redeemedAt"`    // redemption time; 0 while active
}
