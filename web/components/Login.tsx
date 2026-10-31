"use client";
import { useState } from "react";
import { api, setToken } from "@/lib/api";

function Logo() {
  return (
    <svg width="40" height="40" viewBox="0 0 32 32" aria-hidden>
      <defs>
        <linearGradient id="llg" x1="0" y1="0" x2="1" y2="1">
          <stop offset="0" stopColor="var(--accent)" />
          <stop offset="1" stopColor="var(--wait)" />
        </linearGradient>
      </defs>
      <rect x="2" y="2" width="28" height="28" rx="8" fill="var(--accent-soft)" />
      <circle cx="9" cy="16" r="3" fill="url(#llg)" />
      <circle cx="23" cy="9" r="3" fill="url(#llg)" />
      <circle cx="23" cy="23" r="3" fill="url(#llg)" />
      <path d="M11.6 14.6 20.4 10M11.6 17.4 20.4 22" stroke="url(#llg)" strokeWidth="2.2" strokeLinecap="round" />
    </svg>
  );
}

export function Login({ onSignedIn }: { onSignedIn: () => void }) {
  const [email, setEmail] = useState("");
  const [password, setPassword] = useState("");
  const [err, setErr] = useState("");
  const [busy, setBusy] = useState(false);

  async function submit(e: React.FormEvent) {
    e.preventDefault();
    setBusy(true);
    setErr("");
    try {
      const { token } = await api.login(email, password);
      setToken(token);
      onSignedIn();
    } catch (e) {
      setErr((e as Error).message || "Sign in failed");
    } finally {
      setBusy(false);
    }
  }

  return (
    <div className="login-wrap">
      <form className="login-card" onSubmit={submit}>
        <div className="login-brand">
          <Logo />
          <span className="wordmark" style={{ fontSize: 22 }}>Conduit</span>
        </div>
        <p className="login-sub">Sign in to your control plane</p>
        <div className="field">
          <label htmlFor="email">Email</label>
          <input id="email" className="inp" type="email" autoComplete="username" value={email} onChange={(e) => setEmail(e.target.value)} required />
        </div>
        <div className="field">
          <label htmlFor="password">Password</label>
          <input id="password" className="inp" type="password" autoComplete="current-password" value={password} onChange={(e) => setPassword(e.target.value)} required />
        </div>
        {err && <div className="login-err">{err}</div>}
        <button className="btn primary" type="submit" disabled={busy} style={{ justifyContent: "center" }}>
          {busy ? "Signing in…" : "Sign in"}
        </button>
        <p className="hint" style={{ textAlign: "center" }}>
          First run defaults to <code>admin@conduit.local</code> / <code>conduit</code> — change it after signing in.
        </p>
      </form>
    </div>
  );
}
