import { Suspense } from "react";
import { Link, NavLink, Outlet, useNavigate } from "react-router-dom";
import { useAuth } from "../state/auth";

export function Layout() {
  const { user, logout } = useAuth();
  const navigate = useNavigate();

  return (
    <div className="app">
      <a href="#content" className="skip-link">Skip to content</a>
      <header className="topbar">
        <div className="topbar__brand">
          <Link to="/" className="logo">
            <span className="logo__mark" aria-hidden>◆</span>
            <span className="logo__name">Nebula</span>
          </Link>
          <nav className="topnav" aria-label="Primary">
            <NavLink to="/markets">Markets</NavLink>
            <NavLink to="/trade/BTC-USDT">Spot</NavLink>
            <NavLink to="/futures/BTC-PERP">Futures</NavLink>
            <NavLink to="/wallet">Wallet</NavLink>
          </nav>
        </div>
        <div className="topbar__account">
          {user ? (
            <>
              {user.kycStatus !== "verified" && (
                <span className="badge badge--warn" title="Verify identity in Wallet to enable withdrawals">
                  Unverified
                </span>
              )}
              <span className="topbar__email">{user.email}</span>
              <button
                className="btn btn--ghost"
                onClick={() => {
                  logout();
                  navigate("/login");
                }}
              >
                Log out
              </button>
            </>
          ) : (
            <>
              <Link className="btn btn--ghost" to="/login">Log in</Link>
              <Link className="btn btn--primary" to="/register">Sign up</Link>
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
