import { useCallback, useEffect, useState } from "react";
import { Link } from "react-router-dom";
import { api, ApiError } from "../api/client";
import type { PerpMarket, PerpOrder, Position } from "../api/types";
import { useAuth } from "../state/auth";
import { useChannel } from "../hooks/useStream";
import { fmt, fmtSigned, stepDecimals, timeAgo, toNum } from "../utils/format";
import { Alert, Button, Modal, Tabs, tabPanelProps } from "./acme";
import { OrderStatusBadge, Panel, SrOnly } from "./common";

type Tab = "positions" | "open" | "history";

export function PerpPanel({ market }: { market: PerpMarket }) {
  const { user } = useAuth();
  const [tab, setTab] = useState<Tab>("positions");
  const [positions, setPositions] = useState<Position[]>([]);
  const [open, setOpen] = useState<PerpOrder[]>([]);
  const [history, setHistory] = useState<PerpOrder[]>([]);
  const [closing, setClosing] = useState<Position | null>(null);
  const [busy, setBusy] = useState(false);
  const [notice, setNotice] = useState<{ ok: boolean; title: string; text: string } | null>(null);

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

  async function confirmClose() {
    if (!closing) return;
    const p = closing;
    setBusy(true);
    try {
      const o = await api.closePosition(p.market);
      setNotice({ ok: true, title: "Position closed", text: `Closed ${fmt(p.size, qdFor(p.market))} ${p.market} at about ${fmt(toNum(o.avgPrice) > 0 ? o.avgPrice : p.markPrice)}.` });
      loadPositions();
    } catch (err) {
      setNotice({ ok: false, title: "Position not closed", text: err instanceof ApiError ? err.message : "The exchange didn’t respond. Your position is still open." });
    } finally {
      setBusy(false);
      setClosing(null);
    }
  }
  async function cancel(id: string) {
    setOpen((prev) => prev.filter((o) => o.id !== id));
    try { await api.cancelPerpOrder(id); } catch { loadOpen(); }
  }

  if (!user) {
    return (
      <Panel id="positions" title="Your positions">
        <p className="panel__empty"><Link to="/login">Sign in</Link> to see positions and orders.</p>
      </Panel>
    );
  }

  // Positions and open orders span every perp market; use this market's tick when it matches.
  const pdFor = (sym: string) => (sym === market.symbol ? stepDecimals(market.priceTick) : undefined);
  const qdFor = (sym: string) => (sym === market.symbol ? stepDecimals(market.qtyStep) : 4);
  const closingPnl = closing ? toNum(closing.unrealizedPnl) : 0;

  return (
    <Panel
      id="positions"
      title="Your positions"
      tabbed
      aside={
        <Tabs
          tabs={[
            { id: "positions", label: "Positions", count: positions.length },
            { id: "open", label: "Orders", count: open.length },
            { id: "history", label: "History" },
          ]}
          active={tab}
          onChange={setTab}
          idPrefix="pos"
          label="Your positions and orders"
        />
      }
    >
      {notice && (
        <div className="panel__notice">
          <Alert variant={notice.ok ? "success" : "danger"} title={notice.title}>{notice.text}</Alert>
        </div>
      )}
      <div className="table-scroll table-pad" {...tabPanelProps("pos", tab)}>
        {tab === "positions" && (positions.length === 0 ? (
          <p className="panel__empty">No open positions. Go long or short from the order panel.</p>
        ) : (
          <table className="acme-table">
            <caption className="visually-hidden">Open positions</caption>
            <thead>
              <tr>
                <th scope="col">Position</th>
                <th scope="col" className="acme-table__num">Size</th>
                <th scope="col" className="acme-table__num">Entry</th>
                <th scope="col" className="acme-table__num">Mark</th>
                <th scope="col" className="acme-table__num">Liq. price</th>
                <th scope="col" className="acme-table__num">Margin</th>
                <th scope="col" className="acme-table__num">PnL (ROE)</th>
                <th scope="col"><SrOnly>Actions</SrOnly></th>
              </tr>
            </thead>
            <tbody>
              {positions.map((p) => {
                const pnl = toNum(p.unrealizedPnl);
                const roe = toNum(p.margin) > 0 ? (pnl / toNum(p.margin)) * 100 : 0;
                const pd = pdFor(p.market);
                return (
                  <tr key={p.market}>
                    <td>
                      <span className="assetcell__text">
                        <strong>{p.market}</strong>
                        <span className={`cell__sub strong ${p.side === "long" ? "up" : "down"}`}>{p.side === "long" ? "Long" : "Short"} {p.leverage}×</span>
                      </span>
                    </td>
                    <td className="acme-table__num">{fmt(p.size, qdFor(p.market))}</td>
                    <td className="acme-table__num">{fmt(p.entryPrice, pd)}</td>
                    <td className="acme-table__num">{fmt(p.markPrice, pd)}</td>
                    <td className="acme-table__num down">{fmt(p.liqPrice, pd)}</td>
                    <td className="acme-table__num">{fmt(p.margin, 2)}</td>
                    <td className={`acme-table__num ${pnl >= 0 ? "up" : "down"}`}>
                      <span className="strong">{fmtSigned(pnl, 2)}</span>
                      <span className="cell__sub">{fmtSigned(roe, 2)}%</span>
                    </td>
                    <td className="acme-table__num">
                      <Button size="sm" onClick={() => { setNotice(null); setClosing(p); }} aria-label={`Close ${p.market} ${p.side} position`}>Close</Button>
                    </td>
                  </tr>
                );
              })}
            </tbody>
          </table>
        ))}
        {tab === "open" && <OrdersTable orders={open} pdFor={pdFor} qdFor={qdFor} empty="No open orders." onCancel={cancel} />}
        {tab === "history" && <OrdersTable orders={history} pdFor={pdFor} qdFor={qdFor} empty="No order history for this market yet." />}
      </div>

      <Modal
        open={closing !== null}
        title={closing ? `Close your ${closing.market} ${closing.side}?` : ""}
        onClose={() => setClosing(null)}
        dismissOnBackdrop={false}
        footer={
          <>
            <Button onClick={() => setClosing(null)}>Keep position</Button>
            <Button variant="primary" onClick={confirmClose} disabled={busy}>
              {busy ? "Closing…" : closing ? `Close ${fmt(closing.size, qdFor(closing.market))} at market` : "Close at market"}
            </Button>
          </>
        }
      >
        {closing && (
          <>
            <p>
              This closes {fmt(closing.size, qdFor(closing.market))} {closing.market} at market, near {fmt(closing.markPrice, pdFor(closing.market))}.
              About {fmtSigned(closingPnl, 2)} {market.settle} of PnL and {fmt(closing.margin, 2)} {market.settle} of margin return to your balance.
            </p>
            <p>It fills right away at the best price in the book and can’t be undone.</p>
          </>
        )}
      </Modal>
    </Panel>
  );
}

function OrdersTable({ orders, pdFor, qdFor, empty, onCancel }: {
  orders: PerpOrder[];
  pdFor: (sym: string) => number | undefined;
  qdFor: (sym: string) => number;
  empty: string;
  onCancel?: (id: string) => void;
}) {
  if (orders.length === 0) return <p className="panel__empty">{empty}</p>;
  return (
    <table className="acme-table">
      <caption className="visually-hidden">{onCancel ? "Open orders" : "Order history"}</caption>
      <thead>
        <tr>
          <th scope="col">Time</th><th scope="col">Contract</th><th scope="col">Side</th><th scope="col">Type</th>
          <th scope="col" className="acme-table__num">Price</th>
          <th scope="col" className="acme-table__num">Qty</th>
          <th scope="col" className="acme-table__num">Filled</th>
          <th scope="col">Status</th>
          {onCancel && <th scope="col"><SrOnly>Actions</SrOnly></th>}
        </tr>
      </thead>
      <tbody>
        {orders.map((o) => {
          const pd = pdFor(o.market);
          const price = o.type === "market" ? (toNum(o.avgPrice) > 0 ? fmt(o.avgPrice, pd) : "Market") : fmt(o.price, pd);
          return (
            <tr key={o.id}>
              <td className="muted">{timeAgo(o.createdAt)}</td>
              <td>{o.market}</td>
              <td className={`strong ${o.side === "buy" ? "up" : "down"}`}>
                {o.reduceOnly ? (o.side === "buy" ? "Close short" : "Close long") : `${o.side === "buy" ? "Long" : "Short"} ${o.leverage}×`}
              </td>
              <td>{o.type === "limit" ? "Limit" : "Market"}</td>
              <td className="acme-table__num">{price}</td>
              <td className="acme-table__num">{fmt(o.quantity, qdFor(o.market))}</td>
              <td className="acme-table__num">{fmt(o.filled, qdFor(o.market))}</td>
              <td><OrderStatusBadge status={o.status} /></td>
              {onCancel && (
                <td className="acme-table__num">
                  <Button variant="ghost" size="sm" onClick={() => onCancel(o.id)} aria-label={`Cancel ${o.market} order at ${fmt(o.price, pd)}`}>Cancel</Button>
                </td>
              )}
            </tr>
          );
        })}
      </tbody>
    </table>
  );
}
