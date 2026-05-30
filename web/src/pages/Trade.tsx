import { useEffect, useState } from "react";
import { useParams } from "react-router-dom";
import { api } from "../api/client";
import type { Market, Ticker } from "../api/types";
import { useChannel } from "../hooks/useStream";
import { TickerBar } from "../components/TickerBar";
import { Chart } from "../components/Chart";
import { OrderBook } from "../components/OrderBook";
import { TradesFeed } from "../components/TradesFeed";
import { OrderForm } from "../components/OrderForm";
import { UserOrders } from "../components/UserOrders";

export function Trade() {
  const { symbol = "BTC-USDT" } = useParams();
  const [market, setMarket] = useState<Market | null>(null);
  const [notFound, setNotFound] = useState(false);
  const [lastPrice, setLastPrice] = useState("0");
  const [pickedPrice, setPickedPrice] = useState("");

  useEffect(() => {
    setMarket(null);
    setNotFound(false);
    api.market(symbol).then(setMarket).catch(() => setNotFound(true));
  }, [symbol]);

  useChannel<Ticker>(market ? `ticker:${market.symbol}` : null, (t) => setLastPrice(t.last));

  if (notFound) return <div className="page"><div className="empty pad">Market “{symbol}” not found.</div></div>;
  if (!market) return <div className="page"><div className="empty pad">Loading market…</div></div>;

  return (
    <div className="terminal">
      <div className="terminal__ticker"><TickerBar market={market} /></div>
      <div className="terminal__chart"><Chart market={market} /></div>
      <div className="terminal__book"><OrderBook market={market} onPickPrice={setPickedPrice} /></div>
      <div className="terminal__trades"><TradesFeed market={market} /></div>
      <div className="terminal__form"><OrderForm market={market} pickedPrice={pickedPrice} lastPrice={lastPrice} /></div>
      <div className="terminal__orders"><UserOrders market={market} /></div>
    </div>
  );
}
