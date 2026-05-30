# 04 — Resilience, DR & Operations

[← 03 Security](03-security.md) · Next: [05 — Roadmap & Decisions](05-roadmap-and-decisions.md)

How the system survives failure, how we operate it across two clouds, and what it
costs.

---

## 1. Failure domains

We design for the loss of any one of these, in increasing blast radius:

```
 node  ⊂  availability zone  ⊂  region  ⊂  CLOUD
                              + cross-cutting: a market shard, the ledger, the interconnect
```

Active/active across **two clouds** means the largest planned failure — **losing an
entire cloud** — is survivable. That sets the bar: every critical component has a
counterpart, or a fast-promotable standby, in the other cloud.

---

## 2. RTO / RPO targets

| Tier | RTO (time to recover) | RPO (data loss) | Mechanism |
|------|----------------------|-----------------|-----------|
| Edge / API / authn / web | **~0** (seconds) | 0 | Active/active; GSLB steers away from the failed cloud |
| Market-data / WS fan-out | seconds | 0 | Rebuilt locally from the mirrored log in the surviving cloud |
| **Matching shard (per market)** | **seconds → ~1 min** | **≈0** (sync log) / **sub-second** (async) | Promote hot standby in other cloud from the mirrored command log |
| **Ledger (Option A, distributed SQL)** | seconds (auto) | **0** | Quorum survives a cloud loss; leaseholders move automatically |
| Ledger (Option B, region-primary) | minutes | seconds | Promote DR replica; small RPO unless synchronous |
| Custody signing | minutes (deliberate) | 0 | MPC threshold met by shares in the surviving cloud(s) |
| Whole-cloud outage | minutes for full reconvergence | as above | Shift all primaries + traffic to the surviving cloud |

The **headline**: no lost trades on a single-cloud failure (RPO≈0 on the order
path), with recovery in **seconds to a minute** — because failover is "a standby
that was already replaying the log keeps going", not a restore.

---

## 3. Failover orchestration (and split-brain prevention)

### Stateless tiers — automatic, continuous
GSLB (Route 53 + Tencent DNSPod health checks, Anycast) continuously steers users
to healthy edges. A cloud going unhealthy simply stops receiving new traffic. No
orchestration needed — it's active/active.

### Market shards — promote the standby
A **placement controller** owns the map of `market → primary cloud/region` and
watches health. On primary failure:

1. **Fence the old primary** (critical — prevents two engines matching the same
   market = split-brain). Fencing uses an **epoch/leadership lease**: the
   sequencer's leadership is itself granted by a consensus group, and every command
   carries the **epoch**. A promoted standby starts a **new epoch**; any late
   writes from the old primary (lower epoch) are **rejected by the log**. Two
   primaries can briefly *think* they're primary, but only one can append.
2. **Promote** the standby in the other cloud — it has been replaying the same
   command log, so it's already at (or microseconds behind) head.
3. **Re-point ingress** for that market to the new primary; clients reconnect via
   the standard WS reconnect.

Because the engine is a **deterministic replica of an ordered log** ([01](01-data-and-consistency.md)),
promotion needs **no state transfer** — the heart of fast RTO with RPO≈0.

### Ledger — quorum or promotion
- **Option A:** distributed SQL handles it — a lost cloud loses some voters, the
  quorum re-forms among survivors, leaseholders move. RPO 0.
- **Option B:** promote the DR replica in the surviving cloud (small RPO);
  fence the old primary to avoid dual-write.

### Whole-cloud loss — the big one
The placement controller moves **all** affected market primaries to the surviving
cloud and GSLB sends **all** traffic there. This only works if the surviving cloud
has **capacity headroom** to carry critical load — so we size for **N+1 across
clouds**: each cloud can carry the *critical* markets and degrade non-critical
features (e.g. shed analytics, widen non-core market quoting) rather than fall over.

---

## 4. Networking & connectivity

- **Redundant private interconnect.** At least **two diverse paths/providers**
  (e.g. Megaport + Equinix Fabric, or Direct Connect + Tencent DC into a shared
  colo) so no single link failure partitions the clouds. BGP for fast reroute. The
  link is **encrypted and authenticated** ([03 §4](03-security.md)).
- **GSLB / Anycast** for user steering; health-based and latency-based.
- **Direct Connect / colocation** offerings for latency-sensitive participants into
  each primary region ([02 §2](02-performance-under-load.md)).
- **Interconnect is for async replication + failover only** — never a synchronous
  dependency on the order hot path. If it's severed, each cloud keeps serving the
  markets it's primary for; only cross-region order *entry* degrades, and DR is
  ready if the partition persists.

---

## 5. Observability (one pane over two clouds)

- **Golden signals** (latency, traffic, errors, saturation) per service, per cloud.
- **Per-market SLOs**: order-ack p99, match latency, reject rate, WS delivery lag —
  the metrics that define "fast under load" ([02](02-performance-under-load.md)),
  alerted on.
- **Distributed tracing** (OpenTelemetry) spanning edge → engine → settlement,
  **across clouds**, so a slow cross-region order is visible end to end.
- **Unified metrics & logs**: Prometheus/Grafana (federated across clouds) and
  centralized logging/SIEM ([03 §8](03-security.md)) — operators never have to
  "guess which cloud".
- **Business KPIs & surveillance**: fill rates, fairness, funding/liquidation
  health, plus the **consistency canary** — the periodic ledger double-entry sum
  ([01 §4](01-data-and-consistency.md)) that alerts on *any* drift.
- **Synthetic monitoring**: continuous probe trades and WS subscriptions from
  multiple geographies validate the *user-perceived* path, not just internals.

---

## 6. Cost & FinOps

Two clouds is **not** simply 2× — active/active shares the load — but there are real
multicloud cost drivers to manage:

| Driver | Control |
|--------|---------|
| **Cross-cloud egress** (biggest) | Mirror only Tier A/B/C, **compress** logs, batch, keep traffic on the **private interconnect** (cheaper than public egress), never replicate projections ([01 §5](01-data-and-consistency.md)) |
| Redundancy headroom (N+1) | Right-size; use cheaper capacity for sheddable workloads; reserve only the critical core |
| Commitments | **Savings Plans/Reserved (AWS)** + **Tencent reserved/savings** for steady core; spot/on-demand for burst & non-critical |
| Operational overhead | One IaC + image (§7) limits the people-cost of two clouds |
| Per-cloud visibility | Tag everything; per-cloud + per-service cost dashboards; FinOps review cadence |

**Right-sizing note:** the matching core is small and CPU-bound (a few pinned
hosts per active shard); the *expensive* tiers are WS fan-out and data egress under
load — budget there.

---

## 7. Delivery: IaC, GitOps & progressive rollout

- **One IaC codebase** (Terraform) with **per-cloud provider modules** behind
  shared abstractions (a "region" module renders to AWS or Tencent). Drift and
  policy (OPA) enforced identically ([03 §10](03-security.md)).
- **One container image** to both clouds; **no cloud-specific application code** —
  cloud differences live in config/IaC, not in Go.
- **GitOps** (Argo CD / Flux) reconciles each cluster (EKS/TKE) to git.
- **Progressive delivery**: canary in one cloud → bake against SLOs → promote to
  the other; **automatic rollback** on SLO breach. The matching shards roll with
  care (drain/standby-promote/upgrade/swap-back) to avoid disrupting open books.
- **Config parity tests** in CI assert both clouds render equivalent topology.
- **DR is tested, not assumed**: scheduled **game days** that fail a region/cloud
  *under load* ([02 §7](02-performance-under-load.md)) and measure real RTO/RPO,
  plus **chaos engineering** on the interconnect and engine nodes.

---

## Key takeaways
- Sized to survive **a whole-cloud loss**: order path **RPO≈0**, RTO **seconds→a
  minute**, because failover = **promote a log-replaying standby**, not a restore.
- **Split-brain is prevented by epoch/lease fencing** in the log — only one primary
  can append per market.
- **Redundant, encrypted private interconnect**; it carries replication/failover,
  **never** synchronous hot-path traffic.
- **One pane of glass** (federated metrics, cross-cloud tracing, SIEM, the ledger
  consistency canary, synthetic probes).
- Manage multicloud cost mainly via **egress discipline** and **commitments**; one
  **IaC + image + GitOps** pipeline keeps two clouds operable by one team.
- **Test DR under load** on a schedule — RTO/RPO you haven't exercised are guesses.
