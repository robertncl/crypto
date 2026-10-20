import type { ButtonHTMLAttributes } from "react";

export type ButtonVariant = "primary" | "secondary" | "ghost" | "danger";
export type ButtonSize = "sm" | "lg";

/**
 * Class list for an ACME button. Exported so router <Link>s can look like
 * buttons without nesting a <button> inside an <a>.
 */
export function buttonClass(variant: ButtonVariant = "secondary", size?: ButtonSize, extra = ""): string {
  return ["acme-btn", `acme-btn--${variant}`, size ? `acme-btn--${size}` : "", extra].filter(Boolean).join(" ");
}

interface ButtonProps extends ButtonHTMLAttributes<HTMLButtonElement> {
  /** Primary is the Clay fill: one per view. Danger is for destructive, confirmed actions only. */
  variant?: ButtonVariant;
  /** sm = 32px (dense rows) · default 40px · lg = 48px */
  size?: ButtonSize;
}

export function Button({ variant = "secondary", size, className = "", type = "button", ...rest }: ButtonProps) {
  return <button type={type} className={buttonClass(variant, size, className)} {...rest} />;
}
