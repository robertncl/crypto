import { useEffect, useState } from "react";
import { api } from "../api/client";
import type { Market, Trade } from "../api/types";
import { useChannel } from "../hooks/useStream";
import { fmt, stepDecimals, timeAgo } from "../utils/format";
import { Panel } from "./common";

const MAX = 40;

export function TradesFeed({ market }: { market: Market }) {
  const [trades, setTrades] = useState<Trade[]>([]);

  useEffect(() => {
    let live = true;
    setTrades([]);
    api.marketTrades(market.symbol, MAX).then((t) => {
      if (live) setTrades(t);
    }).catch(() => {});
    return () => { live = false; };
  }, [market.symbol]);

  useChannel<Trade>(`trades:${market.symbol}`, (tr) => {
    setTrades((prev) => [tr, ...prev].slice(0, MAX));
  });

  const pd = stepDecimals(market.priceTick);
  const qd = stepDecimals(market.qtyStep);

  return (
    <Panel id="tape" title="Recent trades" className="tape">
      <div className="tape__cols" aria-hidden="true">
        <span>Price ({market.quote})</span>
        <span className="r">Amount ({market.base})</span>
        <span className="r">Time</span>
      </div>
      <div className="tape__body">
        {trades.map((tr) => (
          <div className="tape__row" key={tr.id}>
            <span className={`tape__price ${tr.takerSide === "buy" ? "up" : "down"}`}>
              {fmt(tr.price, pd)}
              <span className="visually-hidden">{tr.takerSide === "buy" ? " buy" : " sell"}</span>
            </span>
            <span className="r">{fmt(tr.quantity, qd)}</span>
            <span className="r muted">{timeAgo(tr.createdAt)}</span>
          </div>
        ))}
        {trades.length === 0 && <p className="panel__empty">No trades yet.</p>}
      </div>
    </Panel>
  );
}
