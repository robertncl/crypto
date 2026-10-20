import { useEffect, useState } from "react";
import { Link, useParams } from "react-router-dom";
import { api } from "../api/client";
import type { Market } from "../api/types";
import { useAuth } from "../state/auth";
import { useBalances } from "../hooks/useBalances";
import { useTicker } from "../hooks/useTicker";
import { fmt, stepDecimals, toNum } from "../utils/format";
import { TickerBar } from "../components/TickerBar";
import { Chart } from "../components/Chart";
import { OrderBook } from "../components/OrderBook";
import { TradesFeed } from "../components/TradesFeed";
import { OrderForm } from "../components/OrderForm";
import { UserOrders } from "../components/UserOrders";
import { Panel } from "../components/common";
import { buttonClass } from "../components/acme";

export function Trade() {
  const { symbol = "BTC-USDT" } = useParams();
  const [market, setMarket] = useState<Market | null>(null);
  const [notFound, setNotFound] = useState(false);
  const [pickedPrice, setPickedPrice] = useState("");

  useEffect(() => {
    setMarket(null);
    setNotFound(false);
    api.market(symbol).then(setMarket).catch(() => setNotFound(true));
  }, [symbol]);

  const ticker = useTicker(market?.symbol ?? null);

  if (notFound) return <p className="loading">Market “{symbol}” not found. <Link to="/markets">See all markets</Link></p>;
  if (!market) return <p className="loading">Loading market…</p>;

  const last = ticker?.last ?? "0";
  const lastUp = toNum(ticker?.change) >= 0;

  return (
    <>
      <TickerBar market={market} ticker={ticker} title={`${market.base}/${market.quote}`} subtitle={`Spot · quoted in ${market.quote}`} />
      <div className="terminal">
        <div className="terminal__col terminal__col--main">
          <Chart market={market} />
          <UserOrders market={market} />
        </div>
        <div className="terminal__col">
          <OrderBook market={market} last={last} lastUp={lastUp} onPickPrice={setPickedPrice} />
          <TradesFeed market={market} />
        </div>
        <div className="terminal__col">
          <OrderForm market={market} pickedPrice={pickedPrice} lastPrice={last} />
          <SpotBalances market={market} />
        </div>
      </div>
    </>
  );
}

function SpotBalances({ market }: { market: Market }) {
  const { user } = useAuth();
  const { get } = useBalances();
  if (!user) return null;
  const rows = [
    { asset: market.base, dp: stepDecimals(market.qtyStep) },
    { asset: market.quote, dp: 2 },
  ];
  return (
    <Panel id="balances" title="Balances" aside={<Link className={buttonClass("ghost", "sm")} to="/wallet">Deposit</Link>}>
      <dl className="summary summary--assets panel__body">
        {rows.map(({ asset, dp }) => {
          const b = get(asset);
          return (
            <div key={asset}>
              <dt>{asset}</dt>
              <dd>
                {fmt(b.available, dp)} available
                <span className="summary__sub">{fmt(b.locked, dp)} in orders</span>
              </dd>
            </div>
          );
        })}
      </dl>
    </Panel>
  );
}
