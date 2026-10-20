import { useRef, type KeyboardEvent, type ReactNode } from "react";

export interface TabItem<T extends string> {
  id: T;
  label: ReactNode;
  count?: number;
}

/**
 * Underline tabs: views of the same thing, never page navigation. Tab i is
 * `${idPrefix}-tab-${id}`; give its panel tabPanelProps(idPrefix, id).
 * Arrow keys, Home and End move between tabs.
 */
export function Tabs<T extends string>({ tabs, active, onChange, idPrefix, label }: {
  tabs: TabItem<T>[];
  active: T;
  onChange: (id: T) => void;
  idPrefix: string;
  label: string;
}) {
  const refs = useRef<Partial<Record<T, HTMLButtonElement | null>>>({});

  function move(e: KeyboardEvent, i: number) {
    let n: number | null = null;
    if (e.key === "ArrowRight") n = (i + 1) % tabs.length;
    else if (e.key === "ArrowLeft") n = (i - 1 + tabs.length) % tabs.length;
    else if (e.key === "Home") n = 0;
    else if (e.key === "End") n = tabs.length - 1;
    if (n === null) return;
    e.preventDefault();
    const id = tabs[n].id;
    refs.current[id]?.focus();
    onChange(id);
  }

  return (
    <div className="acme-tabs" role="tablist" aria-label={label}>
      {tabs.map((t, i) => {
        const selected = t.id === active;
        return (
          <button
            key={t.id}
            ref={(el) => { refs.current[t.id] = el; }}
            type="button"
            role="tab"
            className="acme-tab"
            id={`${idPrefix}-tab-${t.id}`}
            aria-controls={`${idPrefix}-panel-${t.id}`}
            aria-selected={selected}
            tabIndex={selected ? 0 : -1}
            onClick={() => onChange(t.id)}
            onKeyDown={(e) => move(e, i)}
          >
            {t.label}
            {t.count !== undefined && <span className="acme-tab__count">{t.count}</span>}
          </button>
        );
      })}
    </div>
  );
}

/** Props for the panel a tab controls. */
export function tabPanelProps(idPrefix: string, id: string) {
  return { id: `${idPrefix}-panel-${id}`, role: "tabpanel", "aria-labelledby": `${idPrefix}-tab-${id}` } as const;
}
