# Chapter 5 — The Order Book & Matching Engine

> **Learning objectives**
> - Understand orders (**limit vs. market**, **buy vs. sell**) and the **order
>   book**.
> - Learn **price-time priority** and the **maker/taker** distinction.
> - Read Nebula's in-memory book and the matching loop that turns two orders into
>   a trade.

This chapter is about *mechanics* — who matches whom, at what price. The next
chapter handles the *money* (fees, locking, settlement). Keeping them separate is
how the code is organized too.

---

## 5.1 Orders: the four shapes

An **order** is an instruction to trade a quantity of the base asset. Two axes:

**Direction**
- **Buy (bid):** you want to acquire the base asset, paying quote.
- **Sell (ask):** you want to dispose of the base asset, receiving quote.

**Type**
- **Limit order:** "trade at this price *or better*." A buy-limit at 95,000 will
  pay 95,000 *or less*; a sell-limit at 95,000 will receive 95,000 *or more*. If
  no one matches it now, it **rests** in the book and waits.
- **Market order:** "trade *right now* at whatever the best available price is."
  It never rests; it executes immediately against resting orders, sweeping as
  much of the book as needed. The risk is **slippage** — sweeping into worse
  prices if you ask for more than is available near the top.

```
                 price
                  ▲
       asks       │  95,300  ◄─ a sell-limit resting here
     (sellers)    │  95,200
   ───────────────│──────────  ◄─ the spread
        bids       │  95,100  ◄─ a buy-limit resting here
     (buyers)     │  95,000
                  │
```

The **best bid** (highest buy) and **best ask** (lowest sell) sit either side of
the **spread**. A market buy lifts the best ask; a market sell hits the best bid.

In Nebula these are `models.Side` (`buy`/`sell`) and `models.OrderType`
(`limit`/`market`) — see `backend/internal/models/models.go`.

---

## 5.2 Makers and takers

This distinction drives fees (Chapter 6) and is worth internalizing:

- A **maker** *adds* liquidity: their order rests in the book, waiting. They
  "make" the market.
- A **taker** *removes* liquidity: their order matches an existing resting order
  immediately. They "take" from the book.

A limit order can be either — if it crosses the spread on arrival it's a taker; if
it rests, it's a maker. A market order is *always* a taker. Exchanges usually
charge takers more (they consume liquidity) and may even pay makers (they provide
it), to encourage a deep, liquid book.

---

## 5.3 The order book as a data structure

What does the engine actually store? For each side it needs to answer two
questions fast: *"what's the best price?"* and *"who's first in line at this
price?"*. That points to:

- A map from **price → price level**.
- Each **price level** is a **FIFO queue** of orders (first come, first served).
- A **sorted list of prices** so we can find the best instantly.

That's exactly `backend/internal/engine/book.go`:

```go
type priceLevel struct {
    price  num.Dec
    orders *list.List   // FIFO queue of *restingOrder (container/list)
}

type side struct {
    isBid  bool
    levels map[int64]*priceLevel  // price → level
    prices []int64                // raw prices, kept sorted ascending
}
```

- `best()` returns the highest price for bids (last in the sorted slice) or the
  lowest for asks (first) — O(1).
- `add()` appends an order to the back of its price level (so earlier orders are
  ahead) and inserts the price into the sorted slice if new.
- `remove()` (used on fill or cancel) unlinks the order; if its level is now
  empty, the price is dropped.

A whole `book` is just `{ bids, asks, index }`, where `index` maps order id →
order for O(1) cancellation lookup.

> Nebula uses a sorted slice + map for clarity. A high-throughput engine would use
> a more specialized structure (e.g. a price-indexed array or a balanced tree),
> but the *semantics* — price levels of FIFO queues — are universal.

---

## 5.4 Price-time priority: the fairness rule

When multiple orders could match, which goes first? The near-universal rule is
**price-time priority**:

1. **Better price wins.** A buyer offering 95,100 is served before one offering
   95,000.
2. **At the same price, earlier wins.** First come, first filled (that's the FIFO
   queue inside each price level).

This rule is *the* definition of fairness in a continuous market, and it's why the
book is "sorted slice of prices, FIFO within each price". The engine simply always
takes the **front order of the best level**.

---

## 5.5 The matching loop

Now the payoff. When an order arrives, the engine walks the **opposite** side of
the book, best price first, matching until the incoming order is exhausted or no
more prices cross. Here's the shape of `match()` in
`backend/internal/engine/engine.go` (lightly abridged):

```go
opp := e.book.asks            // a buy taker matches against asks
if taker.side == models.Sell { opp = e.book.bids }

for {
    if taker.remaining.Sign() <= 0 { return }   // taker fully filled
    lvl := opp.best()
    if lvl == nil { return }                     // book empty on this side

    // For a LIMIT taker, stop once prices no longer cross:
    if !taker.isMarket() {
        if taker.side == Buy  && lvl.price.Gt(taker.price) { return }
        if taker.side == Sell && lvl.price.Lt(taker.price) { return }
    }

    maker := lvl.orders.Front().Value.(*restingOrder)  // time priority
    if maker.userID == taker.userID {                  // self-trade prevention
        e.cancelResting(maker, now); continue
    }

    matchQty := num.Min(maker.remaining, taker.remaining)
    e.executeFill(taker, maker, matchQty, lvl.price, now, affected) // Ch.6
    if maker.remaining.Sign() <= 0 { e.book.remove(maker) }
}
```

Read it slowly — every line encodes a rule:

- **`opp.best()` then `Front()`** = price-time priority, exactly as defined above.
- **The cross checks** are what makes a *limit* order a limit order: a buy-limit
  refuses to pay more than its price; once the best ask is above it, it stops (and
  the remainder rests). A market order skips these checks — it crosses anything.
- **The trade always executes at the maker's price** (`lvl.price`), not the
  taker's. This is **price improvement**: a buy-limit at 95,100 that matches a
  resting ask at 95,000 pays only 95,000. The taker can do better than their
  limit, never worse.
- **Partial fills** fall out naturally: `matchQty` is the smaller of the two
  remaining sizes; whoever has leftover continues (the maker stays in the book,
  or the taker matches the next level).

After matching, if the taker is a *limit* order with quantity left over, it
becomes a maker — it's added to the book to rest:

```go
rest := o.Type == models.TypeLimit && taker.remaining.Sign() > 0
if rest { e.book.add(taker) }     // the taker is now a resting maker
```

---

## 5.6 Self-trade prevention

What if your own buy would match your own resting sell? That's a **self-trade** —
pointless (you trade with yourself) and abusable (fake volume). Nebula prevents it
with the simplest correct policy: when the maker is the same user as the taker,
**cancel the resting maker** and continue matching against the next order
(`cancelResting` above). The taker never trades with itself.

---

## 5.7 Market buys are sized in *quote*

One subtlety unique to market **buys**. If you place a limit buy you specify a
quantity of base (0.1 BTC). But for a market buy you often don't know how much BTC
your money will get (the price moves as you sweep). So exchanges let you specify a
market buy by **how much quote you want to spend** — "buy 1,000 USDT of BTC".

Nebula does exactly this: for a market buy, the order's `Quantity` field carries
the **quote budget**, and matching converts budget→base at each price level until
the budget runs out. (Market sells and all limit orders are sized in base, as
usual.) This is why you'll see a `budget` field on the in-memory order and special
handling for `TypeMarket && Buy`.

---

## 5.8 Rebuilding the book after a restart

The book lives in memory, but orders are persisted in the `orders` table. On
startup, the engine **rebuilds** each book from the open orders so nothing is lost
(`Manager.Init` → `rebuild` in `engine/manager.go`): it loads the working orders
oldest-first (preserving time priority) and re-inserts them — *without* re-locking
funds, since those funds are already locked in the database from when the orders
were first placed.

---

## Key takeaways

- An order is **{side, type}** — buy/sell × limit/market. Limit orders rest;
  market orders sweep immediately and risk **slippage**.
- The **order book** is price levels (a map) of FIFO queues, with a sorted price
  list for the best price — giving **price-time priority**.
- **Makers** add liquidity (rest), **takers** remove it (cross). A limit order can
  be either.
- Matching always executes at the **maker's price** (price improvement for the
  taker), and falls out as a tidy loop over the best level's front order.
- **Self-trades** are prevented by cancelling the offending resting order.

## Try it

- Run the exchange. On a market with the bot quoting, place a **limit buy far
  below** the price — watch it rest in the book (you're a maker). Then place a
  **market buy** — watch it execute instantly against the asks (you're a taker).
- Place a limit buy **above** the best ask. Notice it fills at the *ask's* price,
  not your higher limit — that's price improvement in action.

## Next

→ [Chapter 6 — Spot Settlement: Fills, Fees & Locking](06-spot-settlement.md):
now that two orders have matched, what exactly happens to the money?
