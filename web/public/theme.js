// Applies the saved (or OS) colour theme before first paint so the page never
// flashes the wrong theme. A plain external script because the CSP forbids
// inline ones; hooks/useTheme.ts keeps the attribute in sync after load.
(function () {
  var theme = null;
  try { theme = localStorage.getItem("acme-theme"); } catch (e) { /* storage blocked */ }
  if (theme !== "light" && theme !== "dark") {
    theme = window.matchMedia("(prefers-color-scheme: dark)").matches ? "dark" : "light";
  }
  document.documentElement.dataset.theme = theme;
})();
