"use client";
import { Fragment, useEffect, useRef, useState } from "react";
import {
  GitBranch, PlayCircle, ShieldCheck, Webhook, KeyRound, Boxes, Activity,
  UserCheck, Github, ArrowRight, Zap, Slack, Bot, Terminal, Check,
  Ticket, Code2, NotebookText,
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

/* ----------------------------------------------------------- pipelines */

type PKind = "trigger" | "step" | "gate" | "status";
interface PNode { id: string; label: string; sub?: string; kind: PKind }

// The AI Factory: a real ticket becomes a reviewed PR, with two human gates.
const FACTORY_PIPE: PNode[] = [
  { id: "tk", label: "Ticket", sub: "Jira · Notion", kind: "trigger" },
  { id: "ag", label: "Agents", sub: "plan · review", kind: "step" },
  { id: "a1", label: "Approve", sub: "the plan", kind: "gate" },
  { id: "co", label: "Coding agent", sub: "writes a branch", kind: "step" },
  { id: "a2", label: "Approve", sub: "the changes", kind: "gate" },
  { id: "pr", label: "Open PR", sub: "link to ticket", kind: "status" },
];

// The GitHub flow: a PR is reviewed, security-scanned, approved, then merged.
const GITHUB_PIPE: PNode[] = [
  { id: "ev", label: "On PR", sub: "webhook", kind: "trigger" },
  { id: "rv", label: "Agent review", sub: "reads the diff", kind: "step" },
  { id: "sc", label: "Security scan", sub: "SQLi · high/crit", kind: "gate" },
  { id: "ap", label: "Approve", sub: "maintainers", kind: "gate" },
  { id: "mg", label: "Merge", sub: "squash", kind: "status" },
];

function Pipeline({ nodes, animate, label }: { nodes: PNode[]; animate: boolean; label: string }) {
  const W = 880, Y = 150, RW = 112, RH = 54, margin = 74;
  const n = nodes.length;
  const span = W - margin * 2;
  const laid = nodes.map((nd, i) => ({ ...nd, x: margin + (span * i) / (n - 1), d: i * 1.0 }));
  const edges = laid.slice(0, -1).map((nd, i) => `M${nd.x + RW / 2},${Y} L${laid[i + 1].x - RW / 2},${Y}`);
  const tokenPath = `M${laid[0].x},${Y} ` + laid.slice(1).map((p) => `L${p.x},${Y}`).join(" ");

  return (
    <svg className="cp" viewBox={`0 0 ${W} 300`} role="img" aria-label={label}>
      <defs>
        <linearGradient id="cp-edge" x1="0" y1="0" x2="1" y2="0">
          <stop offset="0" stopColor="var(--accent)" stopOpacity="0.12" />
          <stop offset="1" stopColor="var(--accent)" stopOpacity="0.55" />
        </linearGradient>
        <filter id="cp-glow" x="-60%" y="-60%" width="220%" height="220%">
          <feGaussianBlur stdDeviation="3.4" result="b" /><feMerge><feMergeNode in="b" /><feMergeNode in="SourceGraphic" /></feMerge>
        </filter>
      </defs>

      {edges.map((d, i) => (
        <g key={i}>
          <path d={d} fill="none" stroke="var(--border-strong)" strokeWidth="1.6" />
          {animate && <path d={d} fill="none" stroke="url(#cp-edge)" strokeWidth="2.2" strokeDasharray="6 10" strokeLinecap="round" className="cp-flow" />}
        </g>
      ))}

      {laid.map((nd) => (
        <g key={nd.id} className={`cp-node cp-${nd.kind} ${nd.kind === "gate" ? "cp-gate" : ""}`} style={{ ["--d" as string]: `${nd.d}s` }}>
          {nd.kind === "gate" && animate && <circle cx={nd.x} cy={Y} r="26" className="cp-ring" fill="none" />}
          <rect x={nd.x - RW / 2} y={Y - RH / 2} width={RW} height={RH} rx="11" className="cp-rect" fill="var(--surface)" stroke="var(--border)" />
          <circle cx={nd.x - RW / 2 + 16} cy={Y - RH / 2 + 15} r="3.5" className="cp-dot" fill="var(--idle)" />
          <text x={nd.x} y={Y - 2} className="cp-title">{nd.label}</text>
          {nd.sub && <text x={nd.x} y={Y + 14} className="cp-sub">{nd.sub}</text>}
        </g>
      ))}

      {animate && (
        <circle r="5" fill="var(--accent)" filter="url(#cp-glow)">
          <animateMotion dur="7s" repeatCount="indefinite" calcMode="linear" path={tokenPath} />
        </circle>
      )}
    </svg>
  );
}

/* --------------------------------------------------------------- live log */

type LogLine = { t: string; c: string; s: string };

const FACTORY_LOG: LogLine[] = [
  { t: "▶", c: "run", s: "run started · ENG-482 pulled from Jira" },
  { t: "✓", c: "ok", s: "agents · plan + review drafted" },
  { t: "⏸", c: "wait", s: "awaiting approval · the plan" },
  { t: "✓", c: "ok", s: "approved · generating changes" },
  { t: "✓", c: "ok", s: "coding agent · 2 files → conduit/ENG-482" },
  { t: "⏸", c: "wait", s: "awaiting approval · the changes" },
  { t: "✓", c: "ok", s: "approved · opening pull request" },
  { t: "✓", c: "ok", s: "PR #129 opened · commented on ENG-482" },
];

const GITHUB_LOG: LogLine[] = [
  { t: "▶", c: "run", s: "run started · on pull_request #482" },
  { t: "✓", c: "ok", s: "agent review · posted on #482" },
  { t: "✓", c: "ok", s: "security scan · 0 critical / 0 high" },
  { t: "⏸", c: "wait", s: "awaiting approval · maintainers" },
  { t: "✓", c: "ok", s: "approved · merging" },
  { t: "✓", c: "ok", s: "merged · #482 squashed" },
];

function LiveLog({ log, animate }: { log: LogLine[]; animate: boolean }) {
  const [n, setN] = useState(animate ? 1 : log.length);
  useEffect(() => {
    setN(animate ? 1 : log.length);
    if (!animate) return;
    const id = setInterval(() => setN((v) => (v >= log.length ? 1 : v + 1)), 1100);
    return () => clearInterval(id);
  }, [animate, log]);
  return (
    <div className="cp-log" aria-hidden>
      {log.slice(0, n).map((l, i) => (
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
  { icon: Ticket, title: "Pull tickets from Jira", body: "Fetch a ticket by JQL and inject its summary + description into the run. Comment the result back when it's done." },
  { icon: NotebookText, title: "…or a Notion database", body: "Query a Notion board for the next task. Same downstream pipeline — the source is swappable." },
  { icon: Code2, title: "Write code on a branch", body: "A coding agent plans the files, pulls current contents, edits them, and pushes a branch via the Git Data API." },
  { icon: Github, title: "Open & merge PRs", body: "Open a pull request, comment, or merge — real GitHub API calls after a human approves." },
  { icon: Bot, title: "Review with an agent", body: "An agent reads a PR's real diff and posts a review. Anthropic, OpenAI, or a custom model." },
  { icon: ShieldCheck, title: "Gate on a security scan", body: "Block a merge until a vulnerability + SQL-injection scan passes. High/Critical findings stop the run." },
  { icon: Slack, title: "Notify Slack", body: "Post run results to a Slack webhook, with the message templated from run state." },
  { icon: Terminal, title: "Call any HTTP API", body: "Live HTTP steps with auth headers and bodies; the outcome routes on the status code." },
];

// The two flagship workflows seeded with every install.
const WORKFLOWS = [
  {
    icon: Ticket, tag: "AI Factory", kicker: "Ticket → reviewed PR",
    body: "Pull a ticket from Jira or Notion, let agents refine it into a plan and review it, approve the plan, let a coding agent write the branch, approve the exact file changes, and open a pull request — two human gates, zero blind automation.",
    chain: ["Jira / Notion", "Plan & review", "Approve", "Coding agent", "Approve changes", "Open PR"],
  },
  {
    icon: Github, tag: "GitHub PR review", kicker: "PR → scanned, approved, merged",
    body: "On every pull request an agent reads the real diff and posts a review, a security scan gates on vulnerabilities and SQL injection, a human approves, and Conduit merges — all real GitHub API calls.",
    chain: ["On PR", "Agent review", "Security scan", "Approve", "Merge"],
  },
];

const COMPONENTS = ["webhook", "schedule", "jira", "notion", "agent", "coding_plan", "coding_apply", "github_pr", "github_review", "github_merge", "security_scan", "http", "slack", "if", "filter", "approval", "memory"];

/* ------------------------------------------------------------------- page */

export function Landing({ onSignIn, onGetStarted }: { onSignIn: () => void; onGetStarted: () => void }) {
  const [animate, setAnimate] = useState(true);
  const [pipe, setPipe] = useState<"factory" | "github">("factory");
  const statsRef = useRef<HTMLDivElement>(null);
  const pipeNodes = pipe === "factory" ? FACTORY_PIPE : GITHUB_PIPE;
  const pipeLog = pipe === "factory" ? FACTORY_LOG : GITHUB_LOG;
  const pipeLabel =
    pipe === "factory"
      ? "AI Factory pipeline: a Jira or Notion ticket is pulled in, agents plan and review it, a human approves the plan, a coding agent writes a branch, a human approves the changes, and a pull request opens."
      : "GitHub pipeline: an opened pull request is reviewed by an agent, scanned for security issues, approved by a human, then merged.";
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
          <span className="lx-eyebrow" data-reveal><Zap size={13} /> Human-in-the-loop automation control plane</span>
          <h1 data-reveal>
            Turn a ticket into a reviewed PR —<br />with a <span className="lx-grad">human in the loop</span>.
          </h1>
          <p className="lx-lead" data-reveal>
            Conduit Agent turns a visual graph of components into a live workflow. Pull a ticket from
            Jira or Notion, let agents plan and review it, approve the plan, let a coding agent open a
            real pull request — or review, security-scan and merge the PRs you already have. Every run
            streams live and pauses for a human before anything risky. One self-hosted binary.
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
            <div className="pl-toggle" role="tablist" aria-label="Choose a workflow">
              <button role="tab" aria-selected={pipe === "factory"} className={`pl-tog${pipe === "factory" ? " on" : ""}`} onClick={() => setPipe("factory")}>AI Factory</button>
              <button role="tab" aria-selected={pipe === "github"} className={`pl-tog${pipe === "github" ? " on" : ""}`} onClick={() => setPipe("github")}>GitHub</button>
            </div>
            <span className="lx-live"><span className="lx-live-dot" /> live</span>
          </div>
          <Pipeline nodes={pipeNodes} animate={animate} label={pipeLabel} />
          <LiveLog log={pipeLog} animate={animate} />
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
        <h2 className="lx-h2c" data-reveal>Two workflows, ready the moment you sign in</h2>
        <p className="lx-sub" data-reveal>Seeded with every install — connect your tools and run them for real. Flip between them in the hero above.</p>
        <div className="lx-wf">
          {WORKFLOWS.map((w, i) => (
            <div className="lx-wf-card" key={w.tag} data-reveal data-tilt style={{ ["--i" as string]: String(i) }}>
              <div className="lx-wf-head">
                <span className="lx-ico"><w.icon size={18} /></span>
                <div><b>{w.tag}</b><span className="lx-wf-kick">{w.kicker}</span></div>
              </div>
              <p>{w.body}</p>
              <div className="lx-wf-chain">
                {w.chain.map((c, j) => (
                  <Fragment key={c}>
                    <span className="lx-wf-step">{c}</span>
                    {j < w.chain.length - 1 && <ArrowRight size={12} className="lx-wf-arrow" />}
                  </Fragment>
                ))}
              </div>
            </div>
          ))}
        </div>
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
        <div className="lx-stat"><div className="lx-num"><span data-count="26">0</span></div><div className="lx-stat-l">component types</div></div>
        <div className="lx-stat"><div className="lx-num"><span data-count="2">0</span></div><div className="lx-stat-l">workflows out of the box</div></div>
        <div className="lx-stat"><div className="lx-num"><span data-count="1">0</span></div><div className="lx-stat-l">binary to run it all</div></div>
        <div className="lx-stat"><div className="lx-num"><span data-count="0">0</span></div><div className="lx-stat-l">extra services required</div></div>
      </section>

      <section className="lx-section lx-honest">
        <h2 className="lx-h2c" data-reveal>Straight about the demo</h2>
        <p data-reveal>
          Components that perform real side effects act against the real thing once you add a secret:
          <b> Jira</b> and <b>Notion</b> (fetch tickets, comment back), the <b>coding agent</b> (commits a
          branch via the GitHub Git Data API), <b>GitHub PR / review / merge / comment</b>, the
          <b> security scan</b> gate, <b>live HTTP</b>, <b>Slack</b>, and the <b>AI Agent</b> (set
          <code> live: true</code>). A few components (GraphQL, SSH) are still simulated today, and private-repo
          scans need the token plumbed through. You can sign in with email, or with Google / GitHub when the
          server has those providers configured.
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
