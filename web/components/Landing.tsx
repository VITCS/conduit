"use client";
import { useEffect, useRef, useState } from "react";
import {
  GitBranch, PlayCircle, ShieldCheck, Webhook, Clock, KeyRound,
  Boxes, Activity, UserCheck, Github, ArrowRight,
} from "lucide-react";

function Logo({ size = 28 }: { size?: number }) {
  return (
    <svg width={size} height={size} viewBox="0 0 32 32" aria-hidden>
      <defs>
        <linearGradient id="nlg" x1="0" y1="0" x2="1" y2="1">
          <stop offset="0" stopColor="var(--accent)" />
          <stop offset="1" stopColor="var(--accent-deep)" />
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

// useReveal adds `.in` to elements with [data-reveal] as they scroll into view.
function useReveal() {
  useEffect(() => {
    const els = Array.from(document.querySelectorAll<HTMLElement>("[data-reveal]"));
    if (!("IntersectionObserver" in window)) {
      els.forEach((el) => el.classList.add("in"));
      return;
    }
    const io = new IntersectionObserver(
      (entries) => {
        entries.forEach((e) => {
          if (e.isIntersecting) {
            e.target.classList.add("in");
            io.unobserve(e.target);
          }
        });
      },
      { threshold: 0.18 },
    );
    els.forEach((el) => io.observe(el));
    return () => io.disconnect();
  }, []);
}

// HeroGraph is a looping mini-pipeline: a run token flows trigger → gate → build →
// approve → ship, lighting each node as it passes. It mirrors what the product
// actually does. Falls back to a static, completed graph when motion is reduced.
function HeroGraph({ animate }: { animate: boolean }) {
  // node centers along a single horizontal spine
  const nodes = [
    { x: 70, label: "Trigger", kind: "trigger" },
    { x: 196, label: "Gate", kind: "step" },
    { x: 322, label: "Build", kind: "step" },
    { x: 448, label: "Approve", kind: "gate" },
    { x: 574, label: "Ship", kind: "status" },
  ];
  const cy = 70;
  const spine = `M ${nodes[0].x} ${cy} L ${nodes[nodes.length - 1].x} ${cy}`;
  return (
    <svg className="hg" viewBox="0 0 644 150" role="img" aria-label="An automated workflow executing: trigger, gate, build, approval, ship.">
      <defs>
        <linearGradient id="hg-edge" x1="0" y1="0" x2="1" y2="0">
          <stop offset="0" stopColor="var(--accent)" stopOpacity="0.15" />
          <stop offset="1" stopColor="var(--accent)" stopOpacity="0.6" />
        </linearGradient>
        <filter id="hg-glow" x="-50%" y="-50%" width="200%" height="200%">
          <feGaussianBlur stdDeviation="3.2" result="b" />
          <feMerge><feMergeNode in="b" /><feMergeNode in="SourceGraphic" /></feMerge>
        </filter>
      </defs>

      {/* base spine + flowing dash */}
      <path d={spine} stroke="var(--border-strong)" strokeWidth="2" fill="none" />
      {animate && (
        <path className="hg-flow" d={spine} stroke="url(#hg-edge)" strokeWidth="2.4" fill="none"
          strokeDasharray="7 9" strokeLinecap="round" />
      )}

      {/* nodes */}
      {nodes.map((n, i) => (
        <g key={n.label} className="hg-node" style={{ ["--d" as string]: `${i * 0.9}s` }}>
          <rect x={n.x - 34} y={cy - 20} width="68" height="40" rx="8"
            fill="var(--surface)" stroke="var(--border)" strokeWidth="1" className="hg-rect" />
          <circle cx={n.x - 20} cy={cy} r="4.5" className="hg-dot" fill="var(--idle)" />
          <text x={n.x - 8} y={cy + 4} className="hg-label">{n.label}</text>
        </g>
      ))}

      {/* traveling run token */}
      {animate && (
        <circle r="5.5" fill="var(--accent)" filter="url(#hg-glow)">
          <animateMotion dur="5.4s" repeatCount="indefinite" keyPoints="0;0;0.25;0.25;0.5;0.5;0.78;0.78;1;1"
            keyTimes="0;0.06;0.24;0.30;0.48;0.54;0.72;0.86;0.95;1" calcMode="linear" path={spine} />
        </circle>
      )}
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

// Real component types that ship today — used for the scrolling marquee.
const COMPONENTS = [
  "webhook", "schedule", "http", "graphql", "slack", "github_pr", "github_comment",
  "agent", "ssh", "if", "filter", "merge", "timegate", "wait", "approval", "memory",
];

export function Landing({ onSignIn, onGetStarted }: { onSignIn: () => void; onGetStarted: () => void }) {
  const [animate, setAnimate] = useState(true);
  useReveal();
  useEffect(() => {
    const m = window.matchMedia("(prefers-reduced-motion: reduce)");
    setAnimate(!m.matches);
  }, []);

  return (
    <div className="landing">
      {/* animated backdrop: grid + drifting green glow */}
      <div className="lx-bg" aria-hidden>
        <div className="lx-grid" />
        <div className="lx-glow" />
      </div>

      <header className="lx-nav">
        <div className="lx-brand">
          <Logo />
          <span className="wordmark">Conduit Agent</span>
          <span className="beta">BETA</span>
        </div>
        <div className="lx-nav-actions">
          <button className="btn" onClick={onSignIn} data-testid="nav-signin">Sign in</button>
          <button className="btn primary" onClick={onGetStarted} data-testid="nav-getstarted">Create account</button>
        </div>
      </header>

      <section className="lx-hero">
        <span className="lx-eyebrow" data-reveal>Event-driven automation control plane</span>
        <h1 data-reveal>
          Build, run, and watch automated <span className="lx-grad">workflows</span>
          <br />— with a human in the loop.
        </h1>
        <p className="lx-lead" data-reveal>
          Conduit Agent turns a visual graph of components into a live pipeline. Trigger it from a
          webhook or a schedule, watch every step execute in real time, pause for human approval
          before anything risky, and open a real pull request, call an API, post to Slack, or run
          an AI agent — all from one self-hosted binary.
        </p>
        <div className="lx-cta" data-reveal>
          <button className="btn primary lg" onClick={onGetStarted} data-testid="hero-getstarted">
            Create an account <ArrowRight size={17} />
          </button>
          <button className="btn lg" onClick={onSignIn} data-testid="hero-signin">Sign in</button>
        </div>
        <p className="lx-note" data-reveal>Sign in with email. New here? Create an account and get your own workspace.</p>

        {/* signature animation: a live workflow executing */}
        <div className="lx-stage" data-reveal>
          <div className="lx-stage-bar">
            <span className="lx-dot r" /><span className="lx-dot y" /><span className="lx-dot g" />
            <span className="lx-stage-title">preview-envs · run</span>
            <span className="lx-live"><span className="lx-live-dot" /> live</span>
          </div>
          <HeroGraph animate={animate} />
        </div>

        {/* scrolling marquee of real component types */}
        <div className="lx-marquee" data-reveal aria-hidden>
          <div className={`lx-track${animate ? "" : " paused"}`}>
            {[...COMPONENTS, ...COMPONENTS].map((c, i) => (
              <span className="lx-tag" key={c + i}>{c}</span>
            ))}
          </div>
        </div>
      </section>

      <section className="lx-section">
        <h2 data-reveal>What you can do</h2>
        <div className="lx-grid-cards">
          {FEATURES.map((f, i) => (
            <div className="lx-card" key={f.title} data-reveal style={{ ["--i" as string]: String(i) }}>
              <span className="lx-ico"><f.icon size={18} /></span>
              <h3>{f.title}</h3>
              <p>{f.body}</p>
            </div>
          ))}
        </div>
      </section>

      <section className="lx-section">
        <h2 data-reveal>How it works</h2>
        <ol className="lx-steps">
          <li data-reveal style={{ ["--i" as string]: "0" }}><span className="lx-step-n">1</span><div><b>Build</b><p>Drop triggers and steps on the canvas and connect them. Save validates the graph (no cycles or dangling edges).</p></div></li>
          <li data-reveal style={{ ["--i" as string]: "1" }}><span className="lx-step-n">2</span><div><b>Run</b><p>Press Run, fire a webhook, or let a schedule start it. A worker executes the graph and streams progress back live.</p></div></li>
          <li data-reveal style={{ ["--i" as string]: "2" }}><span className="lx-step-n">3</span><div><b>Approve &amp; act</b><p>When a run reaches an approval gate it waits for you. Approve it and the real actions — PR, HTTP, Slack, agent — run.</p></div></li>
        </ol>
      </section>

      <section className="lx-section lx-honest">
        <h2 data-reveal>Straight about the demo</h2>
        <p data-reveal>
          The seeded example workflows <b>simulate</b> their side effects so they run offline and
          deterministically. Components that perform real side effects — <b>GitHub PR / comment</b>,
          <b> live HTTP</b>, <b>Slack</b>, and the <b>AI Agent</b> — act for real once you configure a
          secret (and set <code>live: true</code> for HTTP/agent). A few components (GraphQL, SSH) are
          simulated today. You can sign in with email, or with Google / GitHub when the server
          has those providers configured.
        </p>
      </section>

      <footer className="lx-foot">
        <div className="lx-brand"><Logo size={22} /><span className="wordmark">Conduit Agent</span></div>
        <div className="lx-foot-actions">
          <button className="btn" onClick={onSignIn}>Sign in</button>
          <button className="btn primary" onClick={onGetStarted}>Create account</button>
        </div>
      </footer>
    </div>
  );
}
