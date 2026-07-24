// Package engine implements the spot matching engine. Each market runs a single
// Engine goroutine that processes place/cancel commands serially, so the
// in-memory order book needs no locking. Every balance change is routed through
// the store's atomic posting/fill primitives, keeping funds, the audit ledger,
// and order state consistent.
package engine

import (
	"errors"
	"fmt"
	"strconv"
	"time"

	"cryptoex/internal/market"
	"cryptoex/internal/models"
	"cryptoex/internal/num"
	"cryptoex/internal/store"
	"cryptoex/internal/ws"

	"github.com/google/uuid"
)

const depthLevels = 50

var (
	ErrMarketHalted  = errors.New("market is not trading")
	ErrBadOrder      = errors.New("invalid order")
	ErrOrderNotFound = errors.New("order not found or not active")
	ErrNotOwner      = errors.New("not order owner")
)

// Engine matches orders for a single market.
type Engine struct {
	mkt  models.Market
	book *book
	st   *store.Store
	md   *market.Service
	hub  *ws.Hub
	cmds chan func()
}

func newEngine(mkt models.Market, st *store.Store, md *market.Service, hub *ws.Hub) *Engine {
	return &Engine{
		mkt:  mkt,
		book: newBook(),
		st:   st,
		md:   md,
		hub:  hub,
		cmds: make(chan func(), 256),
	}
}

func (e *Engine) start() {
	go func() {
		for f := range e.cmds {
			f()
		}
	}()
}

// Place submits an order and blocks until it has been matched/rested.
func (e *Engine) Place(o *models.Order) (*models.Order, error) {
	type res struct {
		o   *models.Order
		err error
	}
	ch := make(chan res, 1)
	e.cmds <- func() {
		out, err := e.place(o)
		ch <- res{out, err}
	}
	r := <-ch
	return r.o, r.err
}

// Cancel removes a working order owned by userID.
func (e *Engine) Cancel(orderID string, userID int64) error {
	ch := make(chan error, 1)
	e.cmds <- func() { ch <- e.cancel(orderID, userID) }
	return <-ch
}

// ---------- placement ----------

func (e *Engine) place(o *models.Order) (*models.Order, error) {
	if e.mkt.Status != "trading" {
		return nil, ErrMarketHalted
	}
	now := time.Now().Unix()
	o.ID = uuid.NewString()
	o.Market = e.mkt.Symbol
	o.CreatedAt, o.UpdatedAt = now, now
	o.Filled, o.QuoteFilled, o.FeePaid = num.Zero, num.Zero, num.Zero
	o.Status = models.StatusOpen

	lockAsset, lockAmt, err := e.validateAndLockAmount(o)
	if err != nil {
		return nil, err
	}

	// Reserve funds (available -> locked). Fails fast on insufficient balance.
	lockTxn := "lock:" + o.ID
	if err := e.st.ApplyPostings(lockTxn, now, []store.Posting{{
		UserID: o.UserID, Asset: lockAsset,
		DeltaAvailable: lockAmt.Neg(), DeltaLocked: lockAmt,
		Reason: "order_lock", Ref: o.ID,
	}}); err != nil {
		if errors.Is(err, store.ErrInsufficientFunds) {
			return nil, store.ErrInsufficientFunds
		}
		return nil, err
	}

	if err := e.st.InsertOrder(o); err != nil {
		return nil, err
	}

	taker := &restingOrder{
		id: o.ID, userID: o.UserID, side: o.Side, typ: o.Type,
		price: o.Price, remaining: o.Quantity, budget: num.Zero,
		locked: lockAmt, createdAt: now, mo: o,
	}
	if o.Type == models.TypeMarket && o.Side == models.Buy {
		taker.budget = o.Quantity // Quantity carries the quote budget
	}

	affected := map[int64]bool{o.UserID: true}
	if err := e.match(taker, now, affected); err != nil {
		// A settlement error mid-match is unexpected; surface it.
		return nil, err
	}

	// Resting vs terminal handling.
	rest := o.Type == models.TypeLimit && taker.remaining.Sign() > 0
	if rest {
		if o.Filled.Sign() > 0 {
			o.Status = models.StatusPartial
		} else {
			o.Status = models.StatusOpen
		}
		o.UpdatedAt = time.Now().Unix()
		e.book.add(taker)
		_ = e.st.UpdateOrder(o)
	} else {
		// Unlock any leftover reserved funds (market remainder / price improvement).
		if taker.locked.Sign() > 0 {
			_ = e.st.ApplyPostings("unlock:"+o.ID, time.Now().Unix(), []store.Posting{{
				UserID: o.UserID, Asset: lockAsset,
				DeltaAvailable: taker.locked, DeltaLocked: taker.locked.Neg(),
				Reason: "order_unlock", Ref: o.ID,
			}})
			taker.locked = num.Zero
		}
		if o.Filled.Sign() > 0 {
			o.Status = models.StatusFilled
		} else {
			o.Status = models.StatusCanceled
		}
		o.UpdatedAt = time.Now().Unix()
		_ = e.st.UpdateOrder(o)
	}

	e.publishOrder(o)
	e.publishBalances(affected)
	e.publishDepth()
	return o, nil
}

// match walks the opposite side of the book, executing fills until the taker is
// exhausted or no more crossing liquidity remains.
func (e *Engine) match(taker *restingOrder, now int64, affected map[int64]bool) error {
	opp := e.book.asks
	if taker.side == models.Sell {
		opp = e.book.bids
	}
	for {
		if e.takerExhausted(taker) {
			return nil
		}
		lvl := opp.best()
		if lvl == nil {
			return nil
		}
		if !taker.isMarket() {
			if taker.side == models.Buy && lvl.price.Gt(taker.price) {
				return nil
			}
			if taker.side == models.Sell && lvl.price.Lt(taker.price) {
				return nil
			}
		}
		front := lvl.orders.Front()
		if front == nil {
			return nil
		}
		maker := front.Value.(*restingOrder)

		// Self-trade prevention: cancel the resting maker and continue.
		if maker.userID == taker.userID {
			e.cancelResting(maker, now)
			affected[maker.userID] = true
			continue
		}

		matchQty := e.matchQuantity(taker, maker, lvl.price)
		if matchQty.Sign() <= 0 {
			return nil // cannot afford even one step at this price
		}
		if err := e.executeFill(taker, maker, matchQty, lvl.price, now, affected); err != nil {
			return err
		}
		if maker.remaining.Sign() <= 0 {
			e.book.remove(maker)
		}
	}
}

func (e *Engine) takerExhausted(taker *restingOrder) bool {
	// For a market buy the order is sized by quote budget; otherwise by base
	// quantity. The "budget too small to afford a step at the current price"
	// case is handled by matchQuantity returning zero inside the match loop.
	if taker.isMarket() && taker.side == models.Buy {
		return taker.budget.Sign() <= 0
	}
	return taker.remaining.Sign() <= 0
}

// matchQuantity returns the base quantity to trade against maker at price,
// respecting the taker's remaining size or quote budget and the market step.
func (e *Engine) matchQuantity(taker, maker *restingOrder, price num.Dec) num.Dec {
	var want num.Dec
	if taker.isMarket() && taker.side == models.Buy {
		want = e.floorStep(taker.budget.Div(price)) // base affordable with remaining budget
	} else {
		want = taker.remaining
	}
	return num.Min(maker.remaining, want)
}

// executeFill performs one trade between taker and maker at price for matchQty
// base units, computes fees, builds settlement postings, and commits atomically.
func (e *Engine) executeFill(taker, maker *restingOrder, matchQty, price num.Dec, now int64, affected map[int64]bool) error {
	quoteCost := price.Mul(matchQty)

	var buyer, seller *restingOrder
	if taker.side == models.Buy {
		buyer, seller = taker, maker
	} else {
		buyer, seller = maker, taker
	}

	buyerRate := e.feeRate(buyer == maker)
	sellerRate := e.feeRate(seller == maker)
	buyerFee := matchQty.Mul(buyerRate)    // charged in base (asset received by buyer)
	sellerFee := quoteCost.Mul(sellerRate) // charged in quote (asset received by seller)

	// Determine how much of the buyer's locked quote to release and any price
	// improvement refund.
	var buyerLockedReduce, buyerRefund num.Dec
	if buyer.isMarket() {
		buyerLockedReduce = quoteCost
		buyer.budget = buyer.budget.Sub(quoteCost)
	} else {
		buyerLockedReduce = buyer.price.Mul(matchQty)
		buyerRefund = buyer.price.Sub(price).Mul(matchQty) // (limit - exec) * qty >= 0
	}
	buyer.locked = buyer.locked.Sub(buyerLockedReduce)
	seller.locked = seller.locked.Sub(matchQty)

	ref := taker.id
	postings := []store.Posting{
		// Buyer receives base (minus fee), releases locked quote, gets refund.
		{UserID: buyer.userID, Asset: e.mkt.Base, DeltaAvailable: matchQty.Sub(buyerFee), Reason: "trade_buy", Ref: ref},
		{UserID: buyer.userID, Asset: e.mkt.Quote, DeltaAvailable: buyerRefund, DeltaLocked: buyerLockedReduce.Neg(), Reason: "trade_buy", Ref: ref},
		// Seller receives quote (minus fee), releases locked base.
		{UserID: seller.userID, Asset: e.mkt.Base, DeltaLocked: matchQty.Neg(), Reason: "trade_sell", Ref: ref},
		{UserID: seller.userID, Asset: e.mkt.Quote, DeltaAvailable: quoteCost.Sub(sellerFee), Reason: "trade_sell", Ref: ref},
		// Exchange collects fees.
		{UserID: store.ExchangeUserID, Asset: e.mkt.Base, DeltaAvailable: buyerFee, Reason: "fee", Ref: ref},
		{UserID: store.ExchangeUserID, Asset: e.mkt.Quote, DeltaAvailable: sellerFee, Reason: "fee", Ref: ref},
	}

	// Update order aggregates.
	applyFillToOrder(buyer.mo, matchQty, quoteCost, buyerFee, now)
	applyFillToOrder(seller.mo, matchQty, quoteCost, sellerFee, now)
	maker.remaining = maker.remaining.Sub(matchQty)
	if !(taker.isMarket() && taker.side == models.Buy) {
		taker.remaining = taker.remaining.Sub(matchQty)
	}
	setOrderStatusAfterFill(maker)
	setOrderStatusAfterFill(taker)

	trade := &models.Trade{
		Market: e.mkt.Symbol, Price: price, Quantity: matchQty, QuoteQty: quoteCost,
		TakerSide: taker.side, BuyOrderID: buyer.id, SellOrderID: seller.id,
		BuyUserID: buyer.userID, SellUserID: seller.userID, CreatedAt: now,
	}
	txnID := "fill:" + uuid.NewString()
	if err := e.st.CommitFill(txnID, now, postings, trade, buyer.mo, seller.mo); err != nil {
		return fmt.Errorf("commit fill: %w", err)
	}

	affected[buyer.userID] = true
	affected[seller.userID] = true
	e.md.OnTrade(*trade)
	e.publishOrder(maker.mo) // taker order is published once after matching completes
	return nil
}

func (e *Engine) feeRate(isMaker bool) num.Dec {
	if isMaker {
		return e.mkt.MakerFee
	}
	return e.mkt.TakerFee
}

// applyFillToOrder accumulates a fill into the persisted order. FeePaid records
// the fee in the asset the order's owner received (base for buys, quote for sells).
func applyFillToOrder(o *models.Order, qty, quote, fee num.Dec, now int64) {
	o.Filled = o.Filled.Add(qty)
	o.QuoteFilled = o.QuoteFilled.Add(quote)
	o.FeePaid = o.FeePaid.Add(fee)
	o.UpdatedAt = now
}

// setOrderStatusAfterFill marks limit orders filled/partial by base remaining.
// Market order terminal status is decided after matching, so it is left alone.
func setOrderStatusAfterFill(ro *restingOrder) {
	if ro.isMarket() {
		return
	}
	if ro.mo.Filled.Gte(ro.mo.Quantity) {
		ro.mo.Status = models.StatusFilled
	} else {
		ro.mo.Status = models.StatusPartial
	}
}

// ---------- cancellation ----------

func (e *Engine) cancel(orderID string, userID int64) error {
	ro := e.book.get(orderID)
	if ro == nil {
		return ErrOrderNotFound
	}
	if ro.userID != userID {
		return ErrNotOwner
	}
	now := time.Now().Unix()
	e.cancelResting(ro, now)
	e.publishOrder(ro.mo)
	e.publishBalances(map[int64]bool{userID: true})
	e.publishDepth()
	return nil
}

// cancelResting removes a resting order from the book, releases its locked
// funds, marks it canceled, and persists.
func (e *Engine) cancelResting(ro *restingOrder, now int64) {
	lockAsset := e.mkt.Quote
	if ro.side == models.Sell {
		lockAsset = e.mkt.Base
	}
	if ro.locked.Sign() > 0 {
		_ = e.st.ApplyPostings("cancel:"+ro.id, now, []store.Posting{{
			UserID: ro.userID, Asset: lockAsset,
			DeltaAvailable: ro.locked, DeltaLocked: ro.locked.Neg(),
			Reason: "order_cancel", Ref: ro.id,
		}})
		ro.locked = num.Zero
	}
	e.book.remove(ro)
	ro.mo.Status = models.StatusCanceled
	ro.mo.UpdatedAt = now
	_ = e.st.UpdateOrder(ro.mo)
}

// ---------- validation ----------

// validateAndLockAmount checks market rules and returns the asset and amount to
// reserve for the order.
func (e *Engine) validateAndLockAmount(o *models.Order) (asset string, amount num.Dec, err error) {
	switch o.Type {
	case models.TypeLimit:
		if o.Price.Sign() <= 0 || o.Quantity.Sign() <= 0 {
			return "", num.Zero, fmt.Errorf("%w: price and quantity must be positive", ErrBadOrder)
		}
		if !isMultiple(o.Price, e.mkt.PriceTick) {
			return "", num.Zero, fmt.Errorf("%w: price must be a multiple of %s", ErrBadOrder, e.mkt.PriceTick)
		}
		if !isMultiple(o.Quantity, e.mkt.QtyStep) {
			return "", num.Zero, fmt.Errorf("%w: quantity must be a multiple of %s", ErrBadOrder, e.mkt.QtyStep)
		}
		notional := o.Price.Mul(o.Quantity)
		if notional.Lt(e.mkt.MinNotional) {
			return "", num.Zero, fmt.Errorf("%w: order value below minimum %s", ErrBadOrder, e.mkt.MinNotional)
		}
		if o.Side == models.Buy {
			return e.mkt.Quote, notional, nil
		}
		return e.mkt.Base, o.Quantity, nil

	case models.TypeMarket:
		if o.Side == models.Buy {
			// Quantity carries the quote budget.
			if o.Quantity.Lt(e.mkt.MinNotional) {
				return "", num.Zero, fmt.Errorf("%w: quote amount below minimum %s", ErrBadOrder, e.mkt.MinNotional)
			}
			return e.mkt.Quote, o.Quantity, nil
		}
		if o.Quantity.Sign() <= 0 {
			return "", num.Zero, fmt.Errorf("%w: quantity must be positive", ErrBadOrder)
		}
		if !isMultiple(o.Quantity, e.mkt.QtyStep) {
			return "", num.Zero, fmt.Errorf("%w: quantity must be a multiple of %s", ErrBadOrder, e.mkt.QtyStep)
		}
		return e.mkt.Base, o.Quantity, nil
	}
	return "", num.Zero, fmt.Errorf("%w: unknown order type", ErrBadOrder)
}

func isMultiple(v, step num.Dec) bool {
	if step.Raw() == 0 {
		return true
	}
	return v.Raw()%step.Raw() == 0
}

func (e *Engine) floorStep(v num.Dec) num.Dec {
	step := e.mkt.QtyStep.Raw()
	if step == 0 {
		return v
	}
	return num.FromRaw((v.Raw() / step) * step)
}

// ---------- publishing ----------

func (e *Engine) publishOrder(o *models.Order) {
	e.hub.Publish("orders:"+strconv.FormatInt(o.UserID, 10), o)
}

func (e *Engine) publishBalances(affected map[int64]bool) {
	for uid := range affected {
		if uid == store.ExchangeUserID {
			continue
		}
		bals, err := e.st.ListBalances(uid)
		if err != nil {
			continue
		}
		e.hub.Publish("balances:"+strconv.FormatInt(uid, 10), bals)
	}
}

func (e *Engine) publishDepth() {
	snap := e.book.snapshot(e.mkt.Symbol, depthLevels)
	e.hub.Publish("depth:"+e.mkt.Symbol, snap)
	e.md.SetBook(e.mkt.Symbol, e.book.bestBid(), e.book.bestAsk())
}

// Depth returns a snapshot for the REST endpoint.
func (e *Engine) Depth(limit int) DepthSnapshot {
	ch := make(chan DepthSnapshot, 1)
	e.cmds <- func() { ch <- e.book.snapshot(e.mkt.Symbol, limit) }
	return <-ch
}
