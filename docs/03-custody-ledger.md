# Chapter 3 — The Custody Ledger

> **Learning objectives**
> - Understand **double-entry bookkeeping** and why exchanges live or die by it.
> - Learn the **available vs. locked** balance model.
> - Read `store.ApplyPostings` — the one function every cent flows through — and
>   the atomic `CommitFill` / `CommitPerp` variants.

---

## 3.1 The most important invariant in the building

An exchange holds other people's money. The single worst thing it can do is lose
track of it: credit money that didn't exist, or debit money twice. So every
serious exchange enforces an **invariant**:

> Money is never created or destroyed by trading. It only **moves** between
> accounts. At all times, the books balance.

The 700-year-old accounting technique that guarantees this is **double-entry
bookkeeping**: every change is recorded as a set of entries that **sum to zero**.
If Alice pays Bob 100 USDT, you don't just write "Bob +100" — you write
"Alice −100, Bob +100". The two entries net to zero, so total money is conserved
by construction. If a bug ever makes the entries *not* sum to zero, you can detect
it by summing the ledger.

This is the backbone of Nebula's custody layer.

---

## 3.2 Two balances per asset: available and locked

When you place an order to buy 0.1 BTC at 95,000, the exchange must **set aside**
9,500 USDT so you can't spend it on something else before the order fills. But the
money hasn't *left* you yet — the order might cancel.

So each `(user, asset)` balance has **two parts**:

- **Available** — money you can freely spend or withdraw.
- **Locked** — money reserved for working orders or open positions.

```
            place buy order                fill / cancel
 available ───────────────────► locked ───────────────────► (spent or returned)
   9,500            lock 9,500     9,500
```

The schema (`backend/internal/db/schema.sql`) is exactly that:

```sql
CREATE TABLE balances (
    user_id   INTEGER NOT NULL,
    asset     TEXT NOT NULL,
    available INTEGER NOT NULL DEFAULT 0,   -- scaled by 1e8 (Chapter 2)
    locked    INTEGER NOT NULL DEFAULT 0,
    PRIMARY KEY (user_id, asset)
);
```

Reserving funds is just moving `available → locked`. Filling consumes `locked`.
Cancelling moves `locked → available`. **Total** money (available + locked) only
changes on deposits, withdrawals, fees, and PnL — never on "internal plumbing".

---

## 3.3 The audit journal

Alongside the live balances, Nebula keeps an **append-only journal** of every
change — the `ledger_entries` table:

```sql
CREATE TABLE ledger_entries (
    id              INTEGER PRIMARY KEY AUTOINCREMENT,
    txn_id          TEXT NOT NULL,    -- groups the entries of one event
    user_id         INTEGER NOT NULL,
    asset           TEXT NOT NULL,
    delta_available INTEGER NOT NULL, -- signed change
    delta_locked    INTEGER NOT NULL,
    reason          TEXT NOT NULL,    -- 'order_lock', 'trade_buy', 'fee', 'deposit'...
    ref             TEXT NOT NULL,    -- e.g. the order or trade id
    created_at      INTEGER NOT NULL
);
```

The live `balances` table is just the running sum of this journal. The journal
answers *"why is my balance what it is?"* — every movement is there, tagged with a
`reason` and a `txn_id` that ties together all the entries of a single event. This
is what auditors, support teams, and your future debugging self will use.

---

## 3.4 The one function every cent flows through

Here is the heart of custody: **`store.ApplyPostings`**
(`backend/internal/store/store.go`). A **posting** is one signed change to one
`(user, asset)` balance:

```go
type Posting struct {
    UserID         int64
    Asset          string
    DeltaAvailable num.Dec
    DeltaLocked    num.Dec
    Reason         string
    Ref            string
}
```

`ApplyPostings` takes a *list* of postings and applies them **atomically** in one
database transaction. For each posting it:

1. Upserts the balance row and applies the deltas.
2. **Re-reads the row and refuses to let `available` or `locked` go negative** —
   returning `ErrInsufficientFunds` and rolling back the whole transaction.
3. Writes a `ledger_entries` row for the audit journal.

```go
// abridged from store.go
if av < 0 || lk < 0 {
    return fmt.Errorf("%w: user=%d asset=%s ...", ErrInsufficientFunds, ...)
}
```

Two properties make this the trustworthy core of the system:

- **Atomic:** either *all* postings apply or *none* do (DB transaction). You can
  never end up with "money left Alice but never reached Bob".
- **Non-negative by construction:** you cannot overdraw. The insufficient-funds
  check is the same line whether you're placing an order, paying a fee, or
  settling a trade — there's one chokepoint to get right.

> **Why pass a *list* of postings?** Because real events touch several balances at
> once and must be all-or-nothing. Reserving funds for an order is a single
> posting (`available −X, locked +X`). Settling a trade is *six* postings (buyer's
> two assets, seller's two assets, and the exchange's fee) — and they must commit
> together.

### Locking funds is one posting

When you place a buy order, the engine reserves quote currency like this:

```go
// move `amount` from available into locked, atomically, with an audit entry
e.st.ApplyPostings("lock:"+orderID, now, []store.Posting{{
    UserID: o.UserID, Asset: lockAsset,
    DeltaAvailable: lockAmt.Neg(), DeltaLocked: lockAmt,
    Reason: "order_lock", Ref: o.ID,
}})
```

If you don't have the funds, the non-negativity check fails and the order is
rejected *before* it ever touches the book. Clean.

---

## 3.5 Settling a trade atomically: `CommitFill`

A trade changes more than balances — it also writes a `trades` row and updates
both orders' fill amounts. If the balance moved but the trade record didn't
(crash in between), your books would be inconsistent. So Nebula has a higher-level
atomic primitive, **`store.CommitFill`**, that does it all in **one transaction**:

```go
func (s *Store) CommitFill(txnID string, createdAt int64,
    postings []Posting, trade *models.Trade, orders ...*models.Order) error
```

It applies the settlement postings, inserts the trade, and updates the order rows
— commit or rollback together. The derivatives engine has the analogous
`CommitPerp` (which also upserts **positions**). You'll see both in action in
Chapters 6 and 9–10.

The takeaway: **state that must be consistent is changed in a single transaction
behind a single function.** There is no code path that moves money without an
audit trail.

---

## 3.6 System accounts

Not every account is a person. Nebula reserves two **system account** ids:

```go
const ExchangeUserID  int64 = 0   // collects trading & withdrawal fees
const InsuranceFundID int64 = -2  // counterparty for derivatives PnL & funding (Ch.10)
```

These are ordinary rows in the `balances` and `ledger_entries` tables — they just
aren't real users. Modeling the exchange's fee income and the insurance fund as
**accounts** keeps double-entry honest: a fee isn't money vanishing, it's money
*moving to the exchange's account*. The books still balance.

> **In the real world**, custody is far more than a ledger: it's **hot wallets**
> (online, for fast withdrawals) vs **cold wallets** (offline, for the bulk of
> funds), multi-signature and **MPC** key management, hardware security modules,
> proof-of-reserves, and strict separation between the internal ledger and the
> on-chain reality. Our ledger is the *accounting* half of custody; Chapter 8
> covers the (simulated) *blockchain* half.

---

## Key takeaways

- The cardinal invariant: **trading moves money, never creates it.** Double-entry
  (postings that sum to zero) enforces it.
- Balances split into **available** (spendable) and **locked** (reserved).
- **`ApplyPostings`** is the only sanctioned way to change a balance: atomic,
  non-negative, and journaled. `CommitFill`/`CommitPerp` extend it to also write
  trades/orders/positions in the same transaction.
- The **exchange (id 0)** and **insurance fund (id −2)** are modeled as accounts
  so fees and PnL stay double-entry.

## Try it

- Run the exchange (Chapter 13), register, and place + cancel a limit order.
  Then open the SQLite file and run:
  `SELECT reason, asset, delta_available, delta_locked FROM ledger_entries ORDER BY id;`
  You'll see `welcome_bonus`, `order_lock`, then `order_cancel` reversing it.
- Find every call site of `ApplyPostings`, `CommitFill`, and `CommitPerp` in the
  backend. That complete list *is* every way money can move in the system — a
  great thing to be able to enumerate.

## Next

→ [Chapter 4 — Accounts, Auth & KYC](04-accounts-auth-kyc.md): who is allowed to
own these balances in the first place?
