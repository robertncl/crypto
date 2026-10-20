import { useEffect, useState, type FormEvent } from "react";
import { Link } from "react-router-dom";
import { api, ApiError } from "../api/client";
import type { OrderType, PerpMarket, PerpOrder, Side } from "../api/types";
import { useAuth } from "../state/auth";
import { useBalances } from "../hooks/useBalances";
import { fmt, roundToStep, stepDecimals, toNum, trimDecimal } from "../utils/format";
import { Alert, Button, Field, Input, Tabs, buttonClass, tabPanelProps } from "./acme";
import { Panel, SideSwitch } from "./common";

const PERCENTS = [25, 50, 75, 100];

function leverageChoices(max: number): number[] {
  return [1, 2, 3, 5, 10, 20, 25, 50, 75, 100].filter((l) => l <= max);
}

function placedText(o: PerpOrder, base: string, qd: number): string {
  if (o.status === "filled") return `${fmt(o.filled, qd)} ${base} filled at ${fmt(o.avgPrice)}.`;
  if (toNum(o.filled) > 0) return `${fmt(o.filled, qd)} ${base} filled so far; the rest is on the book.`;
  return "It’s on the book and opens the position when it fills.";
}

export function PerpOrderForm({ market, pickedPrice, lastPrice }: { market: PerpMarket; pickedPrice: string; lastPrice: string }) {
  const { user } = useAuth();
  const { get } = useBalances();
  const [side, setSide] = useState<Side>("buy");
  const [type, setType] = useState<OrderType>("limit");
  const [leverage, setLeverage] = useState(10);
  const [price, setPrice] = useState("");
  const [qty, setQty] = useState("");
  const [errors, setErrors] = useState<{ price?: string; qty?: string }>({});
  const [result, setResult] = useState<{ ok: boolean; title: string; text: string } | null>(null);
  const [busy, setBusy] = useState(false);

  useEffect(() => { if (pickedPrice) { setPrice(trimDecimal(pickedPrice)); setType("limit"); } }, [pickedPrice]);
  useEffect(() => {
    if (type === "limit" && !price && toNum(lastPrice) > 0) setPrice(trimDecimal(lastPrice));
  }, [lastPrice, type, price]);

  const pd = stepDecimals(market.priceTick);
  const qd = stepDecimals(market.qtyStep);
  const isLong = side === "buy";
  const avail = toNum(get(market.settle).available);
  const effPrice = type === "market" ? toNum(lastPrice) : toNum(price);
  const q = toNum(qty);
  const notional = effPrice * q;
  const margin = leverage > 0 ? notional / leverage : 0; // initial margin
  const feeRate = toNum(market.takerFee);
  const mmr = toNum(market.mmr);
  // Isolated-margin estimate before fees; the server computes the real one.
  const liq = isLong ? effPrice * (1 - 1 / leverage + mmr) : effPrice * (1 + 1 / leverage - mmr);

  function applyPercent(pct: number) {
    if (effPrice <= 0) return;
    setErrors({});
    const maxNotional = avail * leverage * (pct / 100) / (1 + feeRate * leverage);
    setQty(roundToStep(maxNotional / effPrice, market.qtyStep));
  }

  async function submit(e: FormEvent) {
    e.preventDefault();
    setResult(null);
    const next: typeof errors = {};
    if (type === "limit" && toNum(price) <= 0) next.price = `Enter a limit price in ${market.settle}.`;
    if (q <= 0) next.qty = `Enter a quantity in ${market.base}.`;
    else if (notional > 0 && notional < toNum(market.minNotional)) next.qty = `Order value is under the ${trimDecimal(market.minNotional)} ${market.settle} minimum. Increase the quantity.`;
    else if (margin > avail) next.qty = `Not enough margin: this needs ${fmt(margin, 2)} ${market.settle}. Lower the quantity or raise leverage.`;
    setErrors(next);
    if (next.price || next.qty) return;

    setBusy(true);
    try {
      const o = await api.placePerpOrder({
        market: market.symbol, side, type, leverage,
        quantity: roundToStep(q, market.qtyStep),
        ...(type === "limit" ? { price: roundToStep(toNum(price), market.priceTick) } : {}),
      });
      setResult({ ok: true, title: `${isLong ? "Long" : "Short"} order placed`, text: placedText(o, market.base, qd) });
      setQty("");
    } catch (err) {
      setResult({ ok: false, title: "Order not placed", text: err instanceof ApiError ? err.message : "The exchange didn’t respond. Check your connection and try again." });
    } finally {
      setBusy(false);
    }
  }

  const submitLabel = busy
    ? "Placing…"
    : `${isLong ? "Long" : "Short"} ${q > 0 ? `${fmt(q, qd)} ` : ""}${market.base} at ${leverage}×`;

  return (
    <Panel id="ticket" title="Open a position" className="panel--ticket">
      {!user ? (
        <div className="ticket__empty form">
          <p>Sign in to trade perpetuals. New accounts start with 10,000 USDT.</p>
          <Link className={buttonClass("primary", "lg", "btn-block")} to="/login">Sign in to trade</Link>
          <p className="form__note">New here? <Link to="/register">Create an account</Link></p>
        </div>
      ) : (
        <form className="form" onSubmit={submit} noValidate>
          <SideSwitch
            label="Direction"
            value={side}
            onChange={(s) => { setSide(s); setErrors({}); setResult(null); }}
            options={[{ id: "buy", label: "Long", up: true }, { id: "sell", label: "Short", up: false }]}
          />
          <fieldset className="fieldset">
            <legend><span>Leverage</span><span className="fieldset__value">{leverage}×</span></legend>
            <div className="chips">
              {leverageChoices(market.maxLeverage).map((l) => (
                <button key={l} type="button" className="chip" aria-pressed={l === leverage} onClick={() => { setLeverage(l); setErrors({}); }}>
                  {l}×
                </button>
              ))}
            </div>
          </fieldset>
          <Tabs
            tabs={[{ id: "limit", label: "Limit" }, { id: "market", label: "Market" }]}
            active={type}
            onChange={(t) => { setType(t); setErrors({}); }}
            idPrefix="otype"
            label="Order type"
          />
          <div className="form" {...tabPanelProps("otype", type)}>
            <div className="form__row">
              <span>Available margin</span>
              <strong>{fmt(avail, 2)} {market.settle}</strong>
            </div>
            <Field
              label={`Price (${market.settle})`}
              error={errors.price}
              help={type === "market" ? "Fills now at the best price in the book." : undefined}
            >
              <Input
                inputMode="decimal" autoComplete="off"
                placeholder={type === "market" ? "Best available price" : "0.0"}
                value={type === "market" ? "" : price}
                disabled={type === "market"}
                onChange={(e) => { setPrice(e.target.value); setErrors((x) => ({ ...x, price: undefined })); }}
              />
            </Field>
            <Field label={`Quantity (${market.base})`} error={errors.qty}>
              <Input
                inputMode="decimal" autoComplete="off" placeholder="0.000" value={qty}
                onChange={(e) => { setQty(e.target.value); setErrors((x) => ({ ...x, qty: undefined })); }}
              />
            </Field>
            <div className="pcts">
              {PERCENTS.map((p) => (
                <Button key={p} size="sm" onClick={() => applyPercent(p)} aria-label={`Use ${p}% of available margin`}>
                  {p === 100 ? "Max" : `${p}%`}
                </Button>
              ))}
            </div>
            <dl className="summary">
              <div><dt>Order value</dt><dd>{q > 0 && effPrice > 0 ? `${fmt(notional, 2)} ${market.settle}` : "—"}</dd></div>
              <div><dt>Margin required</dt><dd>{q > 0 && effPrice > 0 ? `${fmt(margin, 2)} ${market.settle}` : "—"}</dd></div>
              <div><dt>Est. liquidation price</dt><dd className="down">{q > 0 && effPrice > 0 ? fmt(liq, pd) : "—"}</dd></div>
              <div><dt>Est. fee ({fmt(feeRate * 100, 2)}%)</dt><dd>{q > 0 && effPrice > 0 ? `${fmt(notional * feeRate, 4)} ${market.settle}` : "—"}</dd></div>
            </dl>
            <Button variant="primary" size="lg" type="submit" className="btn-block" disabled={busy}>{submitLabel}</Button>
            {result && <Alert variant={result.ok ? "success" : "danger"} title={result.title}>{result.text}</Alert>}
            <p className="form__note">Isolated margin · Max {market.maxLeverage}× · Maintenance margin {fmt(mmr * 100, 2)}%</p>
          </div>
        </form>
      )}
    </Panel>
  );
}
