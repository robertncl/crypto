// Package bot runs a simple two-account market maker that seeds liquidity and
// generates trades so the exchange looks alive in a demo. A "maker" account
// posts a ladder of limit orders around a drifting mid price; a "taker" account
// periodically crosses the spread with market orders, producing trades, candles
// and ticker movement. The two distinct accounts avoid self-trade prevention.
package bot

import (
	"context"
	"math"
	"math/rand"
	"strconv"
	"time"

	"cryptoex/internal/auth"
	"cryptoex/internal/engine"
	"cryptoex/internal/models"
	"cryptoex/internal/num"
	"cryptoex/internal/store"
)

// seedPrices gives each market a believable starting mid; unknown markets fall
// back to 100.
var seedPrices = map[string]float64{
	"BTC-USDT": 95000,
	"ETH-USDT": 3500,
	"SOL-USDT": 180,
	"BNB-USDT": 600,
}

type Bot struct {
	st      *store.Store
	mgr     *engine.Manager
	markets []models.Market
	makerID int64
	takerID int64
	rng     *rand.Rand
}

func New(st *store.Store, mgr *engine.Manager, markets []models.Market) *Bot {
	return &Bot{st: st, mgr: mgr, markets: markets, rng: rand.New(rand.NewSource(time.Now().UnixNano()))}
}

// Start provisions the bot accounts and launches a maker + taker loop per market.
func (b *Bot) Start(ctx context.Context) error {
	maker, err := b.ensureUser("liquidity-maker@exchange.local")
	if err != nil {
		return err
	}
	taker, err := b.ensureUser("liquidity-taker@exchange.local")
	if err != nil {
		return err
	}
	b.makerID, b.takerID = maker.ID, taker.ID
	b.fund(maker.ID)
	b.fund(taker.ID)

	for _, m := range b.markets {
		mid := seedPrices[m.Symbol]
		if mid == 0 {
			mid = 100
		}
		go b.runMarket(ctx, m, mid)
	}
	return nil
}

func (b *Bot) ensureUser(email string) (*models.User, error) {
	if u, err := b.st.GetUserByEmail(email); err == nil {
		return u, nil
	}
	hash, _ := auth.HashPassword("bot-" + email)
	u, err := b.st.CreateUser(email, hash, "bot", time.Now().Unix())
	if err != nil {
		return nil, err
	}
	_ = b.st.SetKYCStatus(u.ID, "verified")
	return u, nil
}

// fund credits a bot account with a large balance of every asset so it never
// runs out while seeding the market.
func (b *Bot) fund(userID int64) {
	assets, _ := b.st.ListAssets()
	now := time.Now().Unix()
	for _, a := range assets {
		amount := num.MustParse("100000000") // generous
		_ = b.st.ApplyPostings("botseed:"+a.Symbol+":"+itoa(userID), now, []store.Posting{{
			UserID: userID, Asset: a.Symbol, DeltaAvailable: amount, Reason: "bot_seed", Ref: "bot",
		}})
	}
}

func (b *Bot) runMarket(ctx context.Context, m models.Market, mid float64) {
	eng, ok := b.mgr.Get(m.Symbol)
	if !ok {
		return
	}
	vol := 0.0009 // per-tick volatility of the random walk
	makerTick := time.NewTicker(4 * time.Second)
	takerTick := time.NewTicker(2500 * time.Millisecond)
	defer makerTick.Stop()
	defer takerTick.Stop()

	var prevIDs []string
	refresh := func() {
		// Drift the mid price.
		mid *= 1 + b.rng.NormFloat64()*vol
		if mid <= 0 {
			mid = 1
		}
		// Capture previous orders to cancel after re-quoting (keeps liquidity continuous).
		old := prevIDs
		prevIDs = b.quoteLadder(eng, m, mid)
		for _, id := range old {
			_ = eng.Cancel(id, b.makerID)
		}
	}
	refresh() // initial quotes

	for {
		select {
		case <-ctx.Done():
			return
		case <-makerTick.C:
			refresh()
		case <-takerTick.C:
			if b.rng.Float64() < 0.65 {
				b.cross(eng, m, mid)
			}
		}
	}
}

// quoteLadder posts a symmetric ladder of bids and asks around mid and returns
// the resulting order ids.
func (b *Bot) quoteLadder(eng *engine.Engine, m models.Market, mid float64) []string {
	const levels = 4
	var ids []string
	for i := 1; i <= levels; i++ {
		spread := 0.0006 * float64(i) // widening steps
		// Target each level around a random quote notional for realistic sizes.
		notional := 800 + b.rng.Float64()*2500

		bidPrice := roundTo(mid*(1-spread), m.PriceTick)
		askPrice := roundTo(mid*(1+spread), m.PriceTick)
		bidQty := roundTo(notional/bidPrice, m.QtyStep)
		askQty := roundTo(notional/askPrice, m.QtyStep)

		if id := b.placeLimit(eng, m, models.Buy, bidPrice, bidQty); id != "" {
			ids = append(ids, id)
		}
		if id := b.placeLimit(eng, m, models.Sell, askPrice, askQty); id != "" {
			ids = append(ids, id)
		}
	}
	return ids
}

func (b *Bot) placeLimit(eng *engine.Engine, m models.Market, side models.Side, price, qty float64) string {
	p := num.MustParse(ftoa(price))
	q := num.MustParse(ftoa(qty))
	if q.Sign() <= 0 || p.Sign() <= 0 || p.Mul(q).Lt(m.MinNotional) {
		return ""
	}
	o := &models.Order{UserID: b.makerID, Side: side, Type: models.TypeLimit, Price: p, Quantity: q}
	out, err := eng.Place(o)
	if err != nil {
		return ""
	}
	return out.ID
}

// cross sends a small market order from the taker account to hit the book.
func (b *Bot) cross(eng *engine.Engine, m models.Market, mid float64) {
	notional := 60 + b.rng.Float64()*700
	if b.rng.Float64() < 0.5 {
		// market buy with a quote budget
		budget := roundTo(notional, m.QtyStep)
		q := num.MustParse(ftoa(budget))
		if q.Lt(m.MinNotional) {
			q = m.MinNotional
		}
		_, _ = eng.Place(&models.Order{UserID: b.takerID, Side: models.Buy, Type: models.TypeMarket, Quantity: q})
	} else {
		qty := roundTo(notional/mid, m.QtyStep)
		q := num.MustParse(ftoa(qty))
		if q.Sign() <= 0 {
			return
		}
		_, _ = eng.Place(&models.Order{UserID: b.takerID, Side: models.Sell, Type: models.TypeMarket, Quantity: q})
	}
}

// ---------- numeric helpers ----------

// roundTo rounds value to the nearest multiple of step (both as floats).
func roundTo(value float64, step num.Dec) float64 {
	s := step.Float64()
	if s <= 0 {
		return value
	}
	return math.Round(value/s) * s
}

// ftoa formats a float with 8 decimals (matching num.Decimals) for parsing into
// num.Dec.
func ftoa(f float64) string {
	return strconv.FormatFloat(f, 'f', num.Decimals, 64)
}

func itoa(n int64) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		b[i] = '-'
	}
	return string(b[i:])
}
