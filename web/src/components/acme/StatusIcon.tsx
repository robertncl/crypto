// Status glyphs from the ACME system: 24px grid, round stroke, currentColor.
// Success and warning share Stone ink, so these icons carry the meaning.
const PATHS = {
  success: ["M20 6 9 17l-5-5"],
  warning: ["M12 3 2 20h20L12 3z", "M12 10v4", "M12 17h.01"],
  danger: ["M12 21a9 9 0 1 0 0-18 9 9 0 0 0 0 18z", "M15 9l-6 6", "M9 9l6 6"],
  info: ["M12 21a9 9 0 1 0 0-18 9 9 0 0 0 0 18z", "M12 16v-4", "M12 8h.01"],
};

export type StatusVariant = keyof typeof PATHS;

export function StatusIcon({ variant, size, strokeWidth }: { variant: StatusVariant; size: number; strokeWidth: number }) {
  return (
    <svg
      className="acme-status-icon" width={size} height={size} viewBox="0 0 24 24" fill="none"
      stroke="currentColor" strokeWidth={strokeWidth} strokeLinecap="round" strokeLinejoin="round"
      aria-hidden="true" focusable="false"
    >
      {PATHS[variant].map((d) => <path key={d} d={d} />)}
    </svg>
  );
}
