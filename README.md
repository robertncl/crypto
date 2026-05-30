# Nebula — Crypto Exchange & Custody (Spot MVP)

A self-contained spot cryptocurrency exchange and custody platform. It implements the realistic core of an exchange — a
price-time-priority **matching engine**, a double-entry **custody ledger**,
**simulated wallet custody** (deposits/withdrawals), real-time **market data**,
and a polished dark **trading terminal** UI.

> ⚠️ **Demo software.** Custody is *simulated* — no real blockchain, no real
> funds. Do not point this at mainnet or use it to hold real value. It is built
> to demonstrate exchange architecture, not to be production-secure.

---

## Features

**Trading**
- Limit & market orders (market buys are sized by quote budget)
- In-memory price-time-priority order book with a single-writer matching engine per market
- Maker/taker fees, partial fills, price-improvement refunds, self-trade prevention
- Order cancellation with atomic fund release

**Custody & accounts**
- Double-entry ledger: every balance change is journaled; funds can never be created or lost by trading
- Available vs. locked balances; atomic fund reservation on order placement
- Simulated deposits (confirm over time) and withdrawals (KYC-gated, network fees)
- JWT auth, bcrypt password hashing, a KYC verification stub

**Market data (real-time over WebSocket)**
- 24h rolling tickers, OHLCV candles (1m–1d), order-book depth, live trade feed
- A seed market-maker **bot** provides liquidity and price action out of the box

**Frontend**
- React + TypeScript trading terminal: candlestick chart (TradingView lightweight-charts),
  live order book with depth bars, trade feed, order entry, open orders/history, wallet

---

## Tech stack

| Layer    | Tech |
|----------|------|
| Backend  | Go (chi router, gorilla/websocket), pure-Go SQLite (`modernc.org/sqlite`, no CGO) |
| Frontend | React 18 + TypeScript + Vite, lightweight-charts |
| Realtime | WebSocket topic fan-out hub |
| Money    | Fixed-point decimal (`int64` scaled 1e8) — **never floats** for balances/prices |

```
backend/
  cmd/server/         entrypoint + graceful shutdown
  internal/
    num/              fixed-point decimal type (money-safe)
    config/           env configuration
    db/               SQLite open + schema + seed data
    store/            data access + atomic posting/fill primitives (double-entry)
    auth/             JWT + bcrypt + middleware
    engine/           order book + matching engine + per-market actor
    market/           tickers + candle aggregation
    wallet/           simulated custody (addresses, deposits, withdrawals)
    ws/               WebSocket hub + client pumps
    bot/              seed market-maker
    api/              REST + WS HTTP handlers
web/
  src/
    api/              typed REST client, WS client, wire types
    state/            auth context
    hooks/            stream + balances hooks
    components/       Chart, OrderBook, TradesFeed, OrderForm, UserOrders, ...
    pages/            Trade, Markets, Wallet, Login, Register
```

---

## Prerequisites

- **Go 1.24+** (this repo was built with 1.26). If `go` is not on your `PATH`, it
  is installed at `~/.local/go/bin` — add it with:
  ```sh
  export PATH="$HOME/.local/go/bin:$PATH"
  ```
- **Node 18+** (built with Node 22) and npm

---

## Running

### Option A — Development (hot reload, two terminals)

```sh
make deps          # one-time: install frontend deps

# terminal 1: Go API + WebSocket on :8080 (with the seed bot)
make backend

# terminal 2: Vite dev server on http://localhost:5173 (proxies /api and /ws → :8080)
make web
```

Open **http://localhost:5173**.

### Option B — Single self-contained binary (production-style)

```sh
make build         # builds the SPA, then compiles ./nebula with no external deps
./nebula           # serves the API, WebSocket, AND the built UI on one port
```

Open **http://localhost:8080**.

> Run the binary from the repo root so it finds `web/dist`, or set `WEB_DIR` to
> the built SPA directory.

---

## Try it

1. **Sign up** — new accounts are credited **10,000 USDT** to start.
2. **Trade** — go to a market (e.g. BTC-USDT). The bot is quoting both sides, so
   you can immediately place limit or market orders and watch them fill against
   live liquidity. Click an order-book row to set the price.
3. **Wallet** — deposit BTC/ETH/etc. (simulated; it confirms after a few seconds),
   then **Verify identity** to unlock withdrawals.

---

## Configuration (environment variables)

| Var | Default | Description |
|-----|---------|-------------|
| `ADDR` | `:8080` | HTTP listen address |
| `DB_PATH` | `exchange.db` | SQLite file path |
| `JWT_SECRET` | `dev-insecure-secret-change-me` | Token signing secret (**set in prod**) |
| `JWT_TTL_HOURS` | `72` | Access token lifetime |
| `ENABLE_BOT` | `true` | Run the seed market-maker bot |
| `CORS_ORIGIN` | `http://localhost:5173` | Allowed SPA origin |
| `WEB_DIR` | `web/dist` | Built SPA directory to serve (empty to disable) |

---

## API sketch

```
POST /api/auth/register | /api/auth/login        → { token, user }
GET  /api/markets | /api/assets | /api/tickers
GET  /api/markets/{sym}/depth | /trades | /candles?interval=1m
GET  /api/me                                     (auth)
POST /api/kyc/verify                             (auth)
GET  /api/account/balances                       (auth)
POST /api/orders   { market, side, type, price?, quantity }   (auth)
DELETE /api/orders/{id}                          (auth)
GET  /api/orders | /api/orders/history | /api/trades          (auth)
GET  /api/wallet/address?asset=BTC               (auth)
POST /api/wallet/deposit | /api/wallet/withdraw  (auth)
GET  /api/wallet/transactions                    (auth)
WS   /ws?token=...   subscribe to ticker:/depth:/trades:/kline: + private orders/balances/walletTxns
```

---

## Design notes

- **No floating point for money.** All amounts are `int64` scaled by 1e8, with
  multiply/divide done via `math/big` to avoid overflow. Strings carry decimals
  over the wire to preserve precision.
- **Double-entry integrity.** Every balance mutation goes through one atomic
  `ApplyPostings`/`CommitFill` primitive that journals a ledger entry and refuses
  to drive any balance negative — so a trade either settles fully (both sides,
  fees, order rows) or not at all.
- **One writer per market.** Each market's engine runs in its own goroutine
  consuming a command channel, so the order book needs no locks and matching is
  deterministic. Books are rebuilt from open orders on startup.
- **Simulated custody.** Deposit addresses and txids are generated to look real;
  deposits credit after a few simulated confirmations; withdrawals debit
  immediately and "broadcast" asynchronously. The external chain is modeled as
  funds entering/leaving the internal ledger.

## Tests

```sh
make test    # backend unit tests (decimal math, etc.)
```
