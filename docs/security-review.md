# Security Review — Nebula Exchange

**Scope:** entire application — Go backend (`backend/`) and React/TS frontend
(`web/`).
**Date:** 2026-05.
**Methodology:** manual source review of all packages; dependency scanning
(`govulncheck` for Go, `npm audit` for JS); targeted greps for injection, body
limits, rate limiting, security headers, XSS sinks, and error leakage; review of
auth, custody/ledger, matching, wallet, and WebSocket paths.

> **Context that frames every severity below:** Nebula is a *demonstration*
> exchange with **simulated custody** (no real blockchain, no real funds). Several
> findings are **intentional demo behaviors** that are *Critical if the system
> were ever pointed at real money*. They are rated by their **real-world** impact
> with the demo caveat noted, because that is the useful lens for a security
> review.

---

## 1. Executive summary

The application has a **sound security foundation**: SQL is fully parameterized,
the frontend has no XSS sinks, identity is always derived server-side from a
verified token (no IDOR), the JWT algorithm is pinned, passwords use bcrypt, the
custody ledger is atomic and cannot go negative, and dependencies are pinned with
**zero known CVEs** (`govulncheck`: *No vulnerabilities found*; `npm audit`: *0
vulnerabilities*).

The gaps are concentrated in **edge hardening and production-readiness**, not in
the core money logic:

- **No rate limiting** anywhere → brute-force, credential-stuffing, and DoS.
- **No request-body size limit** + **unbounded decimal parsing** → CPU/memory DoS.
- **No security headers** (CSP, X-Frame-Options, etc.) → clickjacking, weaker XSS
  containment.
- **No MFA**, weak password policy, long-lived non-revocable JWTs.
- A few **demo-only behaviors** (arbitrary self-deposit, instant KYC) that mint
  money / bypass compliance and **must not** reach production.

None are remotely exploitable for fund theft *in the demo* beyond the intentional
simulated-deposit behavior, but most must be fixed before any real deployment.

### Findings at a glance

| ID | Severity | Area | Finding |
|----|----------|------|---------|
| F1 | **Critical¹** | Custody | `/wallet/deposit` lets any user credit themselves any amount (mints money) |
| F2 | **Critical²** | Auth | Default/weak `JWT_SECRET` with no fail-fast → token forgery if unset in prod |
| F3 | High | Auth | No MFA; weak password policy; KYC is an instant stub |
| F4 | High | DoS | No rate limiting (login, orders, deposits, WS connections) |
| F5 | Medium | DoS | No request body-size limit + unbounded `num.Parse` (big.Int) |
| F6 | Medium | Hardening | Missing security headers (CSP, X-Frame-Options, nosniff, HSTS, Referrer-Policy) |
| F7 | Medium | Auth | Long-lived (72h) JWT, no server-side revocation; logout is client-only |
| F8 | Medium | Info-disclosure | Raw `err.Error()` returned to clients on the 500 path |
| F9 | Low | WebSocket | `CheckOrigin` allows all origins; JWT passed in query string |
| F10 | Low | Auth | Email enumeration on register (`409 already exists`) |
| F11 | Low | Integrity | `num.Dec` `Add`/`Sub` use unchecked `int64` (overflow wraps) |
| F12 | Low | Observability | No structured auth/audit/access logging |

¹ Intentional in the demo (simulated custody); Critical if productionized.
² Critical only if deployed without setting `JWT_SECRET`.

---

## 2. Detailed findings

### F1 — Arbitrary self-deposit mints money *(Critical if productionized; demo-intentional)*
`POST /api/wallet/deposit` takes a client-supplied `amount` and credits the
caller's balance after a simulated confirmation, with **no cap and no real
on-chain backing** (`backend/internal/wallet/wallet.go:Deposit`,
`handlers_wallet.go:27`). Any authenticated user can grant themselves unlimited
funds.
**Impact:** total loss of integrity if real. In the demo it's the whole point of
"simulated custody".
**Recommendation:** for any real deployment, **delete this endpoint** and replace
with real deposit detection (a chain-watcher credits the ledger from confirmed
on-chain transactions; the amount is never client-controlled). Until then, keep it
clearly demo-gated and never enable on an internet-exposed instance with value.

### F2 — Weak default JWT secret, no fail-fast *(Critical if deployed unset)*
`JWT_SECRET` defaults to the literal `"dev-insecure-secret-change-me"`
(`backend/internal/config/config.go`). If deployed without overriding it, anyone
who knows the default (it's in the source) can **forge tokens for any user**,
including bypassing all authorization.
**Recommendation:** **fail to boot** if the secret is the default (or shorter than
~32 bytes) unless an explicit `DEV=true` is set. Generate per-environment secrets
from a secrets manager (KMS/Vault per the multicloud design).

### F3 — Authentication hardening gaps *(High, prod)*
- **No MFA/2FA** anywhere — table stakes for an exchange.
- **Weak password policy:** minimum 6 characters, no complexity/breach check
  (`handlers_auth.go`).
- **KYC is an instant stub** (`handleKYCVerify` flips status to `verified`) — fine
  for the demo, a compliance hole if shipped.
**Recommendation:** add TOTP/WebAuthn MFA (and require it for withdrawals);
enforce a stronger password policy + breached-password check; integrate a real
identity-verification provider behind the existing KYC gate.

### F4 — No rate limiting → brute force & DoS *(High)*
There is **no rate limiting or throttling** on any endpoint (confirmed: no
limiter middleware). Consequences:
- **Login** can be brute-forced / credential-stuffed (compounded by F3).
- **Order placement** can be flooded (the engine is fast, but the API/DB and WS
  fan-out are not infinite).
- **Deposit/withdraw** each spawn a background goroutine that sleeps for the
  confirmation loop (`wallet.go`); spamming them can exhaust goroutines/memory.
**Recommendation:** per-IP and per-account rate limits at the edge and in-app
(e.g. `httprate` for chi), login attempt throttling + account lockout/backoff, and
a cap on concurrent pending wallet txns per user. The multicloud design's WAF +
edge rate limiting covers the infra layer; the app still needs in-process limits.

### F5 — Unbounded request body + decimal parsing → DoS *(Medium)*
`decode` (`server.go:190`) reads the request body with **no
`http.MaxBytesReader`**, and `num.Parse` (`num/decimal.go`) calls
`big.Int.SetString` on the **full integer portion of the input string with no
length bound**. A request with a multi-megabyte numeric field (e.g. a price of a
million digits) forces large allocations and CPU in `big.Int`, repeated cheaply
(F4 has no rate limit).
**Recommendation:** wrap request bodies in `http.MaxBytesReader` (e.g. 32–64 KB);
reject decimal strings longer than a small bound (e.g. 32 chars) in `num.Parse`
before the `big.Int` conversion.

### F6 — Missing security headers *(Medium)*
No `Content-Security-Policy`, `X-Frame-Options`/`frame-ancestors`,
`X-Content-Type-Options: nosniff`, `Referrer-Policy`, or
`Strict-Transport-Security` are set (confirmed: none in `backend/internal`).
**Impact:** the SPA can be **framed (clickjacking** — dangerous for "confirm
withdrawal"-type actions), MIME-sniffing is possible, and there's no CSP to
contain a future XSS.
**Recommendation:** add a small middleware setting `X-Frame-Options: DENY` (or CSP
`frame-ancestors 'none'`), `X-Content-Type-Options: nosniff`,
`Referrer-Policy: strict-origin-when-cross-origin`, `HSTS` (in prod over TLS), and
a CSP (`default-src 'self'`; the app uses no inline scripts, so a strict CSP is
feasible — Vite output is external files).

### F7 — Long-lived, non-revocable JWTs *(Medium)*
Access tokens last **72h** (`JWT_TTL_HOURS`) and there is **no server-side
revocation** — logout only clears `localStorage` client-side
(`web/src/state/auth.tsx`). A stolen token is valid for up to 72h regardless of
logout or password change.
**Recommendation:** shorten access-token TTL (≈15 min) with refresh tokens, and
maintain a server-side revocation/`jti` blocklist (or token versioning per user)
so logout, password change, and compromise can invalidate sessions.

### F8 — Internal error disclosure on the 500 path *(Medium → Low)*
The `default` branch of `writeDomainErr` (`server.go:186`) returns
`err.Error()` to the client for unmapped errors, which can surface internal
details (DB error text, etc.). (The mapped domain errors that return `err.Error()`
are safe — they're user-facing validation messages.)
**Recommendation:** in the default case, **log the real error server-side** and
return a generic `"internal error"` to the client. Same for the wallet handlers'
non-domain error paths.

### F9 — WebSocket origin & token-in-URL *(Low)*
`upgrader.CheckOrigin` returns `true` for all origins (`api/ws.go:17`), and the
JWT is passed as a **query parameter** (`?token=…`, `api/ws.go:25`), which can land
in proxy/access logs and browser history. CSRF risk is low (bearer-token auth, no
cookies; private channels require a valid token), but public data is exposed
cross-origin and the token placement is risky.
**Recommendation:** restrict `CheckOrigin` to the known web origins in prod; issue
a **short-lived single-use WS ticket** for the handshake instead of the main JWT.

### F10 — Email enumeration on registration *(Low)*
Register returns `409 "an account with that email already exists"`
(`handlers_auth.go`), letting an attacker enumerate registered emails. (Login
correctly returns a generic message.)
**Recommendation:** return a generic success/"check your email" response, or
gate registration behind a rate limit + CAPTCHA.

### F11 — Unchecked integer arithmetic in `num.Dec` *(Low, latent)*
`Add`/`Sub` (`num/decimal.go`) use raw `int64` addition; two near-`int64`
balances overflow and wrap. `Mul`/`Div` are already overflow-safe via `big.Int`.
Reachability today is mainly through F1 (arbitrary deposits to near-max balances).
**Recommendation:** use checked addition (detect overflow → error) or `big.Int`,
and/or cap per-asset balances. Add a fuzz/property test for the arithmetic.

### F12 — No structured auth/audit logging *(Low)*
There's no request or auth-event logging middleware (only `RequestID` +
`Recoverer`). The double-entry ledger gives a strong *financial* audit trail, but
logins, failed auth, KYC changes, and admin-style actions aren't recorded for
security monitoring.
**Recommendation:** add structured logging of auth events (success/failure, IP,
user) routed to the SIEM described in the architecture docs; keep tokens/secrets
out of logs (don't log the WS query string).

---

## 3. What's done right (strengths)

These are genuinely good and worth preserving:

- **No SQL injection:** every query in `store/` is parameterized; no string-built
  SQL (verified by grep).
- **No XSS sinks:** the React app uses no `dangerouslySetInnerHTML`/`eval`; data is
  rendered as text and auto-escaped.
- **No CSRF surface:** state-changing endpoints authenticate via a `Bearer` token
  in the `Authorization` header (not cookies), and CORS uses
  `AllowCredentials: false` — a malicious site cannot forge authenticated requests.
- **JWT algorithm pinned:** `Parse` rejects non-HMAC tokens
  (`auth/auth.go`), preventing `alg=none`/algorithm-confusion attacks.
- **Server-derived identity (no IDOR):** handlers take the user id from the
  verified token (`auth.UserID(ctx)`), never from the request body; ownership is
  checked on cancel/close.
- **Atomic, non-negative ledger:** all money moves through
  `ApplyPostings`/`CommitFill`/`CommitPerp` in single DB transactions that refuse
  negative balances — no double-spend, with a built-in double-entry audit.
- **Private WS channels are server-scoped:** the server appends the authenticated
  user id to private topics; a client cannot subscribe to another user's stream.
- **bcrypt** password hashing; **crypto/rand** for address/txid generation.
- **Supply chain:** Go (`go.sum`) and npm (`package-lock.json`) pinned;
  **0 known CVEs** (`govulncheck` + `npm audit`).
- **Panic safety:** `Recoverer` middleware prevents a single bad request from
  crashing the server.

---

## 4. Production-readiness checklist (demo → real)

Before this could safely touch real funds/users, in priority order:

1. **Remove F1** (client-controlled deposits) and build real custody (HSM/MPC,
   chain-watcher, withdrawal allowlists/approvals) per `docs/architecture/03-security.md`.
2. **Fix F2** (enforce a strong, externally-managed JWT secret; fail-fast on default).
3. **Add MFA + withdrawal 2FA, real KYC/AML, stronger passwords** (F3).
4. **Rate limiting + WAF + bot management** (F4) and **input bounds** (F5).
5. **Security headers + CSP** (F6), **short-lived/revocable sessions** (F7),
   **error hygiene** (F8), **WS hardening** (F9).
6. Address F10–F12 and wire **auth/audit logging into a SIEM** (F12).

---

## 5. Suggested quick wins (low-risk, high-value, applyable now)

These can be implemented without behavioral risk to the demo:

- **Security-headers middleware** (F6) — pure addition.
- **`http.MaxBytesReader` in `decode` + length cap in `num.Parse`** (F5).
- **Sanitize the 500 path** — log internally, return generic message (F8).
- **In-app rate limiting** via chi `httprate` on `/auth/*` and order/wallet
  endpoints (F4).
- **Fail-fast on the default `JWT_SECRET`** unless `DEV=true` (F2).

> Tooling note: `govulncheck` and `npm audit` were run for this review and are
> easy to wire into CI as a gate. `gosec` (Go static analysis) is recommended as a
> further CI step.
