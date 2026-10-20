import type { ReactNode } from "react";
import { StatusIcon, type StatusVariant } from "./StatusIcon";

export type BadgeVariant = "neutral" | StatusVariant;

/** Read-only status pill: one or two words, never a button. */
export function Badge({ variant = "neutral", icon = true, children }: { variant?: BadgeVariant; icon?: boolean; children: ReactNode }) {
  const status = variant === "neutral" ? null : variant;
  return (
    <span className={status ? `acme-badge acme-badge--${status}` : "acme-badge"}>
      {icon && status && <StatusIcon variant={status} size={12} strokeWidth={2.25} />}
      {children}
    </span>
  );
}
