"use client";
import { useEffect, useState } from "react";
import { ArrowLeft } from "lucide-react";
import { api, setToken, setProject } from "@/lib/api";

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

type Mode = "signin" | "signup";

export function Auth({
  initialMode = "signin",
  onSignedIn,
  onBack,
}: {
  initialMode?: Mode;
  onSignedIn: () => void;
  onBack?: () => void;
}) {
  const [mode, setMode] = useState<Mode>(initialMode);
  const [name, setName] = useState("");
  const [email, setEmail] = useState("");
  const [password, setPassword] = useState("");
  const [err, setErr] = useState("");
  const [busy, setBusy] = useState(false);
  const [signupEnabled, setSignupEnabled] = useState(true);

  useEffect(() => {
    api.authConfig().then((c) => setSignupEnabled(c.signup_enabled)).catch(() => {});
  }, []);

  async function submit(e: React.FormEvent) {
    e.preventDefault();
    setBusy(true);
    setErr("");
    try {
      if (mode === "signup") {
        const { token, project } = await api.register(email, name, password);
        setToken(token);
        if (project?.id) setProject(project.id);
      } else {
        const { token } = await api.login(email, password);
        setToken(token);
      }
      onSignedIn();
    } catch (e) {
      setErr((e as Error).message || "Something went wrong");
    } finally {
      setBusy(false);
    }
  }

  const isSignup = mode === "signup";

  return (
    <div className="login-wrap">
      <form className="login-card" onSubmit={submit} data-testid="auth-form">
        {onBack && (
          <button type="button" className="linkbtn" onClick={onBack} style={{ alignSelf: "flex-start", marginBottom: 4 }}>
            <ArrowLeft size={14} /> Back
          </button>
        )}
        <div className="login-brand">
          <Logo />
          <span className="wordmark" style={{ fontSize: 22 }}>Conduit Agent</span>
        </div>

        <div className="auth-tabs" role="tablist">
          <button
            type="button"
            role="tab"
            className="auth-tab"
            data-on={!isSignup}
            data-testid="tab-signin"
            onClick={() => { setMode("signin"); setErr(""); }}
          >
            Sign in
          </button>
          <button
            type="button"
            role="tab"
            className="auth-tab"
            data-on={isSignup}
            data-testid="tab-signup"
            onClick={() => { setMode("signup"); setErr(""); }}
            disabled={!signupEnabled}
            title={signupEnabled ? "" : "Signup is disabled on this server"}
          >
            Create account
          </button>
        </div>

        {isSignup && (
          <div className="field">
            <label htmlFor="name">Name</label>
            <input id="name" className="inp" autoComplete="name" value={name} onChange={(e) => setName(e.target.value)} placeholder="Your name" />
          </div>
        )}
        <div className="field">
          <label htmlFor="email">Email</label>
          <input id="email" className="inp" type="email" autoComplete="username" value={email} onChange={(e) => setEmail(e.target.value)} required />
        </div>
        <div className="field">
          <label htmlFor="password">Password</label>
          <input
            id="password"
            className="inp"
            type="password"
            autoComplete={isSignup ? "new-password" : "current-password"}
            value={password}
            onChange={(e) => setPassword(e.target.value)}
            minLength={isSignup ? 8 : undefined}
            required
          />
          {isSignup && <span className="hint">At least 8 characters.</span>}
        </div>

        {err && <div className="login-err" data-testid="auth-error">{err}</div>}

        <button className="btn primary" type="submit" disabled={busy} data-testid="auth-submit" style={{ justifyContent: "center" }}>
          {busy ? (isSignup ? "Creating account…" : "Signing in…") : isSignup ? "Create account" : "Sign in"}
        </button>

        {!isSignup ? (
          <p className="hint" style={{ textAlign: "center" }}>
            First run defaults to <code>admin@conduit.local</code> / <code>conduit</code> — change it after signing in.
          </p>
        ) : (
          <p className="hint" style={{ textAlign: "center" }}>
            You&apos;ll get your own workspace to build workflows in.
          </p>
        )}
      </form>
    </div>
  );
}
