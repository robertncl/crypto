# Nebula — Active/Active Multicloud Architecture (AWS + Tencent Cloud)

> A design plan for running the Nebula trading platform across **AWS** and
> **Tencent Cloud** in an **active/active** topology, optimized for two priorities
> in order: **(1) performance under load** and **(2) security**.

This document set is a **target architecture and migration plan**, not a
description of the current MVP. The MVP (single Go process, in-memory matching
engine, SQLite, one WebSocket hub) is the starting point; here we define what it
must become and how to get there.

## Read in this order

| Doc | Covers |
|-----|--------|
| **This overview** | Goals, the active/active model, global topology, component map |
| [01 — Data & Consistency](01-data-and-consistency.md) | Data tiers, the ledger, ordering, replication, exactly-once |
| [02 — Performance Under Load](02-performance-under-load.md) | Latency budget, matching-engine scaling, fan-out, caching, load testing |
| [03 — Security](03-security.md) | Threat model, edge/DDoS, zero trust, custody/HSM, IAM, compliance |
| [04 — Resilience & Operations](04-resilience-operations.md) | DR/RTO/RPO, failover, networking, observability, CI/CD, cost |
| [05 — Roadmap & Decisions](05-roadmap-and-decisions.md) | Phased migration, assumptions, decisions needed, risks |

---

## 1. Goals, non-goals, constraints

### Priorities (explicit ordering)
1. **Performance under load** — low, predictable latency for the order path and
   market data; graceful behavior at peak (listing events, volatility spikes,
   DDoS-grade traffic).
2. **Security** — exchanges are among the most-attacked systems on the internet;
   custody is existential.
3. Availability/DR, cost, and operational simplicity follow.

### Goals
- **Active/active across two clouds**: both AWS and Tencent serve live production
  traffic simultaneously (not warm-standby), with users routed to the nearest/
  healthiest cloud.
- **Survive the loss of an entire cloud or region** with bounded, defined RTO/RPO.
- **Horizontal scale** of the order path by **market sharding**.
- **No single shared trust domain** across clouds (blast-radius isolation).

### Non-goals
- True single-order-book matching of *the same market* simultaneously in both
  clouds (see §3 — physically impossible within budget; we shard instead).
- Lift-and-shift parity of every managed service; we accept per-cloud equivalents
  behind common abstractions.

### Hard constraints (call these out to stakeholders)
- **Cross-cloud RTT** between AWS and Tencent regions is **~tens of milliseconds**
  even on private interconnect. Any *synchronous* cross-cloud hop on the order
  path is disqualifying. The architecture keeps the **hot path inside one region**
  and uses cross-cloud links only for **async replication and failover**.
- **Regulatory/geo:** crypto trading is restricted or prohibited in several
  jurisdictions, **including mainland China**. Tencent capacity for this workload
  should target **non-mainland regions** (e.g. Singapore, Hong Kong, Jakarta,
  Frankfurt, Tokyo) and the platform must **geofence** restricted jurisdictions.
  Data-residency laws (PIPL, GDPR) shape where PII may live. See [03 §8](03-security.md).
- **Determinism:** the matching engine must be a deterministic state machine so a
  standby can be promoted bit-for-bit from a replicated input log (see [01](01-data-and-consistency.md)).

---

## 2. Design principles

1. **Shard, then replicate.** Scale by splitting markets across single-writer
   engines; replicate each shard's *input log* for failover — never try to
   consensus-match a single market across clouds.
2. **Hot path stays local; cross-cloud is async.** Synchronous coordination never
   crosses a cloud boundary on the order path.
3. **The event log is the source of truth.** Orders and trades are an ordered,
   durable log; every other store (ledger, market data, search, analytics) is a
   deterministic projection of it. This is what makes replication and recovery
   tractable.
4. **Strong consistency only where money settles.** The custody **ledger** is
   strongly consistent; everything else is eventually consistent and cache-served.
5. **Zero trust, defense in depth.** No flat network, mTLS everywhere, least
   privilege, deny-by-default, custody physically and logically segregated.
6. **Identical app, per-cloud substrate.** One container image and one IaC
   codebase render to AWS and Tencent equivalents; no cloud-specific application
   logic.
7. **Everything is a deterministic projection or an idempotent consumer.** Settle
   exactly once, recompute everything else.

---

## 3. The active/active model — what is, and isn't, active/active

This is the crux. We classify every tier by *what active/active means for it*.

```
 TIER                         ACTIVE/ACTIVE MODEL                          WHY
 ───────────────────────────  ──────────────────────────────────────────  ───────────────────────────
 Global traffic / edge        Fully A/A. Anycast + GeoDNS health-steering. Stateless; route to nearest.
 Static / CDN                 Fully A/A. Both clouds' CDNs, same origin.   Cacheable, read-only.
 API gateway / auth           Fully A/A in both clouds.                    Stateless; verify JWT locally.
 Market-data fan-out / WS      Fully A/A. Each cloud fans out from its       Read-only projection of the
   edge                       local copy of the trade event log.          ordered trade log.
 Matching engine (per market) SINGLE-ACTIVE per market ("primary"), with   One ordered book per market is
                              a HOT STANDBY in the other cloud. Different  required for correctness; cross-
                              markets' primaries split across both clouds. cloud consensus is too slow.
 Risk / liquidation (perps)   Co-located with its market's engine.         Needs the same ordered stream.
 Custody ledger (writes)      Strongly consistent. Either (a) distributed  Money must never double-spend.
                              SQL with quorum, or (b) region-primary per
                              account-shard. Writes are local-region.
 Ledger reads / balances      A/A read replicas + cache in both clouds.    Reads tolerate small staleness.
 Custody keys / signing       Segregated HSM/MPC; NOT shared across clouds.Blast-radius isolation.
```

The key insight: **the *platform* is active/active even though each *market* is
single-active.** Half the market shards run primary in AWS, half in Tencent; both
clouds carry real trading load, each is the hot standby for the other, and a user
in any region hits a local edge that routes their order to the (possibly remote)
*primary region for that specific market* — but that routing decision and the
match happen without a synchronous cross-cloud hop on the critical settle path
(see [02](02-performance-under-load.md) for how).

> **Analogy:** think of it like sharded primaries in a distributed database —
> active/active at the cluster level, single-writer per shard. We apply the same
> proven pattern to markets.

### Why not consensus-match a single market across clouds?

A correct order book requires a **total order** of events (price-time priority,
Chapter 5 of the course). Achieving total order across two clouds needs a
consensus round trip (Raft/Paxos) per event → **tens of ms** of added latency and
a hard dependency on the cross-cloud link for *every match*. That fails priority
#1 and reduces availability (link loss halts matching). Sharding sidesteps it
entirely: each market is ordered by a single local sequencer, replicated async.

---

## 4. Global topology

```
                ┌──────────────── GLOBAL TRAFFIC & EDGE SECURITY (Active/Active) ───────────────┐
   Users  ───►  │  GeoDNS + health steering (Route 53 ⇄ Tencent DNSPod) · Anycast               │
                │  DDoS scrubbing (AWS Shield Adv + Tencent Anti-DDoS Pro) · WAF · Bot mgmt · TLS │
                └─────────────┬──────────────────────────────────────────────┬──────────────────┘
                  NA / EU ◄───┘                                              └───► APAC
            ┌─────────────────▼─────────────────┐          ┌─────────────────▼─────────────────┐
            │  AWS  (e.g. us-east-1, eu-west-1)  │          │  Tencent (e.g. ap-singapore, hk)   │
            │ ┌── Edge / Stateless (A/A) ───────┐│          │ ┌── Edge / Stateless (A/A) ───────┐│
            │ │ CloudFront · API GW · authn      ││          │ │ EdgeOne · API GW · authn         ││
            │ │ WebSocket fan-out fleet          ││          │ │ WebSocket fan-out fleet          ││
            │ └──────────────┬──────────────────┘│          │ └──────────────┬──────────────────┘│
            │ ┌── Stateful core ────────────────┐│          │ ┌── Stateful core ────────────────┐│
            │ │ Sequencers + Matching shards     ││          │ │ Sequencers + Matching shards     ││
            │ │   PRIMARY: market set X           ││  async   │ │   PRIMARY: market set Y           ││
            │ │   STANDBY: market set Y  ◄────────┼┼──mirror──┼┼─►  STANDBY: market set X           ││
            │ │ Perps risk + liquidation engine  ││  (log)   │ │ Perps risk + liquidation engine  ││
            │ └──────────────┬──────────────────┘│          │ └──────────────┬──────────────────┘│
            │ ┌── Data ──────▼──────────────────┐│          │ ┌── Data ──────▼──────────────────┐│
            │ │ Event log (MSK)  ◄── mirror ─────┼┼──────────┼┼─► Event log (CKafka)             ││
            │ │ Cache (ElastiCache) · S3         ││          │ │ Cache (Redis/Tendis) · COS       ││
            │ └─────────────────────────────────┘│          │ └─────────────────────────────────┘│
            │            Ledger: globally-consistent distributed SQL OR region-primary shards     │
            │            (quorum members in BOTH clouds — see 01-data-and-consistency)            │
            └──────────────┬────────────────────┘          └────────────────┬───────────────────┘
                           │  redundant private interconnect (Megaport/Equinix Fabric)            │
                           └──────────────────────────────────────────────────────────────────────┘
            ┌──────────────────────────────────── CUSTODY (segregated trust domain) ──────────────┐
            │  Hot wallet signers in HSM/MPC (per cloud) · Cold storage offline · withdrawal       │
            │  approval workflow · separate accounts/VPCs, no app-tier credentials reach here      │
            └──────────────────────────────────────────────────────────────────────────────────────┘
```

---

## 5. Component map: MVP → target

| Nebula component (today) | Target form | AWS | Tencent |
|--------------------------|-------------|-----|---------|
| `engine` actor (`cmds chan func()`) | Deterministic state machine consuming an **ordered command log**; one primary + cross-cloud standby per market shard | EKS/dedicated + MSK | TKE/CVM + CKafka |
| `derivatives` engine + funding/liq monitors | Co-located with market shard; risk engine consumes same log | " | " |
| `store` (SQLite) + double-entry ledger | **Strongly-consistent distributed SQL**; settlement = idempotent projection of trade log | Aurora PostgreSQL / distributed SQL | TDSQL-C / distributed SQL |
| `ws` hub (in-proc) | Horizontally-scaled **WebSocket edge fleet**, subscribes to local market-data bus | API GW WebSocket / NLB + fleet | API GW / CLB + fleet |
| `market` service (tickers/candles) | Stream processors projecting the trade log into Redis + object storage | MSK + Lambda/Flink | CKafka + Oceanus/Flink |
| `wallet` (simulated custody) | Real custody service in segregated domain (HSM/MPC, hot/cold) | CloudHSM + KMS | Cloud HSM + KMS |
| `auth` (JWT/bcrypt) | Stateless authn at edge in both clouds; central IdP for staff | Cognito/IdP | CAM/IdP |
| `api` (chi REST) | Stateless API tier, A/A, behind WAF | EKS | TKE |
| `web` (React SPA) | Static, multi-CDN | CloudFront + S3 | EdgeOne + COS |
| `bot` (market maker) | Out of scope for prod; replaced by real liquidity providers via the same order API | — | — |

The application code barely changes shape: the engine still processes commands
one at a time per market — it just **consumes them from a replicated ordered log
instead of an in-process channel**, which is what makes a hot standby in the other
cloud possible.

---

## 6. The one-paragraph summary (for an exec)

We run Nebula in both AWS and Tencent Cloud at once. Stateless tiers — the website,
APIs, login, and market-data streaming — are **fully active/active** in both
clouds, so users are served from the nearest, fastest, healthiest location and the
loss of either cloud is invisible to them. The **matching engines are sharded by
market**: each market trades on a single engine for correctness and speed, but
those engines are **split across both clouds** (so both clouds do real work) and
each continuously **mirrors its trade log to a hot standby in the other cloud**,
giving near-instant failover with no lost trades. **Money settlement** runs on a
**strongly-consistent database that spans both clouds**, so balances can never
double-spend. **Custody keys** live in **hardware security modules, segregated**
from everything else and never shared between clouds. The order path **never makes
a synchronous call across clouds**, which is what keeps it fast under load. Read
on for the latency budget, the security controls, and the failure behavior.
