"use client";
import { Fragment, useEffect, useRef, useState } from "react";
import {
  GitBranch, PlayCircle, ShieldCheck, Webhook, KeyRound, Boxes, Activity,
  UserCheck, Github, ArrowRight, Zap, Slack, Bot, Terminal, Check,
  Ticket, Code2, NotebookText, Globe, FileText, Hash, Database, User, Clock,
  type LucideIcon,
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
/* A faithful mini-canvas: the same node cards as the real app, laid out as a
   graph with curved edges, with a run animating through — the camera auto-pans to
   follow the active step and the live log stays in sync. */

type NKind = "trigger" | "step" | "gate" | "status";
type Row = { ic: LucideIcon; k: string; v: string; src?: "config" | "secret" | "event" | "run" };
interface HNode { id: string; title: string; icon: LucideIcon; kind: NKind; type: string; done?: string; x: number; y: number; rows: Row[] }
interface HEdge { from: string; to: string; label: string }
interface HPipe { nodes: HNode[]; edges: HEdge[]; log: { t: string; c: string; s: string }[] }

const CARDW = 212;
const nodeH = (r: number) => 84 + r * 22;

const FACTORY: HPipe = {
  nodes: [
    { id: "tk", title: "Fetch Jira Ticket", icon: Ticket, kind: "trigger", type: "jira", x: 20, y: 120, rows: [
      { ic: Globe, k: "base_url", v: "acme.atlassian.net", src: "config" },
      { ic: FileText, k: "jql", v: "status = Ready", src: "config" },
      { ic: KeyRound, k: "token", v: "••••", src: "secret" },
    ] },
    { id: "pl", title: "Refine & Plan", icon: Bot, kind: "step", type: "agent", x: 300, y: 8, rows: [
      { ic: Bot, k: "provider", v: "anthropic" },
      { ic: Hash, k: "model", v: "claude-…" },
      { ic: Database, k: "output", v: "plan", src: "run" },
    ] },
    { id: "ap", title: "Approve Changes", icon: UserCheck, kind: "step", type: "approval", x: 580, y: 170, rows: [
      { ic: User, k: "approvers", v: "maintainers" },
      { ic: FileText, k: "summary", v: "change plan", src: "run" },
    ] },
    { id: "co", title: "Coding Agent", icon: Code2, kind: "step", type: "coding_apply", x: 860, y: 20, rows: [
      { ic: Github, k: "repo", v: "acme / web", src: "config" },
      { ic: FileText, k: "changes", v: "change_plan", src: "run" },
      { ic: KeyRound, k: "token", v: "••••", src: "secret" },
    ] },
    { id: "pr", title: "Open Pull Request", icon: Github, kind: "status", type: "github_pr", done: "CREATED", x: 1140, y: 182, rows: [
      { ic: GitBranch, k: "head", v: "conduit/ENG-482", src: "run" },
      { ic: GitBranch, k: "base", v: "main" },
      { ic: KeyRound, k: "token", v: "••••", src: "secret" },
    ] },
  ],
  edges: [
    { from: "tk", to: "pl", label: "success" }, { from: "pl", to: "ap", label: "success" },
    { from: "ap", to: "co", label: "approved" }, { from: "co", to: "pr", label: "success" },
  ],
  log: [
    { t: "▶", c: "run", s: "run started · ENG-482 pulled from Jira" },
    { t: "✓", c: "ok", s: "refine & plan · plan + review drafted" },
    { t: "⏸", c: "wait", s: "awaiting approval · the changes" },
    { t: "✓", c: "ok", s: "coding agent · 2 files → conduit/ENG-482" },
    { t: "✓", c: "ok", s: "PR #129 opened · commented on ENG-482" },
  ],
};

const GITHUB: HPipe = {
  nodes: [
    { id: "ev", title: "GitHub Event", icon: Webhook, kind: "trigger", type: "webhook", x: 20, y: 120, rows: [
      { ic: Github, k: "on", v: "pull_request", src: "config" },
      { ic: Webhook, k: "hook", v: "/api/hooks/…" },
    ] },
    { id: "rv", title: "Agent Review", icon: Bot, kind: "step", type: "github_review", x: 300, y: 4, rows: [
      { ic: Github, k: "repo", v: "from event", src: "event" },
      { ic: Bot, k: "provider", v: "anthropic" },
      { ic: KeyRound, k: "token", v: "••••", src: "secret" },
    ] },
    { id: "sc", title: "Security Scan", icon: ShieldCheck, kind: "step", type: "security_scan", x: 580, y: 170, rows: [
      { ic: Globe, k: "repo", v: "from event", src: "event" },
      { ic: KeyRound, k: "token", v: "••••", src: "secret" },
    ] },
    { id: "ap", title: "Human Approval", icon: UserCheck, kind: "step", type: "approval", x: 860, y: 28, rows: [
      { ic: User, k: "approvers", v: "maintainers" },
      { ic: Clock, k: "timeout", v: "1d" },
    ] },
    { id: "mg", title: "Merge PR", icon: Github, kind: "status", type: "github_merge", done: "MERGED", x: 1140, y: 182, rows: [
      { ic: Hash, k: "number", v: "from event", src: "event" },
      { ic: GitBranch, k: "method", v: "squash" },
      { ic: KeyRound, k: "token", v: "••••", src: "secret" },
    ] },
  ],
  edges: [
    { from: "ev", to: "rv", label: "success" }, { from: "rv", to: "sc", label: "success" },
    { from: "sc", to: "ap", label: "success" }, { from: "ap", to: "mg", label: "approved" },
  ],
  log: [
    { t: "▶", c: "run", s: "run started · on pull_request #482" },
    { t: "✓", c: "ok", s: "agent review · posted on #482" },
    { t: "✓", c: "ok", s: "security scan · 0 critical / 0 high" },
    { t: "⏸", c: "wait", s: "awaiting approval · maintainers" },
    { t: "✓", c: "ok", s: "merged · #482 squashed" },
  ],
};

/* Hero connectivity scene: sources (Jira/Notion/GitHub) flow into the AI Factory
   pipeline and out to a reviewed PR, with a live board ribbon. Pure SVG so it
   scales crisply to full width. Motion = a scan beam sweeping the stages, flowing
   connectors, and a board card that advances in sync. */
function FactoryScene({ animate }: { animate: boolean }) {
  const sources = [
    { label: "Jira", y: 150, dot: "var(--wait)" },
    { label: "Notion", y: 226, dot: "var(--fg)" },
    { label: "GitHub", y: 302, dot: "var(--accent)" },
  ];
  const stages = ["Triage", "Plan & review", "Approve", "Coding agent", "Verify CI"];
  const outputs = [
    { label: "branch pushed", y: 164 },
    { label: "checks passed", y: 240 },
    { label: "PR #129 opened", y: 316 },
  ];

  const FX = 440, FY = 96, FW = 320, FH = 376;      // factory panel
  const SX = FX + 20, SW = FW - 40, SH = 50, SGAP = 14, STOP = FY + 54; // stage pills
  const hubL = { x: FX, y: 284 };
  const hubR = { x: FX + FW, y: 284 };
  const stageY = (i: number) => STOP + i * (SH + SGAP);

  return (
    <svg className="fs" viewBox="0 0 1200 552" preserveAspectRatio="xMidYMid meet" role="img"
      aria-label="Sources — Jira, Notion and GitHub — flow into the AI Factory pipeline (triage, plan, approve, code, verify) and out to a reviewed pull request, tracked on a board.">
      <defs>
        <filter id="fs-glow" x="-60%" y="-60%" width="220%" height="220%">
          <feGaussianBlur stdDeviation="3.2" result="b" /><feMerge><feMergeNode in="b" /><feMergeNode in="SourceGraphic" /></feMerge>
        </filter>
        <linearGradient id="fs-beam" x1="0" y1="0" x2="0" y2="1">
          <stop offset="0" stopColor="var(--accent)" stopOpacity="0" />
          <stop offset="0.5" stopColor="var(--accent)" stopOpacity="0.5" />
          <stop offset="1" stopColor="var(--accent)" stopOpacity="0" />
        </linearGradient>
      </defs>

      {/* connectors: sources -> factory hub */}
      {sources.map((s, i) => {
        const d = `M${232},${s.y + 24} C${(232 + hubL.x) / 2},${s.y + 24} ${(232 + hubL.x) / 2},${hubL.y} ${hubL.x},${hubL.y}`;
        return (
          <g key={"c" + i}>
            <path d={d} className="fs-edge" />
            <path d={d} className={`fs-edge flow${animate ? " on" : ""}`} style={{ animationDelay: `${i * 0.5}s` }} />
            {animate && <circle r="3.6" className="fs-tok"><animateMotion dur="2.8s" begin={`${i * 0.5}s`} repeatCount="indefinite" path={d} /></circle>}
          </g>
        );
      })}
      {/* connectors: factory hub -> outputs */}
      {outputs.map((o, i) => {
        const d = `M${hubR.x},${hubR.y} C${(hubR.x + 820) / 2},${hubR.y} ${(hubR.x + 820) / 2},${o.y + 24} ${820},${o.y + 24}`;
        return (
          <g key={"o" + i}>
            <path d={d} className="fs-edge" />
            <path d={d} className={`fs-edge flow out${animate ? " on" : ""}`} style={{ animationDelay: `${1.4 + i * 0.5}s` }} />
            {animate && <circle r="3.6" className="fs-tok out"><animateMotion dur="2.8s" begin={`${1.4 + i * 0.5}s`} repeatCount="indefinite" path={d} /></circle>}
          </g>
        );
      })}

      {/* source chips */}
      <text x="44" y="118" className="fs-cap">SOURCES</text>
      {sources.map((s, i) => (
        <g key={"s" + i} className="fs-src">
          <rect x="44" y={s.y} width="188" height="48" rx="11" />
          <circle cx="68" cy={s.y + 24} r="5.5" fill={s.dot} />
          <text x="86" y={s.y + 30} className="fs-src-t">{s.label}</text>
        </g>
      ))}

      {/* factory panel */}
      <rect x={FX} y={FY} width={FW} height={FH} rx="18" className="fs-panel" />
      <text x={FX + 20} y={FY + 32} className="fs-panel-t">AI Factory</text>
      <circle cx={FX + FW - 24} cy={FY + 26} r="4.5" className="fs-live" />
      {/* scan beam sweeping the stages */}
      {animate && (
        <rect x={SX} width={SW} height={SH} rx="10" fill="url(#fs-beam)" className="fs-beam">
          <animate attributeName="y" dur="5.5s" repeatCount="indefinite"
            keyTimes="0;0.12;0.30;0.48;0.66;0.84;0.92;1"
            values={`${stageY(0)};${stageY(0)};${stageY(1)};${stageY(2)};${stageY(3)};${stageY(4)};${stageY(4)};${stageY(0)}`} />
        </rect>
      )}
      {stages.map((st, i) => {
        const y = stageY(i);
        const gate = st === "Approve";
        return (
          <g key={"g" + i} className={`fs-stage${animate ? " anim" : ""}`} style={{ ["--i" as string]: i }}>
            <rect x={SX} y={y} width={SW} height={SH} rx="10" className={gate ? "fs-pill gate" : "fs-pill"} />
            <circle cx={SX + 18} cy={y + SH / 2} r="4" className="fs-pill-dot" />
            <text x={SX + 34} y={y + SH / 2 + 5} className="fs-pill-t">{st}</text>
            {gate && <text x={SX + SW - 12} y={y + SH / 2 + 5} className="fs-gate-badge">human</text>}
          </g>
        );
      })}

      {/* output chips */}
      <text x="820" y="140" className="fs-cap">DELIVERED</text>
      {outputs.map((o, i) => (
        <g key={"out" + i} className={`fs-out${animate ? " anim" : ""}`} style={{ ["--i" as string]: i }}>
          <rect x="820" y={o.y} width="236" height="48" rx="11" />
          <text x="842" y={o.y + 30} className="fs-out-t">{o.label}</text>
          <text x="1038" y={o.y + 30} className="fs-check">done</text>
        </g>
      ))}

      {/* board ribbon */}
      <g className="fs-board">
        <text x="44" y="486" className="fs-cap">BOARD</text>
        {["Backlog", "Implement", "Verify", "Done"].map((c, i) => (
          <g key={"b" + i}>
            <rect x={44 + i * 254} y="496" width="242" height="44" rx="11" className="fs-lane" />
            <text x={60 + i * 254} y="516" className="fs-lane-t">{c}</text>
            <rect x={60 + i * 254} y="522" width="56" height="12" rx="3" className="fs-ghost" />
          </g>
        ))}
        {animate && (
          <rect width="80" height="30" rx="8" y="503" className="fs-card">
            <animate attributeName="x" dur="5.5s" repeatCount="indefinite"
              keyTimes="0;0.12;0.30;0.48;0.66;0.84;0.92;1"
              values="58;58;312;566;566;820;820;58" />
          </rect>
        )}
      </g>
    </svg>
  );
}


function WorkflowCanvas({ pipe, animate }: { pipe: "factory" | "github"; animate: boolean }) {
  const data = pipe === "factory" ? FACTORY : GITHUB;
  const N = data.nodes.length;
  const viewRef = useRef<HTMLDivElement>(null);
  const [vw, setVw] = useState(560);
  const [a, setA] = useState(0);

  // Reset + drive the run whenever the pipeline or motion pref changes.
  useEffect(() => {
    setA(animate ? 0 : N - 1);
    if (!animate) return;
    const id = setInterval(() => setA((v) => (v + 1) % (N + 1)), 1250); // +1 = brief rest at the end
    return () => clearInterval(id);
  }, [pipe, animate, N]);

  useEffect(() => {
    const el = viewRef.current;
    if (!el || typeof ResizeObserver === "undefined") return;
    const ro = new ResizeObserver(() => setVw(el.clientWidth));
    ro.observe(el);
    setVw(el.clientWidth);
    return () => ro.disconnect();
  }, []);

  const pos = Object.fromEntries(data.nodes.map((n) => [n.id, n]));
  const worldW = Math.max(...data.nodes.map((n) => n.x + CARDW)) + 40;
  const worldH = Math.max(...data.nodes.map((n) => n.y + nodeH(n.rows.length))) + 20;
  const active = Math.min(a, N - 1);

  // Camera pans horizontally so the active node sits near the viewport centre.
  const focus = data.nodes[animate ? active : 0];
  const centerX = focus.x + CARDW / 2;
  const maxPan = Math.max(0, worldW - vw);
  const panX = Math.min(maxPan, Math.max(0, centerX - vw / 2));

  const status = (i: number): "ok" | "run" | "wait" | "idle" => {
    if (!animate) return "ok";
    if (i < active) return "ok";
    if (i === active) return data.nodes[i].kind === "gate" || data.nodes[i].type === "approval" ? "wait" : "run";
    return "idle";
  };
  const badge = (s: string, n: HNode) =>
    s === "ok" ? { c: "ok", t: n.done || "DONE" } : s === "run" ? { c: "run", t: "RUNNING" } : s === "wait" ? { c: "wait", t: "WAITING" } : { c: "idle", t: "PENDING" };

  return (
    <div className="hs-box">
      <div className="hs-view" ref={viewRef} aria-hidden>
        <div className="hs-world" style={{ width: worldW, height: worldH, ["--px" as string]: `${panX}px` }}>
          <svg className="hs-edges" width={worldW} height={worldH} viewBox={`0 0 ${worldW} ${worldH}`}>
            {data.edges.map((e, i) => {
              const A = pos[e.from], B = pos[e.to];
              const sx = A.x + CARDW, sy = A.y + nodeH(A.rows.length) / 2;
              const tx = B.x, ty = B.y + nodeH(B.rows.length) / 2;
              const d = `M${sx},${sy} C${sx + 70},${sy} ${tx - 70},${ty} ${tx},${ty}`;
              const fromIdx = data.nodes.findIndex((n) => n.id === e.from);
              const on = active > fromIdx;
              const mx = (sx + tx) / 2, my = (sy + ty) / 2;
              return (
                <g key={i}>
                  <path d={d} className="hs-edge" fill="none" />
                  <path d={d} className={`hs-edge flow${on ? " on" : ""}`} fill="none" />
                  <g transform={`translate(${mx},${my})`}>
                    <rect x={-27} y={-10} width={54} height={20} rx={10} className={`hs-elabel ${e.label}${on ? " on" : ""}`} />
                    <text x={0} y={4} className="hs-eltext">{e.label}</text>
                  </g>
                </g>
              );
            })}
          </svg>

          {data.nodes.map((n, i) => {
            const s = status(i);
            const b = badge(s, n);
            return (
              <div key={n.id} className={`cnode hs kind-${n.kind}${s === "run" || s === "wait" ? " active" : ""}`} style={{ left: n.x, top: n.y, width: CARDW }}>
                <div className="n-head">
                  <span className="n-ico"><n.icon size={14} /></span>
                  <span className="n-title">{n.title}</span>
                </div>
                <div className="n-cfg">
                  {n.rows.map((r, j) => (
                    <div className="n-cfg-row" key={j}>
                      <span className="ic"><r.ic size={11} /></span>
                      <span><b>{r.k}</b> {r.v}</span>
                      {r.src && <span className={`src ${r.src}`}>{r.src}</span>}
                    </div>
                  ))}
                </div>
                <div className="n-foot">
                  <span className={`badge ${b.c}`}>{b.t}</span>
                  <span className="n-ref">{n.type}</span>
                </div>
              </div>
            );
          })}
        </div>
      </div>

      <div className="cp-log" aria-hidden>
        {data.log.slice(0, active + 1).map((l, i) => (
          <div className="cp-logline" key={i}>
            <span className={`cp-lt ${l.c}`}>{l.t}</span>
            <span>{l.s}</span>
            {animate && i === active && <span className="cp-cursor" />}
          </div>
        ))}
      </div>
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
  const [heroView, setHeroView] = useState<"overview" | "run">("overview");
  const statsRef = useRef<HTMLDivElement>(null);
  useReveal();
  useTilt();
  useCountUp(statsRef);
  useEffect(() => { setAnimate(!prefersReduced()); }, []);
  // Marketing page is always dark, regardless of the saved app theme (restored
  // by the Topbar once the user signs in). Does not touch localStorage.
  useEffect(() => {
    const prev = document.documentElement.getAttribute("data-theme");
    document.documentElement.setAttribute("data-theme", "dark");
    return () => { if (prev) document.documentElement.setAttribute("data-theme", prev); };
  }, []);

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

      <section className="lx-hero lx-hero-center">
        <span className="lx-eyebrow" data-reveal><Zap size={13} /> The open-source AI software factory</span>
        <h1 data-reveal>
          Tickets in. <span className="lx-grad">Reviewed pull requests</span> out —<br />with a human in the loop.
        </h1>
        <p className="lx-lead" data-reveal>
          Conduit is an AI <b>software factory</b>: connect Jira, Notion or GitHub and it turns a ticket
          into a reviewed pull request on a live board — triaged, planned, coded, CI-verified, and merged
          only after a human approves. Under the hood it turns a visual graph of components into a live
          workflow. One self-hosted binary.
        </p>
        <div className="lx-cta" data-reveal>
          <button className="btn primary lg" onClick={onGetStarted} data-testid="hero-getstarted">Start your factory <ArrowRight size={17} /></button>
          <button className="btn lg" onClick={onSignIn} data-testid="hero-signin">Sign in</button>
        </div>
        <p className="lx-trust" data-reveal>Self-hosted · single binary · email or Google / GitHub sign-in</p>
      </section>

      <div className="lx-stagewrap" data-reveal>
        <div className="lx-hero-stage wide">
          <div className="lx-stage-bar">
            <span className="lx-dot r" /><span className="lx-dot y" /><span className="lx-dot g" />
            <span className="lx-stage-title">ai-factory · connected</span>
            <div className="pl-toggle" role="tablist" aria-label="Hero view">
              <button role="tab" aria-selected={heroView === "overview"} className={`pl-tog${heroView === "overview" ? " on" : ""}`} onClick={() => setHeroView("overview")}>Overview</button>
              <button role="tab" aria-selected={heroView === "run"} className={`pl-tog${heroView === "run" ? " on" : ""}`} onClick={() => setHeroView("run")}>Live run</button>
            </div>
            <span className="lx-live"><span className="lx-live-dot" /> live</span>
          </div>
          {heroView === "overview"
            ? <FactoryScene animate={animate} />
            : <WorkflowCanvas pipe="factory" animate={animate} />}
        </div>
      </div>

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
