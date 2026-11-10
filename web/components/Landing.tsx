"use client";
import { useEffect, useRef, useState } from "react";
import { ArrowRight, Github } from "lucide-react";

/* ------------------------------------------------------------------ helpers */

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
      { threshold: 0.18 },
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
        const dur = 1200, start = performance.now();
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

/* --------------------------------------------------------------------- mark */

// A conduit mark: a signal entering one channel and leaving reviewed. Built from
// strokes so it reads at any size and carries the page's two accents.
function Mark({ size = 26 }: { size?: number }) {
  return (
    <svg width={size} height={size} viewBox="0 0 32 32" fill="none" aria-hidden>
      <rect x="1.2" y="1.2" width="29.6" height="29.6" rx="9" stroke="var(--ink)" strokeWidth="1.6" />
      <path d="M7 16h6.5" stroke="var(--flame)" strokeWidth="2.4" strokeLinecap="round" />
      <path d="M13.5 16c3.6 0 3.6-7 7.2-7M13.5 16c3.6 0 3.6 7 7.2 7" stroke="var(--cobalt)" strokeWidth="2.4" strokeLinecap="round" />
      <circle cx="22.4" cy="9" r="2.3" fill="var(--cobalt)" />
      <circle cx="22.4" cy="23" r="2.3" fill="var(--cobalt)" />
    </svg>
  );
}

/* ------------------------------------------------------ the production line */

const STAGES = [
  { k: "Intake", read: "ticket #482 · received" },
  { k: "Triage", read: "confidence 0.86 · ready" },
  { k: "Plan", read: "4 files · change plan" },
  { k: "Build", read: "branch pushed · conduit/482" },
  { k: "Verify", read: "CI green · checks passed" },
  { k: "Review", read: "2 gates · approved" },
  { k: "Ship", read: "merged · PR #129" },
];

// A luminous token travels the conduit, lighting each gate as it passes and
// reading out what happened there — a single kinetic focal point for the page.
function ProductionLine() {
  const [i, setI] = useState(0);
  const [animate, setAnimate] = useState(true);
  useEffect(() => { setAnimate(!prefersReduced()); }, []);
  useEffect(() => {
    if (!animate) { setI(STAGES.length - 1); return; }
    const id = setInterval(() => setI((c) => (c + 1) % STAGES.length), 1500);
    return () => clearInterval(id);
  }, [animate]);

  const n = STAGES.length;
  const pct = (idx: number) => (idx / (n - 1)) * 100;

  return (
    <div className="cl-line" data-reveal>
      <div className="cl-line-top">
        <span className="cl-chip in">
          <span className="cl-chip-dot" /> TICKET
        </span>
        <span className="cl-readout" key={i}>{STAGES[i].read}</span>
        <span className={`cl-chip out${i === n - 1 ? " on" : ""}`}>
          PR <span className="cl-check">✓</span>
        </span>
      </div>

      <div className="cl-track">
        <div className="cl-rail" />
        <div className="cl-fill" style={{ width: `${pct(i)}%` }} />
        <div className="cl-token" style={{ left: `${pct(i)}%` }}>
          <span className="cl-token-core" />
        </div>
        {STAGES.map((s, idx) => (
          <div
            key={s.k}
            className={`cl-gate${idx <= i ? " lit" : ""}${idx === i ? " active" : ""}`}
            style={{ left: `${pct(idx)}%` }}
          >
            <span className="cl-gate-tick" />
            <span className="cl-gate-num">{String(idx + 1).padStart(2, "0")}</span>
            <span className="cl-gate-k">{s.k}</span>
          </div>
        ))}
      </div>
    </div>
  );
}

/* ------------------------------------------------------------------- content */

const INTEGRATIONS = ["Jira", "Notion", "GitHub", "CI checks", "Any model", "Slack"];

const PROBLEMS = [
  { n: "A", t: "The backlog hides the easy wins", b: "Point an agent at your tickets and it grabs work it can't scope. The safe tasks and the risky ones look identical." },
  { n: "B", t: "Unsupervised, it ships slop", b: "One long prompt is not a process. Without rules and verification, agent code is messy, insecure, and nobody's accountable for it." },
  { n: "C", t: "So engineers babysit every PR", b: "No trust means a human copies logs between tools and hand-holds each change from first line to merge." },
];

const STEPS = [
  { t: "Connect", p: "Point Conduit at Jira, Notion or GitHub. Tickets land on a live board." },
  { t: "Triage", p: "Every ticket is scored for readiness — scope, context, a checkable outcome. Ambiguous work waits for a human." },
  { t: "Plan", p: "An agent drafts the exact file-level change plan. You approve the plan before a single line is written." },
  { t: "Build", p: "It writes the change file by file and pushes a real branch through GitHub's API — no local checkout." },
  { t: "Verify", p: "Conduit runs your repo's CI on the commit. On a failure it loops the agent back with the errors." },
  { t: "Review", p: "You approve the exact diff; Conduit opens the pull request and hands it to a review pipeline." },
];

const CONTROLS = [
  { t: "Two human gates, never skipped", b: "Approve the plan, then approve the exact file changes. Nothing reaches your repo without a person saying yes — twice." },
  { t: "Verified, not just finished", b: "A pull request is an output, not proof. Every run is gated on your real CI before it ever opens a PR." },
  { t: "Your models, your stack", b: "Anthropic, OpenAI or a self-hosted gateway. One binary, your keys, your CI, your cost ceiling. No vendor lock-in." },
];

/* ------------------------------------------------------------------- page */

export function Landing({ onSignIn, onGetStarted }: { onSignIn: () => void; onGetStarted: () => void }) {
  const statsRef = useRef<HTMLDivElement>(null);
  useReveal();
  useCountUp(statsRef);

  // The marketing page owns its own look, independent of the dark app theme
  // (which the Topbar restores after sign-in).
  useEffect(() => {
    const prev = document.documentElement.getAttribute("data-theme");
    document.documentElement.setAttribute("data-theme", "landing");
    return () => { if (prev) document.documentElement.setAttribute("data-theme", prev); };
  }, []);

  return (
    <div className="landing cl">
      <div className="cl-grain lx-bg" aria-hidden />

      <header className="cl-nav">
        <div className="cl-brand"><Mark /><span>Conduit</span></div>
        <nav className="cl-nav-r">
          <a href="https://github.com/VITCS/conduit" target="_blank" rel="noreferrer" className="cl-nav-link">
            <Github size={15} /> GitHub
          </a>
          <button className="cl-nav-link" onClick={onSignIn} data-testid="nav-signin">Log in</button>
          <button className="cl-btn sm" onClick={onGetStarted} data-testid="nav-getstarted">Sign up</button>
        </nav>
      </header>

      {/* ── hero ── */}
      <section className="cl-hero">
        <div className="cl-hero-l">
          <span className="cl-eyebrow" data-reveal>Open-source AI software factory</span>
          <h1 data-reveal>
            Ship routine tickets<br />
            as <span className="cl-mark">reviewed pull requests</span>.
          </h1>
          <p className="cl-lead" data-reveal>
            Conduit turns a ticket into a merged-ready pull request — triaged, planned, coded and
            CI-verified by agents, with the guardrails and human approvals your team actually needs.
            Self-hosted, on your models.
          </p>
          <div className="cl-cta" data-reveal>
            <button className="cl-btn lg" onClick={onGetStarted} data-testid="hero-getstarted">
              Start your factory <ArrowRight size={17} />
            </button>
            <button className="cl-btn lg ghost" onClick={onSignIn} data-testid="hero-signin">Log in</button>
          </div>
          <div className="cl-meta" data-reveal>
            <span><b>2</b> approval gates</span>
            <span className="cl-dot" />
            <span><b>1</b> binary</span>
            <span className="cl-dot" />
            <span><b>0</b> vendor lock-in</span>
          </div>
        </div>

        <ProductionLine />
      </section>

      {/* ── trust strip ── */}
      <section className="cl-strip" data-reveal>
        <span className="cl-strip-cap">Built on the tools you already run</span>
        <div className="cl-strip-row">
          {INTEGRATIONS.map((x) => <span className="cl-tool" key={x}>{x}</span>)}
        </div>
      </section>

      {/* ── problem ── */}
      <section className="cl-sec">
        <div className="cl-sec-head" data-reveal>
          <span className="cl-kicker">The problem</span>
          <h2>Agents can&apos;t be trusted on autopilot.</h2>
        </div>
        <div className="cl-probs">
          {PROBLEMS.map((p, idx) => (
            <article className="cl-prob" key={p.n} data-reveal style={{ ["--d" as string]: `${idx * 70}ms` }}>
              <span className="cl-prob-n">{p.n}</span>
              <h3>{p.t}</h3>
              <p>{p.b}</p>
            </article>
          ))}
        </div>
      </section>

      {/* ── how it works ── */}
      <section className="cl-sec">
        <div className="cl-sec-head" data-reveal>
          <span className="cl-kicker">The line</span>
          <h2>Six stations, one reviewed PR.</h2>
          <p className="cl-sec-sub">Every ticket walks the same gated path. You hold the two keys.</p>
        </div>
        <ol className="cl-steps">
          {STEPS.map((s, idx) => (
            <li key={s.t} data-reveal style={{ ["--d" as string]: `${idx * 50}ms` }}>
              <span className="cl-step-n">{String(idx + 1).padStart(2, "0")}</span>
              <div className="cl-step-b">
                <h4>{s.t}</h4>
                <p>{s.p}</p>
              </div>
            </li>
          ))}
        </ol>
      </section>

      {/* ── control band (the one dark moment) ── */}
      <section className="cl-band">
        <div className="cl-band-in">
          <div className="cl-band-head" data-reveal>
            <span className="cl-kicker on-dark">You stay in control</span>
            <h2>Automation you can actually sign off on.</h2>
          </div>
          <div className="cl-controls">
            {CONTROLS.map((c, idx) => (
              <div className="cl-control" key={c.t} data-reveal style={{ ["--d" as string]: `${idx * 70}ms` }}>
                <span className="cl-control-bar" />
                <h3>{c.t}</h3>
                <p>{c.b}</p>
              </div>
            ))}
          </div>
        </div>
      </section>

      {/* ── stats ── */}
      <section className="cl-stats" ref={statsRef} data-reveal>
        <div className="cl-stat"><div className="cl-num"><span data-count="6">0</span></div><span>gated stations per ticket</span></div>
        <div className="cl-stat"><div className="cl-num"><span data-count="2">0</span></div><span>human approval gates</span></div>
        <div className="cl-stat"><div className="cl-num"><span data-count="26">0</span></div><span>component types</span></div>
        <div className="cl-stat"><div className="cl-num"><span data-count="1">0</span></div><span>binary to run it all</span></div>
      </section>

      {/* ── closing ── */}
      <section className="cl-close" data-reveal>
        <h2>Own your software factory.</h2>
        <p>
          Apache-friendly, self-hosted, built around your models, your CI and your cost ceiling.
          Create an account and open your first PR in a minute.
        </p>
        <div className="cl-cta cl-cc">
          <button className="cl-btn lg" onClick={onGetStarted}>Start your factory <ArrowRight size={17} /></button>
          <a className="cl-btn lg ghost" href="https://github.com/VITCS/conduit" target="_blank" rel="noreferrer">
            <Github size={16} /> Read the source
          </a>
        </div>
        <p className="cl-fine">Self-hosted · single binary · email or Google / GitHub sign-in when the server has those providers configured.</p>
      </section>

      <footer className="cl-foot">
        <div className="cl-brand"><Mark size={22} /><span>Conduit</span></div>
        <div className="cl-foot-r">
          <a href="https://github.com/VITCS/conduit" target="_blank" rel="noreferrer">GitHub</a>
          <button className="cl-nav-link" onClick={onSignIn}>Log in</button>
          <button className="cl-btn sm" onClick={onGetStarted}>Sign up</button>
        </div>
      </footer>
    </div>
  );
}
