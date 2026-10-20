import { useCallback, useEffect, useState } from "react";
import { Link } from "react-router-dom";
import { api } from "../api/client";
import type { Market, Order, Trade } from "../api/types";
import { useAuth } from "../state/auth";
import { useChannel } from "../hooks/useStream";
import { fmt, stepDecimals, timeAgo } from "../utils/format";
import { Button, Tabs, tabPanelProps } from "./acme";
import { OrderStatusBadge, Panel, SrOnly } from "./common";

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
      <Panel id="orders" title="Your orders">
        <p className="panel__empty"><Link to="/login">Sign in</Link> to see your orders.</p>
      </Panel>
    );
  }

  return (
    <Panel
      id="orders"
      title="Your orders"
      tabbed
      aside={
        <Tabs
          tabs={[
            { id: "open", label: "Open", count: open.length },
            { id: "history", label: "History" },
            { id: "trades", label: "Trades" },
          ]}
          active={tab}
          onChange={setTab}
          idPrefix="orders"
          label="Your orders"
        />
      }
    >
      <div className="table-scroll table-pad" {...tabPanelProps("orders", tab)}>
        {tab === "open" && <OrdersTable orders={open} market={market} empty="No open orders. Limit orders you place show here until they fill." onCancel={cancel} />}
        {tab === "history" && <OrdersTable orders={history} market={market} empty="No order history yet." />}
        {tab === "trades" && <TradesTable trades={trades} market={market} />}
      </div>
    </Panel>
  );
}

function OrdersTable({ orders, market, empty, onCancel }: { orders: Order[]; market: Market; empty: string; onCancel?: (id: string) => void }) {
  if (orders.length === 0) return <p className="panel__empty">{empty}</p>;
  const pd = stepDecimals(market.priceTick);
  const qd = stepDecimals(market.qtyStep);
  return (
    <table className="acme-table">
      <caption className="visually-hidden">{onCancel ? "Open orders" : "Order history"}</caption>
      <thead>
        <tr>
          <th scope="col">Time</th><th scope="col">Side</th><th scope="col">Type</th>
          <th scope="col" className="acme-table__num">Price</th>
          <th scope="col" className="acme-table__num">Amount</th>
          <th scope="col" className="acme-table__num">Filled ({market.base})</th>
          <th scope="col">Status</th>
          {onCancel && <th scope="col"><SrOnly>Actions</SrOnly></th>}
        </tr>
      </thead>
      <tbody>
        {orders.map((o) => (
          <tr key={o.id}>
            <td className="muted">{timeAgo(o.createdAt)}</td>
            <td className={`strong ${o.side === "buy" ? "up" : "down"}`}>{o.side === "buy" ? "Buy" : "Sell"}</td>
            <td>{o.type === "limit" ? "Limit" : "Market"}</td>
            <td className="acme-table__num">{o.type === "market" ? "Market" : fmt(o.price, pd)}</td>
            {/* A market buy is sized in the quote currency. */}
            <td className="acme-table__num">
              {o.type === "market" && o.side === "buy" ? `${fmt(o.quantity, 2)} ${market.quote}` : `${fmt(o.quantity, qd)} ${market.base}`}
            </td>
            <td className="acme-table__num">{fmt(o.filled, qd)}</td>
            <td><OrderStatusBadge status={o.status} /></td>
            {onCancel && (
              <td className="acme-table__num">
                <Button variant="ghost" size="sm" onClick={() => onCancel(o.id)} aria-label={`Cancel ${o.side} order at ${fmt(o.price, pd)}`}>Cancel</Button>
              </td>
            )}
          </tr>
        ))}
      </tbody>
    </table>
  );
}

function TradesTable({ trades, market }: { trades: Trade[]; market: Market }) {
  if (trades.length === 0) return <p className="panel__empty">No trades yet.</p>;
  const pd = stepDecimals(market.priceTick);
  const qd = stepDecimals(market.qtyStep);
  return (
    <table className="acme-table">
      <caption className="visually-hidden">Your trades</caption>
      <thead>
        <tr>
          <th scope="col">Time</th><th scope="col">Side</th>
          <th scope="col" className="acme-table__num">Price</th>
          <th scope="col" className="acme-table__num">Amount ({market.base})</th>
          <th scope="col" className="acme-table__num">Value ({market.quote})</th>
          <th scope="col">Trade ID</th>
        </tr>
      </thead>
      <tbody>
        {trades.map((t) => (
          <tr key={t.id}>
            <td className="muted">{timeAgo(t.createdAt)}</td>
            <td className={`strong ${t.takerSide === "buy" ? "up" : "down"}`}>{t.takerSide === "buy" ? "Buy" : "Sell"}</td>
            <td className="acme-table__num">{fmt(t.price, pd)}</td>
            <td className="acme-table__num">{fmt(t.quantity, qd)}</td>
            <td className="acme-table__num">{fmt(t.quoteQty, 2)}</td>
            <td className="mono muted">#{t.id}</td>
          </tr>
        ))}
      </tbody>
    </table>
  );
}
