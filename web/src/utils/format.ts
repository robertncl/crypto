// Display formatting helpers. Inputs are decimal strings from the API; these
// convert to Number only for presentation (never for settlement math).

export function toNum(s: string | number | undefined | null): number {
  if (s == null) return 0;
  const n = typeof s === "number" ? s : parseFloat(s);
  return Number.isFinite(n) ? n : 0;
}

/** Format a decimal string with grouping and a fixed/auto number of decimals. */
export function fmt(value: string | number, decimals?: number): string {
  const n = toNum(value);
  if (decimals == null) {
    // auto: more precision for small numbers
    const abs = Math.abs(n);
    decimals = abs === 0 ? 2 : abs < 1 ? 6 : abs < 100 ? 4 : 2;
  }
  return n.toLocaleString("en-US", {
    minimumFractionDigits: decimals,
    maximumFractionDigits: decimals,
  });
}

/** Compact notation for big numbers (volumes): 1.23M, 4.5K. */
export function fmtCompact(value: string | number): string {
  const n = toNum(value);
  return n.toLocaleString("en-US", { notation: "compact", maximumFractionDigits: 2 });
}

export function fmtPct(value: string | number): string {
  const n = toNum(value);
  const sign = n > 0 ? "+" : "";
  return `${sign}${n.toFixed(2)}%`;
}

export function fmtUsd(value: string | number, decimals = 2): string {
  return `$${fmt(value, decimals)}`;
}

/** Trim trailing zeros from a decimal string for compact input display. */
export function trimDecimal(s: string): string {
  if (!s.includes(".")) return s;
  return s.replace(/\.?0+$/, "");
}

/** Number of decimal places implied by a step like "0.001" => 3. */
export function stepDecimals(step: string): number {
  const i = step.indexOf(".");
  if (i < 0) return 0;
  return trimDecimal(step).length - i - 1;
}

/** Round a value to a step (both numbers), returning a fixed string. */
export function roundToStep(value: number, step: string): string {
  const s = toNum(step);
  if (s <= 0) return String(value);
  const rounded = Math.floor(value / s) * s;
  return rounded.toFixed(stepDecimals(step));
}

export function timeAgo(unixSeconds: number): string {
  const d = new Date(unixSeconds * 1000);
  return d.toLocaleTimeString("en-US", { hour12: false });
}

export function shortId(id: string): string {
  return id.length > 10 ? `${id.slice(0, 6)}…${id.slice(-4)}` : id;
}
