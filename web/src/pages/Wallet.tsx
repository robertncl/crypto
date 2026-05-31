import { useEffect, useMemo, useState, type FormEvent } from "react";
import { Link } from "react-router-dom";
import { api, ApiError } from "../api/client";
import type { Asset, Ticker, WalletAddress, WalletTxn } from "../api/types";
import { useAuth } from "../state/auth";
import { useBalances } from "../hooks/useBalances";
import { useChannel } from "../hooks/useStream";
import { fmt, fmtUsd, shortId, timeAgo, toNum, trimDecimal } from "../utils/format";

export function Wallet() {
  const { user, refresh } = useAuth();
  const { get, reload } = useBalances();
  const [assets, setAssets] = useState<Asset[]>([]);
  const [prices, setPrices] = useState<Record<string, number>>({});
  const [txns, setTxns] = useState<WalletTxn[]>([]);

  useEffect(() => {
    api.assets().then(setAssets).catch(() => {});
    api.tickers().then((all: Ticker[]) =>
      setPrices(Object.fromEntries(all.map((t) => [t.market, toNum(t.last)]))),
    ).catch(() => {});
    if (user) api.walletTxns().then(setTxns).catch(() => {});
  }, [user]);

  useChannel<WalletTxn>(user ? "walletTxns" : null, (t) => {
    setTxns((prev) => [t, ...prev.filter((x) => x.id !== t.id)].slice(0, 100));
    reload();
  });

  const priceOf = (sym: string) => (sym === "USDT" ? 1 : prices[`${sym}-USDT`] ?? 0);

  const portfolio = useMemo(() => {
    return assets.reduce((sum, a) => {
      const b = get(a.symbol);
      return sum + (toNum(b.available) + toNum(b.locked)) * priceOf(a.symbol);
    }, 0);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [assets, get, prices]);

  if (!user) {
    return (
      <div className="page">
        <div className="card pad center">
          <h1>Your wallet</h1>
          <p className="muted">Log in to view balances, deposit and withdraw.</p>
          <Link to="/login" className="btn btn--primary">Log in</Link>
        </div>
      </div>
    );
  }

  return (
    <div className="page page--wallet">
      <h1 className="visually-hidden">Wallet</h1>
      <div className="wallet-header card">
        <div>
          <div className="muted">Estimated Portfolio Value</div>
          <div className="wallet-header__value">{fmtUsd(portfolio)}</div>
        </div>
        <div className="wallet-header__kyc">
          {user.kycStatus === "verified" ? (
            <span className="badge badge--ok">Identity Verified</span>
          ) : (
            <button
              className="btn btn--primary"
              onClick={async () => { await api.verifyKyc(); await refresh(); }}
            >
              Verify identity
            </button>
          )}
        </div>
      </div>

      <div className="wallet-grid">
        <section className="card">
          <h2 className="card__title">Balances</h2>
          <table className="dtable">
            <thead>
              <tr><th scope="col">Asset</th><th scope="col" className="r">Available</th><th scope="col" className="r">In Order</th><th scope="col" className="r">Value</th></tr>
            </thead>
            <tbody>
              {assets.map((a) => {
                const b = get(a.symbol);
                const value = (toNum(b.available) + toNum(b.locked)) * priceOf(a.symbol);
                return (
                  <tr key={a.symbol}>
                    <td className="paircell">
                      <span className="paircell__icon" aria-hidden>{a.symbol[0]}</span>
                      <span><strong>{a.symbol}</strong> <span className="muted">{a.name}</span></span>
                    </td>
                    <td className="r mono">{fmt(b.available, 6)}</td>
                    <td className="r mono muted">{fmt(b.locked, 6)}</td>
                    <td className="r mono">{fmtUsd(value)}</td>
                  </tr>
                );
              })}
            </tbody>
          </table>
        </section>

        <DepositCard assets={assets} />
        <WithdrawCard assets={assets} kycOk={user.kycStatus === "verified"} onDone={reload} />

        <section className="card wallet-txns">
          <h2 className="card__title">Transaction History</h2>
          {txns.length === 0 ? (
            <div className="empty pad">No transactions yet</div>
          ) : (
            <table className="dtable">
              <thead>
                <tr><th scope="col">Time</th><th scope="col">Type</th><th scope="col">Asset</th><th scope="col" className="r">Amount</th><th scope="col">Status</th><th scope="col">TxID</th></tr>
              </thead>
              <tbody>
                {txns.map((t) => (
                  <tr key={t.id}>
                    <td className="muted">{timeAgo(t.createdAt)}</td>
                    <td className={t.type === "deposit" ? "up" : "down"}>{t.type}</td>
                    <td>{t.asset}</td>
                    <td className="r mono">{fmt(t.amount, 6)}</td>
                    <td>
                      <span className={`status status--${t.status}`}>
                        {t.status}{t.status !== "completed" && t.confirmations > 0 ? ` (${t.confirmations})` : ""}
                      </span>
                    </td>
                    <td className="mono muted">{t.txid ? shortId(t.txid) : "—"}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          )}
        </section>
      </div>
    </div>
  );
}

function DepositCard({ assets }: { assets: Asset[] }) {
  const [asset, setAsset] = useState("BTC");
  const [addr, setAddr] = useState<WalletAddress | null>(null);
  const [amount, setAmount] = useState("");
  const [msg, setMsg] = useState("");

  useEffect(() => {
    setAddr(null);
    api.walletAddress(asset).then(setAddr).catch(() => {});
  }, [asset]);

  async function simulate(e: FormEvent) {
    e.preventDefault();
    setMsg("");
    try {
      await api.deposit(asset, amount || "0");
      setMsg(`Incoming deposit of ${trimDecimal(amount)} ${asset} detected — confirming on-chain…`);
      setAmount("");
    } catch (err) {
      setMsg(err instanceof ApiError ? err.message : "Deposit failed");
    }
  }

  return (
    <section className="card">
      <h2 className="card__title">Deposit</h2>
      <label className="field field--stack">
        <span className="field__label">Asset</span>
        <select value={asset} onChange={(e) => setAsset(e.target.value)}>
          {assets.map((a) => <option key={a.symbol} value={a.symbol}>{a.symbol} — {a.name}</option>)}
        </select>
      </label>

      <div className="depositaddr">
        <span className="field__label">Your {addr?.network} deposit address</span>
        <div className="depositaddr__row">
          <code>{addr?.address ?? "…"}</code>
          <button className="btn btn--mini" onClick={() => addr && navigator.clipboard?.writeText(addr.address)}>Copy</button>
        </div>
      </div>

      <form className="form" onSubmit={simulate}>
        <p className="muted small">This is a simulated custody demo. Enter an amount to mimic an inbound on-chain transfer; it will credit after a few confirmations.</p>
        <label className="field">
          <span className="field__label">Amount</span>
          <span className="field__input">
            <input inputMode="decimal" placeholder="0.00" value={amount} onChange={(e) => setAmount(e.target.value)} />
            <span className="field__suffix">{asset}</span>
          </span>
        </label>
        <button className="btn btn--primary btn--block">Simulate deposit</button>
        {msg && <div className="formmsg formmsg--ok" role="status">{msg}</div>}
      </form>
    </section>
  );
}

function WithdrawCard({ assets, kycOk, onDone }: { assets: Asset[]; kycOk: boolean; onDone: () => void }) {
  const [asset, setAsset] = useState("USDT");
  const [address, setAddress] = useState("");
  const [amount, setAmount] = useState("");
  const [msg, setMsg] = useState<{ kind: "ok" | "err"; text: string } | null>(null);
  const meta = assets.find((a) => a.symbol === asset);

  async function submit(e: FormEvent) {
    e.preventDefault();
    setMsg(null);
    try {
      await api.withdraw(asset, address, amount || "0");
      setMsg({ kind: "ok", text: "Withdrawal submitted — broadcasting to the network." });
      setAmount("");
      setAddress("");
      onDone();
    } catch (err) {
      setMsg({ kind: "err", text: err instanceof ApiError ? err.message : "Withdrawal failed" });
    }
  }

  return (
    <section className="card">
      <h2 className="card__title">Withdraw</h2>
      {!kycOk && <div className="formmsg formmsg--warn">Identity verification is required before withdrawing. Verify above.</div>}
      <form className="form" onSubmit={submit}>
        <label className="field field--stack">
          <span className="field__label">Asset</span>
          <select value={asset} onChange={(e) => setAsset(e.target.value)}>
            {assets.map((a) => <option key={a.symbol} value={a.symbol}>{a.symbol} — {a.name}</option>)}
          </select>
        </label>
        <label className="field field--stack">
          <span className="field__label">Destination address</span>
          <input value={address} onChange={(e) => setAddress(e.target.value)} placeholder={`${meta?.network ?? ""} address`} />
        </label>
        <label className="field">
          <span className="field__label">Amount</span>
          <span className="field__input">
            <input inputMode="decimal" placeholder="0.00" value={amount} onChange={(e) => setAmount(e.target.value)} />
            <span className="field__suffix">{asset}</span>
          </span>
        </label>
        <div className="form__row muted">
          <span>Network fee</span>
          <span>{meta ? `${trimDecimal(meta.withdrawFee)} ${asset}` : "—"}</span>
        </div>
        <div className="form__row muted">
          <span>Minimum</span>
          <span>{meta ? `${trimDecimal(meta.minWithdraw)} ${asset}` : "—"}</span>
        </div>
        <button className="btn btn--block btn--sell" disabled={!kycOk}>Withdraw</button>
        {msg && <div className={`formmsg ${msg.kind === "ok" ? "formmsg--ok" : "formmsg--err"}`} role={msg.kind === "ok" ? "status" : "alert"}>{msg.text}</div>}
      </form>
    </section>
  );
}
