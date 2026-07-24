import { useEffect, useState } from "react";
import { api } from "../api/client";
import type { Ticker } from "../api/types";
import { toNum } from "../utils/format";

/**
 * Returns a priceOf(symbol) helper giving an asset's USD (USDT) price from
 * live tickers, fetched once on mount. USDT itself is always priced at 1.
 */
export function usePrices() {
  const [prices, setPrices] = useState<Record<string, number>>({});

  useEffect(() => {
    api.tickers().then((all: Ticker[]) =>
      setPrices(Object.fromEntries(all.map((t) => [t.market, toNum(t.last)]))),
    ).catch(() => {});
  }, []);

  const priceOf = (sym: string) => (sym === "USDT" ? 1 : prices[`${sym}-USDT`] ?? 0);

  return { prices, priceOf };
}
