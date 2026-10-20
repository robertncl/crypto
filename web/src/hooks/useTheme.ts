import { useSyncExternalStore } from "react";

export type Theme = "light" | "dark";

const KEY = "acme-theme";
const listeners = new Set<() => void>();

// public/theme.js sets data-theme on <html> before first paint (the saved
// choice, else the OS preference). This store reads and writes that single
// attribute, so every component asking for the theme — the top-bar switch,
// the chart — stays in sync instead of each holding its own copy.
function current(): Theme {
  return document.documentElement.dataset.theme === "dark" ? "dark" : "light";
}

function setTheme(theme: Theme) {
  document.documentElement.dataset.theme = theme;
  try {
    localStorage.setItem(KEY, theme);
  } catch {
    // storage blocked (private mode): the choice lasts for this page only
  }
  listeners.forEach((l) => l());
}

function subscribe(listener: () => void) {
  listeners.add(listener);
  return () => {
    listeners.delete(listener);
  };
}

export function useTheme(): [Theme, (t: Theme) => void] {
  return [useSyncExternalStore(subscribe, current), setTheme];
}
