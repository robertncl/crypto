# 02 — Performance Under Load (Priority #1)

[← 01 Data](01-data-and-consistency.md) · Next: [03 — Security](03-security.md)

Performance is the top priority. This doc defines the **latency budget**, how each
tier scales, the **honest cross-cloud trade-off** on order entry, and how we prove
it under load.

---

## 1. The latency budget

We budget the **order round trip** (submit → accepted/ack) and the **market-data
path** (event → on screen) separately, because they have different physics.

### Order path — *in-region* (user co-located with the market's primary cloud)

| Hop | Target p99 | Notes |
|-----|-----------|-------|
| TLS edge / API gateway | ≤ 1 ms | terminate TLS at edge, keep-alive, no per-request handshakes |
| AuthN (JWT verify) | ≤ 0.2 ms | stateless, local secret; no DB hit (Chapter 4) |
| Risk/pre-trade checks | ≤ 0.5 ms | rate limits, self-trade, notional/leverage caps — in-memory |
| Sequencer append + ack | ≤ 1 ms | append to local Kafka with low-latency acks; batched |
| Matching (in-memory) | ≤ 50 µs | single-thread per market; microseconds (see §3) |
| **Total order ack, in-region** | **≤ ~3–5 ms** | settlement to the ledger is **async** off the trade log |

### Order path — *cross-region* (user far from the market's primary)

A market has **one** primary cloud/region (sharding, [01](01-data-and-consistency.md)).
A user elsewhere must reach it:

```
APAC user ─edge(Tencent)─►[ private interconnect WAN ]─► BTC-USDT primary (AWS) ─► ack back
                                  + ~30–80 ms RTT (region-pair dependent)
```

| Component | Added cost |
|-----------|-----------|
| One WAN RTT over private interconnect | ~30–80 ms (region-pair dependent) |
| **Total order ack, cross-region** | **in-region budget + one WAN RTT** |

**This is the deliberate trade-off of correctness over uniform latency.** We do
*not* synchronously coordinate matching across clouds (that would add WAN latency
to *every* match and couple availability to the link). Instead a single market is
matched in one place, and we shrink the cross-region penalty with the four levers
below.

### Market-data path (always local — fully active/active)

| Hop | Target |
|-----|--------|
| Trade event → local projection (tickers/candles/depth) | ≤ 2 ms |
| Projection → WebSocket fan-out → client | ≤ 5 ms in-region |

Market data is a **local projection of the mirrored log** ([01 §2](01-data-and-consistency.md)),
so it's the same in both clouds and never crosses a cloud on the read path.

---

## 2. Four levers to minimize the cross-region penalty

1. **Locality- and cost-aware primary placement.** Put each market's primary in the
   cloud/region where most of its flow originates (APAC-heavy pairs → Tencent; US/EU
   pairs → AWS), and where latencies are comparable, prefer the **cheaper cloud
   (Tencent)** — see [06](06-cost-and-placement-economics.md). The *placement
   controller* ([04](04-resilience-operations.md)) optimizes this continuously. Most
   users then trade their popular markets locally, on the cost-efficient cloud.
2. **Colocation / Direct Connect for latency-sensitive participants.** Market
   makers and algos that need microseconds connect **directly into the primary
   region** (Direct Connect / Tencent DC / cross-connect in the same metro). Every
   serious exchange offers this; retail latency is dominated by their own internet
   path anyway.
3. **Async-acked, single-hop forwarding.** The far edge forwards the order over the
   **private interconnect** (not the public internet) and streams the result back —
   one WAN hop, no chatty coordination.
4. **Client-side optimism with authoritative correction.** The UI can render the
   order as "submitted" instantly and reconcile on the authoritative
   `orders`/`balances` WebSocket events (the app already updates from those events,
   Chapter 12), hiding the WAN RTT from *perceived* latency.

---

## 3. Matching-engine performance

The engine is the most latency-critical component, and it's where Nebula's design
already helps: **single-writer per market, all in memory** (Chapter 5).

**Keep on the hot path:**
- **One CPU per shard, pinned.** Pin the engine goroutine/thread to a dedicated
  core; isolate that core (`isolcpus`/`nohz_full`) from the scheduler and IRQs.
  Respect **NUMA** — keep the book and the network queue on the same node.
- **No allocations, no GC pauses.** Preallocate order/level objects; reuse buffers;
  avoid per-event garbage. Tune Go GC (raise `GOGC` / use a soft memory limit) or
  move the hottest structures off-heap. The goal is **no stop-the-world during
  matching**.
- **A ring buffer, not a channel.** The MVP's `cmds chan func()` is correct but
  allocates closures. For peak throughput replace it with a **pre-sized ring
  buffer of value-typed commands** (LMAX Disruptor style) — mechanical sympathy,
  no allocation, cache-friendly.
- **Batch the log append.** Append accepted commands/trades to Kafka in **small
  time/size batches** (e.g. ≤1 ms) to amortize syscalls while bounding latency.
- **Kernel-bypass networking at the extreme.** For the lowest tail latency, use
  SR-IOV/ENA enhanced networking, and DPDK/AF_XDP or a userspace stack on the
  ingress nodes. Cloud equivalents: AWS **cluster placement groups** + ENA;
  Tencent high-performance instances + SR-IOV.

**Throughput:** a well-tuned in-memory single-market engine handles **100k+
orders/sec**; we scale the *platform* by adding shards, not by speeding up one
shard past physics. Settlement and persistence are **off the hot path** (async log
consumers), so disk/DB latency never blocks a match.

> The risk/liquidation engine for perps (Chapter 10) runs as a co-located consumer
> of the same per-market stream, so mark-price and liquidation checks don't add to
> the order-ack path.

---

## 4. WebSocket fan-out at scale

Market-data and private-stream delivery is a **massive fan-out** problem (one trade
→ thousands of subscribers). Nebula's hub already has the right backpressure
instinct — **drop for a slow client rather than block producers** (Chapter 7). To
scale it to millions of connections:

- **Dedicated, horizontally-scaled WS edge fleet** (separate from the matching
  hosts), in both clouds, behind an L4 load balancer (NLB / CLB). Each node
  subscribes to the **local market-data bus** and fans out.
- **Shard connections** by market and/or user-hash so a node owns a bounded
  working set.
- **Conflation.** Under load, collapse rapid depth/ticker updates into the latest
  state per topic (send the newest snapshot, not every intermediate) — bounds
  per-client work during volatility.
- **Efficient framing.** Binary frames + permessage-deflate; batch multiple updates
  per flush.
- **Reconnect storms.** A blip can cause a thundering herd of reconnects — the
  client already uses **jittered exponential backoff** (Chapter 12); the server
  adds connection-rate limits and accepts gradually.

---

## 5. Caching & static delivery

- **Static SPA** → both clouds' CDNs (CloudFront / EdgeOne) from object storage
  (S3 / COS), long-cache hashed assets.
- **Public market snapshots** (initial depth/candles/ticker REST loads, Chapter 12)
  → cached at the edge with a **short TTL**, with the live deltas over WebSocket.
  This collapses the "everyone loads the order book" stampede into cache hits.
- **Hot reads** (balances/positions display, Tier D/cache) → regional **Redis**
  (ElastiCache / Tendis), never on the authoritative debit path ([01 §6](01-data-and-consistency.md)).

---

## 6. Autoscaling, admission control & graceful overload

Scaling differs by tier:

| Tier | Scaling strategy |
|------|------------------|
| Stateless (API, authn, WS edge, web) | **Horizontal autoscale** on RPS / CPU / connection count; scale-out in both clouds |
| Matching shards | **Not** autoscaled per shard (single writer); scale by **splitting markets** across more instances; **pre-provision headroom** and dedicated hosts for hot markets |
| Projections / consumers | Autoscale consumer groups by lag |
| Ledger | Scale read replicas horizontally; writes scale by account-shard locality |

**Pre-scale for known events** (token listings, scheduled volatility) — capacity is
provisioned ahead, not reactively.

**Under genuine overload, degrade — don't collapse:**
- **Admission control & fair queueing** at the edge: per-account and per-IP rate
  limits, weighted fair queueing so one abuser can't starve others.
- **Load shedding**: reject *new* order submissions with a retryable `503`/busy
  signal before the engine queue grows unbounded; **never** drop or reorder
  accepted commands (they're already in the log).
- **Circuit breakers** between tiers; **bulkheads** so one market's surge can't
  exhaust shared pools.
- **Backpressure is explicit end-to-end**: bounded queues with defined drop/whait
  policy at every stage (the hub's `default:` drop is the template).

---

## 7. Proving it: performance & load testing

Performance is a **gated, continuous** practice, not a launch-day check:

- **Replay load tests.** Capture real (or historical) order flow and **replay it at
  Nx speed** against staging — the most realistic load.
- **Spike/soak/stress.** Sudden 10–50× spikes (volatility/DDoS shape), multi-hour
  soaks (leak detection), and stress-to-failure to find the real ceiling and verify
  graceful shedding (§6).
- **Latency SLO gates in CI.** A perf regression that pushes order-ack p99 past
  budget **fails the build**.
- **Failover-under-load.** Run a cloud/region failover *while* load testing — the
  only way to know real-world RTO and that the standby keeps up (see [04](04-resilience-operations.md)).
- **Chaos + load together.** Inject latency/packet loss on the interconnect, kill
  engine nodes, throttle the ledger — under load — and watch SLOs.

**Capacity model:** size to **peak**, not average. Peaks during major moves are
routinely **10×+** average; the WS fan-out and order ingress feel it most. Carry
explicit headroom and document the per-shard and per-region ceilings.

---

## Key takeaways
- **Hot path is in-region**: order ack ~3–5 ms locally; matching in **microseconds**
  (in-memory, single writer, pinned, no GC, ring buffer, batched log append).
- The **only** cross-cloud cost on the order path is a **single WAN hop for
  geographically-distant users**, minimized by locality-aware placement,
  colocation, private interconnect, and client-side optimism — never per-match
  coordination.
- **Market data is fully local** (projection of the mirrored log) → active/active
  with no cross-cloud read.
- Scale stateless tiers horizontally; scale matching by **sharding markets**;
  settlement is **off the hot path**.
- Under overload, **shed and degrade gracefully**; prove everything with
  **replay/spike/soak load tests, SLO gates, and failover-under-load**.
