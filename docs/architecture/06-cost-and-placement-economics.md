# 06 — Cost Economics & Cost-Aware Placement (Tencent-advantaged)

[← 05 Roadmap](05-roadmap-and-decisions.md) · [Overview](README.md)

Cost is now a first-class design input. **Tencent Cloud is the cost-advantaged
provider**, so this document defines how we exploit that advantage **without
compromising the two higher priorities** — performance and security.

---

## 1. Where cost sits in the priority stack

```
  Performance (latency under load)  ── HARD CONSTRAINT (must meet the SLO)
  Security / compliance / residency ── HARD CONSTRAINT (non-negotiable)
  ────────────────────────────────────────────────────────────────────
  Cost                              ── OBJECTIVE: minimize WITHIN the constraints above
```

The rule that follows: **place each workload on the cheapest cloud that still
satisfies the user-latency SLO, the data-residency/geofencing rules, and its
security tier.** Because Tencent is cheaper, that rule **systematically tilts
placement toward Tencent** — but never at the expense of a latency or compliance
violation. The result is an **asymmetric, Tencent-weighted active/active** topology
rather than a 50/50 split.

> This refines the symmetric model in the [Overview](README.md): both clouds are
> still active, but **Tencent carries the larger, cost-sensitive share** of
> steady-state load, and **AWS provides NA/EU reach plus resilient counterpart
> capacity.**

---

## 2. Tencent's cost advantages — and the limits

**Where Tencent wins (treat ratios as planning assumptions to validate with real
quotes — they vary by region, commitment, and negotiation):**

| Cost area | Why Tencent helps | Impact on Nebula |
|-----------|-------------------|------------------|
| **Compute** (CVM / TKE) | Lower $/vCPU·hr, especially in APAC | Cheaper matching hosts, WS fan-out fleet, data processing |
| **Bandwidth / egress** | Notably cheaper outbound + bandwidth packages | **Biggest lever** — egress dominates an exchange's bill (§4) |
| **Managed services** (CKafka, TDSQL-C, Tendis, COS, CLB) | Lower than MSK / Aurora / ElastiCache / S3 / NLB | Cheaper logs, cache, object storage, DB |
| **APAC density + peering** | Best latency *and* lowest cost for the region with much of crypto volume | APAC is a cost *and* performance win — no trade-off |
| **Purchasing** | Monthly-subscription / reserved + spot | Commit the Tencent-heavy baseline cheaply (§6) |

**Where AWS is still required (cost must not override the constraints):**

- **NA / EU low-latency edges and the market primaries for those users** — Tencent's
  international footprint there is thinner; forcing NA users onto Tencent Singapore
  to save money would blow the latency budget (priority #1). AWS serves NA/EU.
- **Breadth of managed services, certain compliance attestations, and enterprise/
  counterparty trust** that some institutional flows or regulators expect.
- **Resilient counterpart capacity** so the loss of Tencent is survivable.

So the division of labor is: **Tencent = cost-efficient bulk + APAC primacy;
AWS = global reach + premium resilience.**

---

## 3. The cost-weighted placement map

Apply the §1 rule workload by workload:

| Workload | Cheapest viable cloud | Constraint that decides it |
|----------|----------------------|----------------------------|
| APAC edge / API / WS fan-out | **Tencent** | Cheapest **and** lowest latency for APAC — pure win |
| APAC-popular market primaries (e.g. many USDT pairs, regional perps) | **Tencent** | Locality + cost; standby on AWS |
| Bulk WebSocket fan-out fleet (millions of conns) | **Tencent** (per region) | Compute + egress heavy → biggest savings (§4) |
| Market-data projections, candles, analytics, **surveillance** | **Tencent** | Latency-tolerant, compute-heavy → cost-optimize |
| Log/object storage, dev/staging, **CI runners**, batch | **Tencent** | Not latency-critical; cheapest wins |
| NA/EU edge / API / WS fan-out | **AWS** | Latency to NA/EU users (hard constraint) |
| NA/EU market primaries | **AWS** | Locality; standby on Tencent |
| **Ledger** (strongly consistent) | **Both** (quorum) | Spans clouds; lease­holders pinned home-region ([01 §4](01-data-and-consistency.md)) |
| **Custody** (HSM/MPC, cold) | **Both, segregated** | Security/blast-radius, **not** cost ([03 §5](03-security.md)) |

**Egress gravity principle:** the heaviest sustained outbound traffic (market-data
fan-out to clients) should originate from the **cheaper** cloud for each region —
which, combined with Tencent's APAC strength, means **the single largest bandwidth
consumer runs predominantly on Tencent**.

---

## 4. The dominant cost is egress — optimize it first

For an exchange, **outbound bandwidth, not compute, is the largest variable cost.**
A single trade fans out to thousands of WebSocket subscribers, and traffic spikes
**10×+** during volatility (Chapter 7, [02](02-performance-under-load.md)). The cost
strategy is therefore mostly an **egress strategy**:

1. **Run the fan-out fleet where egress is cheapest** — Tencent, especially for
   APAC (the bulk of connections).
2. **Offload to the CDN.** Serve the static SPA and cacheable public snapshots from
   CDN POPs (CloudFront / EdgeOne), so origin egress is minimized; only live deltas
   leave the origin.
3. **Send fewer bytes.** Conflation (collapse rapid depth/ticker updates),
   **binary frames + compression**, and per-topic snapshotting cut fan-out volume
   directly ([02 §4](02-performance-under-load.md)) — a performance *and* cost win.
4. **Minimize cross-cloud transfer.** Replicate only Tier A/B/C (never projections —
   rebuild them locally), **compress** logs, and keep replication on the **private
   interconnect**, which is cheaper than public-internet egress ([01 §5](01-data-and-consistency.md)).
5. **Avoid cross-AZ chatter.** Co-locate chatty services in the same AZ to dodge
   intra-cloud cross-AZ transfer charges.

> Item 3 is the clearest example of the whole philosophy: the same techniques that
> keep the system fast under load (conflation, compression) are the ones that cut
> the bandwidth bill. **Performance and cost are aligned here, not in tension.**

---

## 5. Cost-aware resilience — don't pay 2× for idle DR

The Tencent-weighting creates a tension: if Tencent carries most active load, **AWS
must be able to absorb Tencent's critical markets on failover** — but paying for
always-on, full-size idle capacity on AWS would erase the savings. Resolve it with
**tiered DR capacity**:

| Tier | Steady-state footprint in the counterpart cloud | On failover |
|------|--------------------------------------------------|-------------|
| **Critical core** (top-market matching, ledger, custody) | **Warm** — the standby is *already* a cheap **log consumer** (reading a stream, not serving), on a small **reserved** footprint | Promote in place — already at head ([04 §3](04-resilience-operations.md)) |
| **Sheddable / stateless** (WS fan-out, API, projections) | **Cold/elastic** — little or nothing running | **Autoscale up** on demand (and **spot**) within seconds |
| **Non-critical** (analytics, surveillance backfill, batch) | None | Deferred until recovery |

Because the stateful standby is *just a log reader* (cheap) and the stateless tiers
**scale out fast**, the counterpart cloud costs little in steady state yet still
meets RTO. **Resilience cost ≈ warm-min core + elastic burst, not full
duplication.** This is how active/active stays affordable.

> Trade-off to decide (D9 in [05](05-roadmap-and-decisions.md)): more Tencent-
> weighting saves more steady-state cost but demands more elastic burst headroom on
> AWS and accepts a slightly more degraded mode during a full-Tencent outage. Tune
> the weighting to your risk appetite.

---

## 6. Commitment & purchasing strategy

| Capacity class | Purchasing | Cloud lean |
|----------------|-----------|-----------|
| Steady-state core (matching hosts, ledger, warm standbys, baseline fleets) | **Commitments** — Tencent monthly-subscription/reserved + AWS Savings Plans/RIs | Commit the **Tencent-heavy baseline**; smaller AWS commit |
| Predictable growth | Convertible commitments / rolling reservations | Follow the placement map |
| Burst & sheddable (stateless fan-out, API surge, CI, analytics) | **On-demand + Spot** (both clouds) | Spot where interruption is safe |
| Matching core & ledger writes | **Never spot** | Stability over savings |

Right-size continuously: the **matching core is small and CPU-bound (cheap)**; the
money is in **fan-out + egress + data services**, which is exactly where the
Tencent lean and the egress strategy (§4) concentrate the savings.

---

## 7. A TCO model you can fill in

Cost is dominated by a few categories. Model them per cloud and per region; the
**relative ratios below are planning assumptions — replace with real quotes.**

```
 Monthly TCO  ≈  Σ over {compute, egress, managed-data, CDN, cross-cloud-transfer,
                          custody/HSM, observability/security tooling, support}

 Illustrative relative unit costs (AWS = 1.00; VALIDATE):
   compute (vCPU·hr)        AWS 1.00   Tencent ~0.6–0.8
   egress (per GB)          AWS 1.00   Tencent ~0.5–0.7      ← biggest line item
   managed Kafka/DB/cache   AWS 1.00   Tencent ~0.6–0.8
   CDN                      comparable, region-dependent
   custody/HSM              comparable (security-driven, not cost-optimized)
   cross-cloud transfer     minimized by design (§4) — keep on interconnect
```

**Worked shape (illustrative, not a quote):** with the Tencent-weighted placement,
a representative steady-state split might land roughly **~60–70% of variable spend
on Tencent / ~30–40% on AWS**, with the *largest single saving coming from running
APAC fan-out + egress on Tencent*. Because the WS/egress tier is both the biggest
cost and the most Tencent-favored, the blended infrastructure cost can land
materially below an AWS-only deployment **while improving APAC latency** — the
cost and performance goals reinforcing each other. Validate the exact numbers with
real pricing and your traffic geography before committing.

**Unit economics to track** (more durable than raw totals): cost per **million
orders matched**, per **active user**, per **GB egressed**, and per **$ of volume
traded**. These expose regressions a total-spend chart hides.

---

## 8. Cost governance (FinOps) so it stays optimized

- **Tag everything** (cloud, env, service, market, team) → per-cloud and
  per-service dashboards; unify both clouds' billing into one view.
- **Budgets + anomaly alerts** per cloud (cross-cloud egress is the line to watch).
- **Cost SLOs** tracked beside performance SLOs (e.g. "infra cost per \$ traded ≤
  target"); a cost regression is a review item, like a latency regression.
- **Showback/chargeback** to teams; **cost gates in design reviews** (a new feature
  states its egress/compute footprint).
- **FinOps cadence**: regular right-sizing, commitment coverage, and
  spot-eligibility reviews.

---

## 9. Guardrails — cost must never break the priorities

Explicit stop-rules so the Tencent-lean can't quietly degrade the platform:

- **Latency is a hard gate.** Never move a user-facing tier to a cheaper-but-farther
  cloud if it breaches the latency budget ([02](02-performance-under-load.md)).
  (NA users are *not* served from Tencent SG to save money.)
- **Security/residency is a hard gate.** Custody, PII, and regulated-data placement
  are fixed by [03](03-security.md)/[01 §7](01-data-and-consistency.md), independent
  of price.
- **No spot/elastic under the matching core or ledger writes.** Stability over
  savings on the critical path.
- **Don't let DR headroom fall below the failover requirement** to save money — the
  warm-min core (§5) is a floor.

---

## Key takeaways
- **Performance and security are hard constraints; cost is minimized within them** —
  and Tencent's advantage tilts placement toward Tencent, giving an **asymmetric,
  Tencent-weighted active/active** topology.
- **Egress is the dominant cost**; running APAC fan-out on Tencent + CDN offload +
  conflation/compression is the biggest saving — and it *also* improves latency.
- **AWS stays essential** for NA/EU latency, reach, and resilient counterpart
  capacity; custody/ledger placement is security-driven, not cost-driven.
- **Cost-aware DR** (warm log-consumer core + cold/elastic sheddable tiers) keeps
  active/active affordable — no paying 2× for idle capacity.
- **Commit the Tencent-heavy baseline, burst on spot**, and govern with **unit-
  economics FinOps** and explicit **guardrails** so cost never breaks priorities #1
  and #2.
