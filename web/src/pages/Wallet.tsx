import { useEffect, useMemo, useState, type FormEvent } from "react";
import { Link } from "react-router-dom";
import { api, ApiError } from "../api/client";
import type { Asset, WalletAddress, WalletTxn } from "../api/types";
import { useAuth } from "../state/auth";
import { useBalances } from "../hooks/useBalances";
import { usePrices } from "../hooks/usePrices";
import { useChannel } from "../hooks/useStream";
import { fmt, fmtDateTime, fmtSigned, fmtUsd, shortId, toNum, trimDecimal } from "../utils/format";
import { Alert, Badge, Button, Field, Input, Modal, Select, Tabs, buttonClass, tabPanelProps, type BadgeVariant } from "../components/acme";
import { AssetCell, SrOnly } from "../components/common";

type MoveTab = "deposit" | "withdraw";

function dp(asset: string): number {
  return asset === "USDT" ? 2 : 6;
}

function txnStatus(t: WalletTxn, needed: number): { label: string; variant: BadgeVariant } {
  switch (t.status) {
    case "completed": return { label: "Completed", variant: "success" };
    case "failed": return { label: "Failed", variant: "danger" };
    case "confirmed": return { label: `Confirming ${t.confirmations}/${needed || t.confirmations}`, variant: "info" };
    default: return { label: t.type === "withdrawal" ? "Broadcasting" : "Pending", variant: "info" };
  }
}

export function Wallet() {
  const { user, refresh } = useAuth();
  const { get, reload } = useBalances();
  const { prices, priceOf } = usePrices();
  const [assets, setAssets] = useState<Asset[]>([]);
  const [txns, setTxns] = useState<WalletTxn[]>([]);
  const [tab, setTab] = useState<MoveTab>("deposit");
  const [asset, setAsset] = useState("BTC");
  const [verifying, setVerifying] = useState(false);
  const [justVerified, setJustVerified] = useState(false);

  useEffect(() => {
    api.assets().then(setAssets).catch(() => {});
    if (user) api.walletTxns().then(setTxns).catch(() => {});
  }, [user]);

  useChannel<WalletTxn>(user ? "walletTxns" : null, (t) => {
    setTxns((prev) => [t, ...prev.filter((x) => x.id !== t.id)].slice(0, 100));
    reload();
  });

  const values = useMemo(() => {
    const rows = assets.map((a) => {
      const b = get(a.symbol);
      return { asset: a, value: (toNum(b.available) + toNum(b.locked)) * priceOf(a.symbol) };
    });
    return { rows, total: rows.reduce((sum, r) => sum + r.value, 0) };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [assets, get, prices]);

  if (!user) {
    return (
      <div className="page">
        <div className="panel empty-card">
          <h1>Your wallet</h1>
          <p>Sign in to see balances, deposit and withdraw.</p>
          <Link to="/login" className={buttonClass("primary")}>Sign in</Link>
        </div>
      </div>
    );
  }

  const kycOk = user.kycStatus === "verified";
  const btcPrice = priceOf("BTC");
  const confirmsFor = (sym: string) => assets.find((a) => a.symbol === sym)?.confirmations ?? 0;

  async function verify() {
    setVerifying(true);
    try {
      await api.verifyKyc();
      await refresh();
      setJustVerified(true);
    } catch {
      // the alert stays up and the button re-enables, so they can retry
    } finally {
      setVerifying(false);
    }
  }

  function openMove(next: MoveTab, sym: string) {
    setTab(next);
    setAsset(sym);
    document.getElementById("move-funds")?.scrollIntoView({ block: "nearest", behavior: "smooth" });
  }

  return (
    <div className="page page--tight">
      <div>
        <h1 className="kicker">Wallet</h1>
        <p className="page-label">Estimated portfolio value</p>
        <p className="bigfigure">{fmtUsd(values.total)}</p>
        {btcPrice > 0 && <p className="bigfigure__sub">≈ {fmt(values.total / btcPrice, 5)} BTC</p>}
      </div>

      {!kycOk && (
        <Alert variant="warning" title="Verify your identity to withdraw">
          <p>Deposits, trading and Earn work without it. In this demo, verification is instant.</p>
          <p><Button variant="primary" onClick={verify} disabled={verifying}>{verifying ? "Verifying…" : "Verify identity"}</Button></p>
        </Alert>
      )}
      {kycOk && justVerified && <Alert variant="success" title="Identity verified">Withdrawals are on.</Alert>}

      <div className="split">
        <section className="split__main section" aria-labelledby="bal-heading">
          <div className="section__head"><h2 id="bal-heading">Balances</h2></div>
          <div className="table-card">
            <div className="table-pad table-pad--mid">
              <table className="acme-table">
                <caption className="visually-hidden">Balances</caption>
                <thead>
                  <tr>
                    <th scope="col">Asset</th>
                    <th scope="col" className="acme-table__num">Available</th>
                    <th scope="col" className="acme-table__num">In orders</th>
                    <th scope="col" className="acme-table__num">Value</th>
                    <th scope="col">Share</th>
                    <th scope="col"><SrOnly>Actions</SrOnly></th>
                  </tr>
                </thead>
                <tbody>
                  {values.rows.map(({ asset: a, value }) => {
                    const b = get(a.symbol);
                    const share = values.total > 0 ? (value / values.total) * 100 : 0;
                    return (
                      <tr key={a.symbol}>
                        <td><AssetCell symbol={a.symbol} title={a.symbol} sub={a.name} /></td>
                        <td className="acme-table__num">{fmt(b.available, dp(a.symbol))}</td>
                        <td className="acme-table__num muted">{fmt(b.locked, dp(a.symbol))}</td>
                        <td className="acme-table__num strong">{fmtUsd(value)}</td>
                        <td>
                          <span className="sharebar">
                            <span className="sharebar__track" aria-hidden="true"><span className="sharebar__fill" style={{ inlineSize: `${share}%` }} /></span>
                            <span className="sharebar__label">{share.toFixed(1)}%</span>
                          </span>
                        </td>
                        <td className="acme-table__num nowrap">
                          <Button variant="ghost" size="sm" onClick={() => openMove("deposit", a.symbol)} aria-label={`Deposit ${a.symbol}`}>Deposit</Button>
                          <Button variant="ghost" size="sm" onClick={() => openMove("withdraw", a.symbol)} aria-label={`Withdraw ${a.symbol}`}>Withdraw</Button>
                        </td>
                      </tr>
                    );
                  })}
                </tbody>
              </table>
            </div>
          </div>
        </section>

        <section id="move-funds" className="split__side panel" aria-labelledby="move-heading">
          <div className="movefunds__head">
            <h2 id="move-heading">Move funds</h2>
            <Tabs
              tabs={[{ id: "deposit", label: "Deposit" }, { id: "withdraw", label: "Withdraw" }]}
              active={tab}
              onChange={setTab}
              idPrefix="move"
              label="Move funds"
            />
          </div>
          <div className="movefunds__body" {...tabPanelProps("move", tab)}>
            <Field label="Asset">
              <Select value={asset} onChange={(e) => setAsset(e.target.value)}>
                {assets.map((a) => <option key={a.symbol} value={a.symbol}>{a.symbol} · {a.name}</option>)}
              </Select>
            </Field>
            {tab === "deposit"
              ? <DepositForm key={asset} asset={asset} meta={assets.find((a) => a.symbol === asset)} />
              : <WithdrawForm key={asset} meta={assets.find((a) => a.symbol === asset)} available={get(asset).available} kycOk={kycOk} onDone={reload} />}
          </div>
        </section>
      </div>

      <section className="section" aria-labelledby="tx-heading">
        <div className="section__head"><h2 id="tx-heading">Transaction history</h2></div>
        <div className="table-card">
          <div className="table-pad table-pad--mid">
            {txns.length === 0 ? (
              <p className="panel__empty">No transactions yet. Deposits and withdrawals show here as they confirm.</p>
            ) : (
              <table className="acme-table">
                <caption className="visually-hidden">Transaction history</caption>
                <thead>
                  <tr>
                    <th scope="col">Time</th>
                    <th scope="col">Type</th>
                    <th scope="col">Asset</th>
                    <th scope="col" className="acme-table__num">Amount</th>
                    <th scope="col">Status</th>
                    <th scope="col">Transaction ID</th>
                  </tr>
                </thead>
                <tbody>
                  {txns.map((t) => {
                    const s = txnStatus(t, confirmsFor(t.asset));
                    const deposit = t.type === "deposit";
                    return (
                      <tr key={t.id}>
                        <td className="muted">{fmtDateTime(t.createdAt)}</td>
                        <td>{deposit ? "Deposit" : "Withdrawal"}</td>
                        <td className="strong">{t.asset}</td>
                        <td className={`acme-table__num strong ${deposit ? "up" : ""}`}>{fmtSigned(deposit ? t.amount : -toNum(t.amount), dp(t.asset))}</td>
                        <td><Badge variant={s.variant}>{s.label}</Badge></td>
                        <td className="mono muted">{t.txid ? shortId(t.txid) : "—"}</td>
                      </tr>
                    );
                  })}
                </tbody>
              </table>
            )}
          </div>
        </div>
      </section>
    </div>
  );
}

function DepositForm({ asset, meta }: { asset: string; meta?: Asset }) {
  const [addr, setAddr] = useState<WalletAddress | null>(null);
  const [copied, setCopied] = useState(false);
  const [amount, setAmount] = useState("");
  const [error, setError] = useState("");
  const [note, setNote] = useState("");

  useEffect(() => {
    api.walletAddress(asset).then(setAddr).catch(() => {});
  }, [asset]);

  async function copy() {
    if (!addr) return;
    try {
      await navigator.clipboard.writeText(addr.address);
      setCopied(true);
    } catch {
      setCopied(false);
    }
  }

  async function simulate(e: FormEvent) {
    e.preventDefault();
    setNote("");
    if (toNum(amount) <= 0) { setError(`Enter an amount of ${asset} to simulate.`); return; }
    try {
      await api.deposit(asset, amount);
      const needed = meta?.confirmations ?? 1;
      setNote(`${trimDecimal(amount)} ${asset} is confirming on-chain. It credits after ${needed} ${needed === 1 ? "confirmation" : "confirmations"}.`);
      setAmount("");
    } catch (err) {
      setError(err instanceof ApiError ? err.message : "The exchange didn’t respond. Try again.");
    }
  }

  const network = addr?.network ?? meta?.network ?? "";

  return (
    <>
      <div className="acme-field">
        <span className="acme-label">Your {network} deposit address</span>
        <div className="address">
          <code>{addr?.address ?? "Loading…"}</code>
          <Button size="sm" onClick={copy} disabled={!addr}>{copied ? "Copied" : "Copy"}</Button>
        </div>
        <p className="acme-help">Send only {asset} on the {network} network. Anything else sent here is lost.</p>
      </div>
      <form className="divider" onSubmit={simulate} noValidate>
        <p className="form__note">Simulated custody: enter an amount to mimic an incoming on-chain transfer.</p>
        <Field label={`Amount (${asset})`} error={error}>
          <Input inputMode="decimal" autoComplete="off" placeholder="0.00" value={amount} onChange={(e) => { setAmount(e.target.value); setError(""); }} />
        </Field>
        <Button type="submit" className="btn-block">Simulate deposit</Button>
        {note && <Alert variant="info" title="Deposit detected">{note}</Alert>}
      </form>
    </>
  );
}

function WithdrawForm({ meta, available, kycOk, onDone }: { meta?: Asset; available: string; kycOk: boolean; onDone: () => void }) {
  const [address, setAddress] = useState("");
  const [amount, setAmount] = useState("");
  const [errors, setErrors] = useState<{ address?: string; amount?: string }>({});
  const [confirming, setConfirming] = useState(false);
  const [busy, setBusy] = useState(false);
  const [result, setResult] = useState<{ ok: boolean; text: string } | null>(null);

  if (!meta) return null;
  const sym = meta.symbol;
  const d = dp(sym);
  const amt = toNum(amount);
  const fee = toNum(meta.withdrawFee);
  // The ledger debits amount + fee; the destination receives the amount.
  const total = amt + fee;

  function review(e: FormEvent) {
    e.preventDefault();
    setResult(null);
    const next: typeof errors = {};
    if (!address.trim()) next.address = `Paste the ${meta!.network} address you’re sending to.`;
    if (amt <= 0) next.amount = `Enter an amount in ${sym}.`;
    else if (amt < toNum(meta!.minWithdraw)) next.amount = `The minimum withdrawal is ${trimDecimal(meta!.minWithdraw)} ${sym}.`;
    else if (total > toNum(available)) next.amount = `You have ${fmt(available, d)} ${sym} available, and the network fee is ${trimDecimal(meta!.withdrawFee)} ${sym}. Lower the amount.`;
    setErrors(next);
    if (!next.address && !next.amount) setConfirming(true);
  }

  async function send() {
    setBusy(true);
    try {
      await api.withdraw(sym, address.trim(), amount);
      setResult({ ok: true, text: `${trimDecimal(amount)} ${sym} is broadcasting to the ${meta!.network} network. Track it in Transaction history.` });
      setAmount("");
      setAddress("");
      onDone();
    } catch (err) {
      setResult({ ok: false, text: err instanceof ApiError ? err.message : "The exchange didn’t respond. Nothing was sent." });
    } finally {
      setBusy(false);
      setConfirming(false);
    }
  }

  return (
    <form className="form" onSubmit={review} noValidate>
      {!kycOk && <Alert variant="warning" title="Verification needed">Verify your identity above to turn on withdrawals.</Alert>}
      <Field label="Destination address" error={errors.address} help={`Withdrawals go out on the ${meta.network} network.`}>
        <Input
          autoComplete="off" spellCheck={false} placeholder={`${meta.network} address`} value={address} disabled={!kycOk}
          onChange={(e) => { setAddress(e.target.value); setErrors((x) => ({ ...x, address: undefined })); }}
        />
      </Field>
      <Field label={`Amount (${sym})`} error={errors.amount}>
        <Input
          inputMode="decimal" autoComplete="off" placeholder="0.00" value={amount} disabled={!kycOk}
          onChange={(e) => { setAmount(e.target.value); setErrors((x) => ({ ...x, amount: undefined })); }}
        />
      </Field>
      <dl className="summary summary--ruled">
        <div><dt>Available</dt><dd>{fmt(available, d)} {sym}</dd></div>
        <div><dt>Network fee</dt><dd>{trimDecimal(meta.withdrawFee)} {sym}</dd></div>
        <div><dt>Minimum</dt><dd>{trimDecimal(meta.minWithdraw)} {sym}</dd></div>
        <div><dt>Total deducted</dt><dd>{amt > 0 ? `${fmt(total, d)} ${sym}` : "—"}</dd></div>
        <div className="summary__total"><dt>They receive</dt><dd>{amt > 0 ? `${trimDecimal(amount)} ${sym}` : "—"}</dd></div>
      </dl>
      <Button variant="primary" type="submit" className="btn-block" disabled={!kycOk || busy}>
        Withdraw {amt > 0 ? `${trimDecimal(amount)} ` : ""}{sym}
      </Button>
      {result && <Alert variant={result.ok ? "info" : "danger"} title={result.ok ? "Withdrawal submitted" : "Withdrawal not sent"}>{result.text}</Alert>}

      <Modal
        open={confirming}
        title={`Send ${trimDecimal(amount)} ${sym}?`}
        onClose={() => setConfirming(false)}
        dismissOnBackdrop={false}
        footer={
          <>
            <Button onClick={() => setConfirming(false)}>Edit details</Button>
            <Button variant="primary" onClick={send} disabled={busy}>{busy ? "Sending…" : `Send ${trimDecimal(amount)} ${sym}`}</Button>
          </>
        }
      >
        <dl className="confirm-list">
          <div className="stack"><dt>To</dt><dd className="mono">{address.trim()}</dd></div>
          <div><dt>Network</dt><dd>{meta.network}</dd></div>
          <div><dt>They receive</dt><dd className="strong">{trimDecimal(amount)} {sym}</dd></div>
          <div><dt>Total deducted</dt><dd>{fmt(total, d)} {sym}</dd></div>
        </dl>
        <p>Check the address character by character. Crypto sent to the wrong address can’t be recovered.</p>
      </Modal>
    </form>
  );
}
