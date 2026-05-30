# Chapter 7 — Market Data & Real-Time WebSockets

> **Learning objectives**
> - Learn the four staple market-data products: **ticker, candles, depth, trades**.
> - See how they're all **derived from the single trade stream**.
> - Understand a **publish/subscribe WebSocket hub**, and how private channels are
>   kept private.

A price chart, the blinking order book, the 24-hour "+3.2%" badge — none of that
is the matching engine. It's **market data**: views *computed from* what the engine
produces. Getting this layer right is what makes an exchange feel alive.

---

## 7.1 The four products

| Product | Answers | Built from |
|---------|---------|-----------|
| **Ticker** | "Price now, and how's the last 24h?" | recent trades |
| **Candles (OHLCV)** | "Show me the price history as a chart." | trades, bucketed by time |
| **Depth** | "How much is bid/offered at each price?" | the live order book |
| **Trades (the tape)** | "What just executed?" | each fill |

The key insight: **everything except depth is derived from the trade stream.**
Every time the engine produces a fill, the market-data service folds it into the
ticker and candles. Depth is the one product that comes straight from the book
(the engine snapshots it).

---

## 7.2 The ticker — a 24-hour rolling summary

A **ticker** is the at-a-glance summary you see on every market row: last price,
24h high/low, 24h volume, and the percent change. "Change" needs a reference: the
price 24 hours ago (`open24h`), so `change% = (last − open24h) / open24h`.

Nebula's `market.Service` (`backend/internal/market/market.go`) recomputes these
every few seconds straight from SQL over the `trades` table:

```sql
SELECT MAX(price), MIN(price), SUM(quantity), SUM(quote_qty)
FROM trades WHERE market = ? AND created_at >= ?   -- ? = now - 24h
```

and updates `last` instantly on every fill (`OnTrade`). Simple, correct, and you
can watch the numbers move.

---

## 7.3 Candles — the chart's raw material

A **candlestick** compresses all trades in a time bucket (1 minute, 1 hour, …)
into four prices — **OHLC** — plus **V**olume:

- **Open** — first trade price in the bucket
- **High** — max price
- **Low** — min price
- **Close** — last trade price
- **Volume** — total quantity traded

```
   high ─┐        A candle: the body spans open→close,
         ┃        the wicks reach high/low. Green if
    open ┣━━┓     close ≥ open (price rose), red if it fell.
         ┃  ┃     A 1-minute candle = every trade in that minute,
   close ┗━━┫     summarized into 5 numbers.
         ┗━ low
```

`OnTrade` maintains the *current* candle for several intervals at once and pushes
each update live, so the chart's last candle grows in real time:

```go
for _, sec := range Intervals {            // 60, 300, 900, 3600, 14400, 86400
    bt := (t.CreatedAt / sec) * sec        // which bucket this trade falls in
    cur := bars[sec]
    if cur == nil || cur.Time != bt {      // new bucket → start a fresh candle
        cur = &models.Candle{Time: bt, Open: t.Price, High: t.Price,
                              Low: t.Price, Close: t.Price, Volume: t.Quantity}
    } else {                               // same bucket → extend it
        cur.High = num.Max(cur.High, t.Price)
        cur.Low  = num.Min(cur.Low,  t.Price)
        cur.Close = t.Price
        cur.Volume = cur.Volume.Add(t.Quantity)
    }
}
```

Historical candles for the chart's initial load are computed on demand from the
same `trades` table (`store.Candles`), so live and historical agree exactly.

---

## 7.4 Depth — the order book, summarized

**Depth** is the book aggregated per price level: at each price, the total
quantity available. It's what draws those green/red bars behind the order book.
The engine produces it with `book.snapshot(limit)`, walking the best N levels of
each side and summing the quantity at each. Because it comes from the live book
(not history), the engine publishes a fresh depth snapshot whenever the book
changes.

---

## 7.5 Pushing it live: the WebSocket hub

REST is request/response — fine for "load the page", useless for "tell me the
instant the price ticks". For that you need a **persistent, server-push**
connection: a **WebSocket**.

Nebula's `internal/ws` package is a classic **publish/subscribe hub**:

- A **topic** is a string like `ticker:BTC-USDT`, `depth:BTC-USDT`,
  `trades:BTC-USDT`, or `kline:BTC-USDT:60`.
- A client sends `{"op":"subscribe","channels":[...]}` to express interest.
- Producers (the engine, the market service) call `hub.Publish(topic, data)`.
- The hub fans the message out to every client subscribed to that topic.

```go
// internal/ws/hub.go — the whole idea, abridged
func (h *Hub) Publish(topic string, data any) {
    payload, _ := json.Marshal(Envelope{Channel: topic, Data: data})
    for c := range h.topics[topic] {        // every subscriber
        select {
        case c.send <- payload:             // queue it
        default:                            // ...unless they're too slow:
            // drop the message rather than block everyone
        }
    }
}
```

That `default:` branch is a small but important production lesson: **one slow
client must never stall the whole exchange.** If a client's send buffer is full,
the hub drops the message for *that* client (its connection will be reaped) and
moves on. Producers are never blocked by a laggy consumer.

Each connection runs two goroutines (`internal/ws/client.go`): a **read pump**
(handles subscribe/unsubscribe) and a **write pump** (drains the send buffer and
sends periodic pings to detect dead connections).

---

## 7.6 Public vs. private channels — and a security trap

Some streams are public (anyone can watch BTC-USDT's price). Others are
**private** — your orders, your balances, your positions. The naive design is to
let clients subscribe to `orders:<userId>` — but then a malicious client could
subscribe to *someone else's* id and watch their private activity. **The client
must not choose whose data it receives.**

Nebula's rule: for private channels the client subscribes with a **bare name** —
`orders`, `balances`, `positions` — and the **server** appends the authenticated
user id from the JWT:

```go
// internal/ws/client.go
func (c *Client) resolveTopic(channel string) (string, bool) {
    if privateChannels[channel] {
        if c.userID == 0 { return "", false }          // must be logged in
        return channel + ":" + strconv.FormatInt(c.userID, 10), true
    }
    return channel, true                                // public: pass through
}
```

So the *server* decides the topic is `orders:7`, using the id it verified from the
token — a client can never read another user's stream. The frontend mirrors this:
it subscribes to `balances` but listens on `balances:<myId>` (see
`web/src/api/ws.ts`). **Authority lives on the server; the client only expresses
intent.**

---

## 7.7 Reconnects

Networks drop. The browser client (`web/src/api/ws.ts`) auto-reconnects with
**exponential backoff** and, crucially, **re-subscribes** all its topics on
reconnect — so a blip is invisible to the user. It also re-opens the socket when
the auth token changes, so private channels start flowing the moment you log in.

---

## Key takeaways

- **Ticker, candles, and the trade tape are all derived from the trade stream;**
  **depth** comes from the live book.
- A **candle** is OHLCV for a time bucket; the current one updates live as trades
  arrive.
- A **WebSocket pub/sub hub** pushes updates by **topic**; slow clients are
  dropped, never allowed to block producers.
- **Private channels are scoped server-side by the authenticated user id** — the
  client expresses intent, the server enforces identity.

## Try it

- Open the browser dev tools → Network → WS while on a trading page. Watch the
  `subscribe` frames go out and the `ticker:`/`depth:`/`trades:` envelopes stream
  back.
- In the same socket, try sending `{"op":"subscribe","channels":["balances"]}`
  while logged out — you get nothing (private + unauthenticated). Log in and the
  server starts delivering `balances:<yourId>`.

## Next

→ [Chapter 8 — Wallets & Simulated Custody](08-wallets-custody.md): how money gets
**in** and **out** of the exchange.
