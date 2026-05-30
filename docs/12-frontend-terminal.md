# Chapter 12 — The Trading Terminal (Frontend)

> **Learning objectives**
> - See how the React client turns backend state into a live **trading terminal**.
> - Understand the **REST + WebSocket** split on the client and the
>   precision-preserving data flow.
> - Learn the layout and real-time patterns behind a dense, fast trading UI.

The backend is the exchange; the frontend (`web/`, React + TypeScript + Vite) is
the **window** onto it. A trading UI has unusual demands: extreme information
density, many simultaneous live streams, and zero tolerance for a frozen price.

---

## 12.1 The shape of the client

```
web/src/
  api/
    types.ts     TypeScript mirrors of the Go models (the wire contract)
    client.ts    typed REST wrapper (fetch + bearer token)
    ws.ts        reconnecting WebSocket client with topic subscriptions
  state/auth.tsx authentication context (token, user, login/logout)
  hooks/         useStream (subscribe to a channel), useBalances
  components/    Chart, OrderBook, TradesFeed, OrderForm, PerpOrderForm,
                 PerpPanel, TickerBar, UserOrders, Layout …
  pages/         Trade (spot), Futures, Markets, Wallet, Login, Register
```

The mental model: **REST loads the initial snapshot; WebSocket keeps it live.**
Open the order book → fetch the current depth once over REST, then subscribe to
`depth:BTC-USDT` and apply every update. This snapshot-then-stream pattern is how
every component stays current without polling.

---

## 12.2 Preserving precision on the client

Chapter 2 warned that JSON numbers are lossy floats. The frontend honors the
contract: every monetary field is a **string**. `web/src/api/types.ts` types them
as `string`, and `utils/format.ts` formats them for display, converting to
`number` only for inherently approximate uses (chart coordinates) via `toNum()`.
Order-entry math that the user sees (cost, margin) uses `number` for *display*, but
the authoritative calculation always happens on the **server**. The client never
decides a balance.

---

## 12.3 The REST client

`client.ts` is a thin typed wrapper over `fetch`. It holds the JWT in module scope
and attaches it to every request, and turns non-2xx responses into a typed
`ApiError` the UI can react to (e.g. show "insufficient funds"):

```ts
async function request<T>(path: string, init?: RequestInit): Promise<T> {
  const headers = new Headers(init?.headers);
  if (token) headers.set("Authorization", `Bearer ${token}`);
  const res = await fetch(`/api${path}`, { ...init, headers });
  const data = await res.json();
  if (!res.ok) throw new ApiError(res.status, data?.error);
  return data as T;
}
```

Every endpoint is one line on the exported `api` object — `api.depth(...)`,
`api.placeOrder(...)`, `api.placePerpOrder(...)` — giving the whole UI a single,
discoverable, typed surface to the backend.

---

## 12.4 The WebSocket client and the `useChannel` hook

`ws.ts` is the browser twin of the server hub (Chapter 7): one shared socket,
topic subscriptions, auto-reconnect with backoff, and **re-subscribe on
reconnect**. It also encodes the private-channel security model from the client
side: it subscribes with the bare name (`balances`) but *listens* on the
server-scoped key (`balances:<myId>`), so the server is always the authority on
identity.

React components tap into it through a tiny hook:

```ts
// hooks/useStream.ts
export function useChannel<T>(channel: string | null, handler: (d: T) => void) {
  const ref = useRef(handler); ref.current = handler;
  useEffect(() => {
    if (!channel) return;
    return wsClient.subscribe(channel, (d) => ref.current(d as T));
  }, [channel]);
}
```

So a component "goes live" in one line: `useChannel('trades:BTC-USDT', addTrade)`.
The `ref` trick keeps the subscription stable across re-renders — important when
prices are updating many times a second.

---

## 12.5 The terminal layout

A trading screen packs a chart, order book, trade tape, order form, and your
orders/positions into one view. That's a job for **CSS Grid** with named areas
(`web/src/index.css`):

```css
.terminal {
  display: grid;
  grid-template-columns: minmax(0,1fr) 320px 340px;
  grid-template-areas:
    "ticker ticker ticker"
    "chart  book   form"
    "orders trades form";
}
```

You can *read the screen layout in the CSS* — the chart and your orders on the
left, the book and tape in the middle, the order form spanning the right. The grid
collapses to a single column on narrow screens via media queries.

Two performance touches worth calling out:

- The high-frequency panels (order book, trade tape) get `contain: layout style
  paint`, which **isolates their re-rendering** so a flickering book doesn't force
  the whole page to re-layout.
- The chart uses **lightweight-charts** (the open-source library behind many
  exchange charts), fed historical candles over REST and live updates over the
  `kline:` channel.

---

## 12.6 Composing a page

`pages/Trade.tsx` is just an assembly of live components sharing one market:

```tsx
<TickerBar market={market} />          {/* ticker: channel */}
<Chart market={market} />              {/* candles + kline: channel */}
<OrderBook market={market} onPickPrice={setPrice} />   {/* depth: channel */}
<TradesFeed market={market} />         {/* trades: channel */}
<OrderForm market={market} pickedPrice={price} />      {/* POST /orders */}
<UserOrders market={market} />         {/* private orders: + balances: */}
```

Each component owns its own data: it fetches its snapshot and subscribes to its
channel. Click a price in the order book and `onPickPrice` drops it into the order
form — small touches that make a terminal feel professional.

`pages/Futures.tsx` reuses the *same* chart, book, and tape components (a perp's
market data is identical in shape to spot's) and swaps in a **leverage-aware order
form**, a **positions panel** (live PnL, liquidation price, one-click close), and a
**funding bar** (mark, index, rate, countdown) — the UI expression of Chapters
9–10.

---

## 12.7 Auth on the client

`state/auth.tsx` is a React context holding the token and user. On login it stores
the JWT (in `localStorage` so a refresh keeps you signed in), points the REST
client at it, and **re-authenticates the WebSocket** so private channels start
flowing. Log out and it clears all three. It's the client mirror of Chapter 4.

---

## Key takeaways

- The client is **REST for the initial snapshot, WebSocket for the live stream** —
  snapshot-then-stream per component.
- It preserves precision by treating money as **strings**, converting to `number`
  only for display/chart use.
- A typed **REST client** and a reconnecting **WS client + `useChannel` hook** give
  the whole UI a clean, live data layer.
- The terminal is **CSS Grid named areas**; hot panels use CSS **containment**;
  the chart uses lightweight-charts.
- **Futures reuses spot's market-data components**, adding leverage, positions, and
  funding UI.

## Try it

- Open the dev tools Network tab and load the Trade page: see the burst of REST
  snapshot calls (`/markets`, `/depth`, `/candles`, `/trades`) followed by a single
  long-lived WS connection carrying everything after.
- Open the same market in two browser tabs; place an order in one and watch the
  *other* tab's order book and trade tape update over WebSocket with no refresh.

## Next

→ [Chapter 13 — Running, Testing & Extending](13-running-testing-extending.md): get
it running yourself, see the tests, and find your first project.
