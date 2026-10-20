import {
  Children, cloneElement, isValidElement, useId,
  type InputHTMLAttributes, type ReactElement, type ReactNode, type SelectHTMLAttributes,
} from "react";

/**
 * Label above, control, then help or error text below. Wires itself: a single
 * child control gets an id, the label's htmlFor, aria-describedby pointing at
 * the message, and aria-invalid when there is an error.
 */
export function Field({ label, optional = false, help, error, htmlFor, children }: {
  label: ReactNode;
  optional?: boolean;
  help?: ReactNode;
  error?: ReactNode;
  htmlFor?: string;
  children: ReactNode;
}) {
  const auto = useId().replace(/:/g, "");
  const only = Children.count(children) === 1 && isValidElement(children)
    ? (children as ReactElement<Record<string, unknown>>)
    : null;
  const controlId = htmlFor ?? (only?.props.id as string | undefined) ?? (only ? `acme-field${auto}` : undefined);
  const msgId = controlId ? `${controlId}-msg` : undefined;
  const msg = error || help;

  let control = children;
  if (only) {
    const describedBy = [only.props["aria-describedby"], msg ? msgId : null].filter(Boolean).join(" ") || undefined;
    control = cloneElement(only, {
      id: controlId,
      "aria-describedby": describedBy,
      ...(error ? { "aria-invalid": true } : null),
    });
  }

  return (
    <div className="acme-field">
      <label className="acme-label" htmlFor={controlId}>
        {label}
        {optional && <span className="acme-label__optional"> (optional)</span>}
      </label>
      {control}
      {error ? <p className="acme-error" id={msgId}>{error}</p> : help ? <p className="acme-help" id={msgId}>{help}</p> : null}
    </div>
  );
}

/** Text input: 40px, radius-md, strong border. Placeholders are examples, never labels. */
export function Input({ className = "", ...rest }: InputHTMLAttributes<HTMLInputElement>) {
  return <input className={`acme-input ${className}`.trim()} {...rest} />;
}

/** Native select sharing the input styling. */
export function Select({ className = "", ...rest }: SelectHTMLAttributes<HTMLSelectElement>) {
  return <select className={`acme-input ${className}`.trim()} {...rest} />;
}

/** Toggle for settings that apply immediately. */
export function Switch({ label, ...rest }: { label: ReactNode } & Omit<InputHTMLAttributes<HTMLInputElement>, "type" | "role">) {
  return (
    <label className="acme-switch">
      <input type="checkbox" role="switch" {...rest} />
      <span>{label}</span>
    </label>
  );
}
