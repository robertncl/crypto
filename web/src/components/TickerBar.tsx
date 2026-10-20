import type { ReactNode } from "react";
import type { Market, Ticker } from "../api/types";
import { fmt, fmtCompact, fmtPct, fmtSigned, stepDecimals, toNum } from "../utils/format";
import { AssetIcon, Direction } from "./common";

/**
 * The band above the terminal: market identity (the page's h1), the live last
 * price, and 24h stats. `children` are extra <Stat>s shown first (the futures
 * page adds mark, index and funding).
 */
export function TickerBar({ market, ticker: t, title, subtitle, badge, children }: {
  market: Market;
  ticker: Ticker | null;
  title: string;
  subtitle: string;
  badge?: ReactNode;
  children?: ReactNode;
}) {
  const pd = stepDecimals(market.priceTick);
  const up = toNum(t?.change) >= 0;

  return (
    <div className="tickerband">
      <div className="tickerbar">
        <div className="tickerbar__id">
          <AssetIcon symbol={market.base} large />
          <div className="tickerbar__title">
            <div className="tickerbar__name"><h1>{title}</h1>{badge}</div>
            <span className="tickerbar__sub">{subtitle}</span>
          </div>
        </div>
        <div className="tickerbar__last" aria-live="off">
          <Direction up={up} className="tickerbar__price">{t ? fmt(t.last, pd) : "—"}</Direction>
        </div>
        <dl className="tickerbar__stats">
          {children}
          <Stat label="24h change">
            <span className={up ? "up" : "down"}>{t ? `${fmtSigned(t.change, pd)} (${fmtPct(t.changePct)})` : "—"}</span>
          </Stat>
          <Stat label="24h high">{t ? fmt(t.high24h, pd) : "—"}</Stat>
          <Stat label="24h low">{t ? fmt(t.low24h, pd) : "—"}</Stat>
          <Stat label={`24h volume (${market.base})`}>{t ? fmtCompact(t.volume24h) : "—"}</Stat>
          <Stat label={`24h volume (${market.quote})`}>{t ? fmtCompact(t.quoteVol24h) : "—"}</Stat>
        </dl>
      </div>
    </div>
  );
}

export function Stat({ label, children }: { label: ReactNode; children: ReactNode }) {
  return (
    <div className="stat">
      <dt>{label}</dt>
      <dd>{children}</dd>
    </div>
  );
}
