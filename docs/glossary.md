# Glossary

Every term introduced in the course, in one place. The chapter where each is
explained in depth is noted in parentheses.

### Markets & orders

- **Asset / coin / token** — something you can hold and trade (BTC, ETH, USDT). (1)
- **Base / quote** — in a pair `BASE-QUOTE`, the base is what's bought/sold, the
  quote is what it's priced and paid in. BTC-USDT: base BTC, quote USDT. (1)
- **Market / trading pair** — two assets you can swap, e.g. `BTC-USDT`. (1)
- **Spot** — trading for immediate ownership of the asset itself. (1, 5–6)
- **Order** — an instruction to trade a quantity at some price. (5)
- **Limit order** — trade at a set price or better; rests in the book if it can't
  fill now. (5)
- **Market order** — trade immediately at the best available price; may incur
  slippage. (5)
- **Buy / bid** — order/price to acquire the base asset. **Sell / ask** — to
  dispose of it. (5)
- **Order book** — the live set of all resting buy and sell orders for a market. (5)
- **Best bid / best ask** — the highest buy and lowest sell currently resting. (5)
- **Spread** — the gap between best bid and best ask. (5, 11)
- **Depth** — quantity available at each price level; the book aggregated. (7)
- **Tick size / step size** — the minimum price increment / quantity increment a
  market allows. (6)
- **Min notional** — the minimum value (`price × qty`) an order must have. (6)

### Matching & execution

- **Matching engine** — the component that pairs buys with sells into trades. (5)
- **Price-time priority** — the fairness rule: better price first; at equal price,
  earlier order first. (5)
- **Maker** — an order that rests in the book, adding liquidity. **Taker** — an
  order that matches immediately, removing liquidity. (5, 6, 11)
- **Fill / trade** — a match between two orders; can be **partial**. (5, 6)
- **Price improvement** — a taker filling at a better price than its limit, because
  trades execute at the maker's price. (5, 6)
- **Self-trade prevention** — stopping a user from trading with their own resting
  order. (5)
- **Slippage** — getting a worse average price than expected because an order swept
  into deeper, worse levels. (5, 11)
- **Liquidity** — the ability to trade size quickly without moving the price;
  tight spread + depth. (11)

### Money, ledger & custody

- **Fixed-point** — representing money as integers of the smallest unit (Nebula:
  8 decimals, `Scale = 10^8`) to avoid float error. (2)
- **Double-entry bookkeeping** — recording every change as postings that sum to
  zero, so money is conserved. (3)
- **Posting** — one signed change to one `(user, asset)` balance, journaled. (3)
- **Available vs. locked** — spendable balance vs. balance reserved for orders or
  positions. (3, 6)
- **Ledger / audit journal** — the append-only record (`ledger_entries`) of every
  money movement, tagged with a reason. (3)
- **Insufficient funds** — the error when a posting would drive a balance negative;
  enforced centrally. (3)
- **Exchange account (id 0)** — the system account that collects fees. (3, 6)
- **Insurance fund (id −2)** — the system account that clears perp PnL/funding and
  absorbs liquidation shortfalls. (3, 10)
- **Fee (maker/taker)** — a charge on a fill, on the asset received; revenue to the
  exchange. (6)
- **Custody** — holding customers' assets (their keys). (8)
- **Wallet / address / private key** — a set of keys; the public address receives
  funds; the private key spends them. (8)
- **Confirmations** — blocks mined after a transaction, awaited before crediting a
  deposit. (8)
- **Hot / cold wallet** — online keys for fast withdrawals vs. offline keys holding
  the bulk of funds. (8)
- **Multi-sig / MPC** — requiring multiple keys/parties to authorize a spend. (8)

### Identity & compliance

- **Authentication / authorization** — proving who you are vs. being allowed to do
  a thing. (4)
- **bcrypt** — a slow, salted password hashing function. (4)
- **JWT** — a signed, self-validating token carrying the user id; stateless
  sessions. (4)
- **KYC (Know Your Customer)** — verifying a customer's real identity. **AML
  (Anti-Money-Laundering)** — monitoring for illicit activity. Exchanges typically
  gate withdrawals on KYC. (4, 8)

### Market data & realtime

- **Ticker** — a market's at-a-glance summary: last price, 24h high/low/volume/
  change. (7)
- **Candle / OHLCV** — open/high/low/close/volume for a time bucket; the chart's
  raw material. (7)
- **Trade tape** — the live stream of executed trades. (7)
- **Mark price** — the (manipulation-resistant) price used for PnL and liquidation,
  anchored to the index. **Index price** — the underlying spot price. **Last
  price** — the most recent trade price. (10)
- **WebSocket** — a persistent, server-push connection for live updates. (7, 12)
- **Pub/sub hub / topic / channel** — the broadcast mechanism: producers publish to
  a topic, subscribers receive it. Private channels are scoped to the user. (7, 12)

### Derivatives

- **Derivative** — a contract whose value derives from an underlying price, without
  owning it. (9)
- **Future** — an agreement to settle the difference between an entry and a future
  price. **Perpetual ("perp")** — a future with no expiry, tethered to spot by
  funding. (9)
- **Long / short** — betting the price rises / falls. (9)
- **Leverage** — controlling a position larger than your cash; multiplies gains and
  losses. (9)
- **Notional** — the full value of a position (`price × size`). (9)
- **Margin** — the collateral backing a position. **Initial margin** =
  `notional ÷ leverage`. In Nebula, margin is locked USDT. (9)
- **Isolated vs. cross margin** — per-position collateral vs. a shared account-wide
  pool. (9)
- **Position** — your net exposure in a perp market: side, size, entry, margin,
  leverage. **Netting / one-way** — at most one position per market. (9)
- **PnL** — profit and loss. **Unrealized** (open, marked to price) vs. **realized**
  (booked on close). **ROE** = PnL ÷ margin. (10)
- **Funding rate** — periodic payment between longs and shorts that tethers a perp
  to spot; positive → longs pay shorts. (10)
- **Maintenance margin (MMR)** — the minimum equity ratio before liquidation. (10)
- **Liquidation** — force-closing a position whose equity hit the maintenance
  level. **Liquidation price** — the mark price at which that happens. (10)
- **Bankruptcy / ADL** — a loss exceeding margin (covered by the insurance fund);
  auto-deleveraging is the fallback when the fund is exhausted. (10)
- **Reduce-only** — an order that may only shrink/close a position, never open one.
  (9)

### Architecture & engineering

- **Actor / one-writer-per-market** — giving each market a single goroutine that
  processes commands serially, avoiding locks. (1, 5)
- **Atomic transaction** — a set of changes that all succeed or all fail together;
  the basis of `ApplyPostings` / `CommitFill` / `CommitPerp`. (3, 6)
- **Rebuild on startup** — reloading in-memory engine state (books, positions) from
  the database after a restart. (5, 13)
- **Random walk** — the toy price model the bot uses to drift the mid price. (2, 11)
- **Settlement** — the act of moving money/positions when a trade executes. (6, 9)

---

[← Back to the course index](README.md)
