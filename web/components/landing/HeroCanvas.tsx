"use client";
import { useEffect, useRef, useState } from "react";
import { AnimatePresence, motion, useInView, useReducedMotion } from "framer-motion";
import {
  Bot, Check, CircleDot, GitBranch, GitPullRequest, NotebookText, ShieldCheck, Ticket, UserCheck,
} from "lucide-react";
import { HERO_HUB, HERO_NODES, HERO_SCRIPT, type HeroNodeId } from "./data";
import { DemoTag } from "./ui";

const W = 720, H = 520;
const LAST = HERO_SCRIPT.length - 1;
const IDLE_MS = 800, DONE_MS = 3000;

const ICONS: Record<HeroNodeId, typeof Bot> = {
  jira: Ticket, issues: CircleDot, notion: NotebookText, model: Bot,
  approver: UserCheck, repo: GitBranch, ci: ShieldCheck, pr: GitPullRequest,
};

type NodeState = "idle" | "active" | "done";

function Pulse({ from, to, color }: { from: { x: number; y: number }; to: { x: number; y: number }; color: string }) {
  const t = { duration: 0.7, ease: "easeInOut" as const };
  return (
    <motion.circle
      r={5.5} fill={color} filter="url(#cvGlow)"
      initial={{ cx: from.x, cy: from.y, opacity: 0 }}
      animate={{ cx: to.x, cy: to.y, opacity: [0, 1, 1, 0] }}
      transition={{ cx: t, cy: t, opacity: { duration: 0.7, times: [0, 0.12, 0.85, 1] } }}
    />
  );
}

/**
 * A scripted replay of one AI Factory run: a ticket arrives, the model triages and
 * plans, a person approves the plan, the files and the diff, a branch is pushed,
 * CI passes, a PR opens.
 * step: -1 = waiting, 0..LAST = that script line, LAST + 1 = finished.
 */
export function HeroCanvas() {
  const ref = useRef<HTMLDivElement>(null);
  const inView = useInView(ref, { margin: "-80px" });
  const reduced = useReducedMotion();
  const [step, setStep] = useState(-1);
  const [runNo, setRunNo] = useState(1);
  const done = step > LAST;

  // Advance while on screen. With reduced motion the canvas rests on the finished run.
  useEffect(() => {
    if (reduced) { setStep(LAST + 1); return; }
    if (!inView) return;
    const ms = step < 0 ? IDLE_MS : done ? DONE_MS : HERO_SCRIPT[step].ms;
    const id = window.setTimeout(() => {
      if (done) { setRunNo((n) => n + 1); setStep(-1); } else setStep(step + 1);
    }, ms);
    return () => window.clearTimeout(id);
  }, [step, done, inView, reduced]);

  const cur = step >= 0 && !done ? HERO_SCRIPT[step] : null;
  const visited = new Set(HERO_SCRIPT.slice(0, Math.min(step, LAST) + 1).map((s) => s.node));
  const stateOf = (id: HeroNodeId): NodeState =>
    cur?.node === id ? "active" : visited.has(id) ? "done" : "idle";
  const node = (id: HeroNodeId) => HERO_NODES.find((n) => n.id === id)!;
  const lines = HERO_SCRIPT.slice(0, Math.min(step, LAST) + 1);
  const gates = lines.filter((l) => l.node === "approver").length;
  const hubState: NodeState = step < 0 ? "idle" : done ? "done" : "active";

  return (
    <div className="cv-stage" ref={ref}>
      <div className="cv-stage-bar">
        <span className="cv-dots" aria-hidden><i /><i /><i /></span>
        <span className="cv-stage-title">AI Factory · Ticket → PR · run #{480 + runNo}</span>
        <DemoTag>Sample run</DemoTag>
        <span className={`cv-stage-status ${done ? "ok" : step >= 0 ? "run" : ""}`}>
          <i /> {done ? "completed" : cur?.node === "approver" ? "waiting for approval" : step >= 0 ? "running" : "waiting for a ticket"}
        </span>
      </div>

      <div className="cv-stage-body">
        <div className="cv-stage-l">
          <div className="cv-float a">
            <span className="cv-float-ico cy"><Ticket size={14} /></span>
            <div><b>ENG-482</b><small>{step >= 1 ? "confidence 86" : "sample ticket"}</small></div>
          </div>
          <div className="cv-float b">
            <span className="cv-float-ico ok"><UserCheck size={14} /></span>
            <div><b>{gates} of 3 approvals</b><small>human in the loop</small></div>
          </div>
        </div>

        <div className="cv-ring" role="img"
          aria-label="Animated sample run: a Jira ticket arrives, an AI model triages and plans it, a person approves the plan and the file changes, a branch is pushed, CI checks pass and a pull request is opened.">
          <svg viewBox={`0 0 ${W} ${H}`} className="cv-ring-svg" aria-hidden>
            <defs>
              <filter id="cvGlow" x="-200%" y="-200%" width="500%" height="500%">
                <feGaussianBlur stdDeviation="4" result="b" />
                <feMerge><feMergeNode in="b" /><feMergeNode in="SourceGraphic" /></feMerge>
              </filter>
              <radialGradient id="cvHubGlow">
                <stop offset="0" stopColor="#8B5CF6" stopOpacity=".34" />
                <stop offset="1" stopColor="#8B5CF6" stopOpacity="0" />
              </radialGradient>
            </defs>
            <circle cx={HERO_HUB.x} cy={HERO_HUB.y} r="150" fill="url(#cvHubGlow)" />
            <circle cx={HERO_HUB.x} cy={HERO_HUB.y} r="118" className="cv-orbit" />
            <circle cx={HERO_HUB.x} cy={HERO_HUB.y} r="196" className="cv-orbit dim" />

            {HERO_NODES.map((n) => (
              <g key={n.id}>
                <line x1={HERO_HUB.x} y1={HERO_HUB.y} x2={n.x} y2={n.y} className="cv-edge" />
                {visited.has(n.id) && (
                  <motion.line
                    key={`${runNo}-${n.id}`}
                    x1={HERO_HUB.x} y1={HERO_HUB.y} x2={n.x} y2={n.y}
                    className={`cv-edge on ${n.id === "model" ? "violet" : ""}`}
                    initial={{ opacity: 0 }} animate={{ opacity: 1 }} transition={{ duration: 0.5 }}
                  />
                )}
              </g>
            ))}

            {cur?.node && (
              <g key={`${runNo}-${step}`}>
                <Pulse
                  from={cur.dir === "in" ? node(cur.node) : HERO_HUB}
                  to={cur.dir === "in" ? HERO_HUB : node(cur.node)}
                  color={cur.tone === "violet" ? "#8B5CF6" : cur.tone === "amber" ? "#FBBF24" : "#22D3EE"}
                />
              </g>
            )}
          </svg>

          <div className={`cv-hub ${hubState}`} style={{ left: `${(HERO_HUB.x / W) * 100}%`, top: `${(HERO_HUB.y / H) * 100}%` }}>
            <span className="cv-hub-ring" aria-hidden />
            <b>Conduit</b>
            <small>AI Factory</small>
          </div>

          {HERO_NODES.map((n) => {
            const Icon = ICONS[n.id];
            const s = stateOf(n.id);
            return (
              <div key={n.id} className={`cv-hn ${s} tone-${n.tone}`}
                style={{ left: `${(n.x / W) * 100}%`, top: `${(n.y / H) * 100}%` }}>
                <span className="cv-hn-ico"><Icon /></span>
                <span className="cv-hn-txt"><b>{n.label}</b><small>{n.sub}</small></span>
                {s === "done" && (
                  <motion.span className="cv-hn-ok" initial={{ scale: 0 }} animate={{ scale: 1 }}
                    transition={{ type: "spring", stiffness: 500, damping: 26 }}>
                    <Check />
                  </motion.span>
                )}
              </div>
            );
          })}

          <AnimatePresence>
            {cur?.bubble && (
              <motion.div className="cv-think" key={cur.bubble}
                initial={{ opacity: 0, y: 6 }} animate={{ opacity: 1, y: 0 }} exit={{ opacity: 0, y: -4 }}
                transition={{ duration: 0.25 }}>
                <span className="cv-think-dots" aria-hidden><i /><i /><i /></span> {cur.bubble}
              </motion.div>
            )}
          </AnimatePresence>
        </div>

        <div className="cv-stage-r">
          <div className="cv-log" aria-live="off">
            <div className="cv-log-head">Run log</div>
            <div className="cv-log-body">
              {lines.length === 0 && <div className="cv-log-line dim">waiting for a ticket…</div>}
              {lines.slice(-6).map((l, i) => (
                <motion.div key={`${runNo}-${l.tag}-${i}`} className={`cv-log-line ${l.tone === "amber" ? "" : l.tone || ""}`}
                  initial={{ opacity: 0, x: -6 }} animate={{ opacity: 1, x: 0 }} transition={{ duration: 0.25 }}>
                  <span className="t">{l.tag}</span>{l.text}
                </motion.div>
              ))}
            </div>
          </div>
          <div className="cv-toast-slot">
            <AnimatePresence>
              {done && (
                <motion.div className="cv-toast" key="toast"
                  initial={{ opacity: 0, y: 10, scale: 0.97 }} animate={{ opacity: 1, y: 0, scale: 1 }}
                  exit={{ opacity: 0, y: -6 }} transition={{ duration: 0.3 }}>
                  <span className="cv-float-ico ok"><Check size={14} /></span>
                  Pull request ready for review
                </motion.div>
              )}
            </AnimatePresence>
          </div>
        </div>
      </div>
    </div>
  );
}
