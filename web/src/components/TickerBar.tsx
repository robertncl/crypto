import { useEffect, useState } from "react";
import { api } from "../api/client";
import type { Market, Ticker } from "../api/types";
import { useChannel } from "../hooks/useStream";
import { fmt, fmtCompact, fmtPct, toNum } from "../utils/format";

export function TickerBar({ market }: { market: Market }) {
  const [t, setT] = useState<Ticker | null>(null);

  useEffect(() => {
    let live = true;
    api.tickers().then((all) => {
      if (live) setT(all.find((x) => x.market === market.symbol) ?? null);
    }).catch(() => {});
    return () => { live = false; };
  }, [market.symbol]);

  useChannel<Ticker>(`ticker:${market.symbol}`, setT);

  const up = toNum(t?.change) >= 0;

  return (
    <div className="tickerbar">
      <div className="tickerbar__symbol">
        <span className="tickerbar__pair">{market.base}<span className="muted">/{market.quote}</span></span>
        <span className="badge">Spot</span>
      </div>
      <div className="tickerbar__last">
        <span className={up ? "up" : "down"}>{fmt(t?.last ?? "0", 2)}</span>
      </div>
      <Stat label="24h Change" value={
        <span className={up ? "up" : "down"}>{fmtPct(t?.changePct ?? "0")}</span>
      } />
      <Stat label="24h High" value={fmt(t?.high24h ?? "0", 2)} />
      <Stat label="24h Low" value={fmt(t?.low24h ?? "0", 2)} />
      <Stat label={`24h Vol (${market.base})`} value={fmtCompact(t?.volume24h ?? "0")} />
      <Stat label={`24h Vol (${market.quote})`} value={fmtCompact(t?.quoteVol24h ?? "0")} />
    </div>
  );
}

function Stat({ label, value }: { label: string; value: React.ReactNode }) {
  return (
    <div className="stat">
      <div className="stat__label">{label}</div>
      <div className="stat__value">{value}</div>
    </div>
  );
}
