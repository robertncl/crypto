import { useCallback, useEffect, useState } from "react";
import { api } from "../api/client";
import type { Balance } from "../api/types";
import { useAuth } from "../state/auth";
import { useChannel } from "./useStream";

/**
 * Returns the authenticated user's balances as a map keyed by asset, kept live
 * via the private "balances" WebSocket channel and refetched on demand.
 */
export function useBalances() {
  const { user } = useAuth();
  const [balances, setBalances] = useState<Record<string, Balance>>({});

  const load = useCallback(() => {
    if (!user) {
      setBalances({});
      return;
    }
    api.balances().then((list) => {
      setBalances(Object.fromEntries(list.map((b) => [b.asset, b])));
    }).catch(() => {});
  }, [user]);

  useEffect(() => { load(); }, [load]);

  useChannel<Balance[]>(user ? "balances" : null, (list) => {
    setBalances(Object.fromEntries(list.map((b) => [b.asset, b])));
  });

  const get = useCallback(
    (asset: string): Balance => balances[asset] ?? { asset, available: "0", locked: "0" },
    [balances],
  );

  return { balances, get, reload: load };
}
