import { useEffect, useId, useRef, type ReactNode } from "react";
import { createPortal } from "react-dom";

const FOCUSABLE = 'a[href], button:not([disabled]), input:not([disabled]), select:not([disabled]), textarea:not([disabled]), [tabindex]:not([tabindex="-1"])';

/**
 * Interrupts for destructive confirms and short focused tasks. Traps focus,
 * focuses the first field (else the first footer action — put the safe one
 * first), closes on Escape, and returns focus on close.
 */
export function Modal({ open, title, footer, onClose, dismissOnBackdrop = true, children }: {
  open: boolean;
  /** Names the action and object: "Close your BTC-PERP long?" */
  title: ReactNode;
  /** Right-aligned actions: the safe one first, primary/danger rightmost. */
  footer?: ReactNode;
  onClose: () => void;
  /** Set false for destructive confirms. */
  dismissOnBackdrop?: boolean;
  children?: ReactNode;
}) {
  const titleId = `acme-modal${useId().replace(/:/g, "")}-title`;
  const ref = useRef<HTMLDivElement>(null);
  const closeRef = useRef(onClose);
  closeRef.current = onClose;

  useEffect(() => {
    const box = ref.current;
    if (!open || !box) return;
    const prev = document.activeElement as HTMLElement | null;
    const first = box.querySelector<HTMLElement>(`.acme-modal__body :is(${FOCUSABLE})`)
      ?? box.querySelector<HTMLElement>(`.acme-modal__footer :is(${FOCUSABLE})`)
      ?? box;
    first.focus();
    const onKey = (e: KeyboardEvent) => {
      if (e.key === "Escape") { e.stopPropagation(); closeRef.current(); return; }
      if (e.key !== "Tab") return;
      const items = Array.from(box.querySelectorAll<HTMLElement>(FOCUSABLE));
      if (items.length === 0) { e.preventDefault(); return; }
      const a = items[0], z = items[items.length - 1];
      if (e.shiftKey && document.activeElement === a) { e.preventDefault(); z.focus(); }
      else if (!e.shiftKey && document.activeElement === z) { e.preventDefault(); a.focus(); }
    };
    box.addEventListener("keydown", onKey);
    return () => {
      box.removeEventListener("keydown", onKey);
      prev?.focus();
    };
  }, [open]);

  if (!open) return null;
  return createPortal(
    <div
      className="acme-modal-backdrop"
      onClick={dismissOnBackdrop ? (e) => { if (e.target === e.currentTarget) onClose(); } : undefined}
    >
      <div className="acme-modal" role="dialog" aria-modal="true" aria-labelledby={titleId} tabIndex={-1} ref={ref}>
        <div className="acme-modal__header">
          <h3 className="acme-modal__title" id={titleId}>{title}</h3>
          <button type="button" className="acme-btn acme-btn--ghost acme-btn--sm acme-btn--icon" aria-label="Close" onClick={onClose}>
            <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" aria-hidden="true" focusable="false">
              <path d="M18 6 6 18" /><path d="M6 6l12 12" />
            </svg>
          </button>
        </div>
        <div className="acme-modal__body">{children}</div>
        {footer && <div className="acme-modal__footer">{footer}</div>}
      </div>
    </div>,
    document.body,
  );
}
