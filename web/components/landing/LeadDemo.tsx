"use client";
import { memo, useCallback, useEffect, useMemo, useRef, useState } from "react";
import {
  Handle, MarkerType, Position, ReactFlow,
  type Edge, type Node, type NodeProps, type ReactFlowInstance,
} from "@xyflow/react";
import { motion, useReducedMotion } from "framer-motion";
import { Bot, Check, GitBranch, Loader2, Play, RotateCcw, Send, TriangleAlert, Webhook, X } from "lucide-react";
import {
  LEAD_BRANCH, LEAD_CHAIN, LEAD_EDGES, LEAD_NODES, SAMPLE_LEADS,
  type LeadNodeDef, type LeadNodeId,
} from "./data";
import { DemoTag, SectionHead } from "./ui";

type Status = "idle" | "running" | "retry" | "done" | "skipped";
type LeadNodeData = { def: LeadNodeDef; status: Status; focused: boolean };
type TimelineRow = { key: string; label: string; state: "ok" | "fail"; ms: number; note?: string };

const KIND_ICON = { trigger: Webhook, ai: Bot, action: Send, logic: GitBranch } as const;
const BY_ID = Object.fromEntries(LEAD_NODES.map((n) => [n.id, n])) as Record<LeadNodeId, LeadNodeDef>;
const IDLE = Object.fromEntries(LEAD_NODES.map((n) => [n.id, "idle"])) as Record<LeadNodeId, Status>;

const LeadNode = memo(function LeadNode({ data }: NodeProps) {
  const { def, status, focused } = data as LeadNodeData;
  const Icon = KIND_ICON[def.kind];
  return (
    <div className={`cv-fn k-${def.kind} s-${status}${focused ? " focus" : ""}`}>
      <Handle type="target" position={Position.Top} className="cv-fh" isConnectable={false} />
      <span className="cv-fn-ico"><Icon size={15} /></span>
      <span className="cv-fn-txt"><b>{def.label}</b><small>{def.sub}</small></span>
      <span className="cv-fn-st" aria-hidden>
        {status === "running" && <Loader2 size={13} className="cv-spin" />}
        {status === "retry" && <TriangleAlert size={13} />}
        {status === "done" && <Check size={13} />}
        {status === "skipped" && <X size={12} />}
      </span>
      {(status === "running" || status === "retry") && <span className="cv-fn-bar" aria-hidden />}
      <Handle type="source" position={Position.Bottom} className="cv-fh" isConnectable={false} />
    </div>
  );
});
const NODE_TYPES = { lead: LeadNode };

export function LeadDemo() {
  const reduced = useReducedMotion();
  const [leadId, setLeadId] = useState<"high" | "low">("high");
  const [injectError, setInjectError] = useState(false);
  const [statuses, setStatuses] = useState<Record<LeadNodeId, Status>>(IDLE);
  const [timeline, setTimeline] = useState<TimelineRow[]>([]);
  const [focus, setFocus] = useState<LeadNodeId>("analyze");
  const [phase, setPhase] = useState<"ready" | "running" | "finished">("ready");
  const runRef = useRef(0);
  const rfRef = useRef<ReactFlowInstance | null>(null);
  const wrapRef = useRef<HTMLDivElement>(null);

  const lead = SAMPLE_LEADS.find((l) => l.id === leadId)!;
  const planned = LEAD_CHAIN.length + LEAD_BRANCH[leadId].length;
  const doneCount = Object.values(statuses).filter((s) => s === "done").length;

  const reset = useCallback(() => {
    runRef.current++;
    setStatuses(IDLE); setTimeline([]); setPhase("ready");
  }, []);

  // Changing the scenario invalidates any run in flight.
  useEffect(() => { reset(); }, [leadId, injectError, reset]);
  useEffect(() => () => { runRef.current++; }, []);

  // Keep the whole graph in view as the container resizes.
  useEffect(() => {
    const el = wrapRef.current;
    if (!el || typeof ResizeObserver === "undefined") return;
    const ro = new ResizeObserver(() => rfRef.current?.fitView({ padding: 0.06 }));
    ro.observe(el);
    return () => ro.disconnect();
  }, []);

  const run = useCallback(async () => {
    const my = ++runRef.current;
    const stale = () => runRef.current !== my;
    const wait = (ms: number) => new Promise<void>((r) => window.setTimeout(r, reduced ? 40 : ms));
    const set = (id: LeadNodeId, s: Status) => setStatuses((cur) => ({ ...cur, [id]: s }));

    setStatuses(IDLE); setTimeline([]); setPhase("running");

    const exec = async (id: LeadNodeId) => {
      const def = BY_ID[id];
      set(id, "running"); setFocus(id);
      let ms = def.ms;
      if (id === "enrich" && injectError) {
        await wait(def.ms * 0.7); if (stale()) return false;
        set(id, "retry");
        setTimeline((t) => [...t, { key: `${id}-fail`, label: def.label, state: "fail", ms: Math.round(def.ms * 0.7), note: "attempt 1 · 503 from enrichment API — retrying" }]);
        await wait(800); if (stale()) return false;
        set(id, "running");
        ms = def.ms + 180;
      }
      await wait(ms); if (stale()) return false;
      set(id, "done");
      const note = id === "score" ? `score ${lead.score}` : id === "decide" ? (lead.score >= 70 ? "high priority" : "low priority") : undefined;
      setTimeline((t) => [...t, { key: id, label: def.label, state: "ok", ms, note }]);
      return true;
    };

    for (const id of LEAD_CHAIN) if (!(await exec(id))) return;
    const taken = lead.id, other = taken === "high" ? "low" : "high";
    setStatuses((cur) => ({ ...cur, ...Object.fromEntries(LEAD_BRANCH[other].map((id) => [id, "skipped"])) }));
    for (const id of LEAD_BRANCH[taken]) if (!(await exec(id))) return;
    if (!stale()) setPhase("finished");
  }, [injectError, lead, reduced]);

  const nodes: Node[] = useMemo(
    () => LEAD_NODES.map((def) => ({
      id: def.id, type: "lead", position: { x: def.x, y: def.y },
      data: { def, status: statuses[def.id], focused: focus === def.id } satisfies LeadNodeData,
      draggable: false, connectable: false,
    })),
    [statuses, focus],
  );

  const edges: Edge[] = useMemo(
    () => LEAD_EDGES.map((e) => {
      const s = statuses[e.from], t = statuses[e.to];
      const on = s === "done" && (t === "running" || t === "retry" || t === "done");
      const skip = t === "skipped";
      return {
        id: `${e.from}-${e.to}`, source: e.from, target: e.to, type: "smoothstep",
        animated: on && t !== "done",
        className: on ? "cv-e-on" : skip ? "cv-e-skip" : "cv-e",
        markerEnd: { type: MarkerType.ArrowClosed, width: 14, height: 14, color: on ? "#8B5CF6" : "#475569" },
        label: e.label,
        labelStyle: { fill: "var(--cv-muted)", fontSize: 11, fontWeight: 600 },
        labelBgStyle: { fill: "var(--cv-s1)" },
        labelBgPadding: [6, 3] as [number, number],
        labelBgBorderRadius: 6,
      };
    }),
    [statuses],
  );

  const focused = BY_ID[focus];
  const totalMs = timeline.filter((r) => r.state === "ok").reduce((a, r) => a + r.ms, 0);

  return (
    <section className="cv-sec" id="demo">
      <SectionHead
        kicker="Live workflow"
        title="See AI automation in action."
        sub="A real workflow, not a diagram: pick a lead, press Run, and watch every step execute, branch and finish."
      />

      <motion.div className="cv-demo"
        initial={{ opacity: 0, y: 22 }} whileInView={{ opacity: 1, y: 0 }}
        viewport={{ once: true, margin: "-80px" }} transition={{ duration: 0.6 }}>
        <div className="cv-demo-bar">
          <div className="cv-demo-title">
            <b>Automated lead management</b>
            <DemoTag>Demo · sample data</DemoTag>
          </div>
          <div className="cv-seg" role="radiogroup" aria-label="Sample lead">
            {SAMPLE_LEADS.map((l) => (
              <button key={l.id} role="radio" aria-checked={leadId === l.id}
                className={leadId === l.id ? "on" : ""} onClick={() => setLeadId(l.id)}>
                {l.company} <span>score {l.score}</span>
              </button>
            ))}
          </div>
          <label className="cv-check">
            <input type="checkbox" checked={injectError} onChange={(e) => setInjectError(e.target.checked)} />
            Simulate an API error
          </label>
          <div className="cv-demo-run">
            {phase !== "ready" && (
              <button className="cv-btn ghost sm" onClick={reset} aria-label="Reset workflow"><RotateCcw size={14} /> Reset</button>
            )}
            <button className="cv-btn primary sm" onClick={run} disabled={phase === "running"} data-testid="demo-run">
              {phase === "running" ? <><Loader2 size={14} className="cv-spin" /> Running…</> : <><Play size={14} /> {phase === "finished" ? "Run again" : "Run Workflow"}</>}
            </button>
          </div>
        </div>

        <div className="cv-demo-progress" aria-hidden>
          <i style={{ width: `${Math.min(100, (doneCount / planned) * 100)}%` }} />
        </div>

        <div className="cv-demo-main">
          <div className="cv-demo-canvas" ref={wrapRef}>
            <ReactFlow
              nodes={nodes} edges={edges} nodeTypes={NODE_TYPES}
              onInit={(inst) => { rfRef.current = inst; }}
              onNodeClick={(_, n) => setFocus(n.id as LeadNodeId)}
              fitView fitViewOptions={{ padding: 0.06 }}
              nodesDraggable={false} nodesConnectable={false} elementsSelectable={false}
              panOnDrag={false} panOnScroll={false} zoomOnScroll={false} zoomOnPinch={false}
              zoomOnDoubleClick={false} preventScrolling={false}
              minZoom={0.3} maxZoom={1}
              style={{ background: "transparent" }}
              aria-label="Lead management workflow"
            />
          </div>

          <div className="cv-demo-side">
          <div className="cv-demo-detail">
            <div className="cv-panel-h">Step details <small>click any step</small></div>
            <div className={`cv-detail-card k-${focused.kind}`}>
              <div className="cv-detail-top">
                <b>{focused.label}</b>
                <span className={`cv-st s-${statuses[focus]}`}>{statuses[focus]}</span>
              </div>
              <p>{focused.desc}</p>
              <dl>
                <dt>in</dt><dd>{focused.input}</dd>
                <dt>out</dt><dd>{focus === "score" && statuses.score === "done" ? `{ score: ${lead.score} }` : focused.output}</dd>
              </dl>
            </div>
            <div className="cv-lead-card">
              <span>Sample lead</span>
              <b>{lead.name} · {lead.role}</b>
              <q>{lead.message}</q>
            </div>
          </div>

          <div className="cv-demo-timeline">
            <div className="cv-panel-h">
              Execution timeline
              <small>{phase === "finished" ? `completed in ${(totalMs / 1000).toFixed(1)}s` : phase === "running" ? "running…" : "press Run Workflow"}</small>
            </div>
            <ol className="cv-tl" aria-live="polite">
              {timeline.length === 0 && <li className="empty">No steps yet. The timeline fills in as the workflow runs.</li>}
              {timeline.map((r) => (
                <motion.li key={r.key} className={r.state}
                  initial={{ opacity: 0, x: -8 }} animate={{ opacity: 1, x: 0 }} transition={{ duration: 0.22 }}>
                  <span className="dot">{r.state === "ok" ? <Check size={11} /> : <X size={11} />}</span>
                  <span className="lbl">{r.label}{r.note && <em>{r.note}</em>}</span>
                  <span className="ms">{r.ms} ms</span>
                </motion.li>
              ))}
            </ol>
          </div>
          </div>
        </div>
      </motion.div>
    </section>
  );
}
