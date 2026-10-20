import { useEffect, useMemo, useState, type FormEvent } from "react";
import { Link } from "react-router-dom";
import { api, ApiError } from "../api/client";
import type { EarnPosition, EarnProduct } from "../api/types";
import { useAuth } from "../state/auth";
import { useBalances } from "../hooks/useBalances";
import { usePrices } from "../hooks/usePrices";
import { useChannel } from "../hooks/useStream";
import { fmt, fmtDate, fmtShortDate, fmtUsd, toNum, trimDecimal } from "../utils/format";
import { Alert, Badge, Button, Field, Input, Tabs, buttonClass, tabPanelProps } from "../components/acme";
import { AssetCell, AssetIcon, SrOnly } from "../components/common";

const DAY = 24 * 60 * 60;

function aprPct(apr: string): string {
  return `${(toNum(apr) * 100).toFixed(2)}%`;
}

function termLabel(kind: EarnProduct["kind"], termDays: number): string {
  return kind === "flexible" ? "Flexible" : `${termDays} days`;
}

/** Display decimals per asset: cents for USDT, more for coins. */
function dp(asset: string): number {
  return asset === "USDT" ? 2 : 6;
}

type Notice = { ok: boolean; title: string; text: string };

export function Earn() {
  const { user } = useAuth();
  const { get, reload } = useBalances();
  const { prices, priceOf } = usePrices();
  const [products, setProducts] = useState<EarnProduct[]>([]);
  const [positions, setPositions] = useState<EarnPosition[]>([]);
  const [filter, setFilter] = useState("all");
  const [selectedId, setSelectedId] = useState("");
  const [notice, setNotice] = useState<Notice | null>(null);

  useEffect(() => {
    api.earnProducts().then((list) => {
      setProducts(list);
      // Start on the highest-yield product.
      const best = [...list].sort((a, b) => toNum(b.apr) - toNum(a.apr))[0];
      if (best) setSelectedId((id) => id || best.id);
    }).catch(() => {});
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

  const byId = useMemo(() => Object.fromEntries(products.map((p) => [p.id, p])), [products]);
  const active = useMemo(() => positions.filter((p) => p.status === "active"), [positions]);
  const redeemed = useMemo(() => positions.filter((p) => p.status === "redeemed"), [positions]);
  const assets = useMemo(() => [...new Set(products.map((p) => p.asset))], [products]);
  const shown = products.filter((p) => filter === "all" || p.asset === filter);
  const selected = byId[selectedId];

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

  async function redeem(pos: EarnPosition) {
    setNotice(null);
    try {
      await api.earnRedeem(pos.id);
      setNotice({ ok: true, title: "Redeemed", text: `${fmt(pos.principal, dp(pos.asset))} ${pos.asset} plus ${fmt(pos.accruedTotal, dp(pos.asset) + 2)} ${pos.asset} of interest is back in your spendable balance.` });
      api.earnPositions().then(setPositions).catch(() => {});
      reload();
    } catch (err) {
      setNotice({ ok: false, title: "Not redeemed", text: err instanceof ApiError ? err.message : "The exchange didn’t respond. Try again." });
    }
  }

  return (
    <div className="page">
      <div className="page-head">
        <div className="page-head__text">
          <p className="kicker">Earn</p>
          <h1>Put idle crypto to work</h1>
          <p className="lede">
            Subscribe an asset and interest pays continuously to your spendable balance. Flexible products redeem
            anytime. Fixed terms lock until maturity for a higher rate.
          </p>
        </div>
        {user && (
          <dl className="stats">
            <div><dt>Subscribed</dt><dd>{fmtUsd(totals.principal)}</dd></div>
            <div><dt>Interest earned</dt><dd>+{fmtUsd(totals.accrued)}</dd></div>
          </dl>
        )}
      </div>

      {user && (
        <section className="section" aria-labelledby="subs-heading">
          <div className="section__head">
            <h2 id="subs-heading">Your subscriptions</h2>
            <span className="section__meta">{active.length} active</span>
          </div>
          {notice && <Alert variant={notice.ok ? "success" : "danger"} title={notice.title}>{notice.text}</Alert>}
          <div className="table-card">
            <div className="table-pad table-pad--mid">
              {active.length === 0 ? (
                <p className="panel__empty">Nothing subscribed yet. Pick a product below to start earning.</p>
              ) : (
                <table className="acme-table">
                  <caption className="visually-hidden">Your active subscriptions</caption>
                  <thead>
                    <tr>
                      <th scope="col">Product</th>
                      <th scope="col" className="acme-table__num">Principal</th>
                      <th scope="col" className="acme-table__num">APR</th>
                      <th scope="col" className="acme-table__num">Interest earned</th>
                      <th scope="col">Status</th>
                      <th scope="col"><SrOnly>Actions</SrOnly></th>
                    </tr>
                  </thead>
                  <tbody>
                    {active.map((p) => (
                      <SubscriptionRow key={p.id} pos={p} termDays={byId[p.productId]?.termDays ?? 0} onRedeem={redeem} />
                    ))}
                  </tbody>
                </table>
              )}
            </div>
          </div>
        </section>
      )}

      <section className="section" aria-labelledby="products-heading">
        <div className="section__head"><h2 id="products-heading">Products</h2></div>
        <div className="split">
          <div className="split__main panel">
            <div className="panel__head panel__head--tabs">
              <Tabs
                tabs={[{ id: "all", label: "All" }, ...assets.map((a) => ({ id: a, label: a }))]}
                active={filter}
                onChange={setFilter}
                idPrefix="asset"
                label="Filter products by asset"
              />
            </div>
            <div className="table-x" {...tabPanelProps("asset", filter)}>
              <div className="table-pad table-pad--narrow">
              <table className="acme-table">
                <caption className="visually-hidden">Earn products</caption>
                <thead>
                  <tr>
                    <th scope="col">Asset</th>
                    <th scope="col">Term</th>
                    <th scope="col" className="acme-table__num">APR</th>
                    <th scope="col" className="acme-table__num">Minimum</th>
                    <th scope="col"><SrOnly>Select</SrOnly></th>
                  </tr>
                </thead>
                <tbody>
                  {shown.map((p) => {
                    const on = p.id === selectedId;
                    return (
                      <tr key={p.id} aria-selected={on}>
                        <td><AssetCell symbol={p.asset} title={p.asset} /></td>
                        <td>{termLabel(p.kind, p.termDays)}</td>
                        <td className="acme-table__num strong">{aprPct(p.apr)}</td>
                        <td className="acme-table__num muted">{trimDecimal(p.minAmount)} {p.asset}</td>
                        <td className="acme-table__num">
                          <Button
                            variant={on ? "ghost" : "secondary"} size="sm" aria-pressed={on}
                            onClick={() => setSelectedId(p.id)}
                            aria-label={`${on ? "Selected" : "Choose"} ${p.asset} ${termLabel(p.kind, p.termDays)} at ${aprPct(p.apr)} APR`}
                          >
                            {on ? "Selected" : "Choose"}
                          </Button>
                        </td>
                      </tr>
                    );
                  })}
                </tbody>
              </table>
              </div>
            </div>
          </div>
          {selected && (
            <SubscribePanel
              key={selected.id}
              product={selected}
              available={get(selected.asset).available}
              price={priceOf(selected.asset)}
              signedIn={!!user}
              onSubscribe={subscribe}
              onDone={setNotice}
            />
          )}
        </div>
      </section>

      {user && redeemed.length > 0 && (
        <section className="section" aria-labelledby="redeemed-heading">
          <div className="section__head"><h2 id="redeemed-heading">Redeemed</h2></div>
          <div className="table-card">
            <div className="table-pad table-pad--narrow">
              <table className="acme-table">
                <caption className="visually-hidden">Redeemed subscriptions</caption>
                <thead>
                  <tr>
                    <th scope="col">Product</th>
                    <th scope="col" className="acme-table__num">Principal</th>
                    <th scope="col" className="acme-table__num">Interest earned</th>
                    <th scope="col">Redeemed</th>
                  </tr>
                </thead>
                <tbody>
                  {redeemed.map((p) => (
                    <tr key={p.id}>
                      <td className="strong">{p.asset} · {termLabel(p.kind, byId[p.productId]?.termDays ?? 0)}</td>
                      <td className="acme-table__num">{fmt(p.principal, dp(p.asset))} {p.asset}</td>
                      <td className="acme-table__num up">+{fmt(p.accruedTotal, dp(p.asset) + 2)} {p.asset}</td>
                      <td className="muted">{p.redeemedAt ? fmtDate(p.redeemedAt) : "—"}</td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          </div>
        </section>
      )}
    </div>
  );
}

function SubscriptionRow({ pos, termDays, onRedeem }: { pos: EarnPosition; termDays: number; onRedeem: (p: EarnPosition) => Promise<void> }) {
  const [busy, setBusy] = useState(false);
  const now = Date.now() / 1000;
  const matured = pos.kind === "flexible" || pos.maturityAt === 0 || now >= pos.maturityAt;
  const term = termLabel(pos.kind, termDays);

  return (
    <tr>
      <td><AssetCell symbol={pos.asset} title={`${pos.asset} · ${term}`} sub={`Since ${fmtDate(pos.startAt)}`} /></td>
      <td className="acme-table__num">{fmt(pos.principal, dp(pos.asset))} {pos.asset}</td>
      <td className="acme-table__num">{aprPct(pos.apr)}</td>
      <td className="acme-table__num up">+{fmt(pos.accruedTotal, dp(pos.asset) + 2)} {pos.asset}</td>
      <td>{matured ? <Badge variant="success">Earning</Badge> : <Badge>Matures {fmtShortDate(pos.maturityAt)}</Badge>}</td>
      <td className="acme-table__num">
        <Button
          size="sm"
          disabled={!matured || busy}
          onClick={async () => { setBusy(true); await onRedeem(pos); setBusy(false); }}
          aria-label={matured ? `Redeem ${fmt(pos.principal, dp(pos.asset))} ${pos.asset}` : `${pos.asset} ${term} is locked until ${fmtDate(pos.maturityAt)}`}
        >
          {busy ? "Redeeming…" : matured ? "Redeem" : "Locked"}
        </Button>
      </td>
    </tr>
  );
}

function SubscribePanel({ product, available, price, signedIn, onSubscribe, onDone }: {
  product: EarnProduct;
  available: string;
  price: number;
  signedIn: boolean;
  onSubscribe: (productId: string, amount: string) => Promise<void>;
  onDone: (n: Notice) => void;
}) {
  const [amount, setAmount] = useState("");
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);
  const a = toNum(amount);
  const d = dp(product.asset);
  const fixed = product.kind === "fixed";
  const days = fixed ? product.termDays : 1;
  const est = (a * toNum(product.apr) * days) / 365;
  const term = termLabel(product.kind, product.termDays);
  const maturity = Date.now() / 1000 + product.termDays * DAY;

  async function submit(e: FormEvent) {
    e.preventDefault();
    let msg = "";
    if (a <= 0) msg = `Enter an amount in ${product.asset}.`;
    else if (a < toNum(product.minAmount)) msg = `The minimum for this product is ${trimDecimal(product.minAmount)} ${product.asset}.`;
    else if (toNum(product.maxAmount) > 0 && a > toNum(product.maxAmount)) msg = `The maximum for this product is ${trimDecimal(product.maxAmount)} ${product.asset}.`;
    else if (a > toNum(available)) msg = `You have ${fmt(available, d)} ${product.asset} available. Lower the amount or deposit more.`;
    setError(msg);
    if (msg) return;
    setBusy(true);
    try {
      await onSubscribe(product.id, amount);
      onDone({
        ok: true,
        title: "Subscribed",
        text: `${trimDecimal(amount)} ${product.asset} is earning ${aprPct(product.apr)} APR${fixed ? ` until ${fmtDate(maturity)}.` : ". Redeem anytime."}`,
      });
      setAmount("");
    } catch (err) {
      setError(err instanceof ApiError ? err.message : "The exchange didn’t respond. Try again.");
    } finally {
      setBusy(false);
    }
  }

  return (
    <aside className="split__side panel subscribe" aria-labelledby="subscribe-title">
      <div className="subscribe__head">
        <div className="subscribe__title">
          <h3 id="subscribe-title"><span className="assetcell"><AssetIcon symbol={product.asset} />{product.asset} · {term}</span></h3>
          <Badge>{fixed ? "Fixed term" : "Flexible"}</Badge>
        </div>
        <p className="subscribe__apr"><strong>{aprPct(product.apr)}</strong> APR</p>
      </div>
      <form className="form" onSubmit={submit} noValidate>
        <Field label={`Amount (${product.asset})`} error={error} help={a > 0 && price > 0 ? `≈ ${fmtUsd(a * price)}` : undefined}>
          <Input
            inputMode="decimal" autoComplete="off" placeholder="0.00" value={amount} disabled={!signedIn}
            onChange={(e) => { setAmount(e.target.value); setError(""); }}
          />
        </Field>
        {signedIn && (
          <div className="form__row">
            <span>Available <strong>{fmt(available, d)} {product.asset}</strong></span>
            <Button variant="ghost" size="sm" onClick={() => { setAmount(trimDecimal(available)); setError(""); }}>Use max</Button>
          </div>
        )}
        <dl className="summary summary--ruled">
          <div><dt>Minimum</dt><dd>{trimDecimal(product.minAmount)} {product.asset}</dd></div>
          <div><dt>{fixed ? "Matures" : "Redeem"}</dt><dd>{fixed ? fmtDate(maturity) : "Anytime"}</dd></div>
          <div className="summary__total">
            <dt>{fixed ? "Interest at maturity" : "Interest per day"}</dt>
            <dd>{a > 0 ? `+${fmt(est, d + 2)} ${product.asset}` : "—"}</dd>
          </div>
        </dl>
        {signedIn ? (
          <Button variant="primary" size="lg" type="submit" className="btn-block" disabled={busy}>
            {busy ? "Subscribing…" : `Subscribe ${a > 0 ? `${trimDecimal(amount)} ` : ""}${product.asset}`}
          </Button>
        ) : (
          <Link className={buttonClass("primary", "lg", "btn-block")} to="/login">Sign in to subscribe</Link>
        )}
        <p className="form__note">
          {fixed ? `Locked for ${product.termDays} days. Fixed terms can’t be redeemed early.` : "Redeem anytime. Interest stops the moment you redeem."}
        </p>
      </form>
    </aside>
  );
}
