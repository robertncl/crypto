import { useEffect, useRef } from "react";
import { wsClient } from "../api/ws";

/**
 * Subscribe to a WebSocket channel for the lifetime of the component. The
 * handler is kept in a ref so re-renders don't churn the subscription; only a
 * change of channel re-subscribes.
 */
export function useChannel<T = unknown>(channel: string | null, handler: (data: T) => void) {
  const ref = useRef(handler);
  ref.current = handler;
  useEffect(() => {
    if (!channel) return;
    return wsClient.subscribe(channel, (d) => ref.current(d as T));
  }, [channel]);
}
