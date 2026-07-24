import { useEffect, useMemo, useState, type FormEvent } from "react";
import { Link } from "react-router-dom";
import { api, ApiError } from "../api/client";
import type { EarnPosition, EarnProduct } from "../api/types";
import { useAuth } from "../state/auth";
import { useBalances } from "../hooks/useBalances";
import { usePrices } from "../hooks/usePrices";
import { useChannel } from "../hooks/useStream";
import { fmt, fmtUsd, toNum, trimDecimal } from "../utils/format";

const DAY = 24 * 60 * 60;

function aprPct(apr: string): string {
  return `${(toNum(apr) * 100).toFixed(2)}%`;
}

function termLabel(p: EarnProduct): string {
  return p.kind === "flexible" ? "Flexible" : `${p.termDays} days`;
}

export function Earn() {
  const { user } = useAuth();
  const { get, reload } = useBalances();
  const { prices, priceOf } = usePrices();
  const [products, setProducts] = useState<EarnProduct[]>([]);
  const [positions, setPositions] = useState<EarnPosition[]>([]);

  useEffect(() => {
    api.earnProducts().then(setProducts).catch(() => {});
  }, []);

  useEffect(() => {
    if (user) api.earnPositions().then(setPositions).catch(() => {});
    else setPositions([]);
  }, [user]);

  // Live updates: the accrual scheduler republishes the full position list (and
  // balances) on every interest tick.
  useChannel<EarnPosition[]>(user ? "earnPositions" : null, (list) => {
    setPositions(list);
    reload();
  });

  const active = useMemo(() => positions.filter((p) => p.status === "active"), [positions]);

  const totals = useMemo(() => {
    let principal = 0;
    let accrued = 0;
    for (const p of positions) {
      accrued += toNum(p.accruedTotal) * priceOf(p.asset);
      if (p.status === "active") principal += toNum(p.principal) * priceOf(p.asset);
    }
    return { principal, accrued };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [positions, prices]);

  async function subscribe(productId: string, amount: string) {
    await api.earnSubscribe(productId, amount);
    // The WS push refreshes positions+balances; refetch as a fallback.
    api.earnPositions().then(setPositions).catch(() => {});
    reload();
  }

  async function redeem(id: string) {
    await api.earnRedeem(id);
    api.earnPositions().then(setPositions).catch(() => {});
    reload();
  }

  return (
    <div className="page page--earn">
      <div className="earn-hero card">
        <div>
          <div className="muted">Earn — put idle crypto to work</div>
          <h1 className="earn-hero__title">Simple, flexible & fixed-term yield</h1>
          <p className="muted">
            Subscribe an asset to start earning interest, paid continuously to your spendable balance.
            Flexible products can be redeemed anytime; fixed-term products lock until maturity for a higher rate.
          </p>
        </div>
        {user && (
          <div className="earn-hero__stats">
            <div>
              <div className="muted small">Subscribed</div>
              <div className="earn-hero__value">{fmtUsd(totals.principal)}</div>
            </div>
            <div>
              <div className="muted small">Total interest earned</div>
              <div className="earn-hero__value up">{fmtUsd(totals.accrued)}</div>
            </div>
          </div>
        )}
      </div>

      {user && active.length > 0 && (
        <section className="card">
          <h2 className="card__title">Your active subscriptions</h2>
          <table className="dtable">
            <thead>
              <tr>
                <th scope="col">Product</th>
                <th scope="col" className="r">Principal</th>
                <th scope="col" className="r">APR</th>
                <th scope="col" className="r">Interest earned</th>
                <th scope="col">Status</th>
                <th scope="col" className="r">Action</th>
              </tr>
            </thead>
            <tbody>
              {active.map((p) => (
                <PositionRow key={p.id} pos={p} onRedeem={redeem} />
              ))}
            </tbody>
          </table>
        </section>
      )}

      <section className="card">
        <h2 className="card__title">Products</h2>
        {!user && (
          <div className="formmsg formmsg--warn">
            <Link to="/login">Log in</Link> to subscribe and start earning.
          </div>
        )}
        <div className="earn-grid">
          {products.map((p) => (
            <ProductCard
              key={p.id}
              product={p}
              available={get(p.asset).available}
              canSubscribe={!!user}
              onSubscribe={subscribe}
            />
          ))}
        </div>
      </section>

      {user && positions.some((p) => p.status === "redeemed") && (
        <section className="card">
          <h2 className="card__title">Redeemed</h2>
          <table className="dtable">
            <thead>
              <tr>
                <th scope="col">Product</th>
                <th scope="col" className="r">Principal</th>
                <th scope="col" className="r">Interest earned</th>
                <th scope="col">Status</th>
              </tr>
            </thead>
            <tbody>
              {positions.filter((p) => p.status === "redeemed").map((p) => (
                <tr key={p.id}>
                  <td><strong>{p.productId}</strong></td>
                  <td className="r mono">{fmt(p.principal, 6)} {p.asset}</td>
                  <td className="r mono up">{fmt(p.accruedTotal, 6)} {p.asset}</td>
                  <td><span className="status status--completed">redeemed</span></td>
                </tr>
              ))}
            </tbody>
          </table>
        </section>
      )}
    </div>
  );
}

function PositionRow({ pos, onRedeem }: { pos: EarnPosition; onRedeem: (id: string) => Promise<void> }) {
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState("");
  const now = Date.now() / 1000;
  const matured = pos.kind === "flexible" || pos.maturityAt === 0 || now >= pos.maturityAt;
  const daysLeft = matured ? 0 : Math.ceil((pos.maturityAt - now) / DAY);

  async function doRedeem() {
    setBusy(true);
    setErr("");
    try {
      await onRedeem(pos.id);
    } catch (e) {
      setErr(e instanceof ApiError ? e.message : "Redeem failed");
    } finally {
      setBusy(false);
    }
  }

  return (
    <tr>
      <td>
        <strong>{pos.productId}</strong>{" "}
        <span className="badge badge--soft">{pos.kind === "flexible" ? "Flexible" : "Fixed"}</span>
      </td>
      <td className="r mono">{fmt(pos.principal, 6)} {pos.asset}</td>
      <td className="r mono">{aprPct(pos.apr)}</td>
      <td className="r mono up">{fmt(pos.accruedTotal, 8)} {pos.asset}</td>
      <td>
        {matured ? (
          <span className="status status--open">earning</span>
        ) : (
          <span className="muted small">matures in {daysLeft}d</span>
        )}
      </td>
      <td className="r">
        <button className="btn btn--mini" disabled={!matured || busy} onClick={doRedeem}>
          {busy ? "…" : "Redeem"}
        </button>
        {err && <div className="formmsg formmsg--err" role="alert">{err}</div>}
      </td>
    </tr>
  );
}

function ProductCard({
  product, available, canSubscribe, onSubscribe,
}: {
  product: EarnProduct;
  available: string;
  canSubscribe: boolean;
  onSubscribe: (productId: string, amount: string) => Promise<void>;
}) {
  const [amount, setAmount] = useState("");
  const [msg, setMsg] = useState<{ kind: "ok" | "err"; text: string } | null>(null);
  const [busy, setBusy] = useState(false);

  async function submit(e: FormEvent) {
    e.preventDefault();
    setMsg(null);
    setBusy(true);
    try {
      await onSubscribe(product.id, amount || "0");
      setMsg({ kind: "ok", text: `Subscribed ${trimDecimal(amount)} ${product.asset} — now earning.` });
      setAmount("");
    } catch (err) {
      setMsg({ kind: "err", text: err instanceof ApiError ? err.message : "Subscription failed" });
    } finally {
      setBusy(false);
    }
  }

  return (
    <div className="earn-card">
      <div className="earn-card__head">
        <span className="paircell__icon" aria-hidden>{product.asset[0]}</span>
        <div>
          <div className="earn-card__asset">{product.asset}</div>
          <span className={`badge ${product.kind === "flexible" ? "badge--soft" : "badge--ok"}`}>
            {termLabel(product)}
          </span>
        </div>
        <div className="earn-card__apr">
          <div className="earn-card__aprval up">{aprPct(product.apr)}</div>
          <div className="muted small">APR</div>
        </div>
      </div>

      <form className="form" onSubmit={submit}>
        <label className="field">
          <span className="field__label">Amount</span>
          <span className="field__input">
            <input
              inputMode="decimal"
              placeholder="0.00"
              value={amount}
              onChange={(e) => setAmount(e.target.value)}
              disabled={!canSubscribe}
            />
            <span className="field__suffix">{product.asset}</span>
          </span>
        </label>
        <div className="form__row muted small">
          <span>Available</span>
          <button
            type="button"
            className="linklike"
            onClick={() => setAmount(trimDecimal(available))}
            disabled={!canSubscribe}
          >
            {fmt(available, 6)} {product.asset}
          </button>
        </div>
        <div className="form__row muted small">
          <span>Minimum</span>
          <span>{trimDecimal(product.minAmount)} {product.asset}</span>
        </div>
        <button className="btn btn--primary btn--block" disabled={!canSubscribe || busy}>
          {busy ? "Subscribing…" : "Subscribe"}
        </button>
        {msg && (
          <div className={`formmsg ${msg.kind === "ok" ? "formmsg--ok" : "formmsg--err"}`} role={msg.kind === "ok" ? "status" : "alert"}>
            {msg.text}
          </div>
        )}
      </form>
    </div>
  );
}
