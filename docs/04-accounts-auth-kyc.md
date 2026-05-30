# Chapter 4 — Accounts, Auth & KYC

> **Learning objectives**
> - Implement stateless sessions with **JWTs** and safe password storage with
>   **bcrypt**.
> - Understand what **KYC/AML** mean and why exchanges are legally required to
>   care.
> - See how authorization gates sensitive actions (like withdrawals).

---

## 4.1 Two different questions: authentication vs. authorization

- **Authentication** = *"Are you who you say you are?"* (login)
- **Authorization** = *"Are you allowed to do this specific thing?"* (e.g.
  withdraw)

Exchanges need both, plus a third concept unique to regulated finance — **KYC**
(Know Your Customer) — which is about *"Have you proven your real-world
identity?"*. We'll take them in turn.

---

## 4.2 Passwords: never store them

The first rule of authentication: **never store a password**, not even encrypted.
Store a **hash** — a one-way fingerprint. When a user logs in, hash what they
typed and compare. If your database leaks, attackers get hashes, not passwords.

But not any hash: fast hashes (SHA-256) are brute-forceable at billions/second.
You want a **deliberately slow**, salted hash designed for passwords. Nebula uses
**bcrypt** (`backend/internal/auth/auth.go`):

```go
func HashPassword(pw string) (string, error) {
    b, err := bcrypt.GenerateFromPassword([]byte(pw), bcrypt.DefaultCost)
    return string(b), err
}
func CheckPassword(hash, pw string) bool {
    return bcrypt.CompareHashAndPassword([]byte(hash), []byte(pw)) == nil
}
```

bcrypt automatically salts each hash and has a tunable "cost" so you can keep it
slow as hardware improves. The stored `password_hash` looks like
`$2a$10$N9qo8uLO...` — useless to an attacker without months of compute per
password.

---

## 4.3 Stateless sessions with JWTs

After login, the server must remember "this request is Alice". Two approaches:

- **Server-side sessions:** store a session id in a database/Redis, look it up on
  every request. Stateful, needs shared storage.
- **Tokens (JWT):** hand the client a **signed** token containing the user id; the
  client sends it back on each request; the server verifies the signature. No
  server-side storage needed.

A **JWT** (JSON Web Token) is three base64 parts: a header, a JSON **payload**
(the "claims"), and a **signature**. The signature is computed with a secret only
the server knows, so the client can *read* the token but cannot *forge* one.
Nebula signs with HMAC-SHA256 (`auth.go`):

```go
claims := Claims{
    Role: role,
    RegisteredClaims: jwt.RegisteredClaims{
        Subject:   strconv.FormatInt(userID, 10), // the user id
        ExpiresAt: jwt.NewNumericDate(now.Add(m.ttl)),
    },
}
return jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(m.secret)
```

The token says "I am user 7, role `user`, valid until Friday", and it's tamper-
proof. On the way back in, middleware verifies it:

```go
// internal/auth/middleware.go — runs before every protected handler
userID, role, err := m.Parse(token)
if err != nil { http.Error(w, "...", http.StatusUnauthorized); return }
ctx := context.WithValue(r.Context(), userIDKey, userID)
next.ServeHTTP(w, r.WithContext(ctx))
```

Now any protected handler can call `auth.UserID(r.Context())` to know who's
asking. Public endpoints (market data) skip this; protected ones (orders, wallet)
are wrapped in the middleware in `internal/api/server.go`.

> **Security note for newcomers:** because a JWT is self-validating, you cannot
> easily *revoke* one before it expires. That's why Nebula's tokens are
> short-ish-lived (`JWT_TTL_HOURS`, default 72h) and the signing secret
> (`JWT_SECRET`) must be strong and private in production. The WebSocket handshake
> accepts the same token via a query parameter, since browsers can't set custom
> headers on a WebSocket.

---

## 4.4 Registration, and a friendly welcome

`handleRegister` (`internal/api/handlers_auth.go`) validates the email/password,
hashes the password, creates the user, **and credits a demo balance** so new
users can trade immediately:

```go
user, _ := s.st.CreateUser(c.Email, hash, "user", time.Now().Unix())
// Grant a demo welcome balance via the ledger (Chapter 3!)
s.st.ApplyPostings("welcome:"+itoa(user.ID), now, []store.Posting{{
    UserID: user.ID, Asset: "USDT", DeltaAvailable: welcomeBonus, // 10,000 USDT
    Reason: "welcome_bonus", Ref: "signup",
}})
```

Notice the welcome bonus isn't a magic `UPDATE` — it goes through `ApplyPostings`
with a `reason`, exactly like every other money movement. Consistency pays off:
even "free demo money" is auditable.

---

## 4.5 KYC and AML — the part that's not in the SDK

Banks and exchanges are legally required to know who their customers are, to
prevent money laundering, fraud, and sanctions violations. Two acronyms:

- **KYC — Know Your Customer:** verify a customer's real identity (government ID,
  proof of address, sometimes a selfie/liveness check).
- **AML — Anti-Money-Laundering:** monitor activity for suspicious patterns,
  screen against sanctions lists, and report as required.

In practice this gates what you can do. A common rule: **you can browse and even
deposit before verifying, but you cannot withdraw until you've passed KYC** (so
the exchange knows who funds are going to).

Nebula models the *shape* of this without the (very heavy) real machinery. A user
has a `kyc_status` (`none` → `verified`), and verification is a stub endpoint that
instantly approves you (`handleKYCVerify`). The important part is the **gate**: the
wallet service refuses withdrawals until you're verified
(`internal/wallet/wallet.go`):

```go
if user.KYCStatus != "verified" {
    return nil, ErrKYCRequired
}
```

That single check is conceptually where a real exchange would invoke an identity-
verification vendor, sanctions screening, and risk scoring.

> **In the real world**, KYC/AML is a whole department and a stack of vendors
> (identity verification, transaction monitoring, blockchain analytics to trace
> the source of deposited coins). Getting it wrong means fines or losing your
> license. Our one-line gate is a placeholder for an enormous amount of real-world
> compliance.

---

## 4.6 Roles

The token carries a `role` (`user`, `admin`, `bot`). Nebula uses it lightly — the
market-maker accounts are tagged `bot` — but it's the hook where an exchange would
build an **admin console**, support tooling, and permissioned actions (halt a
market, adjust fees, freeze an account). Role-based authorization is the natural
next step beyond "is this request authenticated".

---

## Key takeaways

- Store **bcrypt hashes**, never passwords. bcrypt is salted and deliberately
  slow.
- **JWTs** give stateless sessions: a signed, tamper-proof token the client
  carries. Middleware verifies it and injects the user id into the request.
- **KYC** = prove identity; **AML** = monitor for crime. Exchanges are legally
  obligated, and typically **gate withdrawals on KYC** — which Nebula models with
  a single check.
- Even the welcome bonus flows through the ledger — consistency everywhere.

## Try it

- Register via the UI, copy the JWT from the network tab, and paste it into
  <https://jwt.io>. Read the `sub` (your user id) and `exp` claims. Try changing a
  character — the signature no longer verifies.
- Without verifying KYC, attempt a withdrawal in the Wallet page and watch the
  `ErrKYCRequired` (403) response. Then click **Verify identity** and retry.

## Next

→ [Chapter 5 — The Order Book & Matching Engine](05-order-book-matching-engine.md):
the marketplace itself — where buyers meet sellers.
