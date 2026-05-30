import { useEffect, useMemo, useState } from "react";
import { useParams } from "react-router-dom";
import { api } from "../api/client";
import type { FundingInfo, Market, PerpMarket, Ticker } from "../api/types";
import { useChannel } from "../hooks/useStream";
import { TickerBar } from "../components/TickerBar";
import { Chart } from "../components/Chart";
import { OrderBook } from "../components/OrderBook";
import { TradesFeed } from "../components/TradesFeed";
import { PerpOrderForm } from "../components/PerpOrderForm";
import { PerpPanel } from "../components/PerpPanel";
import { fmt, toNum } from "../utils/format";

// Adapt a PerpMarket to the Market shape the shared chart/book/ticker components
// expect (settle currency stands in for the quote).
function asMarket(pm: PerpMarket): Market {
  return {
    symbol: pm.symbol, base: pm.base, quote: pm.settle,
    priceTick: pm.priceTick, qtyStep: pm.qtyStep, minNotional: pm.minNotional,
    makerFee: pm.makerFee, takerFee: pm.takerFee, status: pm.status,
  };
}

export function Futures() {
  const { symbol = "BTC-PERP" } = useParams();
  const [pm, setPm] = useState<PerpMarket | null>(null);
  const [notFound, setNotFound] = useState(false);
  const [lastPrice, setLastPrice] = useState("0");
  const [pickedPrice, setPickedPrice] = useState("");

  useEffect(() => {
    setPm(null); setNotFound(false);
    api.perpMarket(symbol).then(setPm).catch(() => setNotFound(true));
  }, [symbol]);

  useChannel<Ticker>(pm ? `ticker:${pm.symbol}` : null, (t) => setLastPrice(t.last));

  const market = useMemo(() => (pm ? asMarket(pm) : null), [pm]);

  if (notFound) return <div className="page"><div className="empty pad">Perp market “{symbol}” not found.</div></div>;
  if (!pm || !market) return <div className="page"><div className="empty pad">Loading market…</div></div>;

  return (
    <div className="terminal">
      <div className="terminal__ticker perp-ticker">
        <TickerBar market={market} />
        <FundingBar symbol={pm.symbol} />
      </div>
      <div className="terminal__chart"><Chart market={market} /></div>
      <div className="terminal__book"><OrderBook market={market} fetchDepth={api.perpDepth} onPickPrice={setPickedPrice} /></div>
      <div className="terminal__trades"><TradesFeed market={market} /></div>
      <div className="terminal__form"><PerpOrderForm market={pm} pickedPrice={pickedPrice} lastPrice={lastPrice} /></div>
      <div className="terminal__orders"><PerpPanel market={pm} /></div>
    </div>
  );
}

function FundingBar({ symbol }: { symbol: string }) {
  const [fi, setFi] = useState<FundingInfo | null>(null);
  const [now, setNow] = useState(() => Date.now());

  useEffect(() => {
    api.funding(symbol).then(setFi).catch(() => {});
    const t = setInterval(() => setNow(Date.now()), 1000);
    return () => clearInterval(t);
  }, [symbol]);

  useChannel<FundingInfo>(`funding:${symbol}`, setFi);

  const ratePct = toNum(fi?.rate) * 100;
  const rateUp = ratePct >= 0;
  const secsLeft = fi?.nextFundingTime ? Math.max(0, fi.nextFundingTime - Math.floor(now / 1000)) : 0;
  const mm = String(Math.floor(secsLeft / 60)).padStart(2, "0");
  const ss = String(secsLeft % 60).padStart(2, "0");

  return (
    <div className="fundingbar">
      <span className="badge badge--warn">PERP</span>
      <div className="stat"><div className="stat__label">Mark</div><div className="stat__value mono">{fmt(fi?.markPrice ?? "0", 2)}</div></div>
      <div className="stat"><div className="stat__label">Index</div><div className="stat__value mono">{fmt(fi?.indexPrice ?? "0", 2)}</div></div>
      <div className="stat">
        <div className="stat__label">Funding / Countdown</div>
        <div className="stat__value mono">
          <span className={rateUp ? "up" : "down"}>{rateUp ? "+" : ""}{ratePct.toFixed(4)}%</span>
          {fi?.nextFundingTime ? <span className="muted"> · {mm}:{ss}</span> : null}
        </div>
      </div>
    </div>
  );
}
