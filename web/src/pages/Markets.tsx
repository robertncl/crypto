import { useEffect, useMemo, useState } from "react";
import { useNavigate } from "react-router-dom";
import { api } from "../api/client";
import { wsClient } from "../api/ws";
import type { Market, Ticker } from "../api/types";
import { fmt, fmtCompact, fmtPct, toNum } from "../utils/format";

export function Markets() {
  const navigate = useNavigate();
  const [markets, setMarkets] = useState<Market[]>([]);
  const [tickers, setTickers] = useState<Record<string, Ticker>>({});
  const [q, setQ] = useState("");

  useEffect(() => {
    api.markets().then(setMarkets).catch(() => {});
    api.tickers().then((all) => setTickers(Object.fromEntries(all.map((t) => [t.market, t])))).catch(() => {});
  }, []);

  // Subscribe to each market's ticker once markets are known.
  useEffect(() => {
    const unsubs = markets.map((m) =>
      wsClient.subscribe(`ticker:${m.symbol}`, (d) => {
        const t = d as Ticker;
        setTickers((prev) => ({ ...prev, [t.market]: t }));
      }),
    );
    return () => unsubs.forEach((u) => u());
  }, [markets]);

  const rows = useMemo(
    () => markets.filter((m) => m.symbol.toLowerCase().includes(q.toLowerCase())),
    [markets, q],
  );

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
              <th>Pair</th>
              <th className="r">Last Price</th>
              <th className="r">24h Change</th>
              <th className="r">24h High</th>
              <th className="r">24h Low</th>
              <th className="r">24h Volume</th>
              <th></th>
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
                  <td className="r"><button className="btn btn--mini btn--primary">Trade</button></td>
                </tr>
              );
            })}
          </tbody>
        </table>
        {rows.length === 0 && <div className="empty pad">No markets found</div>}
      </div>
    </div>
  );
}
