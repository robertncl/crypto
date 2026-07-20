import { Suspense } from "react";
import { Link, NavLink, Outlet, useNavigate } from "react-router-dom";
import { useAuth } from "../state/auth";
import { useTheme } from "../hooks/useTheme";

export function Layout() {
  const { user, logout } = useAuth();
  const navigate = useNavigate();
  const [theme, setTheme] = useTheme();

  return (
    <div className="app">
      <a href="#content" className="skip-link">Skip to content</a>
      <header className="topbar acme-topbar">
        <div className="topbar__brand">
          <Link to="/" className="acme-wordmark" style={{ fontSize: 15 }}>
            <span className="acme-wordmark__mark">A</span>ACME
          </Link>
          <span style={{ fontSize: "var(--acme-text-xs)", fontWeight: 600, letterSpacing: "0.08em", textTransform: "uppercase", color: "var(--acme-color-text-muted)" }}>
            Exchange
          </span>
          <nav className="topnav acme-topbar__nav" aria-label="Primary">
            <NavLink to="/markets" className="acme-topbar__link">Markets</NavLink>
            <NavLink to="/trade/BTC-USDT" className="acme-topbar__link">Spot</NavLink>
            <NavLink to="/futures/BTC-PERP" className="acme-topbar__link">Futures</NavLink>
            <NavLink to="/earn" className="acme-topbar__link">Earn</NavLink>
            <NavLink to="/wallet" className="acme-topbar__link">Wallet</NavLink>
          </nav>
        </div>
        <div className="topbar__account">
          <label className="acme-switch" style={{ color: "var(--acme-color-text-muted)" }}>
            <input
              type="checkbox"
              checked={theme === "dark"}
              onChange={(e) => setTheme(e.target.checked ? "dark" : "light")}
            />
            Dark
          </label>
          {user ? (
            <>
              {user.kycStatus !== "verified" && (
                <span className="badge acme-badge acme-badge--warning" title="Verify identity in Wallet to enable withdrawals">
                  Unverified
                </span>
              )}
              <span className="topbar__email">{user.email}</span>
              <button
                className="btn acme-btn btn--ghost acme-btn--ghost btn--mini"
                onClick={() => {
                  logout();
                  navigate("/login");
                }}
              >
                Sign out
              </button>
            </>
          ) : (
            <>
              <Link className="btn acme-btn btn--ghost acme-btn--ghost btn--mini" to="/login">Sign in</Link>
              <Link className="btn acme-btn btn--primary acme-btn--primary btn--mini" to="/register">Create account</Link>
            </>
          )}
        </div>
      </header>
      <main className="content" id="content" tabIndex={-1}>
        <Suspense fallback={<div className="empty pad">Loading…</div>}>
          <Outlet />
        </Suspense>
      </main>
    </div>
  );
}
