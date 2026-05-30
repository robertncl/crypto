# 03 — Security (Priority #2)

[← 02 Performance](02-performance-under-load.md) · Next: [04 — Resilience & Ops](04-resilience-operations.md)

Crypto exchanges are among the most attacked systems on the internet, and a single
custody failure is existential. Security here is **defense in depth, zero trust,
and custody segregation**, applied consistently across both clouds.

---

## 1. Threat model

| Asset (what we protect) | Primary threats | Worst case |
|-------------------------|-----------------|-----------|
| **Custody keys / funds** | Key theft, insider, hot-wallet drain, signing-service compromise | Irreversible loss of customer funds |
| **The ledger** | Logic bugs, double-spend, replay, privileged DB access | Phantom balances, theft |
| **User accounts** | Account takeover (ATO), credential stuffing, SIM-swap, session theft | Unauthorized trades/withdrawals |
| **Availability** | DDoS / extortion, resource exhaustion | Outage during volatility, reputational + financial loss |
| **APIs / trading** | API-key abuse, market manipulation (spoofing, wash trading), bots | Manipulation, unfair markets, fines |
| **Data** | PII exfiltration, KYC document theft | Regulatory penalties, user harm |
| **Supply chain** | Malicious dependency, poisoned image, compromised CI | Backdoor to everything |
| **Cloud control plane** | Misconfiguration, leaked credentials, over-broad IAM | Lateral movement, full compromise |
| **Insider** | Privileged operator, rogue admin | Targeted theft, sabotage |

**Adversaries** range from opportunistic bots to **organized, well-funded groups
(incl. nation-state)** that specifically target exchanges. Design for the high end.

---

## 2. Defense-in-depth layers (and where each lives)

```
  Internet
     │  ── DDoS scrubbing · WAF · bot mgmt · TLS · geofencing        (Edge, both clouds)
     ▼
  Edge / DMZ           API gateway · authn · rate limit · admission   (no business state)
     │  ── mTLS, deny-by-default
     ▼
  Application tier     stateless services, matching, market data      (no key material, no raw PII)
     │  ── mTLS, scoped service identity
     ▼
  Data tier            ledger (encrypted) · logs · caches             (private subnets, no internet)
     │  ── separate accounts/VPCs, one-way trust
     ▼
  Custody tier         HSM/MPC signers · cold storage · approvals     (air-gapped/offline; no app creds)
```

Each boundary is **deny-by-default**, authenticated (**mTLS**), and audited.
Compromising one layer must not yield the next.

---

## 3. Edge & DDoS (availability is priority #1, so this is priority #1 security)

Multi-layered, in **both** clouds so an attack on one cloud is absorbed and traffic
shifts to the other:

- **Volumetric (L3/L4):** **AWS Shield Advanced** + **Tencent Anti-DDoS Pro/Advanced**,
  plus **Anycast** edge so attack traffic is dispersed and scrubbed near its source.
- **Application (L7):** **AWS WAF** + **Tencent WAF** with managed rule sets,
  per-IP/per-account **rate limiting**, **bot management**, and **challenge**
  (JS/CAPTCHA) for suspicious clients. Protect expensive endpoints (order placement,
  login, market-data REST) specifically.
- **Capacity as defense:** autoscaling stateless tiers + CDN offload ([02 §5](02-performance-under-load.md))
  means most floods hit cache/edge, never the core.
- **DDoS playbook:** pre-arranged scrubbing escalation, the ability to **fail
  traffic entirely to the other cloud**, and tightened WAF posture under attack.

---

## 4. Zero-trust network

- **No flat network.** Per-cloud isolation: separate **VPCs** (AWS) and **VPCs**
  (Tencent) per environment and per trust tier; private subnets for data/custody
  with **no internet egress** except via controlled proxies.
- **mTLS everywhere** via a **service mesh** (Istio/Linkerd or Cilium): every
  service-to-service call is mutually authenticated and encrypted; identity is the
  workload, not the IP.
- **Deny-by-default** security groups/ACLs; explicit allowlists between tiers;
  **egress filtering** (exfiltration defense).
- **Private, encrypted interconnect** between clouds (Megaport/Equinix or
  DC+CCN) — replication/forwarding never traverses the public internet, and the
  link itself is authenticated and encrypted.
- **Custody tier** sits in its own accounts/VPCs with **one-way trust**: the app
  tier can *submit a withdrawal request*, but holds **no credentials** that can
  reach signing material; signers initiate outbound, app never inbound.

---

## 5. Custody — the crown jewels

Most exchange catastrophes are custody failures. Controls, in layers:

- **Majority cold, minimal hot.** The bulk of funds in **offline/air-gapped cold
  storage**; the **hot wallet** holds only the float needed for near-term
  withdrawals (Chapter 8). Cold→hot top-ups are deliberate, multi-party, audited.
- **HSM / MPC signing.** Private keys never exist in plaintext; signing happens
  inside **HSMs** (AWS CloudHSM / Tencent Cloud HSM) or via **MPC** (key split into
  shares, signing requires a threshold). **Distribute MPC shares across providers
  and geographies** so no single cloud or facility compromise can sign — a direct
  benefit of being multicloud.
- **Withdrawal controls:** destination **address allowlists** (with a cool-down to
  add), **per-address and velocity limits**, **multi-party approval** for large
  amounts, **time-locks** on policy changes, and **anomaly detection** on
  withdrawal patterns. The ledger debit ([01 §4](01-data-and-consistency.md)) is
  necessary but **not sufficient** — these controls gate the *signing*.
- **Travel Rule / sanctions:** screen withdrawal destinations against sanctions and
  exchange counterparties before signing.
- **Key ceremonies & rotation:** documented, multi-person, audited key generation
  and rotation; recovery procedures tested.
- **Proof of reserves:** periodically prove (cryptographically) that held assets ≥
  customer liabilities (the ledger's double-entry sum is the *liabilities* side).

---

## 6. Identity & access management (humans and machines)

- **Staff:** central **IdP + SSO**, **phishing-resistant MFA** (FIDO2/WebAuthn),
  **least privilege**, **no standing production admin**. Elevated access is
  **just-in-time** with approval, time-boxed, and fully audited (**break-glass**
  paths alarmed). Automated **deprovisioning** (SCIM) on offboarding.
- **Separate cloud accounts/projects** per environment and blast-radius domain in
  *both* clouds; production is its own isolated set.
- **Workload identity, not static keys.** Services assume short-lived roles via
  **OIDC/workload identity** (e.g. IRSA on EKS, role binding on TKE) — **no
  long-lived cloud access keys** in pods or images.
- **API keys (customer):** **scoped** (read / trade / withdraw are separate
  capabilities), optional **IP allowlists**, independently revocable, and
  **withdraw scope disabled by default**.

---

## 7. Secrets, data protection & app/supply-chain security

**Secrets**
- Backed by per-cloud **KMS** (CMKs) and a secrets manager (**Vault** or cloud
  native) with **dynamic, short-lived** secrets and **automatic rotation**. No
  secrets in images, env files, or git; CI scans for leaked secrets.

**Data protection**
- **Encryption in transit:** TLS 1.3 externally, **mTLS** internally.
- **Encryption at rest:** CMK-encrypted volumes/DBs/objects in each cloud; **keys
  never shared across clouds** (per-cloud roots).
- **PII** ([01 §7](01-data-and-consistency.md)): field-level encryption /
  **tokenization**, stored in-jurisdiction, kept out of the trading hot path.

**Application & supply chain** (the platform's code is itself an attack surface)
- Secure **SDLC**: mandatory code review on protected branches; **SAST + DAST +
  SCA** in CI. (This repo already pins dependencies — Go `go.sum`, npm
  `package-lock.json` — the baseline for SCA.)
- **Signed artifacts** (cosign) with **admission control** that runs **only signed
  images**; generate and store an **SBOM** per build; **scan images** for CVEs.
- **Least-privilege CI/CD**, isolated runners, and **provenance** (SLSA-style) so a
  compromised pipeline can't silently ship a backdoor.
- The codebase's `/security-review` and `/code-review ultra` workflows are part of
  the pre-merge gate for sensitive changes.

---

## 8. Detection, response & market surveillance

- **Centralized SIEM** aggregating logs/metrics from **both clouds** into one pane;
  correlation and alerting across the multicloud estate.
- **Immutable audit.** The double-entry **ledger** (Chapter 3) and **append-only
  event logs** ([01](01-data-and-consistency.md)) are tamper-evident; ship audit
  records to **WORM** storage with retention.
- **Runtime security** (eBPF: Falco / Cilium Tetragon) on the app tier; **CSPM**
  (cloud posture management) continuously checks both clouds against hardening
  baselines (CIS) and flags drift/misconfiguration.
- **Behavioral detection** on the things that matter: **auth** (impossible travel,
  device fingerprint, credential-stuffing patterns), **withdrawals** (velocity,
  new address, anomaly), and **trading** — a **market-surveillance** consumer of
  the trade log detects **wash trading, spoofing/layering, and manipulation**
  (also a regulatory requirement).
- **Incident response:** runbooks, defined on-call, **regular tabletop + red-team**
  exercises, and a pre-authorized "**halt market / freeze withdrawals**" capability
  for emergencies.

---

## 9. Compliance & regulatory (shapes the architecture, not just paperwork)

- **Certifications:** target **SOC 2** / **ISO 27001**; many jurisdictions require
  specific licensing for exchanges.
- **KYC/AML/CTF:** real identity verification, **sanctions screening** (OFAC and
  others), transaction monitoring, suspicious-activity reporting, and the **Travel
  Rule** for transfers. (Nebula's KYC gate, Chapter 4, is the placeholder for this
  whole stack.)
- **Geofencing.** Crypto trading is **restricted or prohibited in some
  jurisdictions, including mainland China.** The platform must **block restricted
  jurisdictions** at the edge, and **Tencent capacity targets non-mainland regions**
  (Singapore/HK/Jakarta/Frankfurt/Tokyo). This is both legal necessity and a key
  reason the Tencent footprint is APAC-international, not mainland.
- **Data residency.** PIPL (China), GDPR (EU), and others constrain where PII may
  be stored/processed — enforced by the Tier-C, in-jurisdiction PII design ([01 §7](01-data-and-consistency.md)).
- **Records & retention** for trades, orders, and audit per regulatory minimums.

---

## 10. Multicloud-specific security posture

Being in two clouds is a **security asset** (blast-radius isolation, distributed
MPC shares, DDoS absorption) *if* you avoid the classic pitfalls:

- **No shared trust root.** Each cloud has its **own KMS roots and credentials**;
  compromising one cloud's control plane must not yield the other's.
- **One policy, two enforcers.** Define guardrails as **policy-as-code (OPA/
  Gatekeeper)** and **identical IaC**, then enforce in *both* clouds so posture
  doesn't drift between them.
- **Mind the parity gaps.** AWS and Tencent services differ; security reviews must
  cover **each cloud's specifics** (default encryption, logging, IAM semantics),
  not assume parity.
- **Unified identity, separate keys.** Staff use one IdP, but it brokers into
  *separate*, least-privilege cloud roles — convenience without a single key to
  steal.

---

## Key takeaways
- **Defense in depth + zero trust**: deny-by-default, mTLS everywhere, tiered
  network with **custody fully segregated** (no app credentials reach signing).
- **DDoS defense is first-class** (Shield Adv + Anti-DDoS Pro + Anycast + WAF), and
  multicloud lets us **fail an attacked cloud away**.
- **Custody = majority cold + HSM/MPC with shares distributed across clouds** +
  strict withdrawal controls; the ledger debit is necessary but not sufficient.
- **Workload identity (no static keys)**, scoped customer API keys, JIT staff
  access, dynamic secrets.
- **Signed, SBOM'd, scanned supply chain**; centralized SIEM + market surveillance
  + immutable audit; **geofencing & KYC/AML** baked into the edge and data design.
- **Multicloud is a security advantage** only with **no shared trust root** and
  **policy-as-code enforced identically** in both clouds.
