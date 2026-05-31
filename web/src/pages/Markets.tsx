import { useEffect, useMemo, useState } from "react";
import { useNavigate } from "react-router-dom";
import { api } from "../api/client";
import { wsClient } from "../api/ws";
import type { Market, PerpMarket, Ticker } from "../api/types";
import { fmt, fmtCompact, fmtPct, toNum } from "../utils/format";

export function Markets() {
  const navigate = useNavigate();
  const [markets, setMarkets] = useState<Market[]>([]);
  const [perps, setPerps] = useState<PerpMarket[]>([]);
  const [tickers, setTickers] = useState<Record<string, Ticker>>({});
  const [q, setQ] = useState("");

  useEffect(() => {
    api.markets().then(setMarkets).catch(() => {});
    api.perpMarkets().then(setPerps).catch(() => {});
    api.tickers().then((all) => setTickers(Object.fromEntries(all.map((t) => [t.market, t])))).catch(() => {});
  }, []);

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

  const ql = q.toLowerCase();
  const rows = useMemo(() => markets.filter((m) => m.symbol.toLowerCase().includes(ql)), [markets, ql]);
  const perpRows = useMemo(() => perps.filter((m) => m.symbol.toLowerCase().includes(ql)), [perps, ql]);

  return (
    <div className="page page--markets">
      <div className="markets-hero">
        <div>
          <h1>Markets</h1>
          <p className="muted">Spot trading pairs. Real-time prices from the matching engine.</p>
        </div>
        <input
          className="search" placeholder="Search pairs…" value={q}
          onChange={(e) => setQ(e.target.value)} aria-label="Search markets"
        />
      </div>

      <div className="card">
        <table className="dtable dtable--markets">
          <thead>
            <tr>
              <th scope="col">Pair</th>
              <th scope="col" className="r">Last Price</th>
              <th scope="col" className="r">24h Change</th>
              <th scope="col" className="r">24h High</th>
              <th scope="col" className="r">24h Low</th>
              <th scope="col" className="r">24h Volume</th>
              <th aria-label="Actions"></th>
            </tr>
          </thead>
          <tbody>
            {rows.map((m) => {
              const t = tickers[m.symbol];
              const up = toNum(t?.changePct) >= 0;
              return (
                <tr key={m.symbol} className="rowlink" onClick={() => navigate(`/trade/${m.symbol}`)}>
                  <td className="paircell">
                    <span className="paircell__icon" aria-hidden>{m.base[0]}</span>
                    <span><strong>{m.base}</strong><span className="muted">/{m.quote}</span></span>
                  </td>
                  <td className="r mono">{fmt(t?.last ?? "0", 2)}</td>
                  <td className={`r ${up ? "up" : "down"}`}>{fmtPct(t?.changePct ?? "0")}</td>
                  <td className="r mono muted">{fmt(t?.high24h ?? "0", 2)}</td>
                  <td className="r mono muted">{fmt(t?.low24h ?? "0", 2)}</td>
                  <td className="r mono muted">{fmtCompact(t?.quoteVol24h ?? "0")} {m.quote}</td>
                  <td className="r">
                    <button
                      className="btn btn--mini btn--primary"
                      onClick={(e) => { e.stopPropagation(); navigate(`/trade/${m.symbol}`); }}
                      aria-label={`Trade ${m.symbol}`}
                    >Trade</button>
                  </td>
                </tr>
              );
            })}
          </tbody>
        </table>
        {rows.length === 0 && <div className="empty pad">No spot markets found</div>}
      </div>

      {perpRows.length > 0 && (
        <>
          <h2 className="markets-subhead">Perpetual Futures <span className="badge badge--warn">Leverage</span></h2>
          <div className="card">
            <table className="dtable dtable--markets">
              <thead>
                <tr>
                  <th scope="col">Contract</th>
                  <th scope="col" className="r">Last Price</th>
                  <th scope="col" className="r">24h Change</th>
                  <th scope="col" className="r">Index</th>
                  <th scope="col" className="r">Max Leverage</th>
                  <th aria-label="Actions"></th>
                </tr>
              </thead>
              <tbody>
                {perpRows.map((m) => {
                  const t = tickers[m.symbol];
                  const up = toNum(t?.changePct) >= 0;
                  return (
                    <tr key={m.symbol} className="rowlink" onClick={() => navigate(`/futures/${m.symbol}`)}>
                      <td className="paircell">
                        <span className="paircell__icon" aria-hidden>{m.base[0]}</span>
                        <span><strong>{m.symbol}</strong></span>
                      </td>
                      <td className="r mono">{fmt(t?.last ?? "0", 2)}</td>
                      <td className={`r ${up ? "up" : "down"}`}>{fmtPct(t?.changePct ?? "0")}</td>
                      <td className="r mono muted">{m.indexSymbol}</td>
                      <td className="r mono">{m.maxLeverage}×</td>
                      <td className="r">
                        <button
                          className="btn btn--mini btn--primary"
                          onClick={(e) => { e.stopPropagation(); navigate(`/futures/${m.symbol}`); }}
                          aria-label={`Trade ${m.symbol}`}
                        >Trade</button>
                      </td>
                    </tr>
                  );
                })}
              </tbody>
            </table>
          </div>
        </>
      )}
    </div>
  );
}
