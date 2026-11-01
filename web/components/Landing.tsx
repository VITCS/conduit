"use client";
import {
  GitBranch, PlayCircle, ShieldCheck, Webhook, Clock, KeyRound,
  Boxes, Activity, UserCheck, Github, Bot, Server,
} from "lucide-react";

function Logo({ size = 28 }: { size?: number }) {
  return (
    <svg width={size} height={size} viewBox="0 0 32 32" aria-hidden>
      <defs>
        <linearGradient id="nlg" x1="0" y1="0" x2="1" y2="1">
          <stop offset="0" stopColor="var(--accent)" />
          <stop offset="1" stopColor="var(--wait)" />
        </linearGradient>
      </defs>
      <rect x="2" y="2" width="28" height="28" rx="8" fill="var(--accent-soft)" />
      <circle cx="9" cy="16" r="3" fill="url(#nlg)" />
      <circle cx="23" cy="9" r="3" fill="url(#nlg)" />
      <circle cx="23" cy="23" r="3" fill="url(#nlg)" />
      <path d="M11.6 14.6 20.4 10M11.6 17.4 20.4 22" stroke="url(#nlg)" strokeWidth="2.2" strokeLinecap="round" />
    </svg>
  );
}

const FEATURES = [
  { icon: GitBranch, title: "Visual workflow builder", body: "Drag components onto a canvas and wire them into a directed graph. Branches route on each step's outcome — success, failed, or an approval decision." },
  { icon: PlayCircle, title: "Live execution", body: "Runs stream over Server-Sent Events: every step's status, logs, and output update as it happens, with independent branches running in parallel." },
  { icon: UserCheck, title: "Human approval gates", body: "Pause a run for a human decision. A persistent banner (and a desktop notification) prompts to approve or reject before the next step runs." },
  { icon: Webhook, title: "Webhook & cron triggers", body: "Start runs from an inbound webhook (the posted JSON becomes the run's input) or on a cron schedule. Or just press Run." },
  { icon: Github, title: "Real integrations", body: "Open a real GitHub pull request or comment, make live HTTP calls, and post to Slack. An AI Agent step can call Anthropic, OpenAI, or a custom model." },
  { icon: KeyRound, title: "Encrypted secrets", body: "Store credentials encrypted at rest (AES-256-GCM), scoped per project. Reference them in any field as ${secret:NAME} — values are never returned by the API." },
  { icon: Boxes, title: "Projects & roles", body: "Multi-tenant projects with per-project membership and roles (owner, admin, member, viewer). Workflows, runs, and secrets are isolated per project." },
  { icon: Activity, title: "Observability", body: "Prometheus metrics at /metrics, structured JSON logs with request IDs, and an audit log of every change — built in, no extra services." },
  { icon: ShieldCheck, title: "Safe by default", body: "Per-IP rate limiting, an SSRF guard on outbound calls, security headers, and request-size limits. Retries with backoff and run cancellation are first-class." },
];

export function Landing({ onSignIn, onGetStarted }: { onSignIn: () => void; onGetStarted: () => void }) {
  return (
    <div className="landing">
      <header className="lx-nav">
        <div className="lx-brand">
          <Logo />
          <span className="wordmark">Conduit</span>
          <span className="beta">BETA</span>
        </div>
        <div className="lx-nav-actions">
          <button className="btn" onClick={onSignIn} data-testid="nav-signin">Sign in</button>
          <button className="btn primary" onClick={onGetStarted} data-testid="nav-getstarted">Create account</button>
        </div>
      </header>

      <section className="lx-hero">
        <span className="lx-eyebrow">Event-driven automation control plane</span>
        <h1>Build, run, and watch automated workflows — with a human in the loop.</h1>
        <p className="lx-lead">
          Conduit turns a visual graph of components into a live pipeline. Trigger it from a
          webhook or a schedule, watch every step execute in real time, pause for human approval
          before anything risky, and open a real pull request, call an API, post to Slack, or run
          an AI agent — all from one self-hosted binary.
        </p>
        <div className="lx-cta">
          <button className="btn primary lg" onClick={onGetStarted} data-testid="hero-getstarted">Create an account</button>
          <button className="btn lg" onClick={onSignIn} data-testid="hero-signin">Sign in</button>
        </div>
        <p className="lx-note">Sign in with email. New here? Create an account and get your own workspace.</p>
      </section>

      <section className="lx-section">
        <h2>What you can do</h2>
        <div className="lx-grid">
          {FEATURES.map((f) => (
            <div className="lx-card" key={f.title}>
              <span className="lx-ico"><f.icon size={18} /></span>
              <h3>{f.title}</h3>
              <p>{f.body}</p>
            </div>
          ))}
        </div>
      </section>

      <section className="lx-section">
        <h2>How it works</h2>
        <ol className="lx-steps">
          <li><span className="lx-step-n">1</span><div><b>Build</b><p>Drop triggers and steps on the canvas and connect them. Save validates the graph (no cycles or dangling edges).</p></div></li>
          <li><span className="lx-step-n">2</span><div><b>Run</b><p>Press Run, fire a webhook, or let a schedule start it. A worker executes the graph and streams progress back live.</p></div></li>
          <li><span className="lx-step-n">3</span><div><b>Approve &amp; act</b><p>When a run reaches an approval gate it waits for you. Approve it and the real actions — PR, HTTP, Slack, agent — run.</p></div></li>
        </ol>
      </section>

      <section className="lx-section lx-honest">
        <h2>Straight about the demo</h2>
        <p>
          The seeded example workflows <b>simulate</b> their side effects so they run offline and
          deterministically. Components that perform real side effects — <b>GitHub PR / comment</b>,
          <b> live HTTP</b>, <b>Slack</b>, and the <b>AI Agent</b> — act for real once you configure a
          secret (and set <code>live: true</code> for HTTP/agent). A few components (GraphQL, SSH) are
          simulated today. Authentication is email + password; there is no third-party SSO yet.
        </p>
      </section>

      <footer className="lx-foot">
        <div className="lx-brand"><Logo size={22} /><span className="wordmark">Conduit</span></div>
        <div className="lx-foot-actions">
          <button className="btn" onClick={onSignIn}>Sign in</button>
          <button className="btn primary" onClick={onGetStarted}>Create account</button>
        </div>
      </footer>
    </div>
  );
}
