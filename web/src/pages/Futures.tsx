import { useEffect, useMemo, useState } from "react";
import { Link, useParams } from "react-router-dom";
import { api } from "../api/client";
import type { FundingInfo, Market, PerpMarket } from "../api/types";
import { useChannel } from "../hooks/useStream";
import { useTicker } from "../hooks/useTicker";
import { TickerBar, Stat } from "../components/TickerBar";
import { Chart } from "../components/Chart";
import { OrderBook } from "../components/OrderBook";
import { TradesFeed } from "../components/TradesFeed";
import { PerpOrderForm } from "../components/PerpOrderForm";
import { PerpPanel } from "../components/PerpPanel";
import { Badge } from "../components/acme";
import { fmt, fmtPct, stepDecimals, toNum } from "../utils/format";

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
  const [pickedPrice, setPickedPrice] = useState("");

  useEffect(() => {
    setPm(null); setNotFound(false);
    api.perpMarket(symbol).then(setPm).catch(() => setNotFound(true));
  }, [symbol]);

  const ticker = useTicker(pm?.symbol ?? null);

  const market = useMemo(() => (pm ? asMarket(pm) : null), [pm]);

  if (notFound) return <p className="loading">Perpetual market “{symbol}” not found. <Link to="/markets">See all markets</Link></p>;
  if (!pm || !market) return <p className="loading">Loading market…</p>;

  const last = ticker?.last ?? "0";

  return (
    <>
      <TickerBar
        market={market}
        ticker={ticker}
        title={pm.symbol}
        subtitle={`Perpetual · ${pm.settle}-margined · isolated`}
        badge={<Badge variant="warning">Up to {pm.maxLeverage}×</Badge>}
      >
        <FundingStats symbol={pm.symbol} pd={stepDecimals(pm.priceTick)} />
      </TickerBar>
      <div className="terminal">
        <div className="terminal__col terminal__col--main">
          <Chart market={market} />
          <PerpPanel market={pm} />
        </div>
        <div className="terminal__col">
          <OrderBook market={market} last={last} lastUp={toNum(ticker?.change) >= 0} fetchDepth={api.perpDepth} onPickPrice={setPickedPrice} />
          <TradesFeed market={market} />
        </div>
        <div className="terminal__col">
          <PerpOrderForm market={pm} pickedPrice={pickedPrice} lastPrice={last} />
        </div>
      </div>
    </>
  );
}

function FundingStats({ symbol, pd }: { symbol: string; pd: number }) {
  const [fi, setFi] = useState<FundingInfo | null>(null);
  const [now, setNow] = useState(() => Date.now());

  useEffect(() => {
    api.funding(symbol).then(setFi).catch(() => {});
    const t = setInterval(() => setNow(Date.now()), 1000);
    return () => clearInterval(t);
  }, [symbol]);

  useChannel<FundingInfo>(`funding:${symbol}`, setFi);

  const rate = toNum(fi?.rate) * 100;
  const secsLeft = fi?.nextFundingTime ? Math.max(0, fi.nextFundingTime - Math.floor(now / 1000)) : 0;
  const mm = String(Math.floor(secsLeft / 60)).padStart(2, "0");
  const ss = String(secsLeft % 60).padStart(2, "0");

  return (
    <>
      <Stat label="Mark price">{fi ? fmt(fi.markPrice, pd) : "—"}</Stat>
      <Stat label="Index price">{fi ? fmt(fi.indexPrice, pd) : "—"}</Stat>
      <Stat label="Funding rate · next in">
        <span className={rate >= 0 ? "up" : "down"}>{fi ? fmtPct(rate, 4) : "—"}</span>
        {fi?.nextFundingTime ? <span className="muted"> · {mm}:{ss}</span> : null}
      </Stat>
    </>
  );
}
