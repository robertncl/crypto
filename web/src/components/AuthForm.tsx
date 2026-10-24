import { useState, type FormEvent } from "react";
import { Link, useNavigate } from "react-router-dom";
import { ApiError } from "../api/client";
import { useAuth } from "../state/auth";
import { Alert, Button, Field, Input, Wordmark } from "./acme";

const EMAIL = /^[^@\s]+@[^@\s]+\.[^@\s]+$/;

function Point({ icon, children }: { icon: "chart" | "clock" | "shield"; children: string }) {
  return (
    <li>
      <svg width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.75" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
        {icon === "chart" && <><path d="M3 3v18h18" /><path d="m7 15 4-4 3 3 5-6" /></>}
        {icon === "clock" && <><circle cx="12" cy="12" r="9" /><path d="M12 7v5l3 2" /></>}
        {icon === "shield" && <><path d="M12 3 4 6v6c0 4.5 3.4 8.3 8 9 4.6-.7 8-4.5 8-9V6l-8-3z" /><path d="m9 12 2 2 4-4" /></>}
      </svg>
      {children}
    </li>
  );
}

export function AuthForm({ mode }: { mode: "login" | "register" }) {
  const { login, register } = useAuth();
  const navigate = useNavigate();
  const [email, setEmail] = useState("");
  const [password, setPassword] = useState("");
  const [errors, setErrors] = useState<{ email?: string; password?: string }>({});
  const [failure, setFailure] = useState("");
  const [busy, setBusy] = useState(false);

  const isRegister = mode === "register";

  async function submit(e: FormEvent) {
    e.preventDefault();
    setFailure("");
    const next: typeof errors = {};
    if (!EMAIL.test(email.trim())) next.email = isRegister ? "Enter an email address, like you@example.com." : "Enter the email you signed up with, like you@example.com.";
    // Server policy: 8–72 bytes (bcrypt's limit), counted as UTF-8.
    const pwBytes = new TextEncoder().encode(password).length;
    if (!isRegister && !password) next.password = "Enter your password.";
    else if (isRegister && pwBytes < 8) next.password = "Use at least 8 characters.";
    else if (isRegister && pwBytes > 72) next.password = "Use 72 characters or fewer.";
    setErrors(next);
    if (next.email || next.password) return;

    setBusy(true);
    try {
      if (isRegister) await register(email.trim(), password);
      else await login(email.trim(), password);
      navigate("/trade/BTC-USDT");
    } catch (err) {
      setFailure(err instanceof ApiError ? err.message : "The exchange didn’t respond. Check your connection and try again.");
    } finally {
      setBusy(false);
    }
  }

  return (
    <div className="auth">
      <aside className="auth__brand">
        <Wordmark to="/markets" onDark />
        <div className="auth__pitch">
          <p className="auth__kicker">ACME Exchange</p>
          <p className="auth__headline">{isRegister ? "10,000 USDT to start. Nothing real at risk." : "Every order, accounted for."}</p>
          <p className="auth__lede">
            {isRegister
              ? "Every new account gets demo funds the moment it opens. Trade, go long or short, or park it in Earn."
              : "Spot pairs, perpetual futures and Earn, settled on a double-entry ledger you can audit line by line."}
          </p>
          <ul className="auth__points">
            <Point icon="chart">Spot pairs and perpetual contracts, up to 100×</Point>
            <Point icon="clock">Earn interest on idle balances</Point>
            <Point icon="shield">Simulated custody: no real funds move</Point>
          </ul>
        </div>
        <p className="auth__foot">A demo exchange. Prices come from a market-maker bot.</p>
      </aside>

      <main className="auth__main">
        <form className="auth__form" onSubmit={submit} noValidate>
          <div>
            <h1>{isRegister ? "Create your account" : "Sign in"}</h1>
            <p className="muted">
              {isRegister ? "Takes a minute. Demo funds land as soon as you’re in." : "Welcome back. Your orders and balances are where you left them."}
            </p>
          </div>
          {failure && <Alert variant="danger" title={isRegister ? "Account not created" : "Couldn’t sign you in"}>{failure}</Alert>}
          <Field label="Email" error={errors.email}>
            <Input
              type="email" autoComplete="email" placeholder="you@example.com" value={email}
              onChange={(e) => { setEmail(e.target.value); setErrors((x) => ({ ...x, email: undefined })); }}
            />
          </Field>
          <Field label="Password" error={errors.password} help={isRegister ? "8 to 72 characters." : undefined}>
            <Input
              type="password" autoComplete={isRegister ? "new-password" : "current-password"} value={password}
              onChange={(e) => { setPassword(e.target.value); setErrors((x) => ({ ...x, password: undefined })); }}
            />
          </Field>
          <Button variant="primary" size="lg" type="submit" className="btn-block" disabled={busy}>
            {busy ? "One moment…" : isRegister ? "Create account" : "Sign in"}
          </Button>
          <p className="auth__alt">
            {isRegister
              ? <>Already have an account? <Link to="/login">Sign in</Link></>
              : <>New to ACME Exchange? <Link to="/register">Create an account</Link></>}
          </p>
        </form>
      </main>
    </div>
  );
}
