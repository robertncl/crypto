# 05 — Roadmap, Assumptions & Decisions

[← 04 Resilience & Ops](04-resilience-operations.md) · [Overview](README.md)

How to get from today's MVP to the target, what we assumed, and what stakeholders
must decide.

---

## 1. Assumptions (validate these)

This design assumes, until told otherwise:

- **Scale:** design point ~**100k+ orders/sec** aggregate, **millions** of
  concurrent WebSocket connections at peak, **10×+** average→peak burst. (Re-size
  if your numbers differ — it changes shard counts and fleet sizing, not the
  shape.)
- **Latency SLO:** in-region order ack **p99 ≤ 5 ms**; cross-region adds one WAN
  RTT, accepted for distant retail, mitigated by colocation for HFT
  ([02](02-performance-under-load.md)).
- **Regions:** AWS for NA/EU primacy; **Tencent in non-mainland APAC** (Singapore/
  HK/Jakarta/Tokyo) for regulatory reasons ([03 §9](03-security.md)).
- **Regulatory:** the operating entity is licensed where it serves; restricted
  jurisdictions (incl. mainland China) are **geofenced**.
- **Custody:** real custody is **build-with-vendors** (HSM + MPC), not pure
  in-house, at least initially.

---

## 2. Phased migration

Each phase is independently valuable and has explicit **exit criteria**. Don't
start a phase until the previous one's criteria are met.

### Phase 0 — Productionize on one cloud (AWS), one region
*Goal: a secure, observable, load-tested single-region system.*
- Replace **SQLite → managed Postgres** (Aurora) for the ledger; keep the
  double-entry primitives (`ApplyPostings`/`CommitFill`/`CommitPerp`) unchanged.
- Introduce the **event log** (MSK) and make the engine **consume an ordered
  command log** instead of an in-process channel; **remove hot-path nondeterminism**
  (`time.Now()`, `uuid`) per [01 §2](01-data-and-consistency.md).
- Split deployables: **edge/API**, **matching**, **market-data/WS**, **wallet**,
  **settlement** — same code, separate scaling units. Containerize; EKS + IaC.
- Baseline security: WAF + Shield, mTLS mesh, KMS, workload identity, SIEM,
  CI scanning + signed images ([03](03-security.md)).
- **Exit:** meets the latency SLO under replay load in one region; security
  baseline passed; observability + the ledger consistency canary live.

### Phase 1 — Multi-region within AWS
*Goal: survive a region loss; prove sharding + standby promotion.*
- **Shard markets**; one primary + a **hot standby in a second AWS region** per
  shard, fed by the mirrored command log. Ledger HA across AZs/regions.
- Placement controller + **epoch fencing** ([04 §3](04-resilience-operations.md)).
- **Exit:** kill an AWS region under load → bounded RTO, RPO≈0, no lost trades.

### Phase 2 — Add Tencent: active/active edge + DR standby
*Goal: both clouds live for stateless/market-data; cross-cloud failover for the
core.*
- Stand up Tencent (TKE, CKafka, edge, WS fan-out, CDN) **active/active** for
  edge/auth/market-data, serving APAC.
- **Mirror command logs cross-cloud**; Tencent hosts **hot standbys** for AWS
  market shards. Ledger: **region-primary per cloud (Option B)**.
- Private **interconnect**, **GSLB**, **geofencing**, per-cloud security parity
  (policy-as-code in both).
- **Exit:** APAC served locally; fail AWS→Tencent for stateless invisibly;
  cross-cloud standby promotion tested under load.

### Phase 3 — True active/active matching + global ledger
*Goal: both clouds match real markets; whole-cloud loss survivable.*
- **Split market primaries across both clouds** (locality-aware placement).
- Migrate the ledger to **distributed SQL with a cross-cloud quorum (Option A)**,
  leaseholders pinned to home regions ([01 §4](01-data-and-consistency.md)).
- **Exit:** whole-cloud failover under load within target RTO/RPO; both clouds
  carry production matching load.

### Phase 4 — Custody & compliance hardening
*Goal: audited, production-grade custody and controls.*
- **HSM/MPC** with shares distributed across clouds; cold-storage ops; withdrawal
  controls, allowlists, approvals, anomaly detection; **proof of reserves**.
- **Market surveillance**, **red-team**, **SOC 2 / ISO 27001**, full KYC/AML/
  Travel-Rule stack.
- **Exit:** external audit passed; custody controls exercised; surveillance live.

```
 Phase 0 ──► Phase 1 ──► Phase 2 ──► Phase 3 ──► Phase 4
 1 cloud     +region     +Tencent    A/A match   custody &
 hardened    DR          A/A edge    global      compliance
                         + DR core   ledger
```

---

## 3. Decisions needed (stakeholder input)

| # | Decision | Why it matters | Default if unspecified |
|---|----------|----------------|------------------------|
| D1 | Target scale & latency SLOs | Sizes shards, fleets, headroom | §1 assumptions |
| D2 | Operating jurisdictions & licensing | Drives geofencing, regions, residency | License where served; geofence the rest |
| D3 | Tencent regions | Regulatory + latency | Non-mainland APAC (SG/HK/JK/TYO) |
| D4 | Ledger: Option A vs B, and *when* | Strong-consistency vs operational simplicity | B in Phase 2, A in Phase 3 |
| D5 | Distributed SQL engine (A) | CockroachDB / YugabyteDB / TiDB — ops & licensing | Evaluate all three in Phase 2 |
| D6 | Custody: buy (Fireblocks-class) vs self-MPC | Time-to-market vs control/cost | Vendor MPC first, revisit |
| D7 | Managed vs self-managed Kafka / mesh / observability | Ops burden vs control/cost | Managed where each cloud offers parity |
| D8 | Cross-region order-entry SLA for distant retail | Sets colocation/placement investment | Accept +1 WAN RTT, optimize placement |

---

## 4. Risk register

| Risk | Likelihood | Impact | Mitigation |
|------|-----------|--------|-----------|
| Cross-cloud latency breaks an over-ambitious SLA | Med | High | Hot path stays in-region; market data local; colocation; set SLOs honestly (D1/D8) |
| Engine nondeterminism → standby diverges | Med | High | Remove `time.Now()`/`uuid` from hot path; **replay-equivalence tests** in CI ([01 §2](01-data-and-consistency.md)) |
| Split-brain (two primaries per market) | Low | Critical | Epoch/lease **fencing** in the log; the log rejects stale epochs ([04 §3](04-resilience-operations.md)) |
| Distributed-SQL write latency under cross-cloud quorum | Med | Med | Leaseholder locality (home-region pinning); start with Option B |
| Custody compromise | Low | Critical | HSM/MPC, shares across clouds, cold majority, withdrawal controls ([03 §5](03-security.md)) |
| DDoS during volatility | High | High | Multi-cloud DDoS + WAF + CDN offload; fail attacked cloud away ([03 §3](03-security.md)) |
| Cloud-service parity gaps (AWS vs Tencent) | High | Med | Abstractions in IaC; per-cloud security reviews; test both ([03 §10](03-security.md)) |
| Cross-cloud egress cost blowout | Med | Med | Replicate only A/B/C, compress, private interconnect, no projection replication ([04 §6](04-resilience-operations.md)) |
| Operational complexity of two clouds | High | Med | One IaC + image + GitOps; strong observability; game days |
| Regulatory change (jurisdiction bans) | Med | High | Geofencing + multi-region flexibility to exit a market quickly |

---

## 5. Decision log (the big calls, and why)

- **ADR-1: Shard markets; don't consensus-match across clouds.** Total-order
  matching across clouds costs tens of ms and couples availability to the link.
  Sharding gives active/active utilization with single-writer correctness and
  microsecond matching. *(Overview §3, [02](02-performance-under-load.md).)*
- **ADR-2: The event log is the source of truth; engines are deterministic
  replicas.** Enables zero-state-transfer failover and full auditability/replay.
  *([01](01-data-and-consistency.md).)*
- **ADR-3: Hot path never makes a synchronous cross-cloud call.** The interconnect
  carries async replication and one-hop forwarding only. *(Overview §1, [02](02-performance-under-load.md).)*
- **ADR-4: Strong consistency only for the ledger.** Everything else is an eventual
  projection recomputed locally per cloud. *([01](01-data-and-consistency.md).)*
- **ADR-5: Custody is a segregated trust domain with no shared cross-cloud root;**
  MPC shares are distributed across clouds. Multicloud as a *security* asset.
  *([03 §5,§10](03-security.md).)*
- **ADR-6: One image + one IaC; cloud differences live in config, not code.** Keeps
  two clouds operable and prevents posture drift. *([04 §7](04-resilience-operations.md).)*

---

## Where this connects back to the codebase
The MVP already embodies the two ideas this whole design leans on: a **single-writer
matching actor** (which becomes a deterministic log consumer) and a **double-entry
ledger funneled through one atomic primitive** (which becomes the strongly-
consistent settlement sink). The multicloud target is mostly about **replacing the
substrate** (channel→log, SQLite→distributed SQL, in-proc hub→edge fleet) — not
rewriting the trading logic. That continuity is by design, and it's why this is an
evolution, not a rewrite.
