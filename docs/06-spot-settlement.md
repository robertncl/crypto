# Chapter 6 — Spot Settlement: Fills, Fees & Locking

> **Learning objectives**
> - Follow the money through an order's life: **reserve → match → settle**.
> - Understand **maker/taker fees** and where they go.
> - See **price-improvement refunds** and how a fill becomes six ledger postings.

Chapter 5 matched two orders. Now we move the money — using the ledger primitives
from Chapter 3.

---

## 6.1 Step 1 — Reserve funds when the order is placed

Before an order can enter the book, the engine **locks** the funds it might spend,
so you can't double-spend them. The amount depends on the order
(`validateAndLockAmount` in `engine.go`):

| Order | Locks | Amount |
|-------|-------|--------|
| Limit **buy** | quote (USDT) | `price × quantity` |
| Limit **sell** | base (BTC) | `quantity` |
| Market **buy** | quote (USDT) | the quote budget |
| Market **sell** | base (BTC) | `quantity` |

A buy reserves the cash it will pay; a sell reserves the coins it will deliver.
The reservation is the single posting you saw in Chapter 3 (`available → locked`,
reason `order_lock`). If you lack the funds, `ApplyPostings` returns
`ErrInsufficientFunds` and the order is rejected before it touches the book.

The engine also validates the order against the market's rules first:

- **Price tick** — price must be a multiple of `priceTick` (no sub-cent noise).
- **Quantity step** — size must be a multiple of `qtyStep`.
- **Min notional** — `price × qty` must clear a floor (Nebula seeds `5` USDT), so
  the book isn't clogged with dust orders.

---

## 6.2 Step 2 — Settle each fill

When two orders match (Chapter 5), `executeFill` computes how money moves. Let's
use a concrete trade and follow every cent.

> **Scenario.** Alice has a resting **sell** of 0.002 BTC at 95,000 (she's the
> **maker**). Bob sends a **buy** that matches it (he's the **taker**). The fee
> rate is 0.1% on each side (Nebula's seeded spot fee).

The value of the trade (the **notional**) is `95,000 × 0.002 = 190 USDT`.

Fees are charged on the asset each side *receives*:

- Bob receives **BTC**, so his fee is in BTC: `0.002 × 0.1% = 0.000002 BTC`.
- Alice receives **USDT**, so her fee is in USDT: `190 × 0.1% = 0.19 USDT`.

Now the settlement, expressed as ledger postings (this is the real list built in
`executeFill`):

| # | Account | Asset | available | locked | Why |
|---|---------|-------|-----------|--------|-----|
| 1 | Bob (buyer) | BTC | **+0.001998** | | receives base, minus his fee |
| 2 | Bob (buyer) | USDT | +refund | **−190** | releases the locked quote he's spending |
| 3 | Alice (seller) | BTC | | **−0.002** | releases the locked base she's delivering |
| 4 | Alice (seller) | USDT | **+189.81** | | receives quote, minus her fee |
| 5 | Exchange (id 0) | BTC | +0.000002 | | Bob's fee |
| 6 | Exchange (id 0) | USDT | +0.19 | | Alice's fee |

Check the conservation (Chapter 3's invariant):

- **BTC:** Alice releases 0.002 from locked → Bob gets 0.001998 + exchange gets
  0.000002 = 0.002. Balanced. ✅
- **USDT:** Bob pays 190 from locked → Alice gets 189.81 + exchange gets 0.19 =
  190. Balanced. ✅

All six postings commit in **one transaction** via `CommitFill`, which *also*
inserts the `trades` row and updates both orders' `filled` amounts. Either the
whole fill happens or none of it does.

This is precisely what the integration test
`backend/internal/engine/engine_integration_test.go` asserts — go read
`TestLimitCrossSettlement` and you'll see these exact numbers.

---

## 6.3 Price-improvement refunds (the "refund" in posting #2)

Remember from Chapter 5 that a buy fills at the *maker's* price, which can be
better (lower) than the buyer's limit. But the buyer **locked funds at their own
limit price**. So when they fill cheaper, the difference must be returned.

Example: you place a limit buy of 0.002 BTC at **96,000**, locking
`96,000 × 0.002 = 192` USDT. It matches a resting ask at **95,000**, costing only
`190`. The settlement:

- consumes 190 from your locked quote (you actually pay it), and
- **refunds** `(96,000 − 95,000) × 0.002 = 2` USDT from locked back to available.

In the code, the engine tracks each order's `locked` remainder precisely and
posts the refund as part of the buyer's USDT posting. When the order is fully done
(or cancelled), any still-locked remainder is released. That's why the model
stores a per-order locked amount, not just a global number — so refunds and
cancellations are exact to the satoshi.

---

## 6.4 Step 3 — Finish the order

After matching, the engine finalizes the taker order:

- **Limit order with quantity left** → it rests in the book as a maker (its
  remaining funds stay locked).
- **Filled, or a market order** → terminal. Any leftover locked funds (e.g. a
  market buy that didn't spend its whole budget) are unlocked back to available,
  and the order is marked `filled` (or `canceled` if nothing matched).

Then the engine publishes the updates — the trade, the new order-book depth, the
affected balances, and the order's new status — over the WebSocket hub (Chapter
7). That's how your screen updates the instant your order fills.

---

## 6.5 Where fees actually live

Notice fees didn't vanish — they moved to the **exchange account (id 0)**. Fees
are an exchange's primary revenue, and modeling them as a real account keeps the
books balanced (Chapter 3). You can literally query the exchange's accumulated
fees:

```sql
SELECT asset, available FROM balances WHERE user_id = 0;
```

**Maker vs. taker rates.** Nebula seeds both at 0.1% for simplicity, but the code
charges them independently — `e.feeRate(isMaker)` returns the maker rate for the
resting side and the taker rate for the incoming side. Set them differently in the
seed data and you've implemented a real fee schedule. Production exchanges go
further with **tiered fees** (cheaper as your 30-day volume grows) and referral
rebates — all just variations on "which rate applies to this fill".

---

## 6.6 Cancellation: the reverse of reservation

Cancelling a resting order is the mirror image of placing it. The engine removes
it from the book and **unlocks** its reserved funds — `locked → available`, reason
`order_cancel` — then marks it `canceled`. Because the reservation and the release
both go through `ApplyPostings`, the books never drift: whatever was locked is
exactly what's returned. (`TestCancelReleasesFunds` proves it.)

---

## Key takeaways

- An order's life is **reserve → match → settle**, all in ledger postings.
- Placement **locks** the funds you might spend (quote for buys, base for sells)
  after checking tick/step/min-notional.
- A fill is **six postings** (both sides × two assets + the exchange's fees) that
  commit atomically with the trade record via `CommitFill`, conserving every
  asset.
- Buyers get **price improvement**; over-locked funds are **refunded**.
- **Fees** flow to the exchange account (id 0); maker/taker rates are charged
  independently.

## Try it

- Reproduce §6.2 live: deposit BTC to one account, place the resting sell, cross
  it from a second account, then inspect both balances. Compare with the table.
- Place a buy-limit **above** the market, let it fill, and check that your USDT
  available reflects a **refund** — you paid less than your limit.

## Next

→ [Chapter 7 — Market Data & Real-Time WebSockets](07-market-data-websockets.md):
turning the trade stream into tickers, candles, and live pushes to the browser.
