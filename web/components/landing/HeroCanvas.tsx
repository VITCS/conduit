"use client";
import { useEffect, useRef, useState } from "react";
import { AnimatePresence, motion, useInView, useReducedMotion } from "framer-motion";
import {
  Bot, Check, Database, Mail, NotebookText, Slack, Table2, Users, Webhook, Zap,
} from "lucide-react";
import { HERO_HUB, HERO_NODES, HERO_OUTPUTS, type HeroNodeId } from "./data";
import { DemoTag } from "./ui";

const W = 720, H = 520;

// One loop of the demo run. Each phase advances after `ms`.
const PHASES = [
  { id: "idle", ms: 700 },
  { id: "trigger", ms: 850 },
  { id: "toHub", ms: 750 },
  { id: "toAgent", ms: 750 },
  { id: "think", ms: 1700 },
  { id: "fromAgent", ms: 650 },
  { id: "fanout", ms: 1200 },
  { id: "done", ms: 2800 },
] as const;
const DONE = PHASES.length - 1;

const LOG: { at: number; tag: string; text: string; tone?: string }[] = [
  { at: 1, tag: "trigger", text: "webhook received · POST /hooks/lead" },
  { at: 2, tag: "route", text: "payload → orchestrator" },
  { at: 3, tag: "agent", text: "context loaded · 3 sources" },
  { at: 4, tag: "agent", text: "reasoning over lead…", tone: "violet" },
  { at: 5, tag: "agent", text: "decision: 6 actions planned" },
  { at: 6, tag: "run", text: "dispatching to 6 tools" },
  { at: 7, tag: "ok", text: "12 tasks completed · 3.4s", tone: "ok" },
];

const ICONS: Record<HeroNodeId, typeof Bot> = {
  webhooks: Webhook, database: Database, agent: Bot, gmail: Mail,
  slack: Slack, sheets: Table2, crm: Users, notion: NotebookText,
};

type NodeState = "idle" | "active" | "done";
function stateOf(id: HeroNodeId, step: number): NodeState {
  if (id === "webhooks") return step === 0 ? "idle" : step <= 2 ? "active" : "done";
  if (id === "agent") return step < 3 ? "idle" : step <= 4 ? "active" : "done";
  return step < 6 ? "idle" : step === 6 ? "active" : "done";
}

function Pulse({ from, to, dur, delay = 0, color }: {
  from: { x: number; y: number }; to: { x: number; y: number }; dur: number; delay?: number; color: string;
}) {
  const t = { duration: dur, delay, ease: "easeInOut" as const };
  return (
    <motion.circle
      r={5.5} fill={color} filter="url(#cvGlow)"
      initial={{ cx: from.x, cy: from.y, opacity: 0 }}
      animate={{ cx: to.x, cy: to.y, opacity: [0, 1, 1, 0] }}
      transition={{ cx: t, cy: t, opacity: { duration: dur, delay, times: [0, 0.12, 0.85, 1] } }}
    />
  );
}

export function HeroCanvas() {
  const ref = useRef<HTMLDivElement>(null);
  const inView = useInView(ref, { margin: "-80px" });
  const reduced = useReducedMotion();
  const [step, setStep] = useState(0);
  const [runNo, setRunNo] = useState(1);

  // Advance the demo run while the canvas is on screen. With reduced motion the
  // canvas rests on the completed state instead of looping.
  useEffect(() => {
    if (reduced) { setStep(DONE); return; }
    if (!inView) return;
    const id = window.setTimeout(() => {
      if (step === DONE) { setRunNo((n) => n + 1); setStep(0); } else setStep(step + 1);
    }, PHASES[step].ms);
    return () => window.clearTimeout(id);
  }, [step, inView, reduced]);

  const node = (id: HeroNodeId) => HERO_NODES.find((n) => n.id === id)!;
  const hubState: NodeState = step < 2 ? "idle" : step < DONE ? "active" : "done";
  const lit = (id: HeroNodeId) =>
    id === "webhooks" ? step >= 2 : id === "agent" ? step >= 3 : step >= 6;
  const lines = LOG.filter((l) => l.at <= step);
  const key = `${runNo}-${step}`;

  return (
    <div className="cv-stage" ref={ref}>
      <div className="cv-stage-bar">
        <span className="cv-dots" aria-hidden><i /><i /><i /></span>
        <span className="cv-stage-title">lead-routing · run #{1040 + runNo}</span>
        <DemoTag>Live demo</DemoTag>
        <span className={`cv-stage-status ${step === DONE ? "ok" : step > 0 ? "run" : ""}`}>
          <i /> {step === DONE ? "completed" : step > 0 ? "running" : "waiting for trigger"}
        </span>
      </div>

      <div className="cv-stage-body">
        {/* left: floating stats */}
        <div className="cv-stage-l">
          <div className="cv-float a">
            <span className="cv-float-ico ok"><Check size={14} /></span>
            <div><b>{step === DONE ? 12 : Math.min(12, Math.max(0, (step - 1) * 2))} Tasks Completed</b><small>this run</small></div>
          </div>
          <div className="cv-float b">
            <span className="cv-float-ico cy"><Zap size={14} /></span>
            <div><b>98.7% Success Rate</b><small>last 30 days</small></div>
          </div>
        </div>

        {/* center: the orchestration ring */}
        <div className="cv-ring" role="img"
          aria-label="Animated demo: a webhook triggers the orchestrator, an AI agent reasons over the payload, and tasks are dispatched to Gmail, Slack, Google Sheets, a CRM, Notion and a database.">
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
                {lit(n.id) && (
                  <motion.line
                    key={`${runNo}-${n.id}`}
                    x1={n.id === "webhooks" ? n.x : HERO_HUB.x} y1={n.id === "webhooks" ? n.y : HERO_HUB.y}
                    x2={n.id === "webhooks" ? HERO_HUB.x : n.x} y2={n.id === "webhooks" ? HERO_HUB.y : n.y}
                    className={`cv-edge on ${n.id === "agent" ? "violet" : ""}`}
                    initial={{ pathLength: 0, opacity: 0 }}
                    animate={{ pathLength: 1, opacity: 1 }}
                    transition={{ duration: 0.7, ease: "easeInOut" }}
                  />
                )}
              </g>
            ))}

            {/* light pulses + task particles for the current phase */}
            <g key={key}>
              {step === 2 && <Pulse from={node("webhooks")} to={HERO_HUB} dur={0.72} color="#22D3EE" />}
              {step === 3 && <Pulse from={HERO_HUB} to={node("agent")} dur={0.72} color="#8B5CF6" />}
              {step === 5 && <Pulse from={node("agent")} to={HERO_HUB} dur={0.6} color="#8B5CF6" />}
              {step === 6 && HERO_OUTPUTS.map((id, i) => (
                <g key={id}>
                  <Pulse from={HERO_HUB} to={node(id)} dur={0.8} delay={i * 0.05} color="#22D3EE" />
                  <Pulse from={HERO_HUB} to={node(id)} dur={0.8} delay={0.26 + i * 0.05} color="#8B5CF6" />
                </g>
              ))}
            </g>
          </svg>

          {/* orchestrator hub */}
          <div className={`cv-hub ${hubState}`} style={{ left: `${(HERO_HUB.x / W) * 100}%`, top: `${(HERO_HUB.y / H) * 100}%` }}>
            <span className="cv-hub-ring" aria-hidden />
            <b>Conduit</b>
            <small>orchestrator</small>
          </div>

          {HERO_NODES.map((n) => {
            const Icon = ICONS[n.id];
            const s = stateOf(n.id, step);
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
            {step === 4 && (
              <motion.div className="cv-think" key="think"
                initial={{ opacity: 0, y: 6 }} animate={{ opacity: 1, y: 0 }} exit={{ opacity: 0, y: -4 }}
                transition={{ duration: 0.25 }}>
                <span className="cv-think-dots" aria-hidden><i /><i /><i /></span> AI Agent Processing…
              </motion.div>
            )}
          </AnimatePresence>
        </div>

        {/* right: execution log + success toast */}
        <div className="cv-stage-r">
          <div className="cv-log" aria-live="off">
            <div className="cv-log-head">Execution log</div>
            <div className="cv-log-body">
              {lines.length === 0 && <div className="cv-log-line dim">waiting for trigger…</div>}
              {lines.slice(-6).map((l) => (
                <motion.div key={`${runNo}-${l.at}`} className={`cv-log-line ${l.tone || ""}`}
                  initial={{ opacity: 0, x: -6 }} animate={{ opacity: 1, x: 0 }} transition={{ duration: 0.25 }}>
                  <span className="t">{l.tag}</span>{l.text}
                </motion.div>
              ))}
            </div>
          </div>
          <div className="cv-toast-slot">
            <AnimatePresence>
              {step === DONE && (
                <motion.div className="cv-toast" key="toast"
                  initial={{ opacity: 0, y: 10, scale: 0.97 }} animate={{ opacity: 1, y: 0, scale: 1 }}
                  exit={{ opacity: 0, y: -6 }} transition={{ duration: 0.3 }}>
                  <span className="cv-float-ico ok"><Check size={14} /></span>
                  Workflow Executed Successfully
                </motion.div>
              )}
            </AnimatePresence>
          </div>
        </div>
      </div>
    </div>
  );
}
