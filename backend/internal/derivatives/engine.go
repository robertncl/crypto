// Package derivatives implements a linear USDT-margined perpetual futures
// engine: a price-time-priority matching engine whose fills open/reduce/flip
// netted positions, with isolated margin sourced from the user's locked USDT
// balance, realized/unrealized PnL settled against an insurance fund, periodic
// funding, and mark-price liquidation. Each market runs a single goroutine, so
// the order book and in-memory positions need no locks.
package derivatives

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
	ErrReduceOnly    = errors.New("reduce-only order would open or increase a position")
	ErrNoLiquidity   = errors.New("no liquidity to estimate margin for market order")
)

const settle = "USDT"

// Engine matches and settles one perpetual market.
type Engine struct {
	mkt       models.PerpMarket
	book      *book
	positions map[int64]*models.Position // userID -> open position (size>0)
	st        *store.Store
	md        *market.Service
	hub       *ws.Hub
	cmds      chan func()
}

func newEngine(mkt models.PerpMarket, st *store.Store, md *market.Service, hub *ws.Hub) *Engine {
	return &Engine{
		mkt: mkt, book: newBook(), positions: map[int64]*models.Position{},
		st: st, md: md, hub: hub, cmds: make(chan func(), 256),
	}
}

func (e *Engine) start() { go func() { for f := range e.cmds { f() } }() }

// Place submits a perp order and blocks until matched/rested.
func (e *Engine) Place(o *models.PerpOrder) (*models.PerpOrder, error) {
	type res struct {
		o   *models.PerpOrder
		err error
	}
	ch := make(chan res, 1)
	e.cmds <- func() { out, err := e.place(o); ch <- res{out, err} }
	r := <-ch
	return r.o, r.err
}

func (e *Engine) Cancel(orderID string, userID int64) error {
	ch := make(chan error, 1)
	e.cmds <- func() { ch <- e.cancel(orderID, userID) }
	return <-ch
}

// ApplyFunding transfers funding between longs and shorts (via the insurance
// fund) at the given rate, using mark for notional.
func (e *Engine) ApplyFunding(rate, mark num.Dec) {
	done := make(chan struct{})
	e.cmds <- func() { e.applyFunding(rate, mark); close(done) }
	<-done
}

// CheckLiquidations force-closes positions whose margin is exhausted at mark.
func (e *Engine) CheckLiquidations(mark num.Dec) {
	done := make(chan struct{})
	e.cmds <- func() { e.checkLiquidations(mark); close(done) }
	<-done
}

// Depth returns a book snapshot for the REST endpoint.
func (e *Engine) Depth(limit int) DepthSnapshot {
	ch := make(chan DepthSnapshot, 1)
	e.cmds <- func() { ch <- e.book.snapshot(e.mkt.Symbol, limit) }
	return <-ch
}

// ---------- placement ----------

func (e *Engine) place(o *models.PerpOrder) (*models.PerpOrder, error) {
	if e.mkt.Status != "trading" {
		return nil, ErrMarketHalted
	}
	now := time.Now().Unix()
	o.ID = uuid.NewString()
	o.Market = e.mkt.Symbol
	o.CreatedAt, o.UpdatedAt = now, now
	o.Filled, o.AvgPrice = num.Zero, num.Zero
	o.Status = models.StatusOpen
	o.Leverage = e.clampLeverage(o.Leverage)

	if o.Quantity.Sign() <= 0 || !isMultiple(o.Quantity, e.mkt.QtyStep) {
		return nil, fmt.Errorf("%w: quantity must be a positive multiple of %s", ErrBadOrder, e.mkt.QtyStep)
	}
	if o.Type == models.TypeLimit {
		if o.Price.Sign() <= 0 || !isMultiple(o.Price, e.mkt.PriceTick) {
			return nil, fmt.Errorf("%w: price must be a positive multiple of %s", ErrBadOrder, e.mkt.PriceTick)
		}
	}

	pos := e.getPos(o.UserID)
	// reduce-only must oppose an existing position.
	if o.ReduceOnly {
		if pos.Side == models.Flat || sameDir(o.Side, pos.Side) {
			return nil, ErrReduceOnly
		}
	}

	// Estimate fill price for margin sizing and min-notional checks.
	estPrice := o.Price
	if o.Type == models.TypeMarket {
		if o.Side == models.Buy {
			estPrice = e.book.bestAsk()
		} else {
			estPrice = e.book.bestBid()
		}
		if estPrice.Sign() <= 0 {
			return nil, ErrNoLiquidity
		}
	}
	if estPrice.Mul(o.Quantity).Lt(e.mkt.MinNotional) {
		return nil, fmt.Errorf("%w: notional below minimum %s", ErrBadOrder, e.mkt.MinNotional)
	}

	// Pre-lock isolated margin for orders that may open/increase. Reduce-only
	// orders free margin instead, so they lock nothing.
	lockMargin := num.Zero
	if !o.ReduceOnly {
		lockMargin = estPrice.Mul(o.Quantity).Div(num.FromInt(int64(o.Leverage)))
		if err := e.st.ApplyPostings("perplock:"+o.ID, now, []store.Posting{{
			UserID: o.UserID, Asset: settle, DeltaAvailable: lockMargin.Neg(), DeltaLocked: lockMargin,
			Reason: "perp_order_lock", Ref: o.ID,
		}}); err != nil {
			if errors.Is(err, store.ErrInsufficientFunds) {
				return nil, store.ErrInsufficientFunds
			}
			return nil, err
		}
	}

	if err := e.st.InsertPerpOrder(o); err != nil {
		return nil, err
	}

	taker := &restingOrder{
		id: o.ID, userID: o.UserID, side: o.Side, typ: o.Type, price: o.Price,
		remaining: o.Quantity, lockedMargin: lockMargin, leverage: o.Leverage,
		reduceOnly: o.ReduceOnly, createdAt: now, mo: o,
	}

	affected := map[int64]bool{o.UserID: true}
	if err := e.match(taker, now, affected); err != nil {
		return nil, err
	}

	rest := o.Type == models.TypeLimit && taker.remaining.Sign() > 0 && !e.reduceExhausted(taker)
	if rest {
		o.Status = statusFor(o)
		o.UpdatedAt = time.Now().Unix()
		e.book.add(taker)
		_ = e.st.UpdatePerpOrder(o)
	} else {
		e.unlockLeftover(taker, time.Now().Unix())
		if o.Filled.Sign() > 0 {
			o.Status = models.StatusFilled
		} else {
			o.Status = models.StatusCanceled
		}
		o.UpdatedAt = time.Now().Unix()
		_ = e.st.UpdatePerpOrder(o)
	}

	e.publishOrder(o)
	e.publishPositions(affected)
	e.publishBalances(affected)
	e.publishDepth()
	return o, nil
}

// reduceExhausted reports whether a reduce-only order can no longer reduce
// (position already flat in the relevant direction).
func (e *Engine) reduceExhausted(ro *restingOrder) bool {
	if !ro.reduceOnly {
		return false
	}
	pos := e.getPos(ro.userID)
	return pos.Side == models.Flat || sameDir(ro.side, pos.Side)
}

func (e *Engine) match(taker *restingOrder, now int64, affected map[int64]bool) error {
	opp := e.book.asks
	if taker.side == models.Sell {
		opp = e.book.bids
	}
	for {
		if taker.remaining.Sign() <= 0 {
			return nil
		}
		// Cap reduce-only fills at the current position size so they cannot flip.
		reduceCap := num.Zero
		if taker.reduceOnly {
			pos := e.getPos(taker.userID)
			if pos.Side == models.Flat || sameDir(taker.side, pos.Side) {
				return nil
			}
			reduceCap = pos.Size
			if reduceCap.Sign() <= 0 {
				return nil
			}
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
		if maker.userID == taker.userID {
			e.cancelResting(maker, now) // self-trade prevention
			affected[maker.userID] = true
			continue
		}
		matchQty := num.Min(maker.remaining, taker.remaining)
		if taker.reduceOnly {
			matchQty = num.Min(matchQty, reduceCap)
		}
		matchQty = e.floorStep(matchQty)
		if matchQty.Sign() <= 0 {
			return nil
		}
		if err := e.executeFill(taker, maker, matchQty, lvl.price, now, affected); err != nil {
			return err
		}
		if maker.remaining.Sign() <= 0 {
			e.book.remove(maker)
		}
	}
}

func (e *Engine) executeFill(taker, maker *restingOrder, qty, price num.Dec, now int64, affected map[int64]bool) error {
	quote := price.Mul(qty)
	takerPos := e.copyPos(taker.userID)
	makerPos := e.copyPos(maker.userID)

	var postings []store.Posting
	postings = append(postings, e.settle(taker, takerPos, price, qty, e.mkt.TakerFee, now)...)
	postings = append(postings, e.settle(maker, makerPos, price, qty, e.mkt.MakerFee, now)...)

	// Trade tape (buyer/seller by order side).
	buyOrder, sellOrder := taker.mo, maker.mo
	if taker.side == models.Sell {
		buyOrder, sellOrder = maker.mo, taker.mo
	}
	trade := &models.Trade{
		Market: e.mkt.Symbol, Price: price, Quantity: qty, QuoteQty: quote,
		TakerSide: taker.side, BuyOrderID: buyOrder.ID, SellOrderID: sellOrder.ID,
		BuyUserID: buyOrder.UserID, SellUserID: sellOrder.UserID, CreatedAt: now,
	}

	txnID := "perpfill:" + uuid.NewString()
	if err := e.st.CommitPerp(txnID, now, postings, trade, []*models.Position{takerPos, makerPos}, []*models.PerpOrder{taker.mo, maker.mo}); err != nil {
		return fmt.Errorf("commit perp fill: %w", err)
	}

	e.storePos(takerPos)
	e.storePos(makerPos)
	maker.remaining = maker.remaining.Sub(qty)
	taker.remaining = taker.remaining.Sub(qty)

	affected[taker.userID] = true
	affected[maker.userID] = true
	e.md.OnTrade(*trade)
	e.publishOrder(maker.mo)
	e.publishPosition(makerPos)
	return nil
}

// settle applies one fill to one side's position, returning the balance postings
// (margin lock/release, PnL vs insurance, fee) and mutating pos and the order.
func (e *Engine) settle(ro *restingOrder, pos *models.Position, price, qty, feeRate num.Dec, now int64) []store.Posting {
	notional := price.Mul(qty)
	fee := notional.Mul(feeRate)
	ref := ro.id
	ps := []store.Posting{
		{UserID: ro.userID, Asset: settle, DeltaAvailable: fee.Neg(), Reason: "perp_fee", Ref: ref},
		{UserID: store.ExchangeUserID, Asset: settle, DeltaAvailable: fee, Reason: "perp_fee", Ref: ref},
	}

	dirLong := ro.side == models.Buy
	opening := pos.Side == models.Flat || sameDir(ro.side, pos.Side)

	if opening {
		lev := ro.leverage
		if pos.Side != models.Flat {
			lev = pos.Leverage
		}
		need := notional.Div(num.FromInt(int64(lev)))
		ps = append(ps, e.reconcileLock(ro, need)...)
		if pos.Side == models.Flat {
			pos.Side = sideFor(dirLong)
			pos.EntryPrice = price
			pos.Size = qty
			pos.Leverage = lev
		} else {
			newSize := pos.Size.Add(qty)
			pos.EntryPrice = pos.Size.Mul(pos.EntryPrice).Add(qty.Mul(price)).Div(newSize)
			pos.Size = newSize
		}
		pos.Margin = pos.Margin.Add(need)
	} else {
		closeQty := num.Min(qty, pos.Size)
		var pnl num.Dec
		if pos.Side == models.Long {
			pnl = price.Sub(pos.EntryPrice).Mul(closeQty)
		} else {
			pnl = pos.EntryPrice.Sub(price).Mul(closeQty)
		}
		freed := pos.Margin.Mul(closeQty).Div(pos.Size)
		pnl = num.Max(pnl, freed.Neg()) // cap loss at the released margin (isolated)
		pos.Margin = pos.Margin.Sub(freed)
		pos.Size = pos.Size.Sub(closeQty)
		pos.RealizedPnL = pos.RealizedPnL.Add(pnl)
		ps = append(ps,
			store.Posting{UserID: ro.userID, Asset: settle, DeltaAvailable: freed, DeltaLocked: freed.Neg(), Reason: "perp_margin_release", Ref: ref},
			store.Posting{UserID: ro.userID, Asset: settle, DeltaAvailable: pnl, Reason: "perp_pnl", Ref: ref},
			store.Posting{UserID: store.InsuranceFundID, Asset: settle, DeltaAvailable: pnl.Neg(), Reason: "perp_pnl", Ref: ref},
		)
		if pos.Size.Sign() == 0 {
			if pos.Margin.Sign() > 0 { // free rounding residual
				ps = append(ps, store.Posting{UserID: ro.userID, Asset: settle, DeltaAvailable: pos.Margin, DeltaLocked: pos.Margin.Neg(), Reason: "perp_margin_release", Ref: ref})
				pos.Margin = num.Zero
			}
			pos.Side = models.Flat
			pos.EntryPrice = num.Zero
		}
		if rem := qty.Sub(closeQty); rem.Sign() > 0 { // flip remainder opens the other side
			need := price.Mul(rem).Div(num.FromInt(int64(ro.leverage)))
			ps = append(ps, e.reconcileLock(ro, need)...)
			pos.Side = sideFor(dirLong)
			pos.EntryPrice = price
			pos.Size = rem
			pos.Margin = need
			pos.Leverage = ro.leverage
		}
	}

	// Update the order's running fill + average price.
	newFilled := ro.mo.Filled.Add(qty)
	ro.mo.AvgPrice = ro.mo.Filled.Mul(ro.mo.AvgPrice).Add(qty.Mul(price)).Div(newFilled)
	ro.mo.Filled = newFilled
	ro.mo.Status = statusFor(ro.mo)
	ro.mo.UpdatedAt = now
	pos.UpdatedAt = now
	return ps
}

// reconcileLock attributes `need` margin to a position: it consumes the order's
// pre-locked margin, topping up from available if short or leaving the surplus
// locked (refunded when the order finishes).
func (e *Engine) reconcileLock(ro *restingOrder, need num.Dec) []store.Posting {
	if ro.lockedMargin.Gte(need) {
		ro.lockedMargin = ro.lockedMargin.Sub(need)
		return nil // already in locked balance; just re-attributed to the position
	}
	deficit := need.Sub(ro.lockedMargin)
	ro.lockedMargin = num.Zero
	return []store.Posting{{
		UserID: ro.userID, Asset: settle, DeltaAvailable: deficit.Neg(), DeltaLocked: deficit,
		Reason: "perp_margin_lock", Ref: ro.id,
	}}
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
	e.cancelResting(ro, time.Now().Unix())
	e.publishOrder(ro.mo)
	e.publishBalances(map[int64]bool{userID: true})
	e.publishDepth()
	return nil
}

func (e *Engine) cancelResting(ro *restingOrder, now int64) {
	e.unlockLeftover(ro, now)
	e.book.remove(ro)
	ro.mo.Status = models.StatusCanceled
	ro.mo.UpdatedAt = now
	_ = e.st.UpdatePerpOrder(ro.mo)
}

func (e *Engine) unlockLeftover(ro *restingOrder, now int64) {
	if ro.lockedMargin.Sign() > 0 {
		_ = e.st.ApplyPostings("perpunlock:"+ro.id, now, []store.Posting{{
			UserID: ro.userID, Asset: settle, DeltaAvailable: ro.lockedMargin, DeltaLocked: ro.lockedMargin.Neg(),
			Reason: "perp_order_unlock", Ref: ro.id,
		}})
		ro.lockedMargin = num.Zero
	}
}

// ---------- funding & liquidation ----------

func (e *Engine) applyFunding(rate, mark num.Dec) {
	if mark.Sign() <= 0 || rate.IsZero() {
		return
	}
	now := time.Now().Unix()
	for _, pos := range e.snapshotPositions() {
		payment := pos.Size.Mul(mark).Mul(rate) // longs pay shorts when rate > 0
		userDelta := payment.Neg()
		if pos.Side == models.Short {
			userDelta = payment
		}
		p := *pos
		p.FundingPaid = p.FundingPaid.Add(userDelta.Neg())
		p.UpdatedAt = now
		err := e.st.CommitPerp("funding:"+e.mkt.Symbol+":"+strconv.FormatInt(pos.UserID, 10)+":"+strconv.FormatInt(now, 10), now,
			[]store.Posting{
				{UserID: pos.UserID, Asset: settle, DeltaAvailable: userDelta, Reason: "funding", Ref: e.mkt.Symbol},
				{UserID: store.InsuranceFundID, Asset: settle, DeltaAvailable: userDelta.Neg(), Reason: "funding", Ref: e.mkt.Symbol},
			}, nil, []*models.Position{&p}, nil)
		if err != nil {
			continue // best effort: skip if the payer lacks free balance
		}
		e.storePos(&p)
		e.publishPosition(&p)
		e.publishBalances(map[int64]bool{pos.UserID: true})
	}
}

func (e *Engine) checkLiquidations(mark num.Dec) {
	if mark.Sign() <= 0 {
		return
	}
	now := time.Now().Unix()
	for _, pos := range e.snapshotPositions() {
		liq := e.liqPrice(pos)
		breached := (pos.Side == models.Long && mark.Lte(liq)) || (pos.Side == models.Short && mark.Gte(liq))
		if !breached {
			continue
		}
		var pnl num.Dec
		if pos.Side == models.Long {
			pnl = mark.Sub(pos.EntryPrice).Mul(pos.Size)
		} else {
			pnl = pos.EntryPrice.Sub(mark).Mul(pos.Size)
		}
		pnl = num.Max(pnl, pos.Margin.Neg()) // user cannot lose more than isolated margin
		p := *pos
		p.RealizedPnL = p.RealizedPnL.Add(pnl)
		p.Side, p.Size, p.EntryPrice = models.Flat, num.Zero, num.Zero
		margin := pos.Margin
		p.Margin = num.Zero
		p.UpdatedAt = now
		err := e.st.CommitPerp("liq:"+pos.Market+":"+strconv.FormatInt(pos.UserID, 10)+":"+strconv.FormatInt(now, 10), now,
			[]store.Posting{
				{UserID: pos.UserID, Asset: settle, DeltaAvailable: margin.Add(pnl), DeltaLocked: margin.Neg(), Reason: "liquidation", Ref: pos.Market},
				{UserID: store.InsuranceFundID, Asset: settle, DeltaAvailable: pnl.Neg(), Reason: "liquidation", Ref: pos.Market},
			}, nil, []*models.Position{&p}, nil)
		if err != nil {
			continue
		}
		e.storePos(&p)
		e.publishPosition(&p)
		e.publishBalances(map[int64]bool{pos.UserID: true})
	}
}

// liqPrice is the mark price at which a position's margin is exhausted down to
// the maintenance requirement.
func (e *Engine) liqPrice(pos *models.Position) num.Dec {
	if pos.Size.Sign() == 0 {
		return num.Zero
	}
	marginPer := pos.Margin.Div(pos.Size)
	maint := pos.EntryPrice.Mul(e.mkt.MMR)
	if pos.Side == models.Long {
		return pos.EntryPrice.Sub(marginPer).Add(maint)
	}
	return pos.EntryPrice.Add(marginPer).Sub(maint)
}

// ---------- position map helpers ----------

func (e *Engine) getPos(userID int64) *models.Position {
	if p, ok := e.positions[userID]; ok {
		return p
	}
	return &models.Position{UserID: userID, Market: e.mkt.Symbol, Side: models.Flat}
}

func (e *Engine) copyPos(userID int64) *models.Position {
	p := *e.getPos(userID)
	return &p
}

func (e *Engine) storePos(p *models.Position) {
	if p.Size.Sign() <= 0 {
		delete(e.positions, p.UserID)
		return
	}
	cp := *p
	e.positions[p.UserID] = &cp
}

func (e *Engine) snapshotPositions() []*models.Position {
	out := make([]*models.Position, 0, len(e.positions))
	for _, p := range e.positions {
		out = append(out, p)
	}
	return out
}

// ---------- validation helpers ----------

func (e *Engine) clampLeverage(l int) int {
	if l <= 0 {
		l = 10
	}
	if l > e.mkt.MaxLeverage {
		l = e.mkt.MaxLeverage
	}
	if l < 1 {
		l = 1
	}
	return l
}

func (e *Engine) floorStep(v num.Dec) num.Dec {
	step := e.mkt.QtyStep.Raw()
	if step == 0 {
		return v
	}
	return num.FromRaw((v.Raw() / step) * step)
}

func isMultiple(v, step num.Dec) bool {
	if step.Raw() == 0 {
		return true
	}
	return v.Raw()%step.Raw() == 0
}

func sameDir(s models.Side, ps models.PositionSide) bool {
	return (s == models.Buy && ps == models.Long) || (s == models.Sell && ps == models.Short)
}

func sideFor(long bool) models.PositionSide {
	if long {
		return models.Long
	}
	return models.Short
}

func statusFor(o *models.PerpOrder) models.OrderStatus {
	if o.Filled.Gte(o.Quantity) {
		return models.StatusFilled
	}
	if o.Filled.Sign() > 0 {
		return models.StatusPartial
	}
	return models.StatusOpen
}

// ---------- publishing ----------

func (e *Engine) publishOrder(o *models.PerpOrder) {
	e.hub.Publish("perpOrders:"+strconv.FormatInt(o.UserID, 10), o)
}

func (e *Engine) publishPositions(affected map[int64]bool) {
	for uid := range affected {
		if uid < 0 {
			continue
		}
		e.publishPosition(e.copyPos(uid))
	}
}

func (e *Engine) publishPosition(p *models.Position) {
	if p.UserID < 0 {
		return
	}
	mark := e.markPrice()
	e.hub.Publish("positions:"+strconv.FormatInt(p.UserID, 10), e.enrich(*p, mark))
}

func (e *Engine) publishBalances(affected map[int64]bool) {
	for uid := range affected {
		if uid < 0 {
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
	e.hub.Publish("depth:"+e.mkt.Symbol, e.book.snapshot(e.mkt.Symbol, depthLevels))
	e.md.SetBook(e.mkt.Symbol, e.book.bestBid(), e.book.bestAsk())
}

func (e *Engine) markPrice() num.Dec {
	if t, ok := e.md.GetTicker(e.mkt.Symbol); ok && t.Last.Sign() > 0 {
		return t.Last
	}
	return num.Zero
}

// enrich fills the computed (non-persisted) fields of a position for API/WS use.
func (e *Engine) enrich(p models.Position, mark num.Dec) models.Position {
	if p.Side == models.Flat || p.Size.Sign() == 0 {
		return p
	}
	if mark.Sign() <= 0 {
		mark = p.EntryPrice
	}
	p.MarkPrice = mark
	p.Notional = mark.Mul(p.Size)
	if p.Side == models.Long {
		p.UnrealizedPnL = mark.Sub(p.EntryPrice).Mul(p.Size)
	} else {
		p.UnrealizedPnL = p.EntryPrice.Sub(mark).Mul(p.Size)
	}
	p.LiqPrice = e.liqPrice(&p)
	equity := p.Margin.Add(p.UnrealizedPnL)
	if p.Notional.Sign() > 0 {
		p.MarginRatio = equity.Div(p.Notional)
	}
	return p
}

// Enrich exposes position enrichment for the API layer (mark-priced view).
func (e *Engine) Enrich(p models.Position, mark num.Dec) models.Position { return e.enrich(p, mark) }

// Market returns the engine's market definition.
func (e *Engine) Market() models.PerpMarket { return e.mkt }
