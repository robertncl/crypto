import { Suspense } from "react";
import { Link, NavLink, Outlet, useNavigate } from "react-router-dom";
import { useAuth } from "../state/auth";
import { Badge, Button, Wordmark, buttonClass } from "./acme";

const NAV = [
  { to: "/markets", label: "Markets" },
  { to: "/trade", label: "Spot" },
  { to: "/futures", label: "Futures" },
  { to: "/earn", label: "Earn" },
  { to: "/wallet", label: "Wallet" },
];

export function Layout() {
  const { user, logout } = useAuth();
  const navigate = useNavigate();

  return (
    <div className="app">
      <a href="#content" className="skip-link">Skip to content</a>
      <header className="acme-topbar topbar">
        <Wordmark to="/markets" />
        {/* NavLink marks the current page with aria-current, which the ACME
            top bar turns into its Signal Red-tinted chip. */}
        <nav className="acme-topbar__nav" aria-label="Primary">
          {NAV.map((n) => (
            <NavLink key={n.to} to={n.to} className="acme-topbar__link">{n.label}</NavLink>
          ))}
        </nav>
        <div className="topbar__account">
          {user ? (
            <>
              {user.kycStatus !== "verified" && <Badge variant="warning">Unverified</Badge>}
              <span className="topbar__email">{user.email}</span>
              <Button variant="ghost" size="sm" onClick={() => { logout(); navigate("/login"); }}>Sign out</Button>
            </>
          ) : (
            <>
              <Link className={buttonClass("ghost", "sm")} to="/login">Sign in</Link>
              <Link className={buttonClass("secondary", "sm")} to="/register">Create account</Link>
            </>
          )}
        </div>
      </header>
      <main className="content" id="content" tabIndex={-1}>
        <Suspense fallback={<div className="loading">Loading…</div>}>
          <Outlet />
        </Suspense>
      </main>
    </div>
  );
}

/** Sign-in and sign-up bring their own brand band, so they skip the top bar. */
export function BareLayout() {
  return (
    <Suspense fallback={<div className="loading">Loading…</div>}>
      <Outlet />
    </Suspense>
  );
}
