"use client";
import { useEffect, useRef, useState } from "react";
import {
  GitBranch, PlayCircle, ShieldCheck, Webhook, KeyRound, Boxes, Activity,
  UserCheck, Github, ArrowRight, Zap, Slack, Bot, Terminal, Check,
} from "lucide-react";

/* ------------------------------------------------------------------ helpers */

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

function prefersReduced() {
  return typeof window !== "undefined" && window.matchMedia("(prefers-reduced-motion: reduce)").matches;
}

function useReveal() {
  useEffect(() => {
    const els = Array.from(document.querySelectorAll<HTMLElement>("[data-reveal]"));
    if (!("IntersectionObserver" in window) || prefersReduced()) {
      els.forEach((el) => el.classList.add("in"));
      return;
    }
    const io = new IntersectionObserver(
      (es) => es.forEach((e) => { if (e.isIntersecting) { e.target.classList.add("in"); io.unobserve(e.target); } }),
      { threshold: 0.16 },
    );
    els.forEach((el) => io.observe(el));
    return () => io.disconnect();
  }, []);
}

function useCountUp(ref: React.RefObject<HTMLElement | null>) {
  useEffect(() => {
    const root = ref.current;
    if (!root) return;
    const run = () => {
      root.querySelectorAll<HTMLElement>("[data-count]").forEach((el) => {
        const target = parseInt(el.dataset.count || "0", 10);
        if (prefersReduced()) { el.textContent = String(target); return; }
        const dur = 1100; const start = performance.now();
        const tick = (t: number) => {
          const p = Math.min(1, (t - start) / dur);
          el.textContent = String(Math.round(target * (1 - Math.pow(1 - p, 3))));
          if (p < 1) requestAnimationFrame(tick);
        };
        requestAnimationFrame(tick);
      });
    };
    if (!("IntersectionObserver" in window)) { run(); return; }
    const io = new IntersectionObserver((es) => es.forEach((e) => { if (e.isIntersecting) { run(); io.disconnect(); } }), { threshold: 0.4 });
    io.observe(root);
    return () => io.disconnect();
  }, [ref]);
}

function useTilt() {
  useEffect(() => {
    if (prefersReduced()) return;
    const els = Array.from(document.querySelectorAll<HTMLElement>("[data-tilt]"));
    const handlers: Array<[HTMLElement, (e: PointerEvent) => void, () => void]> = [];
    els.forEach((el) => {
      const move = (e: PointerEvent) => {
        const r = el.getBoundingClientRect();
        const px = (e.clientX - r.left) / r.width - 0.5;
        const py = (e.clientY - r.top) / r.height - 0.5;
        el.style.setProperty("--rx", `${(-py * 5).toFixed(2)}deg`);
        el.style.setProperty("--ry", `${(px * 6).toFixed(2)}deg`);
      };
      const reset = () => { el.style.setProperty("--rx", "0deg"); el.style.setProperty("--ry", "0deg"); };
      el.addEventListener("pointermove", move); el.addEventListener("pointerleave", reset);
      handlers.push([el, move, reset]);
    });
    return () => handlers.forEach(([el, m, r]) => { el.removeEventListener("pointermove", m); el.removeEventListener("pointerleave", r); });
  }, []);
}

/* ----------------------------------------------------------- control plane */

function ControlPlane({ animate }: { animate: boolean }) {
  const nodes = [
    { id: "trig", x: 72, y: 150, label: "On PR", kind: "trigger", d: 0 },
    { id: "build", x: 250, y: 80, label: "Build", kind: "step", d: 0.6 },
    { id: "test", x: 250, y: 220, label: "Test", kind: "step", d: 0.9 },
    { id: "merge", x: 428, y: 150, label: "Merge", kind: "step", d: 1.6 },
    { id: "appr", x: 568, y: 150, label: "Approve", kind: "gate", d: 2.4 },
    { id: "ship", x: 700, y: 150, label: "Ship", kind: "status", d: 3.2 },
  ];
  const E = [
    "M72,150 C160,150 175,80 250,80",
    "M72,150 C160,150 175,220 250,220",
    "M250,80 C340,80 355,150 428,150",
    "M250,220 C340,220 355,150 428,150",
    "M428,150 L568,150",
    "M568,150 L700,150",
  ];
  const tokenTop = "M72,150 C160,150 175,80 250,80 C340,80 355,150 428,150 L568,150 L700,150";
  const tokenBot = "M72,150 C160,150 175,220 250,220 C340,220 355,150 428,150";

  return (
    <svg className="cp" viewBox="0 0 760 300" role="img" aria-label="A control plane executing a workflow: an opened PR triggers parallel build and test, they merge, a human approves, then it ships.">
      <defs>
        <linearGradient id="cp-edge" x1="0" y1="0" x2="1" y2="0">
          <stop offset="0" stopColor="var(--accent)" stopOpacity="0.12" />
          <stop offset="1" stopColor="var(--accent)" stopOpacity="0.55" />
        </linearGradient>
        <filter id="cp-glow" x="-60%" y="-60%" width="220%" height="220%">
          <feGaussianBlur stdDeviation="3.4" result="b" /><feMerge><feMergeNode in="b" /><feMergeNode in="SourceGraphic" /></feMerge>
        </filter>
      </defs>

      {E.map((d, i) => (
        <g key={i}>
          <path d={d} fill="none" stroke="var(--border-strong)" strokeWidth="1.6" />
          {animate && <path d={d} fill="none" stroke="url(#cp-edge)" strokeWidth="2.2" strokeDasharray="6 10" strokeLinecap="round" className="cp-flow" />}
        </g>
      ))}

      {nodes.map((n) => (
        <g key={n.id} className={`cp-node ${n.kind === "gate" ? "cp-gate" : ""}`} style={{ ["--d" as string]: `${n.d}s` }}>
          {n.kind === "gate" && animate && <circle cx={n.x} cy={n.y} r="30" className="cp-ring" fill="none" />}
          <rect x={n.x - 46} y={n.y - 22} width="92" height="44" rx="9" className="cp-rect" fill="var(--surface)" stroke="var(--border)" />
          <circle cx={n.x - 30} cy={n.y} r="4.5" className="cp-dot" fill="var(--idle)" />
          <text x={n.x - 18} y={n.y + 4} className="cp-label">{n.label}</text>
        </g>
      ))}

      {animate && (
        <>
          <circle r="5" fill="var(--accent)" filter="url(#cp-glow)">
            <animateMotion dur="6.5s" repeatCount="indefinite" calcMode="linear"
              keyPoints="0;0.34;0.5;0.5;0.66;0.82;1" keyTimes="0;0.26;0.42;0.60;0.74;0.88;1" path={tokenTop} />
          </circle>
          <circle r="4" fill="var(--accent-deep)" filter="url(#cp-glow)">
            <animateMotion dur="3.1s" repeatCount="indefinite" path={tokenBot} />
          </circle>
        </>
      )}
    </svg>
  );
}

/* --------------------------------------------------------------- live log */

const LOG = [
  { t: "▶", c: "run", s: "run started · on pull_request #482" },
  { t: "✓", c: "ok", s: "build · passed (1m24s)" },
  { t: "✓", c: "ok", s: "test · 128 passed" },
  { t: "✓", c: "ok", s: "merge · fast-forward clean" },
  { t: "⏸", c: "wait", s: "awaiting approval · maintainers" },
  { t: "✓", c: "ok", s: "approved · shipping" },
  { t: "✓", c: "ok", s: "merged · #482 squashed" },
];
function LiveLog({ animate }: { animate: boolean }) {
  const [n, setN] = useState(animate ? 1 : LOG.length);
  useEffect(() => {
    if (!animate) return;
    const id = setInterval(() => setN((v) => (v >= LOG.length ? 1 : v + 1)), 1100);
    return () => clearInterval(id);
  }, [animate]);
  return (
    <div className="cp-log" aria-hidden>
      {LOG.slice(0, n).map((l, i) => (
        <div className="cp-logline" key={i}>
          <span className={`cp-lt ${l.c}`}>{l.t}</span>
          <span>{l.s}</span>
          {animate && i === n - 1 && <span className="cp-cursor" />}
        </div>
      ))}
    </div>
  );
}

/* ------------------------------------------------------------------- data */

const FEATURES = [
  { icon: GitBranch, title: "Visual workflow builder", body: "Drag components onto a canvas and wire them into a directed graph. Branches route on each step's outcome — success, failed, or an approval decision.", big: true },
  { icon: UserCheck, title: "Human approval gates", body: "Pause a run for a human decision. A persistent banner and a desktop notification prompt to approve or reject before the next step runs.", big: true },
  { icon: PlayCircle, title: "Live execution", body: "Runs stream over Server-Sent Events — status, logs and output update as they happen, with branches running in parallel." },
  { icon: Webhook, title: "Webhook & cron triggers", body: "Start runs from an inbound webhook (its JSON becomes the run input) or on a schedule. Or just press Run." },
  { icon: KeyRound, title: "Encrypted secrets", body: "Credentials encrypted at rest (AES-256-GCM), scoped per project, referenced as ${secret:NAME} — never returned by the API." },
  { icon: Boxes, title: "Projects & roles", body: "Multi-tenant projects with per-project membership and roles. Workflows, runs and secrets are isolated per project." },
  { icon: Activity, title: "Observability", body: "Prometheus /metrics, structured JSON logs with request IDs, and an audit log of every change — built in." },
  { icon: ShieldCheck, title: "Safe by default", body: "Per-IP rate limiting, an SSRF guard on outbound calls, security headers, retries with backoff, and run cancellation." },
];

const ACTIONS = [
  { icon: Github, title: "Open & merge PRs", body: "Open a pull request, comment, or merge — real GitHub API calls after a human approves." },
  { icon: Bot, title: "Review with an agent", body: "An agent reads a PR's real diff and posts a review. Anthropic, OpenAI, or a custom model." },
  { icon: Slack, title: "Notify Slack", body: "Post run results to a Slack webhook, with the message templated from run state." },
  { icon: Terminal, title: "Call any HTTP API", body: "Live HTTP / GraphQL steps with auth headers and bodies; the outcome routes on the status code." },
];

const COMPONENTS = ["webhook", "schedule", "http", "graphql", "slack", "github_pr", "github_comment", "github_review", "github_merge", "agent", "ssh", "if", "filter", "merge", "timegate", "approval", "memory"];

/* ------------------------------------------------------------------- page */

export function Landing({ onSignIn, onGetStarted }: { onSignIn: () => void; onGetStarted: () => void }) {
  const [animate, setAnimate] = useState(true);
  const statsRef = useRef<HTMLDivElement>(null);
  useReveal();
  useTilt();
  useCountUp(statsRef);
  useEffect(() => { setAnimate(!prefersReduced()); }, []);

  return (
    <div className="landing">
      <div className="lx-bg" aria-hidden>
        <div className="lx-aurora" /><div className="lx-aurora two" />
        <div className="lx-grid" /><div className="lx-vignette" />
      </div>

      <header className="lx-nav">
        <div className="lx-brand"><Logo /><span className="wordmark">Conduit Agent</span><span className="beta">BETA</span></div>
        <div className="lx-nav-actions">
          <button className="btn" onClick={onSignIn} data-testid="nav-signin">Sign in</button>
          <button className="btn primary" onClick={onGetStarted} data-testid="nav-getstarted">Create account</button>
        </div>
      </header>

      <section className="lx-hero">
        <div className="lx-hero-copy">
          <span className="lx-eyebrow" data-reveal><Zap size={13} /> Event-driven automation control plane</span>
          <h1 data-reveal>
            Automate the work that<br />needs a <span className="lx-grad">human in the loop</span>.
          </h1>
          <p className="lx-lead" data-reveal>
            Conduit Agent turns a visual graph of components into a live workflow. Trigger it from a
            webhook or a schedule, watch every step run in real time, and pause for human approval
            before anything risky — then open a real pull request, review it with an agent, post to
            Slack, or merge. One self-hosted binary.
          </p>
          <div className="lx-cta" data-reveal>
            <button className="btn primary lg" onClick={onGetStarted} data-testid="hero-getstarted">Create an account <ArrowRight size={17} /></button>
            <button className="btn lg" onClick={onSignIn} data-testid="hero-signin">Sign in</button>
          </div>
          <p className="lx-trust" data-reveal>Self-hosted · single binary · email or Google / GitHub sign-in</p>
        </div>

        <div className="lx-hero-stage" data-reveal data-tilt>
          <div className="lx-stage-bar">
            <span className="lx-dot r" /><span className="lx-dot y" /><span className="lx-dot g" />
            <span className="lx-stage-title">pr-review · live run</span>
            <span className="lx-live"><span className="lx-live-dot" /> live</span>
          </div>
          <ControlPlane animate={animate} />
          <LiveLog animate={animate} />
        </div>
      </section>

      <div className="lx-marquee" data-reveal aria-hidden>
        <div className={`lx-track${animate ? "" : " paused"}`}>
          {[...COMPONENTS, ...COMPONENTS].map((c, i) => <span className="lx-tag" key={c + i}>{c}</span>)}
        </div>
      </div>

      <section className="lx-section lx-split">
        <div data-reveal>
          <span className="lx-kicker">The problem</span>
          <h2>Automation you can&apos;t see, gated by scripts you can&apos;t trust.</h2>
          <p className="lx-para">Ad-hoc shell scripts and cron jobs deploy, merge, and message with no live view, approvals faked with <code>sleep</code>, no retries, and secrets in plain env files. When something goes wrong, there&apos;s no record of what ran or who said yes.</p>
        </div>
        <div data-reveal style={{ ["--i" as string]: "1" }}>
          <span className="lx-kicker ok">With Conduit Agent</span>
          <h2>A visible graph, a human gate, and real actions.</h2>
          <p className="lx-para">Every workflow is a graph you can read and change. Runs stream live, pause for a human at approval gates, branch on success / failure, retry with backoff, and act for real — with encrypted secrets, metrics, and an audit trail out of the box.</p>
        </div>
      </section>

      <section className="lx-section">
        <h2 className="lx-h2c" data-reveal>From event to action in three steps</h2>
        <ol className="lx-steps">
          <li data-reveal style={{ ["--i" as string]: "0" }}><span className="lx-step-n">1</span><div><b>Build</b><p>Drop triggers and steps on the canvas and connect them. Save validates the graph — no cycles or dangling edges.</p></div></li>
          <li data-reveal style={{ ["--i" as string]: "1" }}><span className="lx-step-n">2</span><div><b>Run</b><p>Press Run, fire a webhook, or let a schedule start it. A worker executes the graph and streams progress back live.</p></div></li>
          <li data-reveal style={{ ["--i" as string]: "2" }}><span className="lx-step-n">3</span><div><b>Approve &amp; act</b><p>At an approval gate the run waits for you. Approve it and the real actions — PR, review, Slack, merge — run.</p></div></li>
        </ol>
      </section>

      <section className="lx-section">
        <h2 className="lx-h2c" data-reveal>Everything you need, nothing you don&apos;t</h2>
        <div className="lx-bento">
          {FEATURES.map((f, i) => (
            <div className={`lx-card${f.big ? " big" : ""}`} key={f.title} data-reveal data-tilt style={{ ["--i" as string]: String(i % 4) }}>
              <span className="lx-ico"><f.icon size={18} /></span>
              <h3>{f.title}</h3>
              <p>{f.body}</p>
            </div>
          ))}
        </div>
      </section>

      <section className="lx-section">
        <h2 className="lx-h2c" data-reveal>Real actions, not demos</h2>
        <p className="lx-sub" data-reveal>Configure a secret and these components act against the real thing — no mocks.</p>
        <div className="lx-actions">
          {ACTIONS.map((a, i) => (
            <div className="lx-act" key={a.title} data-reveal style={{ ["--i" as string]: String(i) }}>
              <span className="lx-ico sm"><a.icon size={16} /></span>
              <div><b>{a.title}</b><p>{a.body}</p></div>
            </div>
          ))}
        </div>
      </section>

      <section className="lx-section lx-spotlight" data-reveal>
        <div className="lx-approve-demo">
          <span className="lx-ab-dot" />
          <div className="lx-ab-text"><b>Approval required</b> — “Merge PR” is waiting for your decision.</div>
          <span className="lx-ab-btn ok"><Check size={14} /> Approve</span>
          <span className="lx-ab-btn">Reject</span>
        </div>
        <h2>Nothing risky happens without a human saying yes.</h2>
        <p className="lx-para center">Approval is a first-class component. A run stops at the gate, everyone sees it, and the decision — who approved, and when — is recorded in the audit log.</p>
      </section>

      <section className="lx-stats" ref={statsRef} data-reveal>
        <div className="lx-stat"><div className="lx-num"><span data-count="17">0</span></div><div className="lx-stat-l">component types</div></div>
        <div className="lx-stat"><div className="lx-num"><span data-count="4">0</span></div><div className="lx-stat-l">trigger kinds</div></div>
        <div className="lx-stat"><div className="lx-num"><span data-count="1">0</span></div><div className="lx-stat-l">binary to run it all</div></div>
        <div className="lx-stat"><div className="lx-num"><span data-count="0">0</span></div><div className="lx-stat-l">extra services required</div></div>
      </section>

      <section className="lx-section lx-honest">
        <h2 className="lx-h2c" data-reveal>Straight about the demo</h2>
        <p data-reveal>
          The seeded example workflows <b>simulate</b> their side effects so they run offline and
          deterministically. Components that perform real side effects — <b>GitHub PR / review / merge / comment</b>,
          <b> live HTTP</b>, <b>Slack</b>, and the <b>AI Agent</b> — act for real once you configure a secret
          (and set <code>live: true</code> for HTTP / agent). A few components (GraphQL, SSH) are simulated
          today. You can sign in with email, or with Google / GitHub when the server has those providers configured.
        </p>
      </section>

      <section className="lx-closing" data-reveal>
        <h2>Build your first workflow in a minute.</h2>
        <p>Create an account, get your own workspace, and press Run.</p>
        <div className="lx-cta">
          <button className="btn primary lg" onClick={onGetStarted}>Create an account <ArrowRight size={17} /></button>
          <button className="btn lg" onClick={onSignIn}>Sign in</button>
        </div>
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
