import { useEffect, useMemo, useState, type CSSProperties } from "react";
import { api } from "../api/client";
import type { Depth, DepthLevel, Market } from "../api/types";
import { useChannel } from "../hooks/useStream";
import { fmt, stepDecimals, toNum } from "../utils/format";
import { Direction, Panel } from "./common";

const ROWS = 10;

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

export function OrderBook({ market, last, lastUp, onPickPrice, fetchDepth }: {
  market: Market;
  /** Last traded price, shown between the asks and bids. */
  last: string;
  lastUp: boolean;
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

  const pd = stepDecimals(market.priceTick);
  const qd = stepDecimals(market.qtyStep);
  const bestBid = toNum(bids[0]?.price);
  const bestAsk = toNum(asks[0]?.price);
  const spread = bestBid && bestAsk ? bestAsk - bestBid : 0;
  const spreadPct = bestBid ? (spread / bestBid) * 100 : 0;
  const rows = { "--rows": ROWS } as CSSProperties;

  function row(r: Row, kind: "ask" | "bid") {
    return (
      <button
        type="button"
        className="book__row"
        key={`${kind}${r.price}`}
        onClick={() => onPickPrice(r.price)}
        aria-label={`Use ${kind} price ${fmt(r.price, pd)}`}
      >
        <span className="book__bar" style={{ inlineSize: `${(r.cum / maxCum) * 100}%` }} />
        <span className={`book__price ${kind === "ask" ? "down" : "up"}`}>{fmt(r.price, pd)}</span>
        <span className="r">{fmt(r.qty, qd)}</span>
        <span className="r muted">{fmt(r.cum, qd)}</span>
      </button>
    );
  }

  return (
    <Panel id="book" title="Order book" aside={<span className="panel__hint">Select a price to use it</span>} className="book">
      <div className="book__cols" aria-hidden="true">
        <span>Price ({market.quote})</span>
        <span className="r">Amount ({market.base})</span>
        <span className="r">Total ({market.base})</span>
      </div>
      <div className="book__side book__side--asks" style={rows}>
        {[...asks].reverse().map((r) => row(r, "ask"))}
      </div>
      <div className="book__mid">
        <Direction up={lastUp} className="book__last">{toNum(last) > 0 ? fmt(last, pd) : "—"}</Direction>
        <span className="book__spread">Spread {spread ? `${fmt(spread, pd)} (${spreadPct.toFixed(3)}%)` : "—"}</span>
      </div>
      <div className="book__side" style={rows}>
        {bids.map((r) => row(r, "bid"))}
      </div>
    </Panel>
  );
}
