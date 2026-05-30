import { useEffect, useState, type FormEvent } from "react";
import { Link } from "react-router-dom";
import { api, ApiError } from "../api/client";
import type { OrderType, PerpMarket, Side } from "../api/types";
import { useAuth } from "../state/auth";
import { useBalances } from "../hooks/useBalances";
import { fmt, roundToStep, stepDecimals, toNum, trimDecimal } from "../utils/format";

const PERCENTS = [25, 50, 75, 100];

function leverageChoices(max: number): number[] {
  return [1, 2, 3, 5, 10, 20, 25, 50, 75, 100].filter((l) => l <= max);
}

export function PerpOrderForm({ market, pickedPrice, lastPrice }: { market: PerpMarket; pickedPrice: string; lastPrice: string }) {
  const { user } = useAuth();
  const { get } = useBalances();
  const [side, setSide] = useState<Side>("buy");
  const [type, setType] = useState<OrderType>("limit");
  const [leverage, setLeverage] = useState(10);
  const [price, setPrice] = useState("");
  const [qty, setQty] = useState("");
  const [msg, setMsg] = useState<{ kind: "ok" | "err"; text: string } | null>(null);
  const [busy, setBusy] = useState(false);

  useEffect(() => { if (pickedPrice) setPrice(trimDecimal(pickedPrice)); }, [pickedPrice]);
  useEffect(() => {
    if (type === "limit" && !price && toNum(lastPrice) > 0) setPrice(trimDecimal(lastPrice));
  }, [lastPrice, type, price]);

  const qtyDp = stepDecimals(market.qtyStep);
  const avail = toNum(get(market.settle).available);
  const effPrice = type === "market" ? toNum(lastPrice) : toNum(price);
  const notional = effPrice * toNum(qty);
  const cost = leverage > 0 ? notional / leverage : 0; // initial margin
  const feeRate = toNum(market.takerFee);

  function applyPercent(pct: number) {
    if (effPrice <= 0) return;
    const maxNotional = avail * leverage * (pct / 100);
    setQty(roundToStep(maxNotional / effPrice, market.qtyStep));
  }

  async function submit(e: FormEvent) {
    e.preventDefault();
    setMsg(null);
    if (!user) return;
    if (toNum(qty) <= 0) { setMsg({ kind: "err", text: "Enter a quantity" }); return; }
    if (type === "limit" && toNum(price) <= 0) { setMsg({ kind: "err", text: "Enter a price" }); return; }
    setBusy(true);
    try {
      const body = {
        market: market.symbol, side, type, leverage,
        quantity: roundToStep(toNum(qty), market.qtyStep),
        ...(type === "limit" ? { price: roundToStep(toNum(price), market.priceTick) } : {}),
      };
      const o = await api.placePerpOrder(body);
      setMsg({ kind: "ok", text: `${side === "buy" ? "Long" : "Short"} ${o.status} · ${fmt(o.filled, qtyDp)} ${market.base} filled` });
      setQty("");
    } catch (err) {
      setMsg({ kind: "err", text: err instanceof ApiError ? err.message : "Order failed" });
    } finally {
      setBusy(false);
    }
  }

  return (
    <section className="panel panel--form">
      <div className="sidetabs">
        <button className={`sidetab sidetab--buy ${side === "buy" ? "is-active" : ""}`} onClick={() => setSide("buy")}>Long</button>
        <button className={`sidetab sidetab--sell ${side === "sell" ? "is-active" : ""}`} onClick={() => setSide("sell")}>Short</button>
      </div>

      <div className="lev">
        <div className="lev__head">
          <span className="field__label">Leverage</span>
          <span className="lev__val">{leverage}×</span>
        </div>
        <div className="lev__opts">
          {leverageChoices(market.maxLeverage).map((l) => (
            <button key={l} className={`lev__btn ${l === leverage ? "is-active" : ""}`} onClick={() => setLeverage(l)}>{l}×</button>
          ))}
        </div>
      </div>

      <div className="seg seg--type">
        {(["limit", "market"] as OrderType[]).map((t) => (
          <button key={t} className={`seg__btn ${t === type ? "is-active" : ""}`} onClick={() => setType(t)}>
            {t === "limit" ? "Limit" : "Market"}
          </button>
        ))}
      </div>

      <form className="form" onSubmit={submit}>
        <div className="form__avail muted">Available <strong>{fmt(avail, 2)}</strong> {market.settle}</div>

        {type === "limit" && (
          <label className="field">
            <span className="field__label">Price</span>
            <span className="field__input">
              <input inputMode="decimal" placeholder="0.00" value={price} onChange={(e) => setPrice(e.target.value)} />
              <span className="field__suffix">{market.settle}</span>
            </span>
          </label>
        )}

        <label className="field">
          <span className="field__label">Quantity</span>
          <span className="field__input">
            <input inputMode="decimal" placeholder="0.00" value={qty} onChange={(e) => setQty(e.target.value)} />
            <span className="field__suffix">{market.base}</span>
          </span>
        </label>

        <div className="pcts">
          {PERCENTS.map((p) => (
            <button type="button" key={p} className="pct" onClick={() => applyPercent(p)}>{p}%</button>
          ))}
        </div>

        <div className="form__row muted"><span>Order Value</span><span>{fmt(notional, 2)} {market.settle}</span></div>
        <div className="form__row muted"><span>Margin Required</span><span>{fmt(cost, 2)} {market.settle}</span></div>
        <div className="form__row muted"><span>Est. Fee</span><span>{fmt(notional * feeRate, 4)} {market.settle}</span></div>

        {user ? (
          <button className={`btn btn--block ${side === "buy" ? "btn--buy" : "btn--sell"}`} disabled={busy}>
            {busy ? "Placing…" : `${side === "buy" ? "Long" : "Short"} ${market.base} ${leverage}×`}
          </button>
        ) : (
          <Link to="/login" className="btn btn--block btn--primary">Log in to trade</Link>
        )}

        {msg && <div className={`formmsg ${msg.kind === "ok" ? "formmsg--ok" : "formmsg--err"}`} role={msg.kind === "ok" ? "status" : "alert"}>{msg.text}</div>}
        <div className="form__hint muted">Isolated margin · Max {market.maxLeverage}× · MMR {fmt(toNum(market.mmr) * 100, 2)}%</div>
      </form>
    </section>
  );
}
