import { useEffect, useState } from "react";
import { Link } from "react-router-dom";
import { api, ApiError } from "../api/client";
import type { Market, OrderType, Side } from "../api/types";
import { useAuth } from "../state/auth";
import { useBalances } from "../hooks/useBalances";
import { fmt, roundToStep, stepDecimals, toNum, trimDecimal } from "../utils/format";

const PERCENTS = [25, 50, 75, 100];

export function OrderForm({ market, pickedPrice, lastPrice }: { market: Market; pickedPrice: string; lastPrice: string }) {
  const { user } = useAuth();
  const { get } = useBalances();
  const [side, setSide] = useState<Side>("buy");
  const [type, setType] = useState<OrderType>("limit");
  const [price, setPrice] = useState("");
  const [amount, setAmount] = useState(""); // base for limit/sell, quote budget for market buy
  const [msg, setMsg] = useState<{ kind: "ok" | "err"; text: string } | null>(null);
  const [busy, setBusy] = useState(false);

  // Seed and react to clicks in the order book / last price.
  useEffect(() => { if (pickedPrice) setPrice(trimDecimal(pickedPrice)); }, [pickedPrice]);
  useEffect(() => {
    if (type === "limit" && !price && lastPrice && toNum(lastPrice) > 0) setPrice(trimDecimal(lastPrice));
  }, [lastPrice, type, price]);

  const priceDp = stepDecimals(market.priceTick);
  const qtyDp = stepDecimals(market.qtyStep);

  const availQuote = toNum(get(market.quote).available);
  const availBase = toNum(get(market.base).available);
  const effPrice = type === "market" ? toNum(lastPrice) : toNum(price);
  const isMarketBuy = type === "market" && side === "buy";

  // total (quote) for display
  const total = isMarketBuy ? toNum(amount) : effPrice * toNum(amount);
  const feeRate = toNum(market.takerFee);

  function applyPercent(pct: number) {
    const f = pct / 100;
    if (side === "buy") {
      if (type === "market") {
        setAmount(roundToStep(availQuote * f, market.qtyStep));
      } else if (effPrice > 0) {
        setAmount(roundToStep((availQuote * f) / effPrice, market.qtyStep));
      }
    } else {
      setAmount(roundToStep(availBase * f, market.qtyStep));
    }
  }

  async function submit(e: React.FormEvent) {
    e.preventDefault();
    setMsg(null);
    if (!user) return;
    const qty = toNum(amount);
    if (qty <= 0) { setMsg({ kind: "err", text: "Enter an amount" }); return; }
    if (type === "limit" && toNum(price) <= 0) { setMsg({ kind: "err", text: "Enter a price" }); return; }

    setBusy(true);
    try {
      const body =
        isMarketBuy
          ? { market: market.symbol, side, type, quantity: roundToStep(qty, market.qtyStep) }
          : type === "limit"
            ? { market: market.symbol, side, type, price: roundToStep(toNum(price), market.priceTick), quantity: roundToStep(qty, market.qtyStep) }
            : { market: market.symbol, side, type, quantity: roundToStep(qty, market.qtyStep) };
      const o = await api.placeOrder(body);
      setMsg({ kind: "ok", text: `${o.side === "buy" ? "Buy" : "Sell"} order ${o.status} (${fmt(o.filled, qtyDp)} ${market.base} filled)` });
      setAmount("");
    } catch (err) {
      setMsg({ kind: "err", text: err instanceof ApiError ? err.message : "Order failed" });
    } finally {
      setBusy(false);
    }
  }

  const amountLabel = isMarketBuy ? `Total (${market.quote})` : `Amount (${market.base})`;

  return (
    <section className="panel panel--form">
      <div className="sidetabs">
        <button className={`sidetab sidetab--buy ${side === "buy" ? "is-active" : ""}`} onClick={() => setSide("buy")}>Buy</button>
        <button className={`sidetab sidetab--sell ${side === "sell" ? "is-active" : ""}`} onClick={() => setSide("sell")}>Sell</button>
      </div>

      <div className="seg seg--type">
        {(["limit", "market"] as OrderType[]).map((t) => (
          <button key={t} className={`seg__btn ${t === type ? "is-active" : ""}`} onClick={() => setType(t)}>
            {t === "limit" ? "Limit" : "Market"}
          </button>
        ))}
      </div>

      <form className="form" onSubmit={submit}>
        <div className="form__avail muted">
          Available <strong>{fmt(side === "buy" ? availQuote : availBase, side === "buy" ? 2 : qtyDp)}</strong> {side === "buy" ? market.quote : market.base}
        </div>

        {type === "limit" && (
          <label className="field">
            <span className="field__label">Price</span>
            <span className="field__input">
              <input
                inputMode="decimal" placeholder="0.00" value={price}
                onChange={(e) => setPrice(e.target.value)}
              />
              <span className="field__suffix">{market.quote}</span>
            </span>
          </label>
        )}

        <label className="field">
          <span className="field__label">{amountLabel}</span>
          <span className="field__input">
            <input
              inputMode="decimal" placeholder="0.00" value={amount}
              onChange={(e) => setAmount(e.target.value)}
            />
            <span className="field__suffix">{isMarketBuy ? market.quote : market.base}</span>
          </span>
        </label>

        <div className="pcts">
          {PERCENTS.map((p) => (
            <button type="button" key={p} className="pct" onClick={() => applyPercent(p)}>{p}%</button>
          ))}
        </div>

        {!isMarketBuy && (
          <div className="form__row muted">
            <span>Order Value</span>
            <span>{fmt(total, 2)} {market.quote}</span>
          </div>
        )}
        <div className="form__row muted">
          <span>Est. Fee ({fmt(feeRate * 100, 3)}%)</span>
          <span>{fmt(total * feeRate, 4)} {market.quote}</span>
        </div>

        {user ? (
          <button className={`btn btn--block ${side === "buy" ? "btn--buy" : "btn--sell"}`} disabled={busy}>
            {busy ? "Placing…" : `${side === "buy" ? "Buy" : "Sell"} ${market.base}`}
          </button>
        ) : (
          <Link to="/login" className="btn btn--block btn--primary">Log in to trade</Link>
        )}

        {msg && <div className={`formmsg ${msg.kind === "ok" ? "formmsg--ok" : "formmsg--err"}`}>{msg.text}</div>}
        <div className="form__hint muted">Min order: {fmt(market.minNotional, 2)} {market.quote} · Tick {trimDecimal(market.priceTick)} · Step {trimDecimal(market.qtyStep)}</div>
      </form>
    </section>
  );
}
