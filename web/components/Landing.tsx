"use client";
import { useEffect, useRef, useState } from "react";
import {
  ShieldCheck, UserCheck, Github, ArrowRight, Slack, Bot,
  Ticket, NotebookText, Boxes, Activity, PlayCircle,
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

/* ----------------------------------------------------- animated factory board */

const BOARD_STAGES = ["Backlog", "Implement", "Verify", "Review", "Done"];
const LIVE_STATUS = [
  { t: "queued", c: "idle" },
  { t: "coding agent", c: "run" },
  { t: "CI checks", c: "wait" },
  { t: "in review", c: "run" },
  { t: "merged #129", c: "ok" },
];
type Card = { title: string; who: string; when: string; chip?: string; chipc?: string };
const FILLER: Card[][] = [
  [
    { title: "Re-run hooks on client navigation", who: "JN", when: "2m", chip: "confidence 2", chipc: "idle" },
    { title: "Mute digest notifications", who: "LE", when: "9m" },
  ],
  [{ title: "Return local paths from retrieve()", who: "MA", when: "12m", chip: "agent", chipc: "run" }],
  [{ title: "Fix deploy & retrieve tests", who: "DI", when: "34m", chip: "tests failing", chipc: "fail" }],
  [{ title: "Link artifact type", who: "KW", when: "1d", chip: "review #27", chipc: "run" }],
  [
    { title: "Store Work Orders state at client", who: "ZU", when: "1d", chip: "merged #27", chipc: "ok" },
    { title: "Mute digests so users can focus", who: "LE", when: "1d", chip: "merged #25", chipc: "ok" },
  ],
];

function Avatar({ who }: { who: string }) {
  return <span className="sb-av">{who}</span>;
}

function TaskCard({ c }: { c: Card }) {
  return (
    <div className="sb-card">
      <div className="sb-card-t">{c.title}</div>
      <div className="sb-card-m">
        <Avatar who={c.who} />
        <span className="sb-when">{c.when}</span>
        {c.chip && <span className={`sb-chip ${c.chipc || "idle"}`}>{c.chip}</span>}
      </div>
    </div>
  );
}

function FactoryBoard({ animate }: { animate: boolean }) {
  const [col, setCol] = useState(0);
  useEffect(() => {
    if (!animate) return;
    const id = setInterval(() => setCol((c) => (c + 1) % (BOARD_STAGES.length + 1)), 1800);
    return () => clearInterval(id);
  }, [animate]);
  const live = Math.min(col, BOARD_STAGES.length - 1);
  const st = LIVE_STATUS[live];

  return (
    <div className="sb" data-tilt>
      <div className="sb-bar">
        <span className="sb-wd r" /><span className="sb-wd y" /><span className="sb-wd g" />
        <span className="sb-bar-t">ai-factory · board</span>
        <span className="sb-live"><span className="sb-live-dot" /> live</span>
      </div>
      <div className="sb-cols">
        {BOARD_STAGES.map((s, i) => (
          <div className="sb-col" key={s}>
            <div className="sb-colhead">
              <span className={`sb-coldot s-${s.toLowerCase()}`} />
              <b>{s}</b>
              <span className="sb-n">{FILLER[i].length + (live === i && animate ? 1 : 0)}</span>
            </div>
            <div className="sb-slots">
              {animate && live === i && (
                <div className="sb-card hot">
                  <div className="sb-card-t">Add rate limiting to the public API</div>
                  <div className="sb-card-m">
                    <Avatar who="AI" />
                    <span className="sb-when">now</span>
                    <span className={`sb-chip ${st.c}`}>{st.t}</span>
                  </div>
                </div>
              )}
              {FILLER[i].map((c, j) => <TaskCard c={c} key={j} />)}
            </div>
          </div>
        ))}
      </div>
    </div>
  );
}

/* ------------------------------------------------------------------- data */

const INTEGRATIONS = [
  { icon: Ticket, label: "Jira" },
  { icon: NotebookText, label: "Notion" },
  { icon: Github, label: "GitHub" },
  { icon: ShieldCheck, label: "CI checks" },
  { icon: Bot, label: "Any model" },
  { icon: Slack, label: "Slack" },
];

const PROBLEMS = [
  { icon: Boxes, title: "Hard to tell what's safe to hand off", body: "Point an agent at your backlog and it grabs work it can't scope. The wrong tasks look the same as the right ones." },
  { icon: ShieldCheck, title: "Without guardrails, it ships slop", body: "Unsupervised code is messy and insecure. One long prompt is not a process you can trust on a real repository." },
  { icon: UserCheck, title: "Engineers babysit every PR", body: "No trust means a human copies logs between tools and hand-holds each change from start to merge." },
];

const SOLUTIONS = [
  { icon: Activity, title: "Triage finds what's safe to automate", body: "Conduit scores each ticket by readiness — scope, context, a checkable success condition — and leaves ambiguous work for a person." },
  { icon: ShieldCheck, title: "Your workflow owns the rules", body: "Define the repo, allowed steps, required checks, review policy and approval gates once. Every run complies, whoever starts it." },
  { icon: PlayCircle, title: "Verified, not just finished", body: "A pull request is an output, not proof. Conduit runs the repo's CI, and on a failure it loops the coding agent back with the errors." },
];

const STEPS = [
  { n: "1", t: "Connect", p: "Point Conduit at Jira, Notion or GitHub. Tickets flow onto a live board." },
  { n: "2", t: "Triage", p: "Each ticket is scored; high-confidence work is ready to start, the rest waits for a human." },
  { n: "3", t: "Approve", p: "Agents plan, code and review. You approve the plan and the exact file changes — two gates, no blind automation." },
  { n: "4", t: "Ship", p: "Conduit pushes a branch, verifies CI, opens the PR and reports back on the board." },
];

/* ------------------------------------------------------------------- page */

export function Landing({ onSignIn, onGetStarted }: { onSignIn: () => void; onGetStarted: () => void }) {
  const [animate, setAnimate] = useState(true);
  const statsRef = useRef<HTMLDivElement>(null);
  useReveal();
  useTilt();
  useCountUp(statsRef);
  useEffect(() => { setAnimate(!prefersReduced()); }, []);
  // The marketing page carries its own light (cream + amber) palette scoped to
  // .landing.sp in CSS, so it is independent of the dark app theme.

  return (
    <div className="landing sp">
      <header className="sp-nav">
        <div className="sp-brand"><Logo /><span className="sp-word">Conduit</span></div>
        <nav className="sp-links">
          <a href="https://github.com/VITCS/conduit" target="_blank" rel="noreferrer">GitHub</a>
          <button className="sp-link-btn" onClick={onSignIn} data-testid="nav-signin">Log in</button>
          <button className="sp-btn primary" onClick={onGetStarted} data-testid="nav-getstarted">Sign up</button>
        </nav>
      </header>

      <section className="sp-hero">
        <span className="sp-eyebrow" data-reveal>Open-source AI software factory</span>
        <h1 data-reveal>Routine tickets, shipped as<br /><span className="sp-mark">reviewed pull requests</span> — in half the time.</h1>
        <p className="sp-lead" data-reveal>
          Conduit is an open-source AI software factory. Agents automate routine development work —
          triaged, planned, coded and CI-verified — with the rules, verification and human approval
          your team needs. Self-hosted, on your stack.
        </p>
        <div className="sp-cta" data-reveal>
          <button className="sp-btn primary lg" onClick={onGetStarted} data-testid="hero-getstarted">Start your factory <ArrowRight size={17} /></button>
          <button className="sp-btn lg" onClick={onSignIn} data-testid="hero-signin">Log in</button>
        </div>
        <div className="sp-board-wrap" data-reveal>
          <FactoryBoard animate={animate} />
        </div>
      </section>

      <section className="sp-logos" data-reveal>
        <span className="sp-logos-cap">Built on the tools you already use</span>
        <div className="sp-logos-row">
          {INTEGRATIONS.map((it) => (
            <span className="sp-logo" key={it.label}><it.icon size={16} /> {it.label}</span>
          ))}
        </div>
      </section>

      <section className="sp-section">
        <span className="sp-kicker" data-reveal>The problem</span>
        <h2 className="sp-h2" data-reveal>Agents can&apos;t be trusted on autopilot.</h2>
        <div className="sp-grid3">
          {PROBLEMS.map((p, i) => (
            <div className="sp-card prob" key={p.title} data-reveal style={{ ["--i" as string]: String(i) }}>
              <span className="sp-ic warn"><p.icon size={18} /></span>
              <h3>{p.title}</h3>
              <p>{p.body}</p>
            </div>
          ))}
        </div>
      </section>

      <section className="sp-section sp-section-dark">
        <span className="sp-kicker on-dark" data-reveal>With Conduit</span>
        <h2 className="sp-h2 on-dark" data-reveal>Make agents work like they should.</h2>
        <div className="sp-grid3">
          {SOLUTIONS.map((s, i) => (
            <div className="sp-card sol" key={s.title} data-reveal style={{ ["--i" as string]: String(i) }}>
              <span className="sp-ic"><s.icon size={18} /></span>
              <h3>{s.title}</h3>
              <p>{s.body}</p>
            </div>
          ))}
        </div>
      </section>

      <section className="sp-section">
        <h2 className="sp-h2 center" data-reveal>How one-shot engineering works</h2>
        <ol className="sp-steps">
          {STEPS.map((s, i) => (
            <li key={s.n} data-reveal style={{ ["--i" as string]: String(i) }}>
              <span className="sp-step-n">{s.n}</span>
              <div><b>{s.t}</b><p>{s.p}</p></div>
            </li>
          ))}
        </ol>
      </section>

      <section className="sp-stats" ref={statsRef} data-reveal>
        <div className="sp-stat"><div className="sp-num"><span data-count="2">0</span></div><div className="sp-stat-l">human approval gates per task</div></div>
        <div className="sp-stat"><div className="sp-num"><span data-count="26">0</span></div><div className="sp-stat-l">component types</div></div>
        <div className="sp-stat"><div className="sp-num"><span data-count="1">0</span></div><div className="sp-stat-l">binary to run it all</div></div>
        <div className="sp-stat"><div className="sp-num"><span data-count="0">0</span></div><div className="sp-stat-l">vendor lock-in</div></div>
      </section>

      <section className="sp-closing" data-reveal>
        <h2>Own your software factory.</h2>
        <p>Apache-friendly, self-hosted, and built around your models, CI and cost constraints. Create an account and start your first factory in a minute.</p>
        <div className="sp-cta center">
          <button className="sp-btn primary lg" onClick={onGetStarted}>Start your factory <ArrowRight size={17} /></button>
          <a className="sp-btn lg" href="https://github.com/VITCS/conduit" target="_blank" rel="noreferrer"><Github size={16} /> Check the repo</a>
        </div>
        <p className="sp-trust">Self-hosted · single binary · email or Google / GitHub sign-in when the server has those providers configured.</p>
      </section>

      <footer className="sp-foot">
        <div className="sp-brand"><Logo size={22} /><span className="sp-word">Conduit</span></div>
        <div className="sp-foot-links">
          <a href="https://github.com/VITCS/conduit" target="_blank" rel="noreferrer">GitHub</a>
          <button className="sp-link-btn" onClick={onSignIn}>Log in</button>
          <button className="sp-btn primary" onClick={onGetStarted}>Sign up</button>
        </div>
      </footer>
    </div>
  );
}

