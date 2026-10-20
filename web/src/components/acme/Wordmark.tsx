import type { CSSProperties } from "react";
import { Link } from "react-router-dom";

/**
 * The ACME wordmark: Clay mark with an italic "A" plus "ACME". Never recolor or
 * redraw it. `onDark` is for Oat 900/800 grounds inside a light page.
 */
export function Wordmark({ to = "/", onDark = false, style }: { to?: string; onDark?: boolean; style?: CSSProperties }) {
  return (
    <Link className={onDark ? "acme-wordmark acme-wordmark--on-dark" : "acme-wordmark"} to={to} style={style} aria-label="ACME Exchange home">
      <span className="acme-wordmark__mark" aria-hidden="true">A</span>ACME
    </Link>
  );
}
