# 01 — Data Architecture & Consistency

[← Overview](README.md) · Next: [02 — Performance](02-performance-under-load.md)

The active/active model in the overview only works if the data layer is designed
for it. This doc defines **what data exists, how strongly it's consistent, and how
it replicates across clouds.**

---

## 1. Classify every piece of state

Not all data needs the same guarantees. Forcing global strong consistency on
everything would destroy performance; allowing it nowhere would lose money. We
put each dataset in exactly one tier:

| Tier | Guarantee | Examples | Store | Cross-cloud |
|------|-----------|----------|-------|-------------|
| **A. Ordered log (source of truth)** | Total order per market, durable | order commands, trades/fills, funding & liquidation events | Kafka (MSK / CKafka) | **async mirror**, per-shard |
| **B. Strongly consistent (money)** | Linearizable, no double-spend | balances, ledger entries, positions, withdrawals | Distributed SQL (quorum) | **synchronous quorum** (writes local-region, see §4) |
| **C. Regional authoritative** | Consistent within a region | open-orders index, KYC status, API keys, sessions | Per-region SQL/replica | async replica for DR |
| **D. Eventual / projection** | Recompute from the log | tickers, candles, depth, trade tape, search | Redis + object storage | independently rebuilt in each cloud |
| **E. Edge cache / static** | Best-effort, TTL'd | SPA assets, public market snapshots | CDN POPs | per-cloud CDN |

**Rule of thumb:** if losing or reordering it can move money incorrectly, it's
Tier A or B. If you can recompute it by replaying the log, it's Tier D.

---

## 2. The event log is the spine

Everything hangs off an **append-only, totally-ordered log per market**:

```
        order/cancel commands                      projections (Tier D, per cloud)
 client ──► API ──► Sequencer ──► [ Kafka topic: orders.BTC-USDT ] ──┬─► market-data (ticks/candles/depth)
                      (assigns                                        ├─► WebSocket fan-out
                       monotonic seq#                                 ├─► search / analytics / surveillance
                       + authoritative                               └─► settlement → Ledger (Tier B)
                       timestamp)
                              │
                              └─► Matching engine (primary)  ──► [ Kafka topic: trades.BTC-USDT ]
                                  Matching engine (standby, other cloud) consumes the SAME command log
```

Why this shape:
- **One writer, one order.** The per-market **sequencer** is the only thing that
  assigns sequence numbers, giving the total order price-time priority needs.
- **Deterministic replay.** Both the primary and the standby consume the identical
  ordered command log, so they reach **identical state**. Promotion = "standby
  keeps reading; now it also serves." No state transfer.
- **Everything downstream is a consumer.** Settlement, market data, surveillance,
  and analytics are all independent, **idempotent** consumers of the trade log. We
  can add or rebuild any of them by replaying.

This is the classic exchange/LMAX pattern, and it's the single most important
architectural decision for both performance and recoverability.

### What this requires of our code (determinism)

Nebula's engine is *already* a single-writer actor (`cmds chan func()`), which is
90% of the way there. To make it a **deterministic** state machine we must remove
hidden nondeterminism so primary and standby agree exactly:

- **Time:** today the engine calls `time.Now().Unix()` (e.g. trade/order
  timestamps). Replace with the **sequencer-stamped time carried in the command**,
  so every replica uses the same value.
- **IDs:** `uuid.NewString()` for order/trade ids is nondeterministic. Derive ids
  from `(market, sequence#)` or have the sequencer assign them.
- **Iteration order / maps:** never let Go map iteration affect outputs on the hot
  path (the book uses ordered structures already — keep it that way).

After this change, "replay the command log" reproduces the exact same trades,
which is what makes cross-cloud standbys and audits trustworthy.

---

## 3. Sharding markets

Each market (`BTC-USDT`, `ETH-PERP`, …) is an independent shard: its own
sequencer, command topic, engine primary, standby, and trade topic. Benefits:

- **Horizontal scale** — busy markets get dedicated engine instances/hosts; quiet
  ones are packed together.
- **Independent failure** — a problem in one market never touches another.
- **Active/active utilization** — assign roughly half the markets' primaries to
  AWS and half to Tencent. A *placement controller* owns this map and rebalances
  on failure (see [04](04-resilience-operations.md)).

The ledger (Tier B) is **not** sharded by market — it's sharded by **account**
(more precisely, settlement is serialized per account), because a single user's
balance is touched by many markets. See §4.

---

## 4. The custody ledger — the hard part

The ledger holds balances, `ledger_entries`, positions, and withdrawals (Chapters
3, 6, 9–10 of the course). It must be **linearizable**: a balance can never be
spent twice, even across clouds. Two viable designs:

### Option A — Distributed SQL with a quorum spanning both clouds (recommended)

Run a Raft/Paxos-based distributed SQL engine (**CockroachDB**, **YugabyteDB**, or
**TiDB** — all used in fintech, all multi-cloud capable) with replicas in **AWS and
Tencent**. Place **3 or 5 voting replicas across ≥3 failure domains** so a quorum
survives losing any one cloud.

- **Pros:** one logical ledger, automatic failover, strong consistency, no custom
  conflict logic.
- **Cons:** a write commits only after a quorum acks. If the quorum spans clouds,
  some commits pay cross-cloud RTT. **Mitigation:** use the engine's *table/range
  locality* features to **pin each account's leaseholder to its home region**, so
  the *common* case (user trades in their home region) commits locally; only the
  rare cross-region case pays the WAN. With 5 replicas (e.g. 3 AWS + 2 Tencent, or
  geographically: 2+2+1) a region can fail without losing quorum.

```
   Ledger range for accounts [home=AWS]  ── leaseholder in AWS ── local-quorum commit (fast)
   Ledger range for accounts [home=TC ]  ── leaseholder in TC  ── local-quorum commit (fast)
   A full-cloud outage moves leaseholders to surviving voters automatically.
```

### Option B — Region-primary per account shard (simpler infra, more app logic)

Each account belongs to a **home region**; all of that account's ledger writes go
to that region's primary Postgres (Aurora / TDSQL-C), replicated async to the
other cloud for DR. Cross-region trades settle via a **two-phase, log-driven**
flow.

- **Pros:** boring, well-understood managed Postgres per cloud; lowest latency for
  home-region activity.
- **Cons:** cross-region settlement needs careful orchestration; failover of a
  region's primary has a small RPO unless you add synchronous replication.

> **Recommendation:** start with **Option B per cloud** during early multicloud
> (Phase 2, [05](05-roadmap-and-decisions.md)) because it's operationally simple,
> and move the ledger to **Option A** when true cross-cloud strong consistency is
> required (Phase 3). Both are driven by the same trade log, so the migration is a
> change of sink, not of business logic.

### Settlement is exactly-once and idempotent

Settlement consumes the **trade log** and applies postings (Nebula's
`ApplyPostings`/`CommitFill`/`CommitPerp`, Chapter 3). To be safe under retries,
replays, and failover:

- Every trade has a **stable id** (from §2). Settlement records "applied trade
  ids" and **ignores duplicates** — so replaying the log re-settles nothing.
- Postings for one trade commit in **one ledger transaction** (already true in our
  code). On distributed SQL that transaction is linearizable; on region-primary it
  runs in the account's home region.
- The double-entry invariant (postings sum to zero, no negative balances) is the
  built-in audit: a periodic job sums `ledger_entries` per asset and **alerts on
  any nonzero drift** — your canary that consistency is intact.

> Custody **withdrawals** are settled the same way but additionally gated by the
> security workflow in [03 §5](03-security.md) (approvals, allowlists, HSM signing)
> — they leave the ledger boundary, so they get extra controls.

---

## 5. Cross-cloud replication topology

| What | Mechanism | Direction | Lag target |
|------|-----------|-----------|-----------|
| Command & trade logs (Tier A) | Kafka geo-replication (MirrorMaker 2 / Cluster Linking / Connect) | per-shard, primary→standby cloud | < 50 ms typical; bounded |
| Ledger (Tier B, Option A) | Distributed-SQL native Raft | quorum across clouds | synchronous (quorum) |
| Ledger (Tier B, Option B) | Postgres logical/physical replication | home→DR cloud | seconds (async) |
| Regional data (Tier C) | Async replica | per-region→DR | seconds |
| Projections (Tier D) | **None** — rebuilt locally from the mirrored log | n/a | self-healing |
| Objects (Tier E) | S3↔COS replication (or rebuild) | bi-di | minutes |

**Egress matters.** Cross-cloud replication is the main driver of inter-cloud data
transfer cost. Mitigate by mirroring only Tier A/B/C (not projections), enabling
**compression** on Kafka topics, batching, and keeping replication on the private
interconnect (cheaper + faster than public egress). See [04 §6](04-resilience-operations.md).

---

## 6. Consistency on the read path

Reads must be fast and are allowed to be slightly stale **except** when a user is
about to spend money:

- **Balances/positions display** — served from a regional **read replica + Redis**
  cache; eventual consistency is fine for showing a number.
- **Order placement fund check** — the *authoritative* check happens **inside the
  settlement/engine path** against Tier B, not against the cache. The UI may show a
  cached balance, but the lock (`ApplyPostings`, Chapter 6) is linearizable and
  will reject an over-spend regardless of what the cache showed. **Never gate a
  debit on a cached read.**
- **Market data** — always from local Tier D projections; identical in both clouds
  because both replay the same trade log.

---

## 7. Data residency & PII

PII (KYC documents, names, addresses) is governed by **PIPL (China), GDPR (EU)**,
and others. Keep it in **Tier C**, stored in the user's jurisdictional region, and
**tokenize/reference** it from the trading path so the hot systems never carry raw
PII. Custody and trading data (balances, trades) are pseudonymous (account ids)
and can replicate more freely. This separation also shrinks the security blast
radius (see [03](03-security.md)).

---

## Key decisions captured here
- **Event log per market = source of truth**; engines are deterministic replicas
  of it (requires removing `time.Now()`/`uuid` nondeterminism from the hot path).
- **Markets sharded** (primaries split across clouds); **ledger sharded by
  account**, strongly consistent.
- **Ledger:** region-primary per cloud first, distributed-SQL quorum later — same
  log-driven settlement either way.
- **Settlement is idempotent**; the double-entry sum is the consistency canary.
- **Projections are never replicated — they're recomputed locally**, which makes
  each cloud self-healing.
