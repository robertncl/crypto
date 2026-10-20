import type { ReactNode } from "react";
import { StatusIcon, type StatusVariant } from "./StatusIcon";

/**
 * Inline message for the region it sits in. Only danger interrupts a screen
 * reader (role="alert"); the rest are announced politely.
 */
export function Alert({ variant = "info", title, children }: { variant?: StatusVariant; title?: ReactNode; children?: ReactNode }) {
  return (
    <div className={`acme-alert acme-alert--${variant}`} role={variant === "danger" ? "alert" : "status"}>
      <span className="acme-alert__icon"><StatusIcon variant={variant} size={16} strokeWidth={2} /></span>
      <div>
        {title && <p className="acme-alert__title">{title}</p>}
        {typeof children === "string" ? <p>{children}</p> : children}
      </div>
    </div>
  );
}
