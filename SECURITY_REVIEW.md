# Security Review

**Scope note:** The branch diff is empty — the Earn feature was already committed (`ae3ffb5 add earn`) and the working tree is clean. I reviewed the committed Earn feature (the substantial new code from this session) plus the core security surfaces it touches (auth/JWT, the ledger `ApplyPostings` primitive, and handler authorization). Auth (HS256 with algorithm-confusion guard, bcrypt, expiry enforced), SQL access (fully parameterized), and authorization (`auth.UserID` from JWT everywhere; ownership checks on cancel/redeem) are sound. One real vulnerability was found.

---

## Vuln 1: Double-Redeem via TOCTOU on Earn Position Status — `internal/earn/earn.go` (Redeem) + `internal/store/earn.go` (CommitEarnPosting/updateEarnPositionTx)

* **Severity: High**
* **Category:** `business_logic` / race condition (check-then-act / TOCTOU)
* **Confidence:** 9/10

* **Description:**
  `Service.Redeem` guards against re-redeeming a closed position with a non-transactional check:

  ```go
  pos, err := s.st.GetEarnPosition(positionID) // read #1 (own connection, then released)
  ...
  if pos.Status != models.EarnActive { return nil, ErrPositionClosed }
  ...
  payout := pos.Principal.Add(interest)
  postings := []store.Posting{ // pool -> user (principal + interest)
      {UserID: store.EarnPoolID, Asset: pos.Asset, DeltaAvailable: payout.Neg(), ...},
      {UserID: userID,           Asset: pos.Asset, DeltaAvailable: payout, ...},
  }
  pos.Status = models.EarnRedeemed
  s.st.CommitEarnPosting("earn_redeem:"+pos.ID, now, postings, pos)
  ```

  The status read and the payout transaction are separate operations. `CommitEarnPosting` applies the payout and then calls `updateEarnPositionTx`, which updates the row **unconditionally** (`UPDATE earn_positions SET ... WHERE id=?` — no `AND status='active'` guard, and no re-read of status inside the transaction). Two concurrent redeem requests for the same position both pass the `status == active` check before either commits, so both execute the payout.

  Crucially, the payout is funded from the `EarnPoolID` account, which `Service.Init` seeds with `1_000_000_000` per asset. The non-negativity check inside `ApplyPostings` — the safeguard that prevents double-spend in `wallet.Withdraw` (where a user debits their own balance) — does **not** stop this, because the oversized pool balance never goes negative. So the principal (plus interest) is credited to the attacker twice.

* **Exploit Scenario:**
  1. Attacker subscribes 1,000 USDT to a flexible product (`POST /earn/subscribe`) → 1,000 USDT moves into the Earn pool, an active position is created.
  2. Attacker fires N concurrent `POST /earn/positions/{id}/redeem` requests with the same valid token and position ID (well within the 600/min IP limit).
  3. Multiple requests read `status=active` before any commits; each commits a full `principal + interest` payout from the pool.
  4. Attacker ends up with several thousand USDT for a 1,000 USDT principal — funds are minted out of the pool. Repeating drains the pool / inflates balances, and those balances are withdrawable via the normal wallet flow.

* **Recommendation:**
  Make the close transition atomic and conditional inside the same transaction as the payout. In `CommitEarnPosting` (for the redeem path), perform the status transition as a guarded update and verify it actually transitioned before committing:

  ```sql
  UPDATE earn_positions
     SET accrued_total=?, status='redeemed', last_accrual_at=?, redeemed_at=?
   WHERE id=? AND status='active'
  ```

  Check `RowsAffected() == 1`; if it's 0, roll back and return `ErrPositionClosed` so the second concurrent request performs no payout. (A dedicated `RedeemEarn` store method that enforces the `status='active'` predicate is cleaner than overloading the shared `CommitEarnPosting`, which is also used by the accrual path where an unconditional update is fine.) The same guard pattern should be applied to any future state-transition-then-pay flow funded from a reserved pool account.
