// Reconnecting WebSocket client with topic subscriptions.
//
// Channels are either public ("ticker:BTC-USDT", "depth:BTC-USDT",
// "trades:BTC-USDT", "kline:BTC-USDT:60") or private ("orders", "balances",
// "walletTxns"). For private channels the client SUBSCRIBES with the bare name
// so the server can scope the topic to the authenticated user id; the server
// then publishes to "orders:<uid>", which the client listens for under that
// resolved key. This keeps the server authoritative — a client cannot read
// another user's private stream by guessing a topic name.

type Listener = (data: unknown) => void;

const PRIVATE = new Set(["orders", "balances", "walletTxns", "perpOrders", "positions"]);

interface Sub {
  sendChannel: string; // what we send in the subscribe op
  listeners: Set<Listener>;
}

class WSClient {
  private ws: WebSocket | null = null;
  private subs = new Map<string, Sub>(); // listenKey -> sub
  private token: string | null = null;
  private userId: number | null = null;
  private reconnectTimer: number | null = null;
  private backoff = 1000;
  private shouldRun = false;

  setAuth(token: string | null, userId: number | null) {
    this.token = token;
    this.userId = userId;
    // Reconnect so the new token applies to private channels.
    if (this.shouldRun) this.reconnect();
  }

  private url(): string {
    const proto = location.protocol === "https:" ? "wss:" : "ws:";
    const q = this.token ? `?token=${encodeURIComponent(this.token)}` : "";
    return `${proto}//${location.host}/ws${q}`;
  }

  start() {
    this.shouldRun = true;
    this.open();
  }

  private open() {
    if (this.ws && (this.ws.readyState === WebSocket.OPEN || this.ws.readyState === WebSocket.CONNECTING)) {
      return;
    }
    const ws = new WebSocket(this.url());
    this.ws = ws;
    ws.onopen = () => {
      this.backoff = 1000;
      // Re-subscribe everything on (re)connect.
      const channels = [...new Set([...this.subs.values()].map((s) => s.sendChannel))];
      if (channels.length) this.rawSend("subscribe", channels);
    };
    ws.onmessage = (ev) => {
      try {
        const env = JSON.parse(ev.data as string) as { channel: string; data: unknown };
        const sub = this.subs.get(env.channel);
        if (sub) sub.listeners.forEach((fn) => fn(env.data));
      } catch {
        /* ignore malformed frames */
      }
    };
    ws.onclose = () => {
      this.ws = null;
      if (this.shouldRun) this.scheduleReconnect();
    };
    ws.onerror = () => ws.close();
  }

  private scheduleReconnect() {
    if (this.reconnectTimer != null) return;
    this.reconnectTimer = window.setTimeout(() => {
      this.reconnectTimer = null;
      this.open();
    }, this.backoff);
    this.backoff = Math.min(this.backoff * 1.6, 10000);
  }

  private reconnect() {
    if (this.ws) this.ws.close();
    else this.open();
  }

  private rawSend(op: "subscribe" | "unsubscribe", channels: string[]) {
    if (this.ws?.readyState === WebSocket.OPEN) {
      this.ws.send(JSON.stringify({ op, channels }));
    }
  }

  private listenKey(channel: string): string {
    if (PRIVATE.has(channel) && this.userId != null) return `${channel}:${this.userId}`;
    return channel;
  }

  /** Subscribe to a channel; returns an unsubscribe function. */
  subscribe(channel: string, cb: Listener): () => void {
    if (PRIVATE.has(channel) && this.userId == null) return () => {};
    const key = this.listenKey(channel);
    let sub = this.subs.get(key);
    if (!sub) {
      sub = { sendChannel: channel, listeners: new Set() };
      this.subs.set(key, sub);
      this.rawSend("subscribe", [channel]);
    }
    sub.listeners.add(cb);
    return () => {
      const s = this.subs.get(key);
      if (!s) return;
      s.listeners.delete(cb);
      if (s.listeners.size === 0) {
        this.subs.delete(key);
        this.rawSend("unsubscribe", [channel]);
      }
    };
  }
}

export const wsClient = new WSClient();
