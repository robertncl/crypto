import type { ReactNode } from "react";
import type { OrderStatus } from "../api/types";
import { Badge, type BadgeVariant } from "./acme";

const ORDER_STATUS: Record<OrderStatus, { label: string; variant: BadgeVariant }> = {
  open: { label: "Open", variant: "info" },
  partial: { label: "Partial", variant: "warning" },
  filled: { label: "Filled", variant: "success" },
  canceled: { label: "Canceled", variant: "neutral" },
  rejected: { label: "Rejected", variant: "danger" },
};

export function OrderStatusBadge({ status }: { status: OrderStatus }) {
  const s = ORDER_STATUS[status] ?? { label: status, variant: "neutral" };
  return <Badge variant={s.variant}>{s.label}</Badge>;
}

/** Square tile with the asset's initial (the system ships no crypto logos). */
export function AssetIcon({ symbol, large = false }: { symbol: string; large?: boolean }) {
  return <span className={large ? "asset-icon asset-icon--lg" : "asset-icon"} aria-hidden="true">{symbol.charAt(0)}</span>;
}

/** Asset tile, a bold name and a muted second line. */
export function AssetCell({ symbol, title, sub }: { symbol: string; title: ReactNode; sub?: ReactNode }) {
  return (
    <span className="assetcell">
      <AssetIcon symbol={symbol} />
      <span className="assetcell__text">
        <strong>{title}</strong>
        {sub && <span className="assetcell__sub">{sub}</span>}
      </span>
    </span>
  );
}

export function ArrowIcon({ up, size = 14 }: { up: boolean; size?: number }) {
  return (
    <svg width={size} height={size} viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2.25" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true" focusable="false">
      {up ? <><path d="M7 17 17 7" /><path d="M7 7h10v10" /></> : <><path d="M7 7l10 10" /><path d="M17 7v10H7" /></>}
    </svg>
  );
}

/**
 * A value that moves with the market. Up/buy reads in Blue and down/sell in
 * Crimson (ACME's info and danger hues; Signal Red stays reserved for actions and
 * orientation), always with an arrow so colour never carries it alone.
 */
export function Direction({ up, children, className = "" }: { up: boolean; children: ReactNode; className?: string }) {
  return (
    <span className={`dir ${up ? "up" : "down"} ${className}`.trim()}>
      <ArrowIcon up={up} />
      {children}
    </span>
  );
}

/** Bordered surface with a serif title row; `aside` holds a hint or tabs. */
export function Panel({ id, title, aside, tabbed = false, className = "", children }: {
  id: string;
  title: ReactNode;
  aside?: ReactNode;
  tabbed?: boolean;
  className?: string;
  children: ReactNode;
}) {
  return (
    <section className={`panel ${className}`.trim()} aria-labelledby={`${id}-title`}>
      <header className={tabbed ? "panel__head panel__head--tabs" : "panel__head"}>
        <h2 className="panel__title" id={`${id}-title`}>{title}</h2>
        {aside}
      </header>
      {children}
    </section>
  );
}

/** Two-way buy/sell (long/short) switch; the pressed side takes its direction colour. */
export function SideSwitch<T extends string>({ label, value, onChange, options }: {
  label: string;
  value: T;
  onChange: (v: T) => void;
  options: { id: T; label: string; up: boolean }[];
}) {
  return (
    <div className="sideswitch" role="group" aria-label={label}>
      {options.map((o) => (
        <button
          key={o.id}
          type="button"
          aria-pressed={o.id === value}
          className={`sideswitch__btn ${o.up ? "sideswitch__btn--up" : "sideswitch__btn--down"}`}
          onClick={() => onChange(o.id)}
        >
          <ArrowIcon up={o.up} />
          {o.label}
        </button>
      ))}
    </div>
  );
}

/** Visually hidden text for screen readers (column labels for action cells). */
export function SrOnly({ children }: { children: ReactNode }) {
  return <span className="visually-hidden">{children}</span>;
}
