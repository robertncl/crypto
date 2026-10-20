import { useEffect, useMemo, useState } from "react";
import { Link } from "react-router-dom";
import { api } from "../api/client";
import { wsClient } from "../api/ws";
import type { FundingInfo, Market, PerpMarket, Ticker } from "../api/types";
import { fmt, fmtCompact, fmtPct, stepDecimals, toNum } from "../utils/format";
import { Badge, Field, Input, buttonClass } from "../components/acme";
import { AssetIcon, Direction, SrOnly } from "../components/common";

const SPARK_W = 96;
const SPARK_H = 28;

/** Polyline points for a 24h trend line of hourly closes. */
function sparkPoints(closes: number[]): string {
  if (closes.length < 2) return "";
  const lo = Math.min(...closes), hi = Math.max(...closes), span = hi - lo || 1;
  return closes
    .map((c, i) => `${((i / (closes.length - 1)) * SPARK_W).toFixed(1)},${(SPARK_H - 2 - ((c - lo) / span) * (SPARK_H - 4)).toFixed(1)}`)
    .join(" ");
}

export function Markets() {
  const [markets, setMarkets] = useState<Market[]>([]);
  const [perps, setPerps] = useState<PerpMarket[]>([]);
  const [tickers, setTickers] = useState<Record<string, Ticker>>({});
  const [names, setNames] = useState<Record<string, string>>({});
  const [funding, setFunding] = useState<Record<string, FundingInfo>>({});
  const [sparks, setSparks] = useState<Record<string, string>>({});
  const [q, setQ] = useState("");

  useEffect(() => {
    api.markets().then(setMarkets).catch(() => {});
    api.perpMarkets().then(setPerps).catch(() => {});
    api.tickers().then((all) => setTickers(Object.fromEntries(all.map((t) => [t.market, t])))).catch(() => {});
    api.assets().then((all) => setNames(Object.fromEntries(all.map((a) => [a.symbol, a.name])))).catch(() => {});
  }, []);

  // 24h trend lines from hourly candles, one request per spot pair.
  useEffect(() => {
    let live = true;
    for (const m of markets) {
      api.candles(m.symbol, "1h", 24).then((rows) => {
        if (live) setSparks((prev) => ({ ...prev, [m.symbol]: sparkPoints(rows.map((r) => toNum(r.close))) }));
      }).catch(() => {});
    }
    return () => { live = false; };
  }, [markets]);

  // Mark, index and funding for each perpetual, kept live.
  useEffect(() => {
    let live = true;
    for (const p of perps) {
      api.funding(p.symbol).then((f) => { if (live) setFunding((prev) => ({ ...prev, [p.symbol]: f })); }).catch(() => {});
    }
    const unsubs = perps.map((p) =>
      wsClient.subscribe(`funding:${p.symbol}`, (d) => setFunding((prev) => ({ ...prev, [p.symbol]: d as FundingInfo }))),
    );
    return () => { live = false; unsubs.forEach((u) => u()); };
  }, [perps]);

  // Subscribe to every market's ticker (spot + perp) once known.
  useEffect(() => {
    const symbols = [...markets.map((m) => m.symbol), ...perps.map((p) => p.symbol)];
    const unsubs = symbols.map((sym) =>
      wsClient.subscribe(`ticker:${sym}`, (d) => {
        const t = d as Ticker;
        setTickers((prev) => ({ ...prev, [t.market]: t }));
      }),
    );
    return () => unsubs.forEach((u) => u());
  }, [markets, perps]);

  const ql = q.trim().toLowerCase();
  const rows = useMemo(
    () => markets.filter((m) => !ql || `${m.base}/${m.quote} ${m.symbol} ${names[m.base] ?? ""}`.toLowerCase().includes(ql)),
    [markets, names, ql],
  );
  const perpRows = useMemo(() => perps.filter((m) => !ql || m.symbol.toLowerCase().includes(ql)), [perps, ql]);
  const maxLev = perps.reduce((n, p) => Math.max(n, p.maxLeverage), 0);

  return (
    <div className="page">
      <div className="page-head">
        <div className="page-head__text">
          <p className="kicker">ACME Exchange</p>
          <h1>Markets</h1>
          <p className="lede">Spot pairs and perpetual contracts, priced live by the matching engine.</p>
        </div>
        <div className="searchbox">
          <Field label="Search markets">
            <Input type="search" placeholder="BTC, ETH, SOL" value={q} onChange={(e) => setQ(e.target.value)} />
          </Field>
        </div>
      </div>

      <section className="section" aria-labelledby="spot-heading">
        <div className="section__head">
          <h2 id="spot-heading">Spot</h2>
          <span className="section__meta">{rows.length} {rows.length === 1 ? "pair" : "pairs"}</span>
        </div>
        <div className="table-card">
          <div className="table-pad table-pad--wide">
            <table className="acme-table">
              <caption className="visually-hidden">Spot markets</caption>
              <thead>
                <tr>
                  <th scope="col">Pair</th>
                  <th scope="col" className="acme-table__num">Last price</th>
                  <th scope="col" className="acme-table__num">24h change</th>
                  <th scope="col">24h trend</th>
                  <th scope="col" className="acme-table__num">24h high</th>
                  <th scope="col" className="acme-table__num">24h low</th>
                  <th scope="col" className="acme-table__num">24h volume</th>
                  <th scope="col"><SrOnly>Actions</SrOnly></th>
                </tr>
              </thead>
              <tbody>
                {rows.map((m) => {
                  const t = tickers[m.symbol];
                  const pd = stepDecimals(m.priceTick);
                  return (
                    <tr key={m.symbol}>
                      <td>
                        <Link className="assetcell" to={`/trade/${m.symbol}`}>
                          <AssetIcon symbol={m.base} />
                          <span className="assetcell__text">
                            <strong>{m.base}<span className="muted">/{m.quote}</span></strong>
                            <span className="assetcell__sub">{names[m.base] ?? " "}</span>
                          </span>
                        </Link>
                      </td>
                      <td className="acme-table__num strong">{t ? fmt(t.last, pd) : "—"}</td>
                      <td className="acme-table__num">{t ? <Direction up={toNum(t.changePct) >= 0}>{fmtPct(t.changePct)}</Direction> : "—"}</td>
                      <td>
                        {sparks[m.symbol] ? (
                          <svg className="spark" width={SPARK_W} height={SPARK_H} viewBox={`0 0 ${SPARK_W} ${SPARK_H}`} aria-hidden="true">
                            <polyline points={sparks[m.symbol]} />
                          </svg>
                        ) : <span className="muted">Not enough data yet</span>}
                      </td>
                      <td className="acme-table__num muted">{t ? fmt(t.high24h, pd) : "—"}</td>
                      <td className="acme-table__num muted">{t ? fmt(t.low24h, pd) : "—"}</td>
                      <td className="acme-table__num muted">{t ? `${fmtCompact(t.quoteVol24h)} ${m.quote}` : "—"}</td>
                      <td className="acme-table__num">
                        <Link className={buttonClass("secondary", "sm")} to={`/trade/${m.symbol}`} aria-label={`Trade ${m.base}/${m.quote}`}>Trade</Link>
                      </td>
                    </tr>
                  );
                })}
              </tbody>
            </table>
            {rows.length === 0 && (
              <p className="panel__empty">{ql ? `No spot pairs match “${q}”. Try a symbol like BTC.` : "Loading markets…"}</p>
            )}
          </div>
        </div>
      </section>

      {perps.length > 0 && (
        <section className="section" aria-labelledby="perp-heading">
          <div className="section__head">
            <h2 id="perp-heading">Perpetual futures</h2>
            <span className="section__meta">{perpRows.length} {perpRows.length === 1 ? "contract" : "contracts"}</span>
            <Badge variant="warning">Leverage up to {maxLev}×</Badge>
          </div>
          <div className="table-card">
            <div className="table-pad table-pad--wide">
              <table className="acme-table">
                <caption className="visually-hidden">Perpetual futures markets</caption>
                <thead>
                  <tr>
                    <th scope="col">Contract</th>
                    <th scope="col" className="acme-table__num">Last price</th>
                    <th scope="col" className="acme-table__num">24h change</th>
                    <th scope="col" className="acme-table__num">Mark price</th>
                    <th scope="col" className="acme-table__num">Index price</th>
                    <th scope="col" className="acme-table__num">Funding rate</th>
                    <th scope="col" className="acme-table__num">Max leverage</th>
                    <th scope="col"><SrOnly>Actions</SrOnly></th>
                  </tr>
                </thead>
                <tbody>
                  {perpRows.map((m) => {
                    const t = tickers[m.symbol];
                    const f = funding[m.symbol];
                    const pd = stepDecimals(m.priceTick);
                    const rate = toNum(f?.rate) * 100;
                    return (
                      <tr key={m.symbol}>
                        <td>
                          <Link className="assetcell" to={`/futures/${m.symbol}`}>
                            <AssetIcon symbol={m.base} />
                            <span className="assetcell__text">
                              <strong>{m.symbol}</strong>
                              <span className="assetcell__sub">{m.settle}-margined · isolated</span>
                            </span>
                          </Link>
                        </td>
                        <td className="acme-table__num strong">{t ? fmt(t.last, pd) : "—"}</td>
                        <td className="acme-table__num">{t ? <Direction up={toNum(t.changePct) >= 0}>{fmtPct(t.changePct)}</Direction> : "—"}</td>
                        <td className="acme-table__num">{f ? fmt(f.markPrice, pd) : "—"}</td>
                        <td className="acme-table__num muted">{f ? fmt(f.indexPrice, pd) : "—"}</td>
                        <td className={`acme-table__num ${rate >= 0 ? "up" : "down"}`}>{f ? fmtPct(rate, 4) : "—"}</td>
                        <td className="acme-table__num">{m.maxLeverage}×</td>
                        <td className="acme-table__num">
                          <Link className={buttonClass("secondary", "sm")} to={`/futures/${m.symbol}`} aria-label={`Trade ${m.symbol}`}>Trade</Link>
                        </td>
                      </tr>
                    );
                  })}
                </tbody>
              </table>
              {perpRows.length === 0 && <p className="panel__empty">No contracts match “{q}”.</p>}
            </div>
          </div>
        </section>
      )}

      <p className="footnote">Prices update in real time. ACME Exchange is a simulated custody demo: balances are not real funds.</p>
    </div>
  );
}
