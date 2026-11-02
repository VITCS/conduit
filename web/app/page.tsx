"use client";

import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import {
  ReactFlow,
  ReactFlowProvider,
  Background,
  BackgroundVariant,
  Controls,
  MiniMap,
  MarkerType,
  addEdge,
  useNodesState,
  useEdgesState,
  useReactFlow,
  type Node,
  type Edge,
  type Connection,
  type NodeTypes,
  type EdgeTypes,
} from "@xyflow/react";
import { Sparkles } from "lucide-react";

import { ConduitNode } from "@/components/ConduitNode";
import { ConduitEdge } from "@/components/ConduitEdge";
import { Inspector } from "@/components/Inspector";
import { Topbar } from "@/components/Topbar";
import { RunRail } from "@/components/RunRail";
import { Palette } from "@/components/Palette";
import { SecretsPanel } from "@/components/SecretsPanel";
import { ConfigPanel } from "@/components/ConfigPanel";
import { MembersPanel } from "@/components/MembersPanel";
import { Landing } from "@/components/Landing";
import { Auth } from "@/components/Auth";
import { EditorContext } from "@/lib/editorContext";
import { api, getToken, setToken, setOnUnauthorized, getProject, setProject } from "@/lib/api";
import { type CatalogItem } from "@/lib/catalog";
import type { EdgeData, NodeData, Run, SSEvent, Status, Workflow, WFNode } from "@/lib/types";

const nodeTypes: NodeTypes = { conduit: ConduitNode };
const edgeTypes: EdgeTypes = { conduit: ConduitEdge };
const OK = (s?: Status) => s === "success" || s === "created";

const edgeId = (from: string, to: string) => `${from}__${to}`;

function wfToNodes(wf: Workflow): Node[] {
  return wf.nodes.map((n) => ({
    id: n.id,
    type: "conduit",
    position: { x: n.x, y: n.y },
    data: { title: n.title, typeName: n.type, icon: n.icon, kind: n.kind, config: n.config, status: "pending" } as NodeData,
  }));
}
function wfToEdges(wf: Workflow): Edge[] {
  return wf.edges.map((e) => ({
    id: edgeId(e.from, e.to),
    source: e.from,
    target: e.to,
    type: "conduit",
    data: { label: e.label ?? "" } as EdgeData,
    markerEnd: { type: MarkerType.ArrowClosed, width: 16, height: 16 },
  }));
}

type Tab = "canvas" | "console" | "memory" | "files";
interface LogLine { t: string; level: string; msg: string; }

function FlowApp({ onSignOut }: { onSignOut: () => void }) {
  const rf = useReactFlow();

  const [user, setUser] = useState<{ name: string; email: string; role: string } | null>(null);
  const [wf, setWf] = useState<Workflow | null>(null);
  const [name, setName] = useState("Loading…");
  const [nodes, setNodes, onNodesChange] = useNodesState<Node>([]);
  const [edges, setEdges, onEdgesChange] = useEdgesState<Edge>([]);

  const [statuses, setStatuses] = useState<Record<string, Status>>({});
  const [outputs, setOutputs] = useState<Record<string, Record<string, unknown>>>({});
  const [memory, setMemory] = useState<Record<string, unknown>>({});
  const [logs, setLogs] = useState<LogLine[]>([]);

  const [editMode, setEditMode] = useState(false);
  const [dirty, setDirty] = useState(false);
  const [selected, setSelected] = useState<string | null>(null);
  const [tab, setTab] = useState<Tab>("canvas");

  const [runId, setRunId] = useState<string | null>(null);
  const [running, setRunning] = useState(false);
  const [waitingNode, setWaitingNode] = useState<string | null>(null);
  const [conn, setConn] = useState("idle");
  const [runLabel, setRunLabel] = useState("No run yet");
  const [runMeta, setRunMeta] = useState("press Run to execute");
  const [railHidden, setRailHidden] = useState(false);
  const [showHistory, setShowHistory] = useState(false);
  const [showSecrets, setShowSecrets] = useState(false);
  const [showConfig, setShowConfig] = useState(false);
  const [showMembers, setShowMembers] = useState(false);
  const [runs, setRuns] = useState<Run[]>([]);
  const [projects, setProjects] = useState<{ id: string; name: string }[]>([]);
  const [currentProject, setCurrentProject] = useState("default");
  const [toast, setToast] = useState("");
  const esRef = useRef<EventSource | null>(null);
  const seq = useRef(1000);

  const showToast = useCallback((m: string) => {
    setToast(m);
    window.setTimeout(() => setToast((cur) => (cur === m ? "" : cur)), 1900);
  }, []);

  // Best-effort desktop notification for approvals (in addition to the in-app
  // banner), so a reviewer is alerted even when the tab is in the background.
  const notifyApproval = useCallback((body: string) => {
    try {
      if (typeof Notification === "undefined") return;
      if (Notification.permission === "granted") {
        new Notification("Conduit Agent — approval required", { body });
      } else if (Notification.permission === "default") {
        Notification.requestPermission().then((p) => {
          if (p === "granted") new Notification("Conduit Agent — approval required", { body });
        });
      }
    } catch {
      /* notifications unavailable; the in-app banner still shows */
    }
  }, []);
  const markDirty = useCallback(() => setDirty(true), []);

  // --- load the current project's workflow (creating a starter if empty) ---
  const loadWorkflows = useCallback(async () => {
    try {
      const wfs = await api.listWorkflows();
      let w = wfs[0];
      if (!w) {
        // A fresh project has no workflow yet — create a usable starter.
        w = await api.putWorkflow({
          id: `wf_${getProject()}_main`,
          project_id: getProject(),
          name: "New workflow",
          nodes: [{ id: "t1", type: "webhook", title: "Start", icon: "webhook", kind: "trigger", x: 80, y: 200, config: [] }],
          edges: [],
        });
      }
      setWf(w);
      setName(w.name);
      setNodes(wfToNodes(w));
      setEdges(wfToEdges(w));
      setStatuses(Object.fromEntries(w.nodes.map((n) => [n.id, "pending" as Status])));
      setDirty(false);
      setTimeout(() => rf.fitView({ padding: 0.2, duration: 300 }), 60);
    } catch (e) {
      showToast("Load failed: " + (e as Error).message);
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [rf, showToast]);

  // --- initial load: user, projects, workflow ---
  useEffect(() => {
    api.me().then(setUser).catch(() => {});
    (async () => {
      let ps: { id: string; name: string }[] = [];
      try {
        ps = await api.listProjects();
      } catch {
        /* ignore — fall back below */
      }
      if (!ps.length) ps = [{ id: "default", name: "Default" }];
      setProjects(ps);
      // Make sure the selected project is one the caller can actually access;
      // otherwise fall back to the first visible project.
      const cur = getProject();
      const eff = ps.some((p) => p.id === cur) ? cur : ps[0].id;
      if (eff !== cur) setProject(eff);
      setCurrentProject(eff);
      await loadWorkflows();
    })();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  const switchProject = useCallback(
    (id: string) => {
      setProject(id);
      setCurrentProject(id);
      setSelected(null);
      loadWorkflows();
    },
    [loadWorkflows],
  );

  const newProject = useCallback(async () => {
    const name = window.prompt("New project name");
    if (!name) return;
    try {
      const p = await api.createProject(name);
      setProjects(await api.listProjects());
      switchProject(p.id);
      showToast("Project created");
    } catch (e) {
      showToast("Create failed: " + (e as Error).message);
    }
  }, [switchProject, showToast]);

  // --- reflect statuses onto node data + edge styling ---
  useEffect(() => {
    setNodes((ns) =>
      ns.map((n) => {
        const st = statuses[n.id] ?? "pending";
        const d = n.data as NodeData;
        return d.status === st ? n : { ...n, data: { ...d, status: st } };
      }),
    );
    setEdges((es) =>
      es.map((e) => {
        const sa = statuses[e.source];
        const sb = statuses[e.target];
        const label = (e.data as EdgeData | undefined)?.label ?? "";
        let cls = "";
        let animated = false;
        if (label === "failed") {
          if (sa === "failed") cls = "fail";
        } else if (OK(sa) && (sb === "running" || sb === "waiting")) {
          cls = "flow";
          animated = true;
        } else if (OK(sa) && (OK(sb) || sb === "triggered")) {
          cls = "ok";
        }
        return e.className === cls && e.animated === animated ? e : { ...e, className: cls, animated };
      }),
    );
  }, [statuses, setNodes, setEdges]);

  // --- run + SSE ---
  const runWorkflow = useCallback(async () => {
    if (running || !wf) return;
    setRunning(true);
    setStatuses(Object.fromEntries(nodes.map((n) => [n.id, "pending" as Status])));
    setOutputs({});
    setMemory({});
    setLogs([]);
    setWaitingNode(null);
    try {
      const run = await api.createRun(wf.id);
      setRunId(run.id);
      setRunLabel(run.title);
      setRunMeta(run.id);
      openStream(run.id);
    } catch (e) {
      setRunning(false);
      showToast("Run failed: " + (e as Error).message);
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [running, wf, nodes, showToast]);

  // openStream subscribes to a run's live events (used by Run and when opening an
  // in-progress run from history). It drives step statuses, logs, the approval
  // banner, and finish/reconcile — so a webhook-triggered run can be approved here.
  const openStream = useCallback((runId: string) => {
    const addLog = (level: string, msg: string) =>
      setLogs((l) => [...l, { t: new Date().toLocaleTimeString("en-GB", { hour12: false }), level, msg }]);
    esRef.current?.close();
    const es = new EventSource(api.eventsURL(runId));
    esRef.current = es;
    setConn("live");
    es.addEventListener("run.started", () => addLog("info", "Run " + runId + " started"));
    es.addEventListener("step.updated", (ev) => {
      const d: SSEvent = JSON.parse((ev as MessageEvent).data);
      if (d.node_id && d.status) setStatuses((s) => ({ ...s, [d.node_id!]: d.status! }));
      if (d.node_id && d.step?.output) setOutputs((o) => ({ ...o, [d.node_id!]: d.step!.output! }));
    });
    es.addEventListener("log", (ev) => {
      const d: SSEvent = JSON.parse((ev as MessageEvent).data);
      addLog(d.level || "info", d.message || "");
    });
    es.addEventListener("run.waiting", (ev) => {
      const d: SSEvent = JSON.parse((ev as MessageEvent).data);
      if (d.node_id) {
        setStatuses((s) => ({ ...s, [d.node_id!]: "waiting" }));
        setWaitingNode(d.node_id);
        setSelected(d.node_id);
      }
      addLog("info", d.message || "awaiting approval");
      showToast("Approval required");
      notifyApproval(d.message || "A run is waiting for your approval.");
      // Load memory/outputs now so the agent review + diff are visible while the
      // run waits for a decision (not only after it finishes).
      api.getRun(runId).then((full) => applyRun(full)).catch(() => {});
    });
    es.addEventListener("run.finished", (ev) => {
      const d: SSEvent = JSON.parse((ev as MessageEvent).data);
      setRunning(false);
      setWaitingNode(null);
      setConn(d.status || "done");
      es.close();
      esRef.current = null;
      addLog(d.status === "failed" ? "fail" : "ok", "Run " + d.status);
      api.getRun(runId).then((full) => applyRun(full));
      showToast("Run " + d.status);
    });
    es.onerror = () => {
      setConn("reconnecting");
      reconcileRun(runId);
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [showToast, notifyApproval]);

  function applyRun(full: Run) {
    const st: Record<string, Status> = {};
    const out: Record<string, Record<string, unknown>> = {};
    Object.entries(full.steps || {}).forEach(([id, s]) => {
      st[id] = s.status;
      if (s.output) out[id] = s.output;
    });
    setStatuses((cur) => ({ ...cur, ...st }));
    setOutputs((cur) => ({ ...cur, ...out }));
    setMemory(full.memory || {});
  }

  const TERMINAL: Status[] = ["success", "failed", "canceled", "created", "skipped"];

  // reconcileRun pulls the authoritative run state and reflects it in the UI. Used
  // when the SSE stream drops or right after cancel, so the Run/Cancel button and
  // the approval banner never get stuck out of sync with the backend.
  const reconcileRun = useCallback(
    async (id: string) => {
      try {
        const full = await api.getRun(id);
        applyRun(full);
        if (TERMINAL.includes(full.status)) {
          setRunning(false);
          setWaitingNode(null);
          setConn(full.status);
          esRef.current?.close();
          esRef.current = null;
        } else if (full.status === "waiting") {
          const wn = Object.entries(full.steps || {}).find(([, s]) => s.status === "waiting")?.[0] || null;
          setRunning(true);
          if (wn) setWaitingNode(wn);
        } else {
          setRunning(true);
        }
      } catch {
        /* leave current state; a later event or manual retry will reconcile */
      }
    },
    // eslint-disable-next-line react-hooks/exhaustive-deps
    [],
  );

  const cancelRun = useCallback(async () => {
    if (!runId) return;
    // Optimistically leave the running state so the button recovers immediately,
    // then reconcile against the backend's final status.
    setRunning(false);
    setWaitingNode(null);
    setConn("canceled");
    esRef.current?.close();
    esRef.current = null;
    try {
      await api.cancelRun(runId);
      showToast("Run canceled");
    } catch (e) {
      showToast("Cancel failed: " + (e as Error).message);
    }
    reconcileRun(runId);
  }, [runId, showToast, reconcileRun]);

  const approve = useCallback(
    async (decision: "approved" | "rejected") => {
      if (!runId || !waitingNode) return;
      try {
        await api.approve(runId, waitingNode, decision);
        showToast(decision === "approved" ? "Approved" : "Rejected");
        setWaitingNode(null);
      } catch (e) {
        showToast("Approve failed: " + (e as Error).message);
      }
    },
    [runId, waitingNode, showToast],
  );

  // --- editor actions ---
  const onConnect = useCallback(
    (c: Connection) => {
      if (!c.source || !c.target || c.source === c.target) return;
      setEdges((es) => {
        if (es.some((e) => e.source === c.source && e.target === c.target)) return es;
        markDirty();
        return addEdge(
          {
            id: edgeId(c.source!, c.target!),
            source: c.source!,
            target: c.target!,
            type: "conduit",
            data: { label: "" } as EdgeData,
            markerEnd: { type: MarkerType.ArrowClosed, width: 16, height: 16 },
          },
          es,
        );
      });
    },
    [setEdges, markDirty],
  );

  const setEdgeLabel = useCallback(
    (id: string, label: string) => {
      setEdges((es) => es.map((e) => (e.id === id ? { ...e, data: { ...(e.data as EdgeData), label } } : e)));
      markDirty();
    },
    [setEdges, markDirty],
  );
  const deleteEdge = useCallback(
    (id: string) => {
      setEdges((es) => es.filter((e) => e.id !== id));
      markDirty();
      showToast("Edge deleted");
    },
    [setEdges, markDirty, showToast],
  );

  const addComponent = useCallback(
    (c: CatalogItem) => {
      const id = "n" + ++seq.current;
      const center = rf.screenToFlowPosition({ x: window.innerWidth / 2, y: window.innerHeight / 2 });
      const node: Node = {
        id,
        type: "conduit",
        position: { x: Math.round(center.x) - 122, y: Math.round(center.y) - 60 },
        data: {
          title: c.name,
          typeName: c.type,
          icon: c.icon,
          kind: c.kind,
          config: [{ key: "status", icon: "", value: "not configured" }],
          status: "pending",
        } as NodeData,
      };
      setNodes((ns) => [...ns, node]);
      setStatuses((s) => ({ ...s, [id]: "pending" }));
      setSelected(id);
      markDirty();
      showToast("Added " + c.name);
    },
    [rf, setNodes, markDirty, showToast],
  );

  const deleteNode = useCallback(
    (id: string) => {
      setNodes((ns) => ns.filter((n) => n.id !== id));
      setEdges((es) => es.filter((e) => e.source !== id && e.target !== id));
      setSelected((cur) => (cur === id ? null : cur));
      markDirty();
      showToast("Node deleted");
    },
    [setNodes, setEdges, markDirty, showToast],
  );

  const patchNode = useCallback(
    (id: string, patch: Partial<NodeData>) => {
      setNodes((ns) => ns.map((n) => (n.id === id ? { ...n, data: { ...(n.data as NodeData), ...patch } } : n)));
      markDirty();
    },
    [setNodes, markDirty],
  );

  const save = useCallback(async () => {
    if (!wf) return;
    const payload: Workflow = {
      id: wf.id,
      name: name.trim() || wf.name,
      nodes: nodes.map((n): WFNode => {
        const d = n.data as NodeData;
        return {
          id: n.id,
          type: d.typeName,
          title: d.title,
          icon: d.icon,
          kind: d.kind,
          x: Math.round(n.position.x),
          y: Math.round(n.position.y),
          config: d.config,
        };
      }),
      edges: edges.map((e) => {
        const label = (e.data as EdgeData | undefined)?.label;
        return label ? { from: e.source, to: e.target, label } : { from: e.source, to: e.target };
      }),
    };
    try {
      const saved = await api.putWorkflow(payload);
      setWf(saved);
      setDirty(false);
      showToast("Workflow saved");
    } catch (e) {
      showToast("Save failed: " + (e as Error).message);
    }
  }, [wf, name, nodes, edges, showToast]);

  // keyboard delete for selected node (edit mode)
  useEffect(() => {
    const h = (e: KeyboardEvent) => {
      if (!editMode || !selected) return;
      const tag = (document.activeElement?.tagName || "").toUpperCase();
      if (tag === "INPUT" || tag === "TEXTAREA" || tag === "SELECT") return;
      if (e.key === "Delete" || e.key === "Backspace") {
        e.preventDefault();
        deleteNode(selected);
      }
    };
    window.addEventListener("keydown", h);
    return () => window.removeEventListener("keydown", h);
  }, [editMode, selected, deleteNode]);

  // warn on unsaved changes
  useEffect(() => {
    const h = (e: BeforeUnloadEvent) => {
      if (dirty) {
        e.preventDefault();
        e.returnValue = "";
      }
    };
    window.addEventListener("beforeunload", h);
    return () => window.removeEventListener("beforeunload", h);
  }, [dirty]);

  const orderedNodes: WFNode[] = useMemo(
    () =>
      nodes.map((n) => {
        const d = n.data as NodeData;
        return { id: n.id, type: d.typeName, title: d.title, icon: d.icon, kind: d.kind, x: n.position.x, y: n.position.y, config: d.config };
      }),
    [nodes],
  );

  const selectedNode = nodes.find((n) => n.id === selected);
  const selectedData = selectedNode ? (selectedNode.data as NodeData) : null;

  const focusNode = useCallback(
    (id: string) => {
      setSelected(id);
      const n = nodes.find((x) => x.id === id);
      if (n) rf.setCenter(n.position.x + 122, n.position.y + 70, { zoom: 1, duration: 300 });
    },
    [nodes, rf],
  );

  const toggleHistory = useCallback(async () => {
    const next = !showHistory;
    setShowHistory(next);
    if (next) {
      try {
        setRuns(await api.listRuns());
      } catch (e) {
        showToast("Could not load runs: " + (e as Error).message);
      }
    }
  }, [showHistory, showToast]);

  const pickRun = useCallback(
    async (id: string) => {
      try {
        const full = await api.getRun(id);
        setRunId(id);
        setRunLabel(full.title);
        setRunMeta(full.id);
        applyRun(full);
        setShowHistory(false);
        setTab("canvas");
        // If the run is still in flight (e.g. a webhook-triggered run waiting at an
        // approval gate), re-attach the live stream so the approval banner + Approve
        // button appear and updates flow. Otherwise leave it as a static replay.
        if (!TERMINAL.includes(full.status)) {
          const wn = Object.entries(full.steps || {}).find(([, s]) => s.status === "waiting")?.[0] || null;
          setRunning(true);
          if (wn) { setWaitingNode(wn); setSelected(wn); }
          openStream(id);
        } else {
          setRunning(false);
          setWaitingNode(null);
        }
      } catch (e) {
        showToast("Could not load run: " + (e as Error).message);
      }
    },
    // eslint-disable-next-line react-hooks/exhaustive-deps
    [showToast],
  );

  const stepRel = useCallback(
    (delta: number) => {
      if (!selected) return;
      const i = nodes.findIndex((n) => n.id === selected);
      if (i < 0) return;
      focusNode(nodes[(i + delta + nodes.length) % nodes.length].id);
    },
    [selected, nodes, focusNode],
  );

  return (
    <EditorContext.Provider value={{ editMode, setEdgeLabel, deleteEdge }}>
      <div className="app">
        <Topbar
          name={name}
          onName={(v) => {
            setName(v);
            markDirty();
          }}
          onRun={runWorkflow}
          onCancel={cancelRun}
          running={running}
          editMode={editMode}
          onToggleEdit={() => setEditMode((v) => !v)}
          dirty={dirty}
          onSave={save}
          onToggleRail={() => setRailHidden((v) => !v)}
          onOpenSecrets={() => setShowSecrets(true)}
          onOpenConfig={() => setShowConfig(true)}
          onOpenMembers={() => setShowMembers(true)}
          user={user}
          onSignOut={onSignOut}
          projects={projects}
          currentProject={currentProject}
          onSwitchProject={switchProject}
          onNewProject={newProject}
        />
        {waitingNode && (
          <div className="approval-banner" role="alert" data-testid="approval-banner">
            <span className="ab-dot" />
            <span className="ab-text">
              <b>Approval required</b> — “{(nodes.find((n) => n.id === waitingNode)?.data as NodeData | undefined)?.title || "a step"}”
              is waiting for your decision.
            </span>
            {memory.review != null && (
              <button className="btn ghost" onClick={() => setTab("memory")}>View review</button>
            )}
            <button className="btn ok" data-testid="approve-btn" onClick={() => { focusNode(waitingNode); approve("approved"); }}>Approve</button>
            <button className="btn danger" data-testid="reject-btn" onClick={() => { focusNode(waitingNode); approve("rejected"); }}>Reject</button>
            <button className="btn ghost" onClick={() => focusNode(waitingNode)}>View step</button>
          </div>
        )}
        <div className="body">
          <RunRail
            hidden={railHidden}
            nodes={orderedNodes}
            statuses={statuses}
            selectedId={selected}
            onSelect={focusNode}
            runLabel={runLabel}
            runMeta={runMeta}
            showHistory={showHistory}
            onToggleHistory={toggleHistory}
            runs={runs}
            currentRunId={runId}
            onPickRun={pickRun}
          />

          <section className="center">
            <div className="tabs">
              <div className="left-tools">
                <button className="chip" onClick={() => showToast("Agent assist is a prototype stub")}>
                  <Sparkles size={15} /> Agent
                </button>
              </div>
              <div className="tab-group">
                {(["canvas", "console", "memory", "files"] as Tab[]).map((t) => (
                  <button key={t} className="tab" data-on={tab === t} onClick={() => setTab(t)}>
                    {t[0].toUpperCase() + t.slice(1)}
                  </button>
                ))}
              </div>
            </div>

            <div className={`stage${tab === "canvas" ? "" : " hidden"}`}>
              <ReactFlow
                nodes={nodes}
                edges={edges}
                onNodesChange={onNodesChange}
                onEdgesChange={onEdgesChange}
                onConnect={onConnect}
                nodeTypes={nodeTypes}
                edgeTypes={edgeTypes}
                onNodeClick={(_, n) => setSelected(n.id)}
                onPaneClick={() => setSelected(null)}
                nodesDraggable={editMode}
                nodesConnectable={editMode}
                elementsSelectable
                fitView
                proOptions={{ hideAttribution: true }}
                defaultEdgeOptions={{ type: "conduit" }}
                minZoom={0.3}
                maxZoom={1.8}
              >
                <Background variant={BackgroundVariant.Dots} gap={22} size={1.4} color="var(--grid)" />
                <Controls />
                <MiniMap pannable zoomable nodeStrokeWidth={2} nodeColor="var(--border-strong)" maskColor="rgba(0,0,0,.06)" />
              </ReactFlow>
              <div className={`conn${conn === "live" ? " live" : ""}`}>
                <span className="dot" /> {conn}
              </div>
            </div>

            {tab === "console" && (
              <div className="pane">
                <h2>Console</h2>
                <p className="lead">Structured log for the current run. Every step emits events as it executes; failures carry the context sent back for correction.</p>
                {logs.length === 0 ? (
                  <div className="empty">No events yet — press Run.</div>
                ) : (
                  logs.map((l, i) => (
                    <div className="cline" key={i}>
                      <span className="t">{l.t}</span>
                      <span className={`lvl ${l.level}`}>{l.level.toUpperCase()}</span>
                      <span>{l.msg}</span>
                    </div>
                  ))
                )}
              </div>
            )}

            {tab === "memory" && (
              <div className="pane">
                <h2>Memory</h2>
                <p className="lead">Key–value state the workflow writes and reads across steps and runs.</p>
                {Object.keys(memory).length === 0 ? (
                  <div className="empty">No memory written yet — run the workflow.</div>
                ) : (
                  <div className="mem-grid">
                    {Object.entries(memory).map(([k, v]) => (
                      <div className="mem-card" key={k}>
                        <div className="mk">{k}</div>
                        <div className="mv">{typeof v === "object" ? JSON.stringify(v, null, 2) : String(v)}</div>
                      </div>
                    ))}
                  </div>
                )}
              </div>
            )}

            {tab === "files" && (
              <div className="pane">
                <h2>Files</h2>
                <p className="lead">Artifacts produced during the run, scoped to this execution.</p>
                <div className="empty">File capture is a prototype stub.</div>
              </div>
            )}

            {tab === "canvas" && selectedData && (
              <Inspector
                data={selectedData}
                output={outputs[selected!]}
                status={statuses[selected!] ?? "pending"}
                editMode={editMode}
                isApprovalWaiting={selected === waitingNode && statuses[selected!] === "waiting"}
                onChange={(patch) => patchNode(selected!, patch)}
                onDelete={() => deleteNode(selected!)}
                onApprove={approve}
                onPrev={() => stepRel(-1)}
                onNext={() => stepRel(1)}
                onClose={() => setSelected(null)}
              />
            )}
          </section>

          <Palette hidden={!editMode} onAdd={addComponent} />
        </div>
        {showSecrets && <SecretsPanel onClose={() => setShowSecrets(false)} onToast={showToast} />}
        {showConfig && <ConfigPanel onClose={() => setShowConfig(false)} onToast={showToast} />}
        {showMembers && <MembersPanel projectId={currentProject} onClose={() => setShowMembers(false)} onToast={showToast} />}
        {toast && <div className="toast">{toast}</div>}
      </div>
    </EditorContext.Provider>
  );
}

type AuthView = "landing" | "signin" | "signup";

export default function Page() {
  const [signedIn, setSignedIn] = useState<boolean | null>(null);
  const [view, setView] = useState<AuthView>("landing");

  const [authError, setAuthError] = useState("");

  useEffect(() => {
    setOnUnauthorized(() => setSignedIn(false));
    // OAuth callback returns here with the token (or an error) in the URL fragment,
    // which is never sent to a server or written to access logs.
    if (typeof window !== "undefined" && window.location.hash) {
      const h = new URLSearchParams(window.location.hash.slice(1));
      const tok = h.get("access_token");
      const errMsg = h.get("auth_error");
      if (tok) setToken(tok);
      if (errMsg) setAuthError(errMsg);
      if (tok || errMsg) {
        history.replaceState(null, "", window.location.pathname + window.location.search);
      }
    }
    setSignedIn(getToken() !== "");
  }, []);

  if (signedIn === null) return <div className="login-wrap" />; // brief pre-auth frame
  if (!signedIn) {
    if (view === "landing" && !authError) {
      return <Landing onSignIn={() => setView("signin")} onGetStarted={() => setView("signup")} />;
    }
    return (
      <Auth
        initialMode={view === "signup" ? "signup" : "signin"}
        notice={authError}
        onSignedIn={() => setSignedIn(true)}
        onBack={() => { setAuthError(""); setView("landing"); }}
      />
    );
  }

  return (
    <ReactFlowProvider>
      <FlowApp
        onSignOut={() => {
          setToken("");
          setSignedIn(false);
        }}
      />
    </ReactFlowProvider>
  );
}
