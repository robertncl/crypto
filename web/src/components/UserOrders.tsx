import { useCallback, useEffect, useState } from "react";
import { api } from "../api/client";
import type { Market, Order, Trade } from "../api/types";
import { useAuth } from "../state/auth";
import { useChannel } from "../hooks/useStream";
import { fmt, shortId, timeAgo } from "../utils/format";

type Tab = "open" | "history" | "trades";

export function UserOrders({ market }: { market: Market }) {
  const { user } = useAuth();
  const [tab, setTab] = useState<Tab>("open");
  const [open, setOpen] = useState<Order[]>([]);
  const [history, setHistory] = useState<Order[]>([]);
  const [trades, setTrades] = useState<Trade[]>([]);

  const sym = market.symbol;

  const loadOpen = useCallback(() => {
    if (!user) return setOpen([]);
    api.openOrders(sym).then(setOpen).catch(() => {});
  }, [user, sym]);
  const loadHistory = useCallback(() => {
    if (!user) return setHistory([]);
    api.orderHistory(sym).then(setHistory).catch(() => {});
  }, [user, sym]);
  const loadTrades = useCallback(() => {
    if (!user) return setTrades([]);
    api.myTrades(sym).then(setTrades).catch(() => {});
  }, [user, sym]);

  useEffect(() => { loadOpen(); }, [loadOpen]);
  useEffect(() => {
    if (tab === "history") loadHistory();
    if (tab === "trades") loadTrades();
  }, [tab, loadHistory, loadTrades]);

  // Live order updates: keep the open list current and refresh the visible tab.
  useChannel<Order>(user ? "orders" : null, (o) => {
    if (o.market !== sym) return;
    setOpen((prev) => {
      const rest = prev.filter((x) => x.id !== o.id);
      return o.status === "open" || o.status === "partial" ? [o, ...rest] : rest;
    });
    if (tab === "history") loadHistory();
    if (tab === "trades") loadTrades();
  });

  async function cancel(id: string) {
    setOpen((prev) => prev.filter((o) => o.id !== id)); // optimistic
    try { await api.cancelOrder(id); } catch { loadOpen(); }
  }

  if (!user) {
    return (
      <section className="panel panel--orders">
        <div className="empty pad">Log in to view your orders.</div>
      </section>
    );
  }

  return (
    <section className="panel panel--orders">
      <header className="panel__head">
        <div className="seg">
          <button className={`seg__btn ${tab === "open" ? "is-active" : ""}`} onClick={() => setTab("open")}>
            Open Orders {open.length > 0 && <span className="pill">{open.length}</span>}
          </button>
          <button className={`seg__btn ${tab === "history" ? "is-active" : ""}`} onClick={() => setTab("history")}>Order History</button>
          <button className={`seg__btn ${tab === "trades" ? "is-active" : ""}`} onClick={() => setTab("trades")}>Trade History</button>
        </div>
      </header>

      <div className="scroll orders-scroll">
        {tab === "open" && (
          <OrdersTable orders={open} empty="No open orders" market={market} onCancel={cancel} />
        )}
        {tab === "history" && <OrdersTable orders={history} empty="No order history" market={market} />}
        {tab === "trades" && <TradesTable trades={trades} market={market} />}
      </div>
    </section>
  );
}

function OrdersTable({ orders, empty, market, onCancel }: {
  orders: Order[]; empty: string; market: Market; onCancel?: (id: string) => void;
}) {
  if (orders.length === 0) return <div className="empty pad">{empty}</div>;
  return (
    <table className="dtable">
      <thead>
        <tr>
          <th>Time</th><th>Side</th><th>Type</th>
          <th className="r">Price</th><th className="r">Amount</th><th className="r">Filled</th>
          <th>Status</th>{onCancel && <th></th>}
        </tr>
      </thead>
      <tbody>
        {orders.map((o) => (
          <tr key={o.id}>
            <td className="muted">{timeAgo(o.createdAt)}</td>
            <td className={o.side === "buy" ? "up" : "down"}>{o.side}</td>
            <td>{o.type}</td>
            <td className="r">{o.type === "market" ? "—" : fmt(o.price, 2)}</td>
            <td className="r">{fmt(o.quantity, 5)}</td>
            <td className="r">{fmt(o.filled, 5)}</td>
            <td><span className={`status status--${o.status}`}>{o.status}</span></td>
            {onCancel && (
              <td className="r">
                <button className="btn btn--mini" onClick={() => onCancel(o.id)}>Cancel</button>
              </td>
            )}
          </tr>
        ))}
      </tbody>
    </table>
  );
}

function TradesTable({ trades, market }: { trades: Trade[]; market: Market }) {
  if (trades.length === 0) return <div className="empty pad">No trades yet</div>;
  return (
    <table className="dtable">
      <thead>
        <tr>
          <th>Time</th><th>Side</th>
          <th className="r">Price</th><th className="r">Amount ({market.base})</th>
          <th className="r">Value ({market.quote})</th><th>Trade</th>
        </tr>
      </thead>
      <tbody>
        {trades.map((t) => (
          <tr key={t.id}>
            <td className="muted">{timeAgo(t.createdAt)}</td>
            <td className={t.takerSide === "buy" ? "up" : "down"}>{t.takerSide}</td>
            <td className="r">{fmt(t.price, 2)}</td>
            <td className="r">{fmt(t.quantity, 5)}</td>
            <td className="r">{fmt(t.quoteQty, 2)}</td>
            <td className="muted">#{t.id}</td>
          </tr>
        ))}
      </tbody>
    </table>
  );
}
