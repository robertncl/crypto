import { useEffect, useState, type FormEvent } from "react";
import { Link } from "react-router-dom";
import { api, ApiError } from "../api/client";
import type { Market, Order, OrderType, Side } from "../api/types";
import { useAuth } from "../state/auth";
import { useBalances } from "../hooks/useBalances";
import { fmt, roundToStep, stepDecimals, toNum, trimDecimal } from "../utils/format";
import { Alert, Button, Field, Input, Tabs, buttonClass, tabPanelProps } from "./acme";
import { Panel, SideSwitch } from "./common";

const PERCENTS = [25, 50, 75, 100];

function placedText(o: Order, base: string, qd: number): string {
  const filled = toNum(o.filled);
  if (o.status === "filled") return `${fmt(o.filled, qd)} ${base} filled.`;
  if (filled > 0) return `${fmt(o.filled, qd)} ${base} filled so far; the rest is on the book.`;
  return "It’s on the book and fills when the price is met.";
}

export function OrderForm({ market, pickedPrice, lastPrice }: { market: Market; pickedPrice: string; lastPrice: string }) {
  const { user } = useAuth();
  const { get } = useBalances();
  const [side, setSide] = useState<Side>("buy");
  const [type, setType] = useState<OrderType>("limit");
  const [price, setPrice] = useState("");
  const [amount, setAmount] = useState(""); // base for limit/sell, quote budget for market buy
  const [errors, setErrors] = useState<{ price?: string; amount?: string }>({});
  const [result, setResult] = useState<{ ok: boolean; title: string; text: string } | null>(null);
  const [busy, setBusy] = useState(false);

  // Seed and react to clicks in the order book / last price.
  useEffect(() => { if (pickedPrice) { setPrice(trimDecimal(pickedPrice)); setType("limit"); } }, [pickedPrice]);
  useEffect(() => {
    if (type === "limit" && !price && lastPrice && toNum(lastPrice) > 0) setPrice(trimDecimal(lastPrice));
  }, [lastPrice, type, price]);

  const qd = stepDecimals(market.qtyStep);
  const availQuote = toNum(get(market.quote).available);
  const availBase = toNum(get(market.base).available);
  const isBuy = side === "buy";
  const effPrice = type === "market" ? toNum(lastPrice) : toNum(price);
  const isMarketBuy = type === "market" && isBuy;
  const amt = toNum(amount);
  const total = isMarketBuy ? amt : effPrice * amt;
  const feeRate = toNum(market.takerFee);
  const minNotional = toNum(market.minNotional);

  function applyPercent(pct: number) {
    const f = pct / 100;
    setErrors({});
    if (isBuy) {
      if (type === "market") setAmount(roundToStep(availQuote * f, market.qtyStep));
      else if (effPrice > 0) setAmount(roundToStep((availQuote * f) / (effPrice * (1 + feeRate)), market.qtyStep));
    } else {
      setAmount(roundToStep(availBase * f, market.qtyStep));
    }
  }

  async function submit(e: FormEvent) {
    e.preventDefault();
    setResult(null);
    const next: typeof errors = {};
    if (type === "limit" && toNum(price) <= 0) next.price = `Enter a limit price in ${market.quote}.`;
    if (amt <= 0) next.amount = isMarketBuy ? `Enter how much ${market.quote} to spend.` : `Enter an amount in ${market.base}.`;
    else if (total > 0 && total < minNotional) next.amount = `Order value is under the ${trimDecimal(market.minNotional)} ${market.quote} minimum. Increase the amount.`;
    setErrors(next);
    if (next.price || next.amount) return;

    setBusy(true);
    try {
      const quantity = roundToStep(amt, market.qtyStep);
      const o = await api.placeOrder(
        type === "limit"
          ? { market: market.symbol, side, type, price: roundToStep(toNum(price), market.priceTick), quantity }
          : { market: market.symbol, side, type, quantity },
      );
      setResult({ ok: true, title: `${isBuy ? "Buy" : "Sell"} order placed`, text: placedText(o, market.base, qd) });
      setAmount("");
    } catch (err) {
      setResult({ ok: false, title: "Order not placed", text: err instanceof ApiError ? err.message : "The exchange didn’t respond. Check your connection and try again." });
    } finally {
      setBusy(false);
    }
  }

  const submitLabel = busy
    ? "Placing…"
    : isMarketBuy
      ? amt > 0 ? `Buy ${market.base} with ${fmt(amt, 2)} ${market.quote}` : `Buy ${market.base}`
      : `${isBuy ? "Buy" : "Sell"} ${amt > 0 ? `${fmt(amt, qd)} ` : ""}${market.base}`;

  return (
    <Panel id="ticket" title="Place order" className="panel--ticket">
      {!user ? (
        <div className="ticket__empty form">
          <p>Sign in to place orders. New accounts start with 10,000 USDT.</p>
          <Link className={buttonClass("primary", "lg", "btn-block")} to="/login">Sign in to trade</Link>
          <p className="form__note">New here? <Link to="/register">Create an account</Link></p>
        </div>
      ) : (
        <form className="form" onSubmit={submit} noValidate>
          <SideSwitch
            label="Side"
            value={side}
            onChange={(s) => { setSide(s); setErrors({}); setResult(null); }}
            options={[{ id: "buy", label: "Buy", up: true }, { id: "sell", label: "Sell", up: false }]}
          />
          <Tabs
            tabs={[{ id: "limit", label: "Limit" }, { id: "market", label: "Market" }]}
            active={type}
            onChange={(t) => { setType(t); setErrors({}); }}
            idPrefix="otype"
            label="Order type"
          />
          <div className="form" {...tabPanelProps("otype", type)}>
            <div className="form__row">
              <span>Available</span>
              <strong>{isBuy ? `${fmt(availQuote, 2)} ${market.quote}` : `${fmt(availBase, qd)} ${market.base}`}</strong>
            </div>
            <Field
              label={`Price (${market.quote})`}
              error={errors.price}
              help={type === "market" ? "Fills now at the best price in the book." : undefined}
            >
              <Input
                inputMode="decimal" autoComplete="off"
                placeholder={type === "market" ? "Best available price" : "0.00"}
                value={type === "market" ? "" : price}
                disabled={type === "market"}
                onChange={(e) => { setPrice(e.target.value); setErrors((x) => ({ ...x, price: undefined })); }}
              />
            </Field>
            <Field label={isMarketBuy ? `Total (${market.quote})` : `Amount (${market.base})`} error={errors.amount}>
              <Input
                inputMode="decimal" autoComplete="off" placeholder="0.00" value={amount}
                onChange={(e) => { setAmount(e.target.value); setErrors((x) => ({ ...x, amount: undefined })); }}
              />
            </Field>
            <div className="pcts">
              {PERCENTS.map((p) => (
                <Button key={p} size="sm" onClick={() => applyPercent(p)} aria-label={`Use ${p}% of available balance`}>
                  {p === 100 ? "Max" : `${p}%`}
                </Button>
              ))}
            </div>
            <dl className="summary">
              {!isMarketBuy && (
                <div><dt>Order value</dt><dd>{amt > 0 && effPrice > 0 ? `${type === "market" ? "≈ " : ""}${fmt(total, 2)} ${market.quote}` : "—"}</dd></div>
              )}
              <div><dt>Est. fee ({fmt(feeRate * 100, 2)}%)</dt><dd>{total > 0 ? `${fmt(total * feeRate, 4)} ${market.quote}` : "—"}</dd></div>
            </dl>
            <Button variant="primary" size="lg" type="submit" className="btn-block" disabled={busy}>{submitLabel}</Button>
            {result && <Alert variant={result.ok ? "success" : "danger"} title={result.title}>{result.text}</Alert>}
            <p className="form__note">
              Maker {fmt(toNum(market.makerFee) * 100, 2)}% · Taker {fmt(feeRate * 100, 2)}% · Minimum order {trimDecimal(market.minNotional)} {market.quote}
            </p>
          </div>
        </form>
      )}
    </Panel>
  );
}
