# Chapter 9 — Perpetual Futures I: Leverage, Margin & Positions

> **Learning objectives**
> - Understand **derivatives**, **futures**, and **perpetual** contracts.
> - Learn **going long/short**, **leverage**, and **margin**.
> - See how a fill **opens, increases, reduces, or flips** a netted position.

> ⚠️ This is the most conceptually dense pair of chapters. Take it slowly — every
> term builds on the last. Spot trading (Chapters 5–6) is the foundation; this is
> the same matching engine with a very different *settlement*.

---

## 9.1 What is a derivative?

A **derivative** is a contract whose value *derives* from an underlying price,
without you owning the underlying. Instead of buying BTC, you make an agreement
*about* BTC's price. The classic form is a **future**: an agreement to settle the
difference between an entry price and a future price.

A **perpetual future** ("perp") is crypto's signature instrument: a future with
**no expiry date**. You can hold it forever. To keep its price tethered to the
real (spot) price despite never settling, perps use a **funding** mechanism
(Chapter 10). Perps dominate crypto trading volume.

Why trade a contract instead of the coin?

- **Leverage** — control a large position with a small deposit (capital
  efficiency).
- **Shorting** — profit when the price *falls*, which is awkward with spot.
- **No custody** — you never hold the underlying; positions settle in USDT.

Nebula's perps are **linear, USDT-margined**: you post USDT as collateral and your
profit/loss is paid in USDT. Markets are `BTC-PERP`, `ETH-PERP`, `SOL-PERP`, each
tied to its spot pair as an **index** (Chapter 10).

---

## 9.2 Long and short

- **Long** = you bet the price goes **up**. Profit if it rises. (Like buying.)
- **Short** = you bet the price goes **down**. Profit if it falls.

Shorting feels strange at first ("selling something you don't own"). With a perp
it's natural: you're just taking the *sell* side of a contract. If you short at
50,000 and the price drops to 45,000, the contract is now worth 5,000 less to the
other side — and that 5,000 is your profit.

In the engine, **buy = open/extend long**, **sell = open/extend short**. We reuse
the spot `Side` (`buy`/`sell`); the position's direction is `long`/`short`.

---

## 9.3 Leverage and margin

This is the heart of derivatives. **Leverage** lets you control a position larger
than your cash by borrowing buying power. The cash you must put up is the
**margin**.

> With **10× leverage**, 500 USDT of margin controls a **5,000 USDT** position.

The relationship:

```
notional (position value) = price × size
initial margin            = notional ÷ leverage
```

So a 10× long of 0.1 BTC at 50,000 has notional `5,000` and requires
`5,000 ÷ 10 = 500` USDT of margin.

**The double-edged sword.** Leverage multiplies *both* directions:

- The position moves 1% → that's 1% of **5,000** = 50 USDT.
- But your margin is only **500**. So a 1% price move is a **10% swing on your
  margin**. A 10% adverse move wipes your margin entirely — that's **liquidation**
  (Chapter 10). Higher leverage = bigger gains *and* faster wipeouts.

**Isolated vs. cross margin.** Nebula uses **isolated margin**: each position has
its own walled-off margin, so the most you can lose on it is that margin. (Cross
margin shares your whole balance as collateral — more capital-efficient, but one
bad position can drain everything. Isolated is simpler and safer to reason about,
ideal for an MVP.)

### The elegant reuse: margin *is* locked balance

Here's the design payoff. We already have **available vs. locked** balances
(Chapter 3) and an atomic ledger. So a position's margin is simply **locked USDT,
attributed to the position**. Opening a position locks margin; closing frees it;
PnL is a posting to available. No new money primitive — perps ride entirely on the
custody ledger you already understand.

---

## 9.4 A position

In **one-way (netting) mode**, you hold at most **one** position per market: you're
either long, short, or flat. Buying while long increases it; selling while long
reduces it. This is `models.Position`:

```go
type Position struct {
    Market      string       // BTC-PERP
    Side        PositionSide // long | short | flat
    Size        num.Dec      // contracts (base), always ≥ 0; direction is in Side
    EntryPrice  num.Dec      // your average entry
    Margin      num.Dec      // isolated USDT collateral (locked)
    Leverage    int
    RealizedPnL num.Dec      // booked profit/loss from closed portions
    // ...plus computed fields (mark, liq price, unrealized PnL) — Chapter 10
}
```

A perp **order** (`models.PerpOrder`) adds `Leverage` and `ReduceOnly` to the
familiar fields, and — unlike spot market buys — is **always sized in contracts**.

---

## 9.5 How a fill changes a position

When a perp order fills, the engine doesn't swap assets (there are none to swap —
it's all USDT margin). Instead it updates each side's **position**. There are four
cases (`settle()` in `backend/internal/derivatives/engine.go`):

**1. Open / increase** (you're flat, or trading the same direction):
- Allocate more margin: `addMargin = (fillPrice × fillQty) / leverage`.
- Update the **average entry price** (size-weighted, like averaging into a stock):

  ```go
  pos.EntryPrice = pos.Size.Mul(pos.EntryPrice)
                     .Add(qty.Mul(price))
                     .Div(pos.Size.Add(qty))
  ```
- `pos.Size += fillQty`, `pos.Margin += addMargin`.

**2. Reduce / close** (you trade *against* your position):
- **Realize PnL** on the closed portion (Chapter 10's formula).
- **Free margin** proportionally: `freed = margin × (closeQty / size)`, moved
  `locked → available`.
- `pos.Size -= closeQty`. If it hits zero, you're **flat**.

**3. Flip** (a reduce bigger than your position): close the whole thing, then open
a new position in the opposite direction with the remainder.

**4. Fees** apply on every fill (taker/maker, like spot), charged from available.

This is why perps need their *own* settlement (`CommitPerp`) distinct from spot's
`CommitFill`: a fill atomically updates **positions + margin postings + PnL +
orders + the trade record**, all in one transaction.

---

## 9.6 Reserving margin, the right way

Just like spot locks funds on placement, a perp order **pre-locks margin** when
placed (so a resting order's collateral is reserved). The amount is estimated from
the order's price and leverage; as the order fills, the locked margin is
**re-attributed** to the position; any surplus (from price improvement, or a
partial fill) is refunded; **reduce-only** orders lock nothing because they free
margin rather than consume it. The bookkeeping mirrors spot's lock/refund exactly
— the same `available → locked` discipline, now tracking position margin.

> The matching loop itself is the *same* price-time-priority engine from Chapter 5
> (the perps package has its own copy of the book to keep spot untouched). What's
> new is only what happens *on a fill*. If you understood Chapter 5, you already
> understand half of this.

---

## 9.7 A worked example

> Deposit 10,000 USDT. Open a **10× long** of 0.1 BTC-PERP at 50,000.

- Notional = `50,000 × 0.1 = 5,000` USDT.
- Margin = `5,000 ÷ 10 = 500` USDT → moved from available to locked.
- Plus a taker fee on the 5,000 notional.
- Your position: `long 0.1 @ 50,000, margin 500, 10×`.
- Your balance: `available ≈ 9,500 − fee`, `locked = 500`.

This is exactly what `TestPerpOpenSettlement` in
`backend/internal/derivatives/derivatives_test.go` asserts. In the next chapter we
let the price move and see profit, loss, funding, and — if it moves far enough —
liquidation.

---

## Key takeaways

- A **perp** is a no-expiry contract on a price; you trade it with **leverage**,
  can go **long or short**, and never custody the underlying.
- **Leverage** controls a big **notional** with small **margin**
  (`margin = notional ÷ leverage`) — and multiplies risk by the same factor.
- Nebula uses **isolated margin**, and brilliantly reuses the ledger:
  **margin = locked USDT**.
- A fill **opens / increases / reduces / flips** a netted position; reduce/close
  **realizes PnL** and **frees margin** — all atomic via `CommitPerp`.

## Try it

- On the Futures page, set leverage to 5×, then 50×, and watch the **Margin
  Required** for the same quantity shrink — same position, less collateral, more
  risk.
- Open a small long, then **add** to it at a different price and watch your
  **entry price** become the weighted average.

## Next

→ [Chapter 10 — PnL, Funding & Liquidation](10-derivatives-funding-liquidation.md):
the three things that make (or break) a leveraged position.
