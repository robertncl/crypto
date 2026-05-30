# Chapter 11 — The Market-Maker Bot

> **Learning objectives**
> - Understand **liquidity** and why an exchange is useless without it.
> - Learn what **market makers** do and how they're compensated.
> - Read Nebula's two-account bot that keeps every market alive.

---

## 11.1 The empty-restaurant problem

Launch a brand-new exchange and you hit a chicken-and-egg wall: traders won't come
to a market with no orders (nothing to trade against), but there are no orders
because no traders came. An empty order book has no **liquidity** — and liquidity
is the product an exchange actually sells.

**Liquidity** = the ability to trade meaningful size quickly without moving the
price much. A liquid market has a **tight spread** and **depth** (lots of quantity
near the top of book). An illiquid one has a wide spread and thin depth, so your
order suffers **slippage**.

The people who solve this are **market makers**.

---

## 11.2 What a market maker does

A market maker continuously posts **both** a buy and a sell order around the
current price — "I'll buy at 94,990 and sell at 95,010". They're always a
**maker** (Chapter 5), providing liquidity on both sides.

Their profit model: they earn the **spread**. If someone sells to their bid at
94,990 and someone else buys their ask at 95,010, they pocket the 20 difference
without taking a directional view. They also often earn **maker rebates** (negative
fees) from the exchange for providing liquidity. The risk they manage is
**inventory** — ending up too long or too short if the market moves one way — which
they hedge or skew their quotes to control.

Real market making is a sophisticated, latency-sensitive business. For a *demo*,
we just need the book to look and behave like a live one.

---

## 11.3 Nebula's bot: two accounts, one lively market

The bot (`backend/internal/bot/marketmaker.go`) uses **two** accounts on purpose:

- a **maker** account that posts a ladder of resting limit orders around a mid
  price, and
- a **taker** account that periodically crosses the spread with market orders.

Why two? Because of **self-trade prevention** (Chapter 5): one account can't trade
with itself. Splitting maker and taker into different users means their orders
actually match, producing real **trades** — which in turn drive the candles,
ticker, and trade tape (Chapter 7). One account would just post orders that never
execute; two accounts make a living market.

### The mid price does a random walk

Each market keeps a `mid` price that drifts a little every tick — a **random
walk**, the standard toy model of price movement:

```go
mid *= 1 + b.rng.NormFloat64()*vol   // nudge by a small random %
```

This is one of the rare, sanctioned uses of `float64` (Chapter 2): it's choosing a
*simulated* price, not settling money. The value is converted to `num.Dec` before
any order is placed.

### The maker posts a ladder

Around the mid, the maker places several bids below and asks above, widening as
they go — a realistic-looking book:

```go
for i := 1; i <= levels; i++ {
    spread   := 0.0006 * float64(i)              // each level a bit wider
    bidPrice := roundTo(mid*(1-spread), m.PriceTick)
    askPrice := roundTo(mid*(1+spread), m.PriceTick)
    // ...place a limit buy at bidPrice and a limit sell at askPrice...
}
```

Each refresh it **re-quotes**: it places a fresh ladder around the new mid, then
cancels the previous orders — so stale quotes don't pile up and the book tracks
the drifting price.

### The taker creates trades

On its own timer, the taker fires small market orders in random directions,
crossing into the maker's ladder. Each one is a real fill → a real trade → live
chart and ticker movement. That's the activity you see the moment you open the
demo.

---

## 11.4 The bot trades perps too

The same pattern runs on the **perpetual** markets (`runPerpMarket`): the maker
quotes a leveraged ladder, the taker crosses it. This is what makes the Futures
markets show live prices, funding, and depth out of the box — and what gives your
own positions a real counterparty and a moving mark price to be liquidated against
(Chapter 10). The bot uses modest leverage (5×) and quotes near the mid, so its own
positions never approach liquidation.

The bot funds its accounts generously through the **ledger** (`fund()` →
`ApplyPostings`) — even the bot's seed capital is auditable money, not a magic
number. Consistency, again, everywhere.

---

## 11.5 Turning it off

The bot exists purely to make the demo feel real. It's controlled by the
`ENABLE_BOT` environment variable. Set `ENABLE_BOT=false` and you get pristine
empty books — useful when you want to test *your own* orders deterministically
(several of the manual tests in this course's "Try it" boxes are easier with the
bot off, or you can simply run two browser sessions and be your own counterparty).

---

## Key takeaways

- An exchange's real product is **liquidity**: tight spreads and depth. An empty
  book is worthless.
- **Market makers** continuously quote both sides, earning the **spread** (and
  maker rebates) while managing **inventory** risk.
- Nebula simulates this with **two accounts** (maker + taker) so orders actually
  match (self-trade prevention) and generate real trades, candles, and ticks — on
  both spot and perps.
- Even simulated capital flows through the **ledger**; toggle it with
  `ENABLE_BOT`.

## Try it

- Restart with `ENABLE_BOT=false`. The markets are silent and the book is empty —
  feel the "empty restaurant". Now place your own resting limit orders and cross
  them from a second account to hand-make a market.
- With the bot on, watch a market's depth for a minute — you can see the maker
  re-quoting its ladder as the mid wanders.

## Next

→ [Chapter 12 — The Trading Terminal (Frontend)](12-frontend-terminal.md): how all
this backend state becomes a screen a human can trade on.
