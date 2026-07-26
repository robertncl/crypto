# Security Review

**Scope note:** The branch diff is empty — the Earn feature was already committed (`ae3ffb5 add earn`) and the working tree is clean. I reviewed the committed Earn feature (the substantial new code from this session) plus the core security surfaces it touches (auth/JWT, the ledger `ApplyPostings` primitive, and handler authorization). Auth (HS256 with algorithm-confusion guard, bcrypt, expiry enforced), SQL access (fully parameterized), and authorization (`auth.UserID` from JWT everywhere; ownership checks on cancel/redeem) are sound. One real vulnerability was found.

---

## Remediation status (updated)

A follow-up whole-codebase review confirmed **two** vulnerabilities via live exploits, and both are now **FIXED** on branch `claude/security-fixes` with regression tests:

- **Vuln 1 — Earn double-redeem (High): FIXED.** `Store.RedeemEarn`/`AccrueEarn` now perform the status transition as a guarded `UPDATE ... WHERE status='active'` inside the payout transaction and roll back (`ErrNotActive`) if it affects zero rows. Covered by `internal/store/earn_test.go` (idempotent redeem, concurrent-redeem-pays-once, accrual-skips-redeemed).
- **Vuln 2 — WebSocket topic authorization bypass (Critical): FIXED.** `resolveTopic` now rejects any client-supplied topic whose prefix is a private channel (e.g. `balances:42`); private streams are reachable only via the bare name scoped to the socket's own user id. A per-connection subscription cap was also added. Covered by `internal/ws/hub_test.go`.

See **Vuln 2** below for the WebSocket finding details.

---

## Vuln 1: Double-Redeem via TOCTOU on Earn Position Status — `internal/earn/earn.go` (Redeem) + `internal/store/earn.go` (CommitEarnPosting/updateEarnPositionTx) — **FIXED**

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

* **Fix applied:** `Store.RedeemEarn` performs `UPDATE earn_positions SET ... status='redeemed' WHERE id=? AND status='active'` in the same transaction as the payout and returns `store.ErrNotActive` (rolling everything back) when `RowsAffected() != 1`; `earn.Redeem` maps that to `ErrPositionClosed`. The accrual path (`Store.AccrueEarn`) got the same `WHERE status='active'` guard so a concurrent redeem can neither be paid interest nor resurrect a redeemed position. Verified: pre-fix, hammering concurrent redeems minted up to 20,000 USDT from a 5,000 principal; post-fix, exactly one redeem succeeds and the balance is unchanged.

---

## Vuln 2: WebSocket Topic Authorization Bypass — `internal/ws/client.go` (resolveTopic) + `internal/ws/hub.go` (subscribe) — **FIXED**

* **Severity: Critical**
* **Category:** `broken_access_control` / IDOR (missing authorization on subscription)
* **Confidence:** 10/10 (proven live)

* **Description:**
  Private streams are published to user-scoped hub topics (`balances:<id>`, `orders:<id>`, `walletTxns:<id>`, `positions:<id>`, `perpOrders:<id>`, `earnPositions:<id>`). The subscription guard only checked the **bare** channel name against the private set:

  ```go
  if privateChannels[channel] { ... return channel + ":" + userID, true }
  return channel, true   // "balances:42" isn't the bare name → passed through verbatim
  ```

  So a client could subscribe directly to the already-suffixed topic (`balances:42`) and the hub — which does no authorization of its own — routed that user's private stream to the connection. User ids are sequential integers, `handleWS` allows unauthenticated connections (`userID = 0`), there was no per-connection subscription cap, and `CheckOrigin` allows all origins.

* **Exploit Scenario (proven live):**
  A WebSocket client with **no token at all** subscribed to `balances:<victim>`, `orders:<victim>`, `walletTxns:<victim>`; when the victim placed an order the attacker received the victim's full order and balance:

  ```
  UNAUTH ATTACKER RECEIVED: {"channel":"orders:27","data":{...}}
  UNAUTH ATTACKER RECEIVED: {"channel":"balances:27","data":[{"asset":"USDT","available":"9980","locked":"20"}]}
  ```

  Enumerating ids lets one unauthenticated socket eavesdrop on every user's balances, orders, positions, and wallet transactions (including deposit addresses/txids) in real time.

* **Fix applied:** `resolveTopic` now rejects any channel whose prefix (before the first `:`) is a private channel name, authenticated or not — private streams are reachable only by subscribing to the bare name, which the server scopes to the socket's own `userID`. Legitimate scoped **public** channels (`ticker:…`, `depth:…`, `trades:…`, `kline:…`, `funding:…`) are unaffected. A per-connection subscription cap (`maxSubscriptions`) bounds memory. Regression tests in `internal/ws/hub_test.go` assert scoped private topics are rejected for both unauthenticated and cross-user cases while bare-name scoping and public channels still work; verified live that post-fix the attacker receives nothing and legitimate own-stream subscription still works.

---

## Informational (demo-by-design — gate before any real deployment)

* The simulated deposit endpoint (`POST /api/wallet/deposit`) credits arbitrary self-funds, and registration grants a 10,000 USDT welcome bonus. Both are intentional for the demo but must be removed/gated for production.
