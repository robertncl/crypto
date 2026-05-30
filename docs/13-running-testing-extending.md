# Chapter 13 — Running, Testing & Extending

> **Learning objectives**
> - Run the exchange yourself, two ways.
> - Understand what the automated tests prove.
> - Pick a first project from a graded list of extensions.

You learn a system by changing it. This chapter gets Nebula running on your
machine and hands you a runway of projects.

---

## 13.1 Running it

Prerequisites: **Go 1.24+** and **Node 18+**. (In this environment Go lives at
`~/.local/go/bin`; the `Makefile` finds it automatically.)

**Option A — development, with hot reload (two terminals):**

```sh
make deps        # once: install the frontend's npm packages
make backend     # terminal 1: Go API + WebSocket on :8080 (with the bot)
make web         # terminal 2: Vite dev server on http://localhost:5173
```

Open <http://localhost:5173>. The Vite dev server proxies `/api` and `/ws` to the
backend, so the browser sees a single origin.

**Option B — one self-contained binary:**

```sh
make build       # builds the SPA, then compiles ./nebula (no external deps)
./nebula         # serves the API, WebSocket, AND the built UI on :8080
```

Open <http://localhost:8080>.

### A guided first session

1. **Sign up** — you're credited 10,000 USDT (Chapter 4's welcome bonus).
2. **Spot** (Chapter 5–6) — open BTC-USDT. The bot is quoting, so place a limit
   order and watch it rest, then a market order and watch it fill. Open the SQLite
   DB and read `ledger_entries` to see your money move (Chapter 3).
3. **Futures** (Chapter 9–10) — open a leveraged long, watch unrealized PnL and the
   liquidation price update live, then close it.
4. **Wallet** (Chapter 8) — deposit BTC (watch it confirm), verify KYC, withdraw.

Everything you read about in Chapters 1–12 is now happening in front of you.

---

## 13.2 What the tests prove

Run them with `make test` (or `cd backend && go test ./...`). The suite is small
but targets the highest-risk math:

- **`internal/num`** — `TestMulDivNoOverflow`, `TestParseString`, `TestJSON`. These
  guard Chapter 2: fixed-point multiply doesn't overflow, parsing/printing round-
  trips, and JSON is string-encoded.
- **`internal/engine`** — `TestLimitCrossSettlement` asserts the *exact* six-posting
  settlement from Chapter 6 (including fees and balance conservation).
  `TestCancelReleasesFunds` proves locked funds return precisely on cancel.
- **`internal/derivatives`** — `TestPerpOpenSettlement` checks margin and fees when
  opening a leveraged position (Chapter 9); `TestPerpLiquidation` runs the 50×
  liquidation scenario from Chapter 10 and asserts the margin is wiped, no more, no
  less.

These are **integration tests over a real (temporary) SQLite database**, so they
exercise the actual ledger transactions — the things you most want to be correct.
Read them as worked examples; they double as executable documentation.

---

## 13.3 How the pieces start up

`backend/cmd/server/main.go` is the assembly point and a good map to re-read now
that you know the parts:

```
open DB ──► store ──► market service (tickers/candles)
                 ├──► spot engine manager   (rebuilds spot books)
                 ├──► perp engine manager    (rebuilds positions + books, seeds insurance)
                 ├──► wallet service
                 ├──► auth manager
                 ├──► bot (if ENABLE_BOT)     (spot + perp market making)
                 └──► API server (REST + WS)  ──► ListenAndServe
```

Notice the **rebuild on startup**: both engines reload their open orders (and the
perp engine its positions) from the database, so a restart loses nothing. And the
market service, funding scheduler, and liquidation monitor all start as background
goroutines tied to a context that's cancelled on shutdown.

Configuration is all environment variables (`internal/config`): `ADDR`, `DB_PATH`,
`JWT_SECRET`, `ENABLE_BOT`, `PERP_FUNDING_SEC`, `WEB_DIR`, … with sensible
development defaults so it runs with zero setup.

---

## 13.4 Project ideas, graded

Pick one and ship it. Each names the files you'd touch.

**Warm-ups (a few lines, one file):**
- **Add a market.** Add a coin to the asset seed and a new pair (or perp) in
  `internal/db/db.go`. Restart — the bot, UI, and engines pick it up automatically.
- **Change the fee schedule.** Give makers a rebate (negative fee) in the seed and
  watch fees flow differently in `ledger_entries`.
- **Tune funding.** Set `PERP_FUNDING_SEC=10` and a wider cap; watch funding move
  positions faster.

**Intermediate (touch the engine):**
- **Stop / take-profit orders.** A "stop" triggers a market order when the price
  crosses a level. Add a stored trigger list and check it in the market service's
  trade handler. (Where does it fire? What if the book gaps past it?)
- **Tiered fees.** Compute a user's 30-day volume from `trades` and pick a fee tier
  in `engine.feeRate`. (Spec it: how often do you recompute the tier?)
- **Post-only / IOC orders.** "Post-only" rejects an order that would take;
  "immediate-or-cancel" cancels any unfilled remainder. Both are small tweaks to
  the finalize step in `engine.place`.

**Advanced (new subsystems):**
- **Cross margin.** Let positions share one collateral pool (Chapter 9). This
  changes liquidation from per-position to per-account — design the equity and
  maintenance calculation across all positions.
- **An admin console.** Use the `role` claim (Chapter 4) to gate endpoints that
  halt a market, adjust fees, or view the insurance fund and fee revenue.
- **Testnet custody.** Replace the simulated wallet (Chapter 8) with a real
  Bitcoin/Ethereum **testnet** integration: real addresses, real deposit detection
  via an RPC provider, real (testnet) withdrawals. This is the jump from
  "simulation" to "the real, hard part".
- **A risk engine.** Add position limits, max leverage by account tier, and
  auto-deleveraging (ADL) for when the insurance fund is exhausted (Chapter 10).

For any of these, the workflow is the same: write a failing test that encodes the
behavior (copy the style in the `*_test.go` files), implement until green, then run
it in the UI.

---

## 13.5 Where this model is honest, and where it isn't

This codebase faithfully teaches the **mechanics** — matching, the ledger, market
data, margin, funding, liquidation. It deliberately simplifies the parts that are
about *scale, security, and regulation*: in-memory single-process design instead of
distributed services; simulated custody instead of real key management; a stub for
KYC/AML; isolated margin only; an insurance fund without ADL. Each chapter's "In
the real world" callouts mark these seams. Knowing **where the simplifications
are** is itself a senior skill — you now have that map.

---

## Key takeaways

- Run it with `make backend` + `make web` (dev) or `make build && ./nebula` (one
  binary).
- The tests are **integration tests over real SQLite** that pin down the riskiest
  math (precision, settlement, liquidation) — read them as worked examples.
- The system **rebuilds engine state from the DB on startup**; everything is
  wired in `main.go`.
- Extend it: start by **adding a market or a fee tweak**, graduate to **stop
  orders or cross margin**, and — for the real challenge — **testnet custody**.

## Next

→ [Glossary](glossary.md): every term in the course, in one place. Keep it handy.
