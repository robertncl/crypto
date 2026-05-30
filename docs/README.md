# Building a Crypto Exchange — A Technical Course

> A hands-on course that teaches how a cryptocurrency exchange works by reading a
> real one. Each chapter pairs a **market/finance concept** (explained for people
> new to crypto) with the **actual code** in this repository (Nebula) that
> implements it.

## Who this is for

You are a **software engineer, QA, SRE, analyst, or technical PM** who is
comfortable with code, APIs, databases, and concurrency — but who has **never
worked on a trading system or in crypto**. You don't need a finance background.
By the end you'll understand how orders, custody, matching, market data, and
even leveraged derivatives work, and you'll be able to point at the exact code
that makes each one happen.

## How to use it

- Read the chapters in order — each builds on the last.
- Keep the codebase open alongside. Every chapter cites real files like
  `backend/internal/engine/engine.go`.
- Run the exchange (see [Chapter 13](13-running-testing-extending.md)) and watch
  the concepts happen live.
- Terms in **bold** on first use are defined in the [Glossary](glossary.md).

## The system at a glance

Nebula is a spot **and** derivatives exchange with simulated custody:

- **Backend:** Go — a matching engine, a double-entry custody ledger, market
  data, wallets, and a perpetual-futures engine. Pure-Go SQLite, no external
  services.
- **Frontend:** React + TypeScript — a dark "trading terminal".
- **Realtime:** a WebSocket hub streams prices, order books, fills, and positions.

```
                         ┌──────────────────────────────────────────┐
   Browser (React SPA)   │                Go backend                │
  ┌───────────────────┐  │                                          │
  │  Trade / Futures  │  │   REST ─┬─ auth/accounts                 │
  │  Markets / Wallet │◄─┼── + ────┤  spot matching engine          │
  │                   │  │   WS    ├─ derivatives (perps) engine     │
  └───────────────────┘  │         ├─ market data (ticks/candles)    │
                         │         ├─ wallet / custody (simulated)   │
                         │         └─ custody ledger (double-entry)  │
                         │                     │                     │
                         │                  SQLite                   │
                         └──────────────────────────────────────────┘
```

## Curriculum

| # | Chapter | What you'll learn |
|---|---------|-------------------|
| 1 | [Architecture & a Crypto Primer](01-architecture-overview.md) | What an exchange *is*, the major parts, and core market vocabulary |
| 2 | [Money & Precision](02-money-and-precision.md) | Why you never use floats for money; fixed-point decimals |
| 3 | [The Custody Ledger](03-custody-ledger.md) | Double-entry bookkeeping, balances, available vs. locked |
| 4 | [Accounts, Auth & KYC](04-accounts-auth-kyc.md) | Sessions, password hashing, and what KYC/AML mean |
| 5 | [The Order Book & Matching Engine](05-order-book-matching-engine.md) | Bids, asks, price-time priority, how trades are made |
| 6 | [Spot Settlement: Fills, Fees & Locking](06-spot-settlement.md) | What happens to money when a trade executes |
| 7 | [Market Data & Real-Time WebSockets](07-market-data-websockets.md) | Tickers, candles, depth, and pushing live updates |
| 8 | [Wallets & Simulated Custody](08-wallets-custody.md) | Deposits, withdrawals, confirmations, hot/cold wallets |
| 9 | [Perpetual Futures I: Leverage, Margin & Positions](09-derivatives-positions.md) | Derivatives, going long/short, borrowing to trade |
| 10 | [Perpetual Futures II: PnL, Funding & Liquidation](10-derivatives-funding-liquidation.md) | Mark price, funding, and getting liquidated |
| 11 | [The Market-Maker Bot](11-market-maker-bot.md) | Why exchanges need liquidity and how to simulate it |
| 12 | [The Trading Terminal (Frontend)](12-frontend-terminal.md) | Rendering a live market in the browser |
| 13 | [Running, Testing & Extending](13-running-testing-extending.md) | Run it, test it, and where to go next |
|   | [Glossary](glossary.md) | Every term, in one place |

## A standing disclaimer

This is **teaching software**. Custody is *simulated* — there is no real
blockchain and no real money. The goal is to make the mechanics legible, not to
be production-secure. Where the real world is harder than our version, the
chapters say so in a **"In the real world"** callout.
