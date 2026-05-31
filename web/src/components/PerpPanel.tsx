import { useCallback, useEffect, useState } from "react";
import { api } from "../api/client";
import type { PerpMarket, PerpOrder, Position } from "../api/types";
import { useAuth } from "../state/auth";
import { useChannel } from "../hooks/useStream";
import { fmt, timeAgo, toNum } from "../utils/format";

type Tab = "positions" | "open" | "history";

export function PerpPanel({ market }: { market: PerpMarket }) {
  const { user } = useAuth();
  const [tab, setTab] = useState<Tab>("positions");
  const [positions, setPositions] = useState<Position[]>([]);
  const [open, setOpen] = useState<PerpOrder[]>([]);
  const [history, setHistory] = useState<PerpOrder[]>([]);

  const loadPositions = useCallback(() => {
    if (!user) return setPositions([]);
    api.positions().then(setPositions).catch(() => {});
  }, [user]);
  const loadOpen = useCallback(() => {
    if (!user) return setOpen([]);
    api.openPerpOrders().then(setOpen).catch(() => {});
  }, [user]);
  const loadHistory = useCallback(() => {
    if (!user) return setHistory([]);
    api.perpOrderHistory(market.symbol).then(setHistory).catch(() => {});
  }, [user, market.symbol]);

  useEffect(() => { loadPositions(); loadOpen(); }, [loadPositions, loadOpen]);
  useEffect(() => { if (tab === "history") loadHistory(); }, [tab, loadHistory]);

  useChannel<Position>(user ? "positions" : null, (p) => {
    setPositions((prev) => {
      const rest = prev.filter((x) => x.market !== p.market);
      return p.side === "flat" || toNum(p.size) <= 0 ? rest : [...rest, p];
    });
  });
  useChannel<PerpOrder>(user ? "perpOrders" : null, (o) => {
    setOpen((prev) => {
      const rest = prev.filter((x) => x.id !== o.id);
      return o.status === "open" || o.status === "partial" ? [o, ...rest] : rest;
    });
    if (tab === "history") loadHistory();
    loadPositions(); // a fill changed a position
  });

  async function close(symbol: string) {
    try { await api.closePosition(symbol); loadPositions(); } catch { /* ignore */ }
  }
  async function cancel(id: string) {
    setOpen((prev) => prev.filter((o) => o.id !== id));
    try { await api.cancelPerpOrder(id); } catch { loadOpen(); }
  }

  if (!user) {
    return <section className="panel panel--orders"><div className="empty pad">Log in to view positions and orders.</div></section>;
  }

  return (
    <section className="panel panel--orders">
      <header className="panel__head">
        <div className="seg">
          <button className={`seg__btn ${tab === "positions" ? "is-active" : ""}`} onClick={() => setTab("positions")}>
            Positions {positions.length > 0 && <span className="pill">{positions.length}</span>}
          </button>
          <button className={`seg__btn ${tab === "open" ? "is-active" : ""}`} onClick={() => setTab("open")}>
            Open Orders {open.length > 0 && <span className="pill">{open.length}</span>}
          </button>
          <button className={`seg__btn ${tab === "history" ? "is-active" : ""}`} onClick={() => setTab("history")}>History</button>
        </div>
      </header>

      <div className="scroll orders-scroll">
        {tab === "positions" && <PositionsTable positions={positions} onClose={close} />}
        {tab === "open" && <PerpOrdersTable orders={open} onCancel={cancel} />}
        {tab === "history" && <PerpOrdersTable orders={history} />}
      </div>
    </section>
  );
}

function PositionsTable({ positions, onClose }: { positions: Position[]; onClose: (s: string) => void }) {
  if (positions.length === 0) return <div className="empty pad">No open positions</div>;
  return (
    <table className="dtable">
      <thead>
        <tr>
          <th scope="col">Market</th><th scope="col">Side</th><th scope="col" className="r">Size</th><th scope="col" className="r">Entry</th>
          <th scope="col" className="r">Mark</th><th scope="col" className="r">Liq. Price</th><th scope="col" className="r">Margin</th>
          <th scope="col" className="r">Unrealized PnL (ROE)</th><th><span className="visually-hidden">Actions</span></th>
        </tr>
      </thead>
      <tbody>
        {positions.map((p) => {
          const pnl = toNum(p.unrealizedPnl);
          const roe = toNum(p.margin) > 0 ? (pnl / toNum(p.margin)) * 100 : 0;
          return (
            <tr key={p.market}>
              <td><strong>{p.market}</strong></td>
              <td className={p.side === "long" ? "up" : "down"}>{p.side} {p.leverage}×</td>
              <td className="r mono">{fmt(p.size, 4)}</td>
              <td className="r mono">{fmt(p.entryPrice, 2)}</td>
              <td className="r mono">{fmt(p.markPrice, 2)}</td>
              <td className="r mono down">{fmt(p.liqPrice, 2)}</td>
              <td className="r mono">{fmt(p.margin, 2)}</td>
              <td className={`r mono ${pnl >= 0 ? "up" : "down"}`}>{fmt(pnl, 2)} ({roe >= 0 ? "+" : ""}{roe.toFixed(2)}%)</td>
              <td className="r"><button className="btn btn--mini" onClick={() => onClose(p.market)}>Close</button></td>
            </tr>
          );
        })}
      </tbody>
    </table>
  );
}

function PerpOrdersTable({ orders, onCancel }: { orders: PerpOrder[]; onCancel?: (id: string) => void }) {
  if (orders.length === 0) return <div className="empty pad">{onCancel ? "No open orders" : "No order history"}</div>;
  return (
    <table className="dtable">
      <thead>
        <tr>
          <th scope="col">Time</th><th scope="col">Market</th><th scope="col">Side</th><th scope="col">Type</th>
          <th scope="col" className="r">Price</th><th scope="col" className="r">Qty</th><th scope="col" className="r">Filled</th>
          <th scope="col">Status</th>{onCancel && <th><span className="visually-hidden">Actions</span></th>}
        </tr>
      </thead>
      <tbody>
        {orders.map((o) => (
          <tr key={o.id}>
            <td className="muted">{timeAgo(o.createdAt)}</td>
            <td>{o.market}</td>
            <td className={o.side === "buy" ? "up" : "down"}>
              {o.side === "buy" ? "long" : "short"} {o.leverage}×{o.reduceOnly ? " R" : ""}
            </td>
            <td>{o.type}</td>
            <td className="r mono">{o.type === "market" ? "—" : fmt(o.price, 2)}</td>
            <td className="r mono">{fmt(o.quantity, 4)}</td>
            <td className="r mono">{fmt(o.filled, 4)}</td>
            <td><span className={`status status--${o.status}`}>{o.status}</span></td>
            {onCancel && <td className="r"><button className="btn btn--mini" onClick={() => onCancel(o.id)}>Cancel</button></td>}
          </tr>
        ))}
      </tbody>
    </table>
  );
}
