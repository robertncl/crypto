# Chapter 8 — Wallets & Simulated Custody

> **Learning objectives**
> - Understand crypto **deposits** (addresses, confirmations) and **withdrawals**.
> - Learn the **hot/cold wallet** model and why custody is the hardest part of an
>   exchange.
> - See how Nebula *simulates* a blockchain while keeping the ledger honest.

The matching engine moves money *between users inside* the exchange. This chapter
is about money crossing the **boundary** — arriving from, and leaving to, the
outside world (a blockchain).

---

## 8.1 What a "wallet" actually is

In crypto there are no account numbers at a bank. Ownership is proven by
**cryptographic keys**:

- A **private key** is a secret number. Whoever knows it controls the funds.
- A **public address** is derived from it (e.g. `bc1q…` for Bitcoin, `0x…` for
  Ethereum). You share it freely; people send funds *to* it.
- To *spend*, you sign a transaction with the private key.

A "wallet" is really **a set of keys**. The mantra "**not your keys, not your
coins**" captures the whole risk: whoever holds the private keys holds the money.
When you deposit to an exchange, *the exchange* now controls the keys — that's
**custody**, and it's a profound responsibility.

---

## 8.2 Deposits: addresses and confirmations

To receive a deposit, the exchange gives you an **address** to send to (usually
one per user per asset). When you send funds on-chain, the network doesn't confirm
instantly — a transaction must be included in a block, and the exchange waits for
a few **confirmations** (subsequent blocks) before crediting you, to be sure the
transaction won't be reversed. Bitcoin might want 2–3; some chains want more.

Nebula simulates this whole dance (`backend/internal/wallet/wallet.go`):

1. **Generate a plausible address** per user/asset, formatted for the network
   (`bc1q…`, `0x…`, Solana base58, …):

   ```go
   func genAddress(network string) string {
       switch strings.ToUpper(network) {
       case "BITCOIN":        return "bc1q" + randHex(20)
       case "ERC20","BEP20":  return "0x"  + randHex(20)
       case "SOLANA":         return randBase58(44)
       case "TRC20":          return "T"   + randBase58(33)
       }
   }
   ```

2. **A deposit confirms over time.** `Deposit()` records a `pending` transaction,
   then a goroutine ticks through the required confirmations and only **then**
   credits the ledger and marks it `completed`:

   ```go
   for c := 1; c <= needed; c++ {
       time.Sleep(confirmInterval)            // simulate a block
       txn.Confirmations = c
       // ...publish "confirmed (1/2)" to the user's WebSocket...
   }
   // final confirmation → credit the balance via the ledger (Chapter 3):
   s.st.ApplyPostings("deposit:"+txn.ID, now, []store.Posting{{
       UserID: txn.UserID, Asset: txn.Asset, DeltaAvailable: txn.Amount,
       Reason: "deposit", Ref: txn.ID,
   }})
   ```

   Crucially, the credit is a **ledger posting** — a deposit is money *entering*
   the internal books. The frontend shows the live `pending → confirmed (1/2) →
   completed` progression via the private `walletTxns` channel.

---

## 8.3 Withdrawals: gated, fee'd, and broadcast

A withdrawal is the reverse — and it's where exchanges are most careful, because
money is *leaving*. Nebula's `Withdraw()`:

1. **Requires KYC** (Chapter 4) — you can't withdraw until verified.
2. **Validates** the amount against a per-asset minimum and a network fee.
3. **Debits immediately and atomically** — the amount *plus* the network fee
   leaves your `available`, with the fee credited to the exchange account:

   ```go
   s.st.ApplyPostings("withdraw:"+txn.ID, now, []store.Posting{
       {UserID: userID,         Asset: a.Symbol, DeltaAvailable: total.Neg(), ...}, // amount + fee
       {UserID: ExchangeUserID, Asset: a.Symbol, DeltaAvailable: a.WithdrawFee, ...}, // the fee
   })
   ```

4. **"Broadcasts" on-chain** — a goroutine assigns a fake transaction id and walks
   the confirmations to `completed`.

Why debit *before* the funds are confirmed on-chain? Because the user has
committed to sending them out; holding them in limbo would let them be double-
spent on a trade. The network fee is non-refundable revenue (it pays for the
on-chain transaction in reality).

---

## 8.4 Modeling the "outside world"

A subtle accounting point: when you deposit, money **appears** in the ledger; when
you withdraw, it **disappears**. Doesn't that violate Chapter 3's "money is never
created or destroyed"?

No — that invariant is about *trading*. Deposits and withdrawals are the
**boundary** between the exchange's books and the external blockchain. The chain
is, by definition, outside our ledger. So a deposit is the controlled *creation*
of an internal claim backed by real coins arriving on-chain, and a withdrawal is
its controlled *destruction* as coins leave. The audit journal records both with
`deposit` / `withdraw` reasons, so they're fully traceable — they just aren't
trades.

---

## 8.5 Hot wallets, cold wallets, and why custody is hard

Nebula's custody is a teaching simulation. Real custody is arguably the hardest,
highest-stakes part of running an exchange. The core architecture:

- **Hot wallet** — keys kept online to process withdrawals automatically. Fast,
  but internet-exposed, so it holds only a small fraction of funds.
- **Cold wallet** — keys kept **offline** (air-gapped hardware, vaults). The bulk
  of customer funds live here; moving from cold to hot is a deliberate, audited,
  multi-person process.
- **Multi-sig / MPC** — require several keys/parties to authorize a spend, so no
  single compromised laptop can drain the exchange.

The history of crypto is littered with exchanges that got this wrong (Mt. Gox,
and many since). The lesson: **the matching engine can be perfect, but if custody
fails, the exchange fails.**

> **In the real world**, deposits are detected by running blockchain nodes (or
> using providers) that watch each address; withdrawals are signed by the hot
> wallet and rate-limited, with large ones routed to manual review. Add
> **proof-of-reserves** (cryptographically showing you hold what you owe) and
> you have a sense of the real scope. Nebula's `wallet` package is a faithful
> model of the *workflow* (address → pending → confirmations → credit) with the
> cryptography and node infrastructure swapped for simulation.

---

## Key takeaways

- A crypto wallet is **keys**; depositing to an exchange hands it **custody** of
  your keys → funds.
- **Deposits** wait for **confirmations** before crediting; Nebula simulates this
  and credits via a ledger posting.
- **Withdrawals** are **KYC-gated**, charge a network fee, and debit immediately
  before "broadcasting".
- Deposits/withdrawals are the **ledger boundary** with the outside chain — the
  one place money legitimately enters or leaves the internal books.
- Real custody splits funds across **hot/cold wallets** with **multi-sig/MPC**;
  it's the make-or-break discipline of an exchange.

## Try it

- In the Wallet page, deposit BTC and watch the transaction go
  `pending → confirmed → completed` over a few seconds, then see the balance
  appear. That timing is the simulated confirmation loop.
- Try to withdraw before verifying KYC (blocked), then verify and withdraw. Check
  the exchange account (`user_id = 0`) picked up the network fee.

## Next

→ [Chapter 9 — Perpetual Futures I](09-derivatives-positions.md): leaving spot
behind to trade *contracts* with borrowed money.
