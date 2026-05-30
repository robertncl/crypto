import { useState, type FormEvent } from "react";
import { Link, useNavigate } from "react-router-dom";
import { ApiError } from "../api/client";
import { useAuth } from "../state/auth";

export function AuthForm({ mode }: { mode: "login" | "register" }) {
  const { login, register } = useAuth();
  const navigate = useNavigate();
  const [email, setEmail] = useState("");
  const [password, setPassword] = useState("");
  const [err, setErr] = useState("");
  const [busy, setBusy] = useState(false);

  const isRegister = mode === "register";

  async function submit(e: FormEvent) {
    e.preventDefault();
    setErr("");
    setBusy(true);
    try {
      if (isRegister) await register(email, password);
      else await login(email, password);
      navigate("/trade/BTC-USDT");
    } catch (e) {
      setErr(e instanceof ApiError ? e.message : "Something went wrong");
    } finally {
      setBusy(false);
    }
  }

  return (
    <div className="authpage">
      <form className="authcard" onSubmit={submit} noValidate>
        <h1 className="authcard__title">{isRegister ? "Create your account" : "Welcome back"}</h1>
        <p className="authcard__sub muted">
          {isRegister ? "New accounts get 10,000 USDT to explore the exchange." : "Log in to trade and manage your assets."}
        </p>

        <label className="field field--stack">
          <span className="field__label">Email</span>
          <input
            type="email" autoComplete="email" required value={email}
            onChange={(e) => setEmail(e.target.value)} placeholder="you@example.com"
          />
        </label>

        <label className="field field--stack">
          <span className="field__label">Password</span>
          <input
            type="password" required minLength={6}
            autoComplete={isRegister ? "new-password" : "current-password"}
            value={password} onChange={(e) => setPassword(e.target.value)} placeholder="••••••••"
          />
        </label>

        {err && <div className="formmsg formmsg--err" role="alert">{err}</div>}

        <button className="btn btn--primary btn--block" disabled={busy}>
          {busy ? "Please wait…" : isRegister ? "Create account" : "Log in"}
        </button>

        <p className="authcard__alt muted">
          {isRegister ? (
            <>Already have an account? <Link to="/login">Log in</Link></>
          ) : (
            <>New to Nebula? <Link to="/register">Create an account</Link></>
          )}
        </p>
      </form>
    </div>
  );
}
