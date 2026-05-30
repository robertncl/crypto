# Chapter 10 — Perpetual Futures II: PnL, Funding & Liquidation

> **Learning objectives**
> - Compute **unrealized vs. realized PnL** and understand **mark** vs **index**
>   vs **last** price.
> - Understand the **funding rate** — what tethers a perpetual to spot.
> - Understand **liquidation**, the **maintenance margin**, and the **insurance
>   fund**.

This is the risk machinery. It's also where leveraged trading gets people hurt, so
the chapter doubles as a safety briefing.

---

## 10.1 Profit and loss

Your **PnL** is how much you've made or lost on a position.

- **Unrealized PnL** — paper profit/loss on an *open* position, marked to the
  current price. It moves every tick; you haven't booked it.
- **Realized PnL** — locked in when you *close* (or reduce) a position.

The formulas (per unit, times size):

```
Long  unrealized PnL = (mark − entry) × size
Short unrealized PnL = (entry − mark) × size
```

A long profits as price rises above entry; a short profits as price falls below.
Nebula computes this in `enrich()` (`derivatives/engine.go`) and ships it on the
`positions` WebSocket channel, so the UI's green/red PnL updates live. **ROE**
(return on equity) = `PnL ÷ margin` — with 10× leverage a 1% price move is a ~10%
ROE swing.

---

## 10.2 Three prices: last, index, and mark

Newcomers assume there's one "price". A perp has **three**, and the distinction
prevents disasters:

- **Last price** — the price of the most recent perp trade. Noisy; a single large
  order can spike it momentarily.
- **Index price** — the "true" underlying price, taken from the **spot** market
  (Nebula uses the matching spot pair, e.g. BTC-USDT, via the market service).
- **Mark price** — the price used for **PnL and liquidation**. It's deliberately
  *not* the raw last price, so a brief wick or a manipulative trade can't unfairly
  liquidate people.

Why does mark matter? Imagine your liquidation point is 49,250 and someone briefly
pushes the *last* price there with one order before it snaps back. If liquidations
used last price, you'd be wiped on noise. Using a smoother **mark** (anchored to
the index) protects traders. Nebula derives mark from the perp's own price stream
in `MarkPrice()`, with the spot **index** driving funding.

> **In the real world**, the mark price is typically the index price plus a
> decaying funding basis, and the index is a *median of several spot exchanges* to
> resist manipulation. Same purpose, more inputs.

---

## 10.3 Funding: what keeps a perpetual honest

A perp never expires, so what stops its price drifting away from spot forever?
**Funding** — a periodic payment between longs and shorts:

- If the perp trades **above** the index (more longs, bullish), the **funding rate
  is positive** → **longs pay shorts**. This makes being long costly and being
  short attractive, nudging the price back down.
- If the perp trades **below** the index → **rate is negative** → **shorts pay
  longs**, nudging it back up.

It's a self-correcting tether. The payment each interval is
`funding_rate × position_notional`. On a real exchange funding is every 8 hours;
Nebula accelerates it (default every 60s, `PERP_FUNDING_SEC`) so you can watch it.

The manager computes the rate from the premium and clamps it
(`derivatives/manager.go`):

```go
premium := mark.Sub(index).Div(index)
rate = num.Max(fundingCap.Neg(), num.Min(fundingCap, premium))  // clamp to ±0.05%
```

Then each open position pays or receives. Who's the counterparty? The **insurance
fund** acts as clearing house: longs pay it, it pays shorts (and vice-versa).
Because every fill created equal-and-opposite long/short sizes (Chapter 9), the
aggregate nets out and the fund stays neutral on funding.

---

## 10.4 Liquidation: when the margin runs out

With isolated margin, the most you can lose on a position is its margin. When an
adverse move has eaten *almost* all of it, the exchange must **force-close** the
position before it goes negative — otherwise the loss would exceed your collateral
and the exchange (or other users) would eat it. That forced close is
**liquidation**.

**Maintenance margin (MMR).** You're not liquidated at exactly zero equity but at a
small buffer — the **maintenance margin rate** (Nebula seeds 0.5%). When your
equity (`margin + unrealized PnL`) falls to the maintenance level, you're
liquidated.

**Liquidation price.** Solving "equity = maintenance" for the price gives a fixed
price you can display *before* it happens:

```
Long  liq price = entry − margin/size + entry × MMR
Short liq price = entry + margin/size − entry × MMR
```

For a 10× long at 50,000 (margin/size = 5,000, MMR 0.5%): `50,000 − 5,000 + 250 =
~45,250`... and for the 50× long in our test, the buffer is tighter — liq sits
just ~1.5% below entry. **The higher the leverage, the closer liquidation lurks.**

The engine computes this in `liqPrice()`, and a **liquidation monitor** in the
manager checks every open position against the mark price every couple of seconds:

```go
// derivatives/manager.go — the liquidation sweep
case <-liqTick.C:
    for _, e := range m.engines {
        e.CheckLiquidations(m.MarkPrice(e.mkt.Symbol))
    }
```

When a position is breached, `checkLiquidations` closes it at the mark price,
realizes the loss (capped at the margin so you can never go below zero — isolated),
frees the (now ~empty) margin, and routes the shortfall to the **insurance fund**.

---

## 10.5 The insurance fund

What if the market **gaps** — jumps straight through your liquidation price so fast
that closing happens at a worse price than your margin covers? That **bankruptcy**
shortfall has to be absorbed by someone. That's the **insurance fund** (account id
−2): a buffer that covers liquidation gaps so that *winning* traders always get
paid in full.

Nebula seeds it large and it acts as the universal counterparty for perp PnL and
funding. The neat property from Chapter 9 holds: since every fill creates matched
long/short sizes, the *sum* of all traders' PnL is zero, so the fund only ever
drifts by liquidation gaps — exactly its job.

> Real exchanges publish the insurance fund balance, and when even it is
> exhausted, fall back to **auto-deleveraging (ADL)** — closing profitable
> opposing positions to cover the gap. Nebula stops at the insurance fund, which
> is plenty to teach the concept.

---

## 10.6 A worked liquidation

> Open a **50× long** of 0.1 BTC-PERP at 50,000. Margin = `5,000 ÷ 50 = 100` USDT.
> Liquidation price ≈ **49,250** (only ~1.5% below entry!).

Now the mark price falls to **49,000** (below 49,250):

- Unrealized PnL = `(49,000 − 50,000) × 0.1 = −100` USDT — your entire margin.
- The monitor liquidates: the position closes, the 100 margin is gone, you're
  flat. The loss is **capped at your 100 margin** (isolated), and the insurance
  fund absorbs any gap.

A 2% drop in price wiped 100% of your margin — because 50× turns a 2% move into a
100% one. This is the single most important lesson about leverage, and
`TestPerpLiquidation` encodes exactly this scenario.

> **Trader's safety note (and it's in the code's spirit):** high leverage is not
> "more profit" — it's "less room to be wrong". At 50× a 2% wick ends you; at 2×
> you can ride a 40% drawdown. New traders blow up almost exclusively from over-
> leverage. The math doesn't care about your conviction.

---

## Key takeaways

- **Unrealized PnL** is paper P/L at the **mark** price; **realized PnL** is booked
  on close. `Long: (mark−entry)×size`, `Short: (entry−mark)×size`.
- A perp has **last / index / mark** prices; **mark** (anchored to the spot index)
  drives PnL and liquidation to resist manipulation.
- **Funding** periodically transfers between longs and shorts to tether the perp
  to spot — positive rate: longs pay shorts.
- **Liquidation** force-closes a position once equity hits the **maintenance
  margin**; the **liq price** is knowable in advance and gets closer with
  leverage. The **insurance fund** covers bankruptcy gaps.

## Try it

- Open a position, then watch the **funding countdown** and rate on the Futures
  page. After it fires, check your balance changed slightly and the
  `fundingPaid` on your position moved.
- Open a **high-leverage** position with a tiny size, note the **liquidation
  price** shown, and (on the demo) watch the bot-driven price wander toward it.
  See how little room high leverage leaves.

## Next

→ [Chapter 11 — The Market-Maker Bot](11-market-maker-bot.md): why an empty
exchange is useless, and how to fake a crowd.
