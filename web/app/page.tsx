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
import { Sparkles, GitPullRequest, Workflow as WorkflowIcon, KanbanSquare, BarChart3, Hand, SquareTerminal, Database, FileText } from "lucide-react";

import { ConduitNode } from "@/components/ConduitNode";
import { ConduitEdge } from "@/components/ConduitEdge";
import { Inspector } from "@/components/Inspector";
import { Topbar } from "@/components/Topbar";
import { RunRail } from "@/components/RunRail";
import { Palette } from "@/components/Palette";
import { SettingsPanel } from "@/components/SettingsPanel";
import { Board } from "@/components/Board";
import { useDialogs } from "@/components/Dialogs";
import { SendBackDialog } from "@/components/SendBackDialog";
import { DiffReview } from "@/components/DiffReview";
import { Metrics } from "@/components/Metrics";
import { MembersPanel } from "@/components/MembersPanel";
import { Landing } from "@/components/Landing";
import { Auth } from "@/components/Auth";
import { EditorContext } from "@/lib/editorContext";
import { api, getToken, setToken, setOnUnauthorized, getProject, setProject } from "@/lib/api";
import { type CatalogItem } from "@/lib/catalog";
import type { EdgeData, NodeData, Run, SSEvent, Status, Workflow, WFNode, WFEdge, ReviewRequest } from "@/lib/types";

const nodeTypes: NodeTypes = { conduit: ConduitNode };
const edgeTypes: EdgeTypes = { conduit: ConduitEdge };
const OK = (s?: Status) => s === "success" || s === "created";

const edgeId = (from: string, to: string) => `${from}__${to}`;

function wfToNodes(wf: Workflow): Node[] {
  return wf.nodes.map((n) => ({
    id: n.id,
    type: "conduit",
    position: { x: n.x, y: n.y },
    data: { title: n.title, typeName: n.type, icon: n.icon, kind: n.kind, config: n.config, status: "pending", disabled: !!n.disabled } as NodeData,
  }));
}
// toWorkflowPayload builds the Workflow to PUT from the current canvas state.
function toWorkflowPayload(wf: Workflow, name: string, nodes: Node[], edges: Edge[]): Workflow {
  return {
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
        ...(d.disabled ? { disabled: true } : {}),
      };
    }),
    edges: edges.map((e) => {
      const d = e.data as EdgeData | undefined;
      const out: WFEdge = { from: e.source, to: e.target };
      if (d?.label) out.label = d.label;
      if (d?.maxAttempts && d.maxAttempts > 0) out.max_attempts = d.maxAttempts;
      return out;
    }),
  };
}
function wfToEdges(wf: Workflow): Edge[] {
  return wf.edges.map((e) => ({
    id: edgeId(e.from, e.to),
    source: e.from,
    target: e.to,
    type: "conduit",
    data: { label: e.label ?? "", maxAttempts: e.max_attempts } as EdgeData,
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
  const [runInput, setRunInput] = useState<Record<string, unknown> | null>(null);
  const [running, setRunning] = useState(false);
  const [waitingNode, setWaitingNode] = useState<string | null>(null);
  const [approvalDetail, setApprovalDetail] = useState<string>("");
  const [appView, setAppView] = useState<"editor" | "board" | "metrics">("editor");
  const [conn, setConn] = useState("idle");
  const [runLabel, setRunLabel] = useState("No run yet");
  const [runMeta, setRunMeta] = useState("press Run to execute");
  const [railHidden, setRailHidden] = useState(false);
  const [showHistory, setShowHistory] = useState(false);
  const [showSettings, setShowSettings] = useState(false);
  const [showMembers, setShowMembers] = useState(false);
  const [runs, setRuns] = useState<Run[]>([]);
  const [projects, setProjects] = useState<{ id: string; name: string }[]>([]);
  const [currentProject, setCurrentProject] = useState("default");
  const [vars, setVars] = useState<Record<string, string>>({});
  const [secretNames, setSecretNames] = useState<string[]>([]);
  const [reviewRequests, setReviewRequests] = useState<ReviewRequest[]>([]);
  const [toast, setToast] = useState("");
  const { confirm, prompt, dialog } = useDialogs();
  const [sendBackOpen, setSendBackOpen] = useState(false);
  const [diffOpen, setDiffOpen] = useState(false);

  // Project config (vars + secret names) used to show resolved values on nodes.
  const loadProjectConfig = useCallback(async () => {
    try {
      setVars(await api.listVars());
    } catch {
      /* ignore */
    }
    try {
      setSecretNames(await api.listSecrets());
    } catch {
      /* ignore */
    }
  }, []);
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

  // Pending PR review handoffs for the current project (the GitHub review inbox).
  const loadReviewRequests = useCallback(async () => {
    try {
      setReviewRequests(await api.listReviewRequests());
    } catch {
      setReviewRequests([]);
    }
  }, []);

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
      loadProjectConfig();
      loadReviewRequests();
    })();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  const switchProject = useCallback(
    (id: string) => {
      setProject(id);
      setCurrentProject(id);
      setSelected(null);
      setRunId(null);
      setRunInput(null);
      setRunLabel("No run yet");
      setRunMeta("press Run to execute");
      loadWorkflows();
      loadProjectConfig();
      loadReviewRequests();
    },
    [loadWorkflows, loadProjectConfig, loadReviewRequests],
  );

  const newProject = useCallback(async () => {
    const name = await prompt({
      title: "New project",
      message: "A project has its own workflows, board, variables and secrets.",
      label: "Project name",
      placeholder: "e.g. Storefront",
      confirmLabel: "Create project",
    });
    if (!name) return;
    try {
      const p = await api.createProject(name);
      setProjects(await api.listProjects());
      switchProject(p.id);
      showToast("Project created");
    } catch (e) {
      showToast("Create failed: " + (e as Error).message);
    }
  }, [switchProject, showToast, prompt]);

  const deleteProject = useCallback(
    async (id: string) => {
      const proj = projects.find((p) => p.id === id);
      const ok = await confirm({
        title: `Delete project “${proj?.name || id}”?`,
        message: "This deletes the project and all of its workflows and runs. This cannot be undone.",
        confirmLabel: "Delete project",
        danger: true,
      });
      if (!ok) return;
      try {
        await api.deleteProject(id);
        const list = await api.listProjects();
        setProjects(list);
        switchProject(list[0]?.id || "default");
        showToast("Project deleted");
      } catch (e) {
        showToast("Delete failed: " + (e as Error).message);
      }
    },
    [projects, switchProject, showToast, confirm],
  );

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
      const input = runInput && Object.keys(runInput).length > 0 ? runInput : undefined;
      const run = await api.createRun(wf.id, input);
      setRunId(run.id);
      setRunLabel(run.title);
      setRunMeta(run.id);
      setRunInput(run.input || input || null);
      openStream(run.id);
    } catch (e) {
      setRunning(false);
      showToast("Run failed: " + (e as Error).message);
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [running, wf, nodes, runInput, showToast]);

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
    es.addEventListener("review.requested", (ev) => {
      const d: SSEvent = JSON.parse((ev as MessageEvent).data);
      addLog("ok", d.message || "PR sent to the GitHub review inbox");
      showToast("PR ready for review — open the GitHub project to run it");
      loadReviewRequests();
    });
    es.addEventListener("run.waiting", (ev) => {
      const d: SSEvent = JSON.parse((ev as MessageEvent).data);
      if (d.node_id) {
        setStatuses((s) => ({ ...s, [d.node_id!]: "waiting" }));
        setWaitingNode(d.node_id);
        setSelected(d.node_id);
      }
      setApprovalDetail(d.detail || "");
      addLog("info", d.message || "awaiting approval");
      showToast("Approval required");
      notifyApproval(
        d.detail
          ? `${d.message || "Approval required"}\n\n${d.detail.slice(0, 300)}${d.detail.length > 300 ? "…" : ""}`
          : d.message || "A run is waiting for your approval.",
      );
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
  }, [showToast, notifyApproval, loadReviewRequests]);

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
    setRunInput(full.input || null);
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
          const we = Object.entries(full.steps || {}).find(([, s]) => s.status === "waiting");
          const wn = we?.[0] || null;
          setRunning(true);
          if (wn) setWaitingNode(wn);
          const sum = we?.[1]?.output?.summary;
          setApprovalDetail(typeof sum === "string" ? sum : "");
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
        setApprovalDetail("");
      } catch (e) {
        showToast("Approve failed: " + (e as Error).message);
      }
    },
    [runId, waitingNode, showToast],
  );

  // The waiting gate's own output says whether it can send back, and how often.
  const gateOut = waitingNode ? outputs[waitingNode] : undefined;
  const canSendBack = gateOut?.send_back === true;
  // A diff-review gate carries the prepared code change for the reviewer.
  const gateDiff = typeof gateOut?.diff === "string" ? (gateOut.diff as string) : "";
  const sendBacksLeft = typeof gateOut?.send_backs_left === "number" ? (gateOut.send_backs_left as number) : undefined;
  // The note last sent back from *this* gate (shown so the reviewer can check it was addressed).
  const lastFeedback = (() => {
    const hist = Array.isArray(memory.feedback_history) ? (memory.feedback_history as { gate?: string; feedback?: string }[]) : [];
    const title = (nodes.find((n) => n.id === waitingNode)?.data as NodeData | undefined)?.title;
    for (let i = hist.length - 1; i >= 0; i--) if (hist[i]?.gate === title) return hist[i]?.feedback || "";
    return "";
  })();

  const sendBack = useCallback(
    async (feedback: string) => {
      if (!runId || !waitingNode) return;
      try {
        await api.approve(runId, waitingNode, "sent_back", feedback);
        showToast("Sent back — the agent is revising with your feedback");
        setSendBackOpen(false);
        setWaitingNode(null);
        setApprovalDetail("");
      } catch (e) {
        showToast("Send back failed: " + (e as Error).message);
      }
    },
    [runId, waitingNode, showToast],
  );

  const rerunFrom = useCallback(
    async (nodeId: string) => {
      if (!runId) return;
      try {
        await api.rerun(runId, nodeId);
        showToast("Re-running from this stage");
        setRunning(true);
        openStream(runId);
        reconcileRun(runId);
      } catch (e) {
        showToast("Re-run failed: " + (e as Error).message);
      }
    },
    [runId, showToast, openStream, reconcileRun],
  );

  // Trigger the review workflow for a handed-off PR (the explicit human "go").
  const runReview = useCallback(
    async (id: string) => {
      try {
        const { run } = await api.runReviewRequest(id);
        setRunId(run.id);
        setRunLabel(run.title);
        setRunMeta(run.id);
        setRunInput(run.input || null);
        setRunning(true);
        setAppView("editor");
        setTab("canvas");
        openStream(run.id);
        showToast("Review workflow started");
        loadReviewRequests();
      } catch (e) {
        showToast("Could not start review: " + (e as Error).message);
      }
    },
    [openStream, loadReviewRequests, showToast],
  );
  const dismissReview = useCallback(
    async (id: string) => {
      try {
        await api.dismissReviewRequest(id);
        loadReviewRequests();
      } catch (e) {
        showToast("Dismiss failed: " + (e as Error).message);
      }
    },
    [loadReviewRequests, showToast],
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

  const toggleNodeDisabled = useCallback(
    (id: string) => {
      setNodes((ns) => {
        const next = ns.map((n) =>
          n.id === id ? { ...n, data: { ...(n.data as NodeData), disabled: !(n.data as NodeData).disabled } } : n,
        );
        // Persist the bypass change right away so it actually applies to the next
        // run — no separate Save step (a common trap: toggling then running the
        // still-unsaved workflow). Positions/edges ride along via the same payload.
        const toggled = next.find((n) => n.id === id);
        const nowDisabled = !!(toggled?.data as NodeData | undefined)?.disabled;
        if (wf) {
          api
            .putWorkflow(toWorkflowPayload(wf, name, next, edges))
            .then((saved) => {
              setWf(saved);
              setDirty(false);
              showToast(nowDisabled ? "Step will be skipped on the next run" : "Step re-enabled");
            })
            .catch((e) => showToast("Could not save bypass: " + (e as Error).message));
        }
        return next;
      });
    },
    [setNodes, wf, name, edges, showToast],
  );

  const save = useCallback(async () => {
    if (!wf) return;
    try {
      const saved = await api.putWorkflow(toWorkflowPayload(wf, name, nodes, edges));
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
          const we = Object.entries(full.steps || {}).find(([, s]) => s.status === "waiting");
          const wn = we?.[0] || null;
          setRunning(true);
          if (wn) { setWaitingNode(wn); setSelected(wn); }
          const sum = we?.[1]?.output?.summary;
          setApprovalDetail(typeof sum === "string" ? sum : "");
          openStream(id);
        } else {
          setRunning(false);
          setWaitingNode(null);
          setApprovalDetail("");
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
    <EditorContext.Provider value={{ editMode, setEdgeLabel, deleteEdge, toggleNodeDisabled, vars, secretNames }}>
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
          onOpenSettings={() => setShowSettings(true)}
          onOpenMembers={() => setShowMembers(true)}
          user={user}
          onSignOut={onSignOut}
          projects={projects}
          currentProject={currentProject}
          onSwitchProject={switchProject}
          onNewProject={newProject}
          onDeleteProject={deleteProject}
        />
        {waitingNode && (
          <div className="approval-banner" role="alert" data-testid="approval-banner">
            <div className="ab-row">
              <span className="ab-ico"><Hand size={16} /></span>
              <span className="ab-text">
                <b>Approval required</b> — “{(nodes.find((n) => n.id === waitingNode)?.data as NodeData | undefined)?.title || "a step"}”
                {gateDiff ? " — review the code before it is pushed." : " is waiting for your decision."}
              </span>
              {gateDiff && (
                <button className="btn primary" data-testid="review-diff-btn" onClick={() => setDiffOpen(true)}>Review changes</button>
              )}
              {(memory.review != null || memory.plan != null) && (
                <button className="btn ghost" onClick={() => setTab("memory")}>View in memory</button>
              )}
              <button className="btn ok" data-testid="approve-btn" onClick={() => { focusNode(waitingNode); approve("approved"); }}>Approve</button>
              {canSendBack && (
                <button className="btn" data-testid="send-back-btn" onClick={() => setSendBackOpen(true)}
                  title={sendBacksLeft !== undefined ? `${sendBacksLeft} send-back${sendBacksLeft === 1 ? "" : "s"} left` : undefined}>
                  Send back
                </button>
              )}
              <button className="btn danger" data-testid="reject-btn" onClick={() => { focusNode(waitingNode); approve("rejected"); }}>Reject</button>
              <button className="btn ghost" onClick={() => focusNode(waitingNode)}>View step</button>
            </div>
            {lastFeedback && (
              <div className="ab-last"><b>Your last feedback:</b> {lastFeedback}</div>
            )}
            {approvalDetail && (
              <details className="ab-detail" open>
                <summary>What the agent suggested</summary>
                <pre className="ab-pre">{approvalDetail}</pre>
              </details>
            )}
          </div>
        )}
        {reviewRequests.length > 0 && (
          <div className="review-inbox" role="region" aria-label="Pull requests ready for review">
            <div className="ri-head">
              <GitPullRequest size={16} />
              <b>{reviewRequests.length} pull request{reviewRequests.length > 1 ? "s" : ""} ready for review</b>
              <span className="ri-sub">Opened by an upstream workflow — run the review when you&apos;re ready.</span>
            </div>
            {reviewRequests.map((rr) => (
              <div className="ri-row" key={rr.id}>
                <a className="ri-pr" href={rr.url || undefined} target="_blank" rel="noreferrer">
                  {rr.owner}/{rr.repo} #{rr.number}
                </a>
                <span className="ri-title">{rr.title || ""}</span>
                {rr.source && <span className="ri-src">from {rr.source}</span>}
                <button className="btn ok" onClick={() => runReview(rr.id)}>Run review</button>
                <button className="btn ghost" onClick={() => dismissReview(rr.id)}>Dismiss</button>
              </div>
            ))}
          </div>
        )}
        <div className="viewbar">
          <div className="vseg" role="tablist" aria-label="View">
            <button role="tab" aria-selected={appView === "editor"} className={`vbtn${appView === "editor" ? " on" : ""}`} onClick={() => setAppView("editor")} data-testid="view-editor"><WorkflowIcon size={14} /> Workflow</button>
            <button role="tab" aria-selected={appView === "board"} className={`vbtn${appView === "board" ? " on" : ""}`} onClick={() => setAppView("board")} data-testid="view-board"><KanbanSquare size={14} /> Board</button>
            <button role="tab" aria-selected={appView === "metrics"} className={`vbtn${appView === "metrics" ? " on" : ""}`} onClick={() => setAppView("metrics")} data-testid="view-metrics"><BarChart3 size={14} /> Metrics</button>
          </div>
          {appView === "editor" && (
            <div className="tab-group" role="tablist" aria-label="Workflow panel">
              {(["canvas", "console", "memory", "files"] as Tab[]).map((t) => {
                const TabIcon = t === "canvas" ? WorkflowIcon : t === "console" ? SquareTerminal : t === "memory" ? Database : FileText;
                return (
                  <button key={t} role="tab" aria-selected={tab === t} className="tab" data-on={tab === t} onClick={() => setTab(t)}>
                    <TabIcon size={13} /> {t[0].toUpperCase() + t.slice(1)}
                    {t === "console" && logs.length > 0 && <span className="tab-n">{logs.length}</span>}
                  </button>
                );
              })}
            </div>
          )}
          {appView === "editor" && (
            <div className={`conn${conn === "live" ? " live" : ""}`} title="Run stream status">
              <span className="dot" /> {conn}
            </div>
          )}
        </div>
        {appView === "board" ? (
          <Board onOpenRun={(id) => { setAppView("editor"); pickRun(id); }} onToast={showToast} />
        ) : appView === "metrics" ? (
          <Metrics onToast={showToast} />
        ) : (
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
                <Background variant={BackgroundVariant.Dots} gap={24} size={1.6} color="var(--grid)" />
                <Controls />
                <MiniMap pannable zoomable nodeStrokeWidth={0} nodeBorderRadius={6} nodeColor="var(--border-strong)" maskColor="var(--mask)" />
              </ReactFlow>
              <button className="chip canvas-assist" onClick={() => showToast("Agent assist is a prototype stub")}>
                <Sparkles size={14} /> Agent
              </button>
              {editMode && <div className="canvas-hint">Edit mode — drag nodes, connect ports, click an edge to set its outcome</div>}
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
                {Object.keys(memory).filter((k) => !k.startsWith("_")).length === 0 ? (
                  <div className="empty">No memory written yet — run the workflow.</div>
                ) : (
                  <div className="mem-grid">
                    {Object.entries(memory).filter(([k]) => !k.startsWith("_")).map(([k, v]) => (
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
                onSendBack={selected === waitingNode && canSendBack ? () => setSendBackOpen(true) : undefined}
                onRerun={() => rerunFrom(selected!)}
                onPrev={() => stepRel(-1)}
                onNext={() => stepRel(1)}
                onClose={() => setSelected(null)}
              />
            )}
          </section>

          <Palette hidden={!editMode} onAdd={addComponent} />
        </div>
        )}
        {showSettings && <SettingsPanel onClose={() => { setShowSettings(false); loadProjectConfig(); }} onToast={showToast} />}
        {showMembers && <MembersPanel projectId={currentProject} onClose={() => setShowMembers(false)} onToast={showToast} />}
        {dialog}
        {diffOpen && waitingNode && gateDiff && (
          <DiffReview
            diff={gateDiff}
            title={(nodes.find((n) => n.id === waitingNode)?.data as NodeData | undefined)?.title || "Review changes"}
            summary={typeof gateOut?.summary === "string" ? (gateOut.summary as string) : undefined}
            sendBacksLeft={sendBacksLeft}
            onApprove={() => { setDiffOpen(false); approve("approved"); }}
            onSendBack={canSendBack ? () => { setDiffOpen(false); setSendBackOpen(true); } : undefined}
            onReject={() => { setDiffOpen(false); approve("rejected"); }}
            onClose={() => setDiffOpen(false)}
          />
        )}
        {sendBackOpen && waitingNode && (
          <SendBackDialog
            gateTitle={(nodes.find((n) => n.id === waitingNode)?.data as NodeData | undefined)?.title || "this step"}
            left={sendBacksLeft}
            onSend={sendBack}
            onClose={() => setSendBackOpen(false)}
          />
        )}
        {toast && <div className="toast" role="status">{toast}</div>}
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
