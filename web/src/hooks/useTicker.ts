import { useEffect, useState } from "react";
import { api } from "../api/client";
import type { Ticker } from "../api/types";
import { useChannel } from "./useStream";

/**
 * A market's 24h ticker: the REST snapshot on mount, then live `ticker:`
 * pushes. A push that lands before the snapshot wins, since it's newer.
 */
export function useTicker(symbol: string | null): Ticker | null {
  const [ticker, setTicker] = useState<Ticker | null>(null);

  useEffect(() => {
    setTicker(null);
    if (!symbol) return;
    let live = true;
    api.tickers().then((all) => {
      const snap = all.find((t) => t.market === symbol) ?? null;
      if (live) setTicker((cur) => cur ?? snap);
    }).catch(() => {});
    return () => { live = false; };
  }, [symbol]);

  useChannel<Ticker>(symbol ? `ticker:${symbol}` : null, setTicker);

  return ticker;
}
