# Chapter 1 — Architecture & a Crypto Primer

> **Learning objectives**
> - Understand what a crypto exchange does and the jobs it must perform.
> - Learn the core vocabulary of trading (markets, orders, the book, makers/takers).
> - See how Nebula is structured and how a single user action flows through it.

---

## 1.1 What is an exchange, really?

Strip away the branding and an exchange is a **marketplace plus a bank**:

1. **The marketplace** matches people who want to buy with people who want to
   sell, and records the trades. This is the **matching engine**.
2. **The bank** holds everyone's money, moves it when trades happen, and lets
   people deposit and withdraw. This is **custody** and the **ledger**.

Everything else — charts, the website, the mobile app, market data feeds — is in
service of those two jobs.

A crypto exchange is special in two ways:

- The "money" is **crypto assets** (Bitcoin, Ether, …) and **stablecoins**
  (USDT — a token pegged ~1:1 to the US dollar). Holding them means controlling
  cryptographic keys, which is its own discipline (custody).
- It often runs **24/7** with no market close, and supports **derivatives** like
  perpetual futures (Chapters 9–10).

### The vocabulary you need right now

- **Asset / coin / token:** a thing you can hold, e.g. `BTC`, `ETH`, `USDT`.
- **Market / trading pair:** two assets you can swap, written `BASE-QUOTE`, e.g.
  **`BTC-USDT`**. The **base** is what you're buying/selling (BTC); the **quote**
  is what you price and pay in (USDT). "BTC is at 95,000" means 95,000 USDT buys
  1 BTC.
- **Spot trading:** swapping assets for immediate ownership — you buy 0.1 BTC and
  you *own* 0.1 BTC. (Contrast with derivatives, where you trade a *contract*
  about the price.)
- **Order:** an instruction to trade — "buy 0.1 BTC at 95,000".
- **Order book:** the live list of all unmatched buy and sell orders for a market.
- **Bid / ask:** the best price someone will **buy** at (bid) and **sell** at
  (ask). The gap between them is the **spread**.

We'll go deep on each. For now, hold the mental model: *people post orders, the
engine matches them into trades, and the ledger moves money accordingly.*

---

## 1.2 The jobs an exchange must do

| Job | Plain-English question it answers | Nebula component |
|-----|-----------------------------------|------------------|
| Identity | "Who are you, and are you allowed to do this?" | `internal/auth` |
| Custody / ledger | "How much do you have, and where did it go?" | `internal/store` (ledger), `internal/wallet` |
| Matching | "Whose buy meets whose sell, and at what price?" | `internal/engine` (spot), `internal/derivatives` (perps) |
| Market data | "What's the price, the chart, the order book?" | `internal/market` |
| Realtime | "Push me updates the instant they happen." | `internal/ws` |
| Risk (derivatives) | "Can this leveraged trader still cover their losses?" | `internal/derivatives` |
| Client | "Let a human see and do all of the above." | `web/` (React) |

A useful way to read the rest of this course: **each chapter is one row of this
table.**

---

## 1.3 How Nebula is laid out

```
backend/
  cmd/server/main.go        ← wires everything together and starts the server
  internal/
    num/        fixed-point decimal type — money math without floats   (Ch.2)
    store/      database access + the double-entry ledger primitives    (Ch.3)
    auth/       JWT sessions, password hashing, middleware              (Ch.4)
    engine/     spot order book + matching engine                       (Ch.5–6)
    market/     tickers + candles, computed from the trade stream       (Ch.7)
    ws/         WebSocket hub: topic subscribe + broadcast              (Ch.7)
    wallet/     simulated deposits/withdrawals (custody)                (Ch.8)
    derivatives/perpetual futures: positions, margin, funding, liq.     (Ch.9–10)
    bot/        a market-maker that keeps the demo liquid               (Ch.11)
    api/        the REST + WebSocket HTTP handlers (the "front door")
    db/         schema + seed data
    config/     env-var configuration
    models/     shared domain types (also the JSON wire format)
web/            React + TypeScript trading terminal                     (Ch.12)
```

### Two architectural ideas to notice early

These show up everywhere, so meet them now.

**1. One writer per market (the "actor" pattern).**
Concurrency in a matching engine is dangerous: two goroutines touching the same
order book can corrupt it. Nebula sidesteps locks entirely by giving **each
market its own goroutine** that processes commands one at a time from a channel.

```go
// internal/engine/engine.go
type Engine struct {
    book *book
    cmds chan func()   // every place/cancel is a closure sent here
    // ...
}
func (e *Engine) start() { go func() { for f := range e.cmds { f() } }() }
```

To place an order, the API sends a closure into `cmds` and waits for the result.
Because only the engine goroutine ever touches `book`, there are **no locks** and
matching is deterministic. (You'll see the same pattern in the perps engine.)

**2. Every cent moves through one atomic primitive.**
Money is never updated with an ad-hoc `UPDATE balances`. Instead, all balance
changes go through `store.ApplyPostings` (and its cousins `CommitFill` /
`CommitPerp`), which write an **audit ledger entry** for every change and refuse
to let any balance go negative — all inside one database transaction. Chapter 3
is devoted to this.

---

## 1.4 A trade, end to end

Let's trace one spot buy so the pieces connect. Suppose **Alice** has 10,000
USDT and clicks **Buy 0.1 BTC** at market price.

```
1. Browser  ──POST /api/orders {market:BTC-USDT, side:buy, type:market, ...}──►  api/
2. api/     validates the request, looks up the BTC-USDT engine, calls Place()
3. engine/  (on its single goroutine):
              a. reserve Alice's USDT  ── store.ApplyPostings ──►  ledger/DB
              b. match against resting sell orders in the book
              c. for each match: move money + record the trade atomically
                                 ── store.CommitFill ──►  ledger/DB
4. market/  receives each trade → updates the candle + ticker
5. ws/      broadcasts: trade, new depth, Alice's order + balance updates
6. Browser  the order book, chart, balances, and "my orders" all update live
```

Every arrow in that diagram is a chapter. By Chapter 7 you'll have walked the
whole path; Chapters 9–10 add the leveraged version of step 3.

---

## 1.5 Why these technology choices?

- **Go** for the backend: cheap goroutines make the "one actor per market" model
  natural, and it compiles to a single static binary.
- **SQLite (pure-Go driver)** for storage: zero setup, real SQL transactions
  (which the ledger depends on). A production exchange would use Postgres or a
  custom store, but the *concepts* are identical.
- **React + TypeScript** for the client, talking to the backend over REST +
  WebSocket — the same shape every major exchange uses.

> **In the real world**, the matching engine, risk engine, wallet service, and
> market-data service are usually *separate deployed services* communicating over
> a message bus, often with the hot matching path kept fully in-memory and
> persisted asynchronously for throughput. Nebula keeps them as packages in one
> process so you can read the whole thing — the responsibilities are the same.

---

## Key takeaways

- An exchange = **a marketplace (matching) + a bank (custody/ledger)**; everything
  else supports those two.
- A **market** is a `BASE-QUOTE` pair; **spot** means you actually own the asset.
- Nebula uses **one goroutine per market** (no locks) and routes **all money
  through one atomic ledger primitive**.
- Each backend package maps to one job; the rest of this course is a guided tour.

## Try it

- Open `backend/cmd/server/main.go` and find where each component is constructed
  and started. Match each line to a row in the table in §1.2.
- Skim `backend/internal/models/models.go` — these are the nouns of the system
  (User, Asset, Market, Order, Trade, Balance). You'll meet them all.

## Next

→ [Chapter 2 — Money & Precision](02-money-and-precision.md): the single most
important rule in any financial system, and the 150 lines that enforce it.
