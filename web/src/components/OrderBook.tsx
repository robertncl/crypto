import { useEffect, useMemo, useState } from "react";
import { api } from "../api/client";
import type { Depth, DepthLevel, Market } from "../api/types";
import { useChannel } from "../hooks/useStream";
import { fmt, toNum } from "../utils/format";

const ROWS = 13;

interface Row {
  price: string;
  qty: string;
  cum: number; // cumulative base quantity from the best price
}

function build(levels: DepthLevel[]): Row[] {
  let cum = 0;
  return levels.slice(0, ROWS).map((l) => {
    cum += toNum(l.qty);
    return { price: l.price, qty: l.qty, cum };
  });
}

export function OrderBook({ market, onPickPrice, fetchDepth }: {
  market: Market;
  onPickPrice: (p: string) => void;
  fetchDepth?: (symbol: string, limit: number) => Promise<Depth>;
}) {
  const [depth, setDepth] = useState<Depth>({ market: market.symbol, bids: [], asks: [] });

  useEffect(() => {
    let live = true;
    const load = fetchDepth ?? api.depth;
    load(market.symbol, 50).then((d) => { if (live) setDepth(d); }).catch(() => {});
    return () => { live = false; };
  }, [market.symbol, fetchDepth]);

  useChannel<Depth>(`depth:${market.symbol}`, setDepth);

  const asks = useMemo(() => build(depth.asks), [depth.asks]);
  const bids = useMemo(() => build(depth.bids), [depth.bids]);
  const maxCum = Math.max(asks.at(-1)?.cum ?? 0, bids.at(-1)?.cum ?? 0, 1);

  const bestBid = toNum(bids[0]?.price);
  const bestAsk = toNum(asks[0]?.price);
  const spread = bestBid && bestAsk ? bestAsk - bestBid : 0;
  const spreadPct = bestBid ? (spread / bestBid) * 100 : 0;

  return (
    <section className="panel panel--book" aria-label="Order book">
      <header className="panel__head">
        <h2>Order Book</h2>
      </header>
      <div className="book">
        <div className="book__cols muted">
          <span>Price ({market.quote})</span>
          <span className="r">Amount ({market.base})</span>
          <span className="r">Total</span>
        </div>

        <div className="book__side book__side--asks scroll">
          {[...asks].reverse().map((r) => (
            <button className="book__row" key={`a${r.price}`} onClick={() => onPickPrice(r.price)} aria-label={`Use ask price ${fmt(r.price, 2)}`}>
              <span className="book__bar book__bar--ask" style={{ width: `${(r.cum / maxCum) * 100}%` }} />
              <span className="down">{fmt(r.price, 2)}</span>
              <span className="r">{fmt(r.qty, 5)}</span>
              <span className="r muted">{fmt(r.cum, 4)}</span>
            </button>
          ))}
          {asks.length === 0 && <div className="empty">—</div>}
        </div>

        <div className="book__spread">
          <span className="book__spread-val">{spread ? fmt(spread, 2) : "—"}</span>
          <span className="muted">Spread {spread ? `${spreadPct.toFixed(3)}%` : ""}</span>
        </div>

        <div className="book__side book__side--bids scroll">
          {bids.map((r) => (
            <button className="book__row" key={`b${r.price}`} onClick={() => onPickPrice(r.price)} aria-label={`Use bid price ${fmt(r.price, 2)}`}>
              <span className="book__bar book__bar--bid" style={{ width: `${(r.cum / maxCum) * 100}%` }} />
              <span className="up">{fmt(r.price, 2)}</span>
              <span className="r">{fmt(r.qty, 5)}</span>
              <span className="r muted">{fmt(r.cum, 4)}</span>
            </button>
          ))}
          {bids.length === 0 && <div className="empty">—</div>}
        </div>
      </div>
    </section>
  );
}
