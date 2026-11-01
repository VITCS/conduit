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
          <stop offset="1" stopColor="var(--accent-deep)" />
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

function GoogleGlyph() {
  return (
    <svg width="16" height="16" viewBox="0 0 48 48" aria-hidden>
      <path fill="#FFC107" d="M43.6 20.5H42V20H24v8h11.3C33.7 32.9 29.3 36 24 36c-6.6 0-12-5.4-12-12s5.4-12 12-12c3.1 0 5.9 1.2 8 3.1l5.7-5.7C34.3 6.1 29.4 4 24 4 12.9 4 4 12.9 4 24s8.9 20 20 20 20-8.9 20-20c0-1.2-.1-2.3-.4-3.5z" />
      <path fill="#FF3D00" d="M6.3 14.7l6.6 4.8C14.7 15.1 19 12 24 12c3.1 0 5.9 1.2 8 3.1l5.7-5.7C34.3 6.1 29.4 4 24 4 16.3 4 9.7 8.3 6.3 14.7z" />
      <path fill="#4CAF50" d="M24 44c5.2 0 10-2 13.6-5.2l-6.3-5.3C29.2 35 26.7 36 24 36c-5.3 0-9.7-3.1-11.3-7.6l-6.5 5C9.6 39.6 16.2 44 24 44z" />
      <path fill="#1976D2" d="M43.6 20.5H42V20H24v8h11.3c-.8 2.2-2.2 4.1-4 5.5l6.3 5.3C41.9 36 44 30.5 44 24c0-1.2-.1-2.3-.4-3.5z" />
    </svg>
  );
}
function GithubGlyph() {
  return (
    <svg width="16" height="16" viewBox="0 0 24 24" fill="currentColor" aria-hidden>
      <path d="M12 .5A11.5 11.5 0 0 0 .5 12c0 5.08 3.29 9.39 7.86 10.91.58.1.79-.25.79-.56v-2c-3.2.7-3.88-1.37-3.88-1.37-.52-1.33-1.28-1.69-1.28-1.69-1.05-.72.08-.7.08-.7 1.16.08 1.77 1.2 1.77 1.2 1.03 1.77 2.7 1.26 3.36.96.1-.75.4-1.26.73-1.55-2.55-.29-5.24-1.28-5.24-5.69 0-1.26.45-2.29 1.19-3.1-.12-.29-.52-1.46.11-3.05 0 0 .97-.31 3.18 1.18a11 11 0 0 1 5.8 0c2.2-1.49 3.17-1.18 3.17-1.18.63 1.59.23 2.76.12 3.05.74.81 1.18 1.84 1.18 3.1 0 4.42-2.69 5.39-5.25 5.68.41.36.78 1.06.78 2.14v3.17c0 .31.21.67.8.56A11.5 11.5 0 0 0 23.5 12 11.5 11.5 0 0 0 12 .5z" />
    </svg>
  );
}

type Mode = "signin" | "signup";

export function Auth({
  initialMode = "signin",
  notice = "",
  onSignedIn,
  onBack,
}: {
  initialMode?: Mode;
  notice?: string;
  onSignedIn: () => void;
  onBack?: () => void;
}) {
  const [mode, setMode] = useState<Mode>(initialMode);
  const [name, setName] = useState("");
  const [email, setEmail] = useState("");
  const [password, setPassword] = useState("");
  const [err, setErr] = useState(notice);
  const [busy, setBusy] = useState(false);
  const [signupEnabled, setSignupEnabled] = useState(true);
  const [providers, setProviders] = useState<string[]>([]);

  useEffect(() => {
    api
      .authConfig()
      .then((c) => {
        setSignupEnabled(c.signup_enabled);
        setProviders(c.providers || []);
      })
      .catch(() => {});
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

        {providers.length > 0 && (
          <>
            <div className="oauth-list">
              {providers.includes("google") && (
                <a className="btn oauth" href={api.oauthStartURL("google")} data-testid="oauth-google">
                  <GoogleGlyph /> Continue with Google
                </a>
              )}
              {providers.includes("github") && (
                <a className="btn oauth" href={api.oauthStartURL("github")} data-testid="oauth-github">
                  <GithubGlyph /> Continue with GitHub
                </a>
              )}
            </div>
            <div className="oauth-divider"><span>or {isSignup ? "sign up" : "sign in"} with email</span></div>
          </>
        )}

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
