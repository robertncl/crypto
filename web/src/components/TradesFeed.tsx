import { useEffect, useState } from "react";
import { api } from "../api/client";
import type { Market, Trade } from "../api/types";
import { useChannel } from "../hooks/useStream";
import { fmt, timeAgo } from "../utils/format";

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

  return (
    <section className="panel panel--trades" aria-label="Recent trades">
      <header className="panel__head">
        <h3>Recent Trades</h3>
      </header>
      <div className="tbl tbl--trades">
        <div className="tbl__head">
          <span>Price ({market.quote})</span>
          <span className="r">Amount ({market.base})</span>
          <span className="r">Time</span>
        </div>
        <div className="tbl__body scroll">
          {trades.map((tr) => (
            <div className="tbl__row" key={tr.id}>
              <span className={tr.takerSide === "buy" ? "up" : "down"}>
                <span className="tside" aria-hidden="true">{tr.takerSide === "buy" ? "▲" : "▼"}</span>{fmt(tr.price, 2)}
              </span>
              <span className="r">{fmt(tr.quantity, 5)}</span>
              <span className="r muted">{timeAgo(tr.createdAt)}</span>
            </div>
          ))}
          {trades.length === 0 && <div className="empty">No trades yet</div>}
        </div>
      </div>
    </section>
  );
}
