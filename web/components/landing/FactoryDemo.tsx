"use client";
import { memo, useCallback, useEffect, useMemo, useRef, useState } from "react";
import {
  Handle, MarkerType, Position, ReactFlow,
  type Edge, type Node, type NodeProps, type ReactFlowInstance,
} from "@xyflow/react";
import { motion, useReducedMotion } from "framer-motion";
import {
  Bot, Check, GitPullRequest, Hand, Loader2, Play, RotateCcw, Send, TriangleAlert, UserCheck, Webhook, X,
} from "lucide-react";
import {
  DEMO_EDGES, DEMO_NODES, SAMPLE_CHANGES, SAMPLE_DIFF, SAMPLE_PLAN, SAMPLE_TICKETS,
  type DemoNodeDef, type DemoNodeId,
} from "./data";
import { DemoTag, SectionHead } from "./ui";

type Status = "idle" | "running" | "waiting" | "retry" | "done" | "failed";
type NodeData = { def: DemoNodeDef; status: Status; focused: boolean };
type Row = { key: number; label: string; state: "ok" | "fail"; note?: string };
type Phase = "ready" | "running" | "finished" | "stopped";
type Outcome = { tone: "ok" | "bad"; title: string; body: string };

const KIND_ICON = { trigger: Webhook, ai: Bot, action: Send, gate: UserCheck } as const;
const BY_ID = Object.fromEntries(DEMO_NODES.map((n) => [n.id, n])) as Record<DemoNodeId, DemoNodeDef>;
const IDLE = Object.fromEntries(DEMO_NODES.map((n) => [n.id, "idle"])) as Record<DemoNodeId, Status>;
const STATUS_LABEL: Record<Status, string> = {
  idle: "yet to start", running: "in progress", waiting: "awaiting approval",
  retry: "checks failed", done: "done", failed: "stopped",
};

const FactoryNode = memo(function FactoryNode({ data }: NodeProps) {
  const { def, status, focused } = data as NodeData;
  const Icon = KIND_ICON[def.kind];
  return (
    <div className={`cv-fn k-${def.kind} s-${status}${focused ? " focus" : ""}`}>
      <Handle type="target" position={Position.Top} className="cv-fh" isConnectable={false} />
      <span className="cv-fn-ico"><Icon size={15} /></span>
      <span className="cv-fn-txt"><b>{def.label}</b><small>{def.sub}</small></span>
      <span className="cv-fn-st" aria-hidden>
        {status === "running" && <Loader2 size={13} className="cv-spin" />}
        {status === "waiting" && <Hand size={13} />}
        {status === "retry" && <TriangleAlert size={13} />}
        {status === "done" && <Check size={13} />}
        {status === "failed" && <X size={13} />}
      </span>
      {(status === "running" || status === "retry" || status === "waiting") && <span className="cv-fn-bar" aria-hidden />}
      <Handle type="source" position={Position.Bottom} className="cv-fh" isConnectable={false} />
    </div>
  );
});
const NODE_TYPES = { factory: FactoryNode };

export function FactoryDemo() {
  const reduced = useReducedMotion();
  const [ticketId, setTicketId] = useState<"clear" | "vague">("clear");
  const [failCi, setFailCi] = useState(false);
  const [statuses, setStatuses] = useState<Record<DemoNodeId, Status>>(IDLE);
  const [timeline, setTimeline] = useState<Row[]>([]);
  const [focus, setFocus] = useState<DemoNodeId>("triage");
  const [phase, setPhase] = useState<Phase>("ready");
  const [gate, setGate] = useState<"appr" | "appr2" | "rdiff" | null>(null);
  const [outcome, setOutcome] = useState<Outcome | null>(null);
  const runRef = useRef(0);
  const rowRef = useRef(0);
  const gateRef = useRef<((ok: boolean) => void) | null>(null);
  const rfRef = useRef<ReactFlowInstance | null>(null);
  const wrapRef = useRef<HTMLDivElement>(null);

  const ticket = SAMPLE_TICKETS.find((t) => t.id === ticketId)!;
  const doneCount = Object.values(statuses).filter((s) => s === "done").length;

  const reset = useCallback(() => {
    runRef.current++;
    gateRef.current?.(false); gateRef.current = null;
    setStatuses(IDLE); setTimeline([]); setPhase("ready"); setGate(null); setOutcome(null);
  }, []);

  // Changing the scenario invalidates any run in flight.
  useEffect(() => { reset(); }, [ticketId, failCi, reset]);
  useEffect(() => () => { runRef.current++; }, []);

  useEffect(() => {
    const el = wrapRef.current;
    if (!el || typeof ResizeObserver === "undefined") return;
    const ro = new ResizeObserver(() => rfRef.current?.fitView({ padding: 0.06 }));
    ro.observe(el);
    return () => ro.disconnect();
  }, []);

  const decide = (ok: boolean) => { const r = gateRef.current; gateRef.current = null; r?.(ok); };

  const run = useCallback(async () => {
    reset();
    const my = runRef.current;
    const stale = () => runRef.current !== my;
    const wait = (ms: number) => new Promise<void>((r) => window.setTimeout(r, reduced ? 40 : ms));
    const set = (id: DemoNodeId, s: Status) => setStatuses((cur) => ({ ...cur, [id]: s }));
    const log = (label: string, state: Row["state"], note?: string) =>
      setTimeline((t) => [...t, { key: ++rowRef.current, label, state, note }]);
    const stop = (id: DemoNodeId, o: Outcome, note: string) => {
      set(id, "failed"); log(BY_ID[id].label, "fail", note); setOutcome(o); setPhase("stopped");
    };

    setPhase("running");

    // Run a normal step. Returns false if the demo was reset meanwhile.
    const exec = async (id: DemoNodeId, note?: string, label?: string) => {
      set(id, "running"); setFocus(id);
      await wait(BY_ID[id].ms); if (stale()) return false;
      set(id, "done"); log(label ?? BY_ID[id].label, "ok", note);
      return true;
    };
    // Pause at an approval gate until the visitor decides.
    const approve = async (id: "appr" | "appr2" | "rdiff") => {
      set(id, "waiting"); setFocus(id); setGate(id);
      const ok = await new Promise<boolean>((r) => { gateRef.current = r; });
      if (stale()) return null;
      setGate(null);
      return ok;
    };

    if (!(await exec("t1", ticket.key))) return;
    if (!(await exec("src", "summary + description loaded"))) return;

    set("triage", "running"); setFocus("triage");
    await wait(BY_ID.triage.ms); if (stale()) return;
    if (ticket.score < 60) {
      stop("triage", {
        tone: "bad", title: "Needs clarification",
        body: "The ticket scored below the automation threshold, so it was routed to a person. No agent wrote anything.",
      }, `confidence ${ticket.score} · below threshold 60`);
      return;
    }
    set("triage", "done"); log(BY_ID.triage.label, "ok", `confidence ${ticket.score} · ready to automate`);

    if (!(await exec("plan", `${SAMPLE_PLAN.length}-step plan`))) return;
    if (!(await exec("impl"))) return;
    if (!(await exec("review", "confidence: high"))) return;

    const a1 = await approve("appr");
    if (a1 === null) return;
    if (!a1) {
      stop("appr", { tone: "bad", title: "Needs attention", body: "You rejected the plan. The run stopped and nothing was written to the repository." }, "rejected by reviewer");
      return;
    }
    set("appr", "done"); log(BY_ID.appr.label, "ok", "plan approved by you");

    if (!(await exec("cplan", `${SAMPLE_CHANGES.length} files`))) return;

    const a2 = await approve("appr2");
    if (a2 === null) return;
    if (!a2) {
      stop("appr2", { tone: "bad", title: "Needs attention", body: "You rejected the file changes. No branch was created and nothing was written to the repository." }, "rejected by reviewer");
      return;
    }
    set("appr2", "done"); log(BY_ID.appr2.label, "ok", "file changes approved by you");

    // Write the code, then wait for the reviewer to read the diff before pushing.
    const writeAndReview = async (note: string, label?: string) => {
      if (!(await exec("apply", note, label))) return false;
      const ok = await approve("rdiff");
      if (ok === null) return false;
      if (!ok) {
        stop("rdiff", { tone: "bad", title: "Needs attention", body: "You rejected the code. Nothing was pushed to the repository." }, "rejected by reviewer");
        return false;
      }
      set("rdiff", "done"); log(BY_ID.rdiff.label, "ok", "diff approved by you");
      return exec("push", "pushed conduit/ENG-482");
    };
    if (!(await writeAndReview("4 files written · diff ready"))) return;

    if (failCi) {
      set("vchk", "running"); setFocus("vchk");
      await wait(BY_ID.vchk.ms); if (stale()) return;
      set("vchk", "retry"); log(BY_ID.vchk.label, "fail", "attempt 1 · check \"build\" failed — sent back to the coding agent");
      await wait(900); if (stale()) return;
      set("vchk", "idle"); set("rdiff", "idle"); set("push", "idle");
      if (!(await writeAndReview("fixed with CI feedback · diff ready", "Apply changes · attempt 2"))) return;
    }
    if (!(await exec("vchk", "all checks passed"))) return;
    if (!(await exec("pr", "pull request #129"))) return;
    if (!(await exec("post", "PR link + plan posted"))) return;

    setOutcome({
      tone: "ok", title: "Pull request #129 opened",
      body: "The PR link is posted back on the ticket, and the PR is waiting in the GitHub review workflow's inbox for agent review and your approval.",
    });
    setPhase("finished");
  }, [failCi, reduced, reset, ticket]);

  const nodes: Node[] = useMemo(
    () => DEMO_NODES.map((def) => ({
      id: def.id, type: "factory", position: { x: def.x, y: def.y },
      data: { def, status: statuses[def.id], focused: focus === def.id } satisfies NodeData,
      draggable: false, connectable: false,
    })),
    [statuses, focus],
  );

  const edges: Edge[] = useMemo(
    () => DEMO_EDGES.map((e) => {
      const s = statuses[e.from], t = statuses[e.to];
      const on = s === "done" && t !== "idle";
      return {
        id: `${e.from}-${e.to}`, source: e.from, target: e.to, type: "smoothstep",
        animated: on && t !== "done",
        className: on ? "cv-e-on" : "cv-e",
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
  const hint = gate ? "waiting for your approval" : phase === "running" ? "running…" : phase === "finished" ? "completed" : phase === "stopped" ? "stopped" : "press Run";

  return (
    <section className="cv-sec" id="demo">
      <SectionHead
        kicker="How it runs"
        title="Watch a ticket become a pull request."
        sub="This is the AI Factory workflow that ships with Conduit. Press Run — it will stop and wait for you at all three approval gates — the plan, the files and the diff — exactly as the real one does."
      />

      <motion.div className="cv-demo"
        initial={{ opacity: 0, y: 22 }} whileInView={{ opacity: 1, y: 0 }}
        viewport={{ once: true, margin: "-80px" }} transition={{ duration: 0.6 }}>
        <div className="cv-demo-bar">
          <div className="cv-demo-title">
            <b>AI Factory · Ticket → PR</b>
            <DemoTag>Simulation · sample ticket</DemoTag>
          </div>
          <div className="cv-seg" role="radiogroup" aria-label="Sample ticket">
            {SAMPLE_TICKETS.map((t) => (
              <button key={t.id} role="radio" aria-checked={ticketId === t.id} data-testid={`demo-ticket-${t.id}`}
                className={ticketId === t.id ? "on" : ""} onClick={() => setTicketId(t.id)}>
                {t.id === "clear" ? "Clear ticket" : "Vague ticket"} <span>{t.key}</span>
              </button>
            ))}
          </div>
          <label className="cv-check">
            <input type="checkbox" checked={failCi} onChange={(e) => setFailCi(e.target.checked)} />
            Simulate a failing CI check
          </label>
          <div className="cv-demo-run">
            {phase !== "ready" && (
              <button className="cv-btn ghost sm" onClick={reset} aria-label="Reset the run"><RotateCcw size={14} /> Reset</button>
            )}
            <button className="cv-btn primary sm" onClick={run} disabled={phase === "running"} data-testid="demo-run">
              {phase === "running"
                ? <><Loader2 size={14} className="cv-spin" /> {gate ? "Waiting for you…" : "Running…"}</>
                : <><Play size={14} /> {phase === "ready" ? "Run" : "Run again"}</>}
            </button>
          </div>
        </div>

        <div className="cv-demo-progress" aria-hidden>
          <i style={{ width: `${Math.min(100, (doneCount / DEMO_NODES.length) * 100)}%` }} />
        </div>

        <div className="cv-demo-main">
          <div className="cv-demo-canvas" ref={wrapRef}>
            <ReactFlow
              nodes={nodes} edges={edges} nodeTypes={NODE_TYPES}
              onInit={(inst) => { rfRef.current = inst; }}
              onNodeClick={(_, n) => setFocus(n.id as DemoNodeId)}
              fitView fitViewOptions={{ padding: 0.06 }}
              nodesDraggable={false} nodesConnectable={false} elementsSelectable={false}
              panOnDrag={false} panOnScroll={false} zoomOnScroll={false} zoomOnPinch={false}
              zoomOnDoubleClick={false} preventScrolling={false}
              minZoom={0.3} maxZoom={1}
              style={{ background: "transparent" }}
              aria-label="AI Factory workflow"
            />
          </div>

          <div className="cv-demo-side">
            <div className="cv-demo-detail">
              {gate ? (
                <motion.div className="cv-gate" key={gate} data-testid="demo-gate"
                  initial={{ opacity: 0, y: 8 }} animate={{ opacity: 1, y: 0 }} transition={{ duration: 0.25 }}>
                  <div className="cv-gate-h"><Hand size={15} /> {gate === "appr" ? "Approve the plan?" : gate === "appr2" ? "Approve these file changes?" : "Review the code before it's pushed"}</div>
                  <p className="cv-gate-t"><b>{ticket.key}</b> {ticket.title}</p>
                  {gate === "appr" ? (
                    <>
                      <ol className="cv-gate-plan">{SAMPLE_PLAN.map((p) => <li key={p}>{p}</li>)}</ol>
                      <p className="cv-gate-note">Triage confidence {ticket.score} · review agent: confidence high</p>
                    </>
                  ) : gate === "rdiff" ? (
                    <>
                      <div className="cv-gate-diff">
                        {SAMPLE_DIFF.map((f) => (
                          <div key={f.file}>
                            <code className="f">{f.file}</code>
                            {f.lines.map(([k, t], i) => <code key={i} className={k === "+" ? "a" : k === "-" ? "d" : ""}>{k} {t}</code>)}
                          </div>
                        ))}
                      </div>
                      <p className="cv-gate-note">4 files · +31 −2 · not pushed yet. Approving pushes exactly this change.</p>
                    </>
                  ) : (
                    <>
                      <ul className="cv-gate-files">
                        {SAMPLE_CHANGES.map((c) => (
                          <li key={c.path}><em className={c.action}>{c.action}</em><code>{c.path}</code><span>{c.why}</span></li>
                        ))}
                      </ul>
                      <p className="cv-gate-note">Nothing is written to the repository until you approve.</p>
                    </>
                  )}
                  <div className="cv-gate-b">
                    <button className="ok" onClick={() => decide(true)} data-testid="demo-approve"><Check size={14} /> {gate === "rdiff" ? "Approve & push" : "Approve"}</button>
                    <button onClick={() => decide(false)} data-testid="demo-reject"><X size={14} /> Reject</button>
                  </div>
                </motion.div>
              ) : (
                <>
                  <div className="cv-panel-h">Step details <small>click any step</small></div>
                  <div className={`cv-detail-card k-${focused.kind}`}>
                    <div className="cv-detail-top">
                      <b>{focused.label}</b>
                      <span className={`cv-st s-${statuses[focus]}`}>{STATUS_LABEL[statuses[focus]]}</span>
                    </div>
                    <p>{focused.desc}</p>
                    <dl>
                      <dt>type</dt><dd>{focused.sub}</dd>
                      <dt>out</dt><dd>{focused.output}</dd>
                      {focused.model && <><dt>model</dt><dd>{focused.model}</dd></>}
                    </dl>
                  </div>
                  {outcome ? (
                    <motion.div className={`cv-outcome ${outcome.tone}`} data-testid="demo-outcome"
                      initial={{ opacity: 0, y: 6 }} animate={{ opacity: 1, y: 0 }}>
                      <b>{outcome.tone === "ok" ? <GitPullRequest size={15} /> : <TriangleAlert size={15} />} {outcome.title}</b>
                      <p>{outcome.body}</p>
                    </motion.div>
                  ) : (
                    <div className="cv-lead-card">
                      <span>Sample ticket · {ticket.key}</span>
                      <b>{ticket.title}</b>
                      <q>{ticket.body}</q>
                    </div>
                  )}
                </>
              )}
            </div>

            <div className="cv-demo-timeline">
              <div className="cv-panel-h">Run timeline <small>{hint}</small></div>
              <ol className="cv-tl" aria-live="polite">
                {timeline.length === 0 && <li className="empty">No steps yet. The timeline fills in as the run progresses.</li>}
                {timeline.map((r) => (
                  <motion.li key={r.key} className={r.state}
                    initial={{ opacity: 0, x: -8 }} animate={{ opacity: 1, x: 0 }} transition={{ duration: 0.22 }}>
                    <span className="dot">{r.state === "ok" ? <Check size={11} /> : <X size={11} />}</span>
                    <span className="lbl">{r.label}{r.note && <em>{r.note}</em>}</span>
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
