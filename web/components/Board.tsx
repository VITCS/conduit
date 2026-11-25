"use client";
import { useCallback, useEffect, useMemo, useState } from "react";
import {
  Plus, Play, ExternalLink, Trash2, GitBranch, Gauge, AlertTriangle, X, Search, Inbox, Hammer, ShieldCheck,
  CircleCheckBig, OctagonAlert, GitPullRequest, Activity, type LucideIcon, BellRing } from "lucide-react";
import { api } from "@/lib/api";
import { useDialogs } from "@/components/Dialogs";
import type { Task, TaskStage, SSEvent, Run } from "@/lib/types";

const COLUMNS: { stage: TaskStage; label: string; icon: LucideIcon; empty: string }[] = [
  { stage: "backlog", label: "Backlog", icon: Inbox, empty: "Nothing waiting. Add a task or send a ticket to intake." },
  { stage: "implement", label: "Implement", icon: Hammer, empty: "No task is being worked on." },
  { stage: "verify", label: "Verify", icon: ShieldCheck, empty: "Nothing in verification." },
  { stage: "done", label: "Done", icon: CircleCheckBig, empty: "Finished tasks land here." },
  { stage: "blocked", label: "Blocked", icon: OctagonAlert, empty: "Nothing needs attention." },
];

// Confidence band used only for colouring the meter on a card.
function confBand(n: number): "hi" | "mid" | "lo" {
  return n >= 75 ? "hi" : n >= 50 ? "mid" : "lo";
}

export function Board({ onOpenRun, onToast, needs }: {
  onOpenRun: (runId: string) => void; onToast: (m: string) => void;
  /** Task id → what it waits on ("question", "approval"), from "Needs you". */
  needs?: Record<string, string>;
}) {
  const [tasks, setTasks] = useState<Task[]>([]);
  const [loading, setLoading] = useState(true);
  const [query, setQuery] = useState("");
  const [sourceFilter, setSourceFilter] = useState("");
  const [open, setOpen] = useState<Task | null>(null);
  const [newOpen, setNewOpen] = useState(false);
  const { confirm, dialog } = useDialogs();

  const refresh = useCallback(async () => {
    try {
      setTasks(await api.listTasks());
    } catch (e) {
      onToast("Could not load tasks: " + (e as Error).message);
    } finally {
      setLoading(false);
    }
  }, [onToast]);

  useEffect(() => { refresh(); }, [refresh]);

  // Live board: upsert on task.* events; keep an open drawer in sync.
  useEffect(() => {
    const es = new EventSource(api.eventsAllURL());
    const upsert = (ev: MessageEvent) => {
      try {
        const d: SSEvent = JSON.parse(ev.data);
        if (!d.task) return;
        const t = d.task;
        setTasks((prev) => {
          const i = prev.findIndex((x) => x.id === t.id);
          if (i === -1) return [t, ...prev];
          const next = prev.slice();
          next[i] = t;
          return next;
        });
        setOpen((cur) => (cur && cur.id === t.id ? t : cur));
      } catch { /* ignore */ }
    };
    es.addEventListener("task.created", upsert);
    es.addEventListener("task.updated", upsert);
    return () => es.close();
  }, []);

  const sources = useMemo(() => Array.from(new Set(tasks.map((t) => t.source || "manual"))).sort(), [tasks]);
  const filtered = useMemo(() => {
    const q = query.trim().toLowerCase();
    return tasks.filter((t) =>
      (!sourceFilter || (t.source || "manual") === sourceFilter) &&
      (!q || t.title.toLowerCase().includes(q) || (t.source_ref || "").toLowerCase().includes(q)),
    );
  }, [tasks, query, sourceFilter]);

  async function createTask(body: Partial<Task>) {
    await api.createTask(body);
    onToast("Task created in Backlog");
    setNewOpen(false);
    refresh();
  }

  async function start(t: Task) {
    try {
      const { run } = await api.startTask(t.id);
      onToast("Started — running the factory");
      onOpenRun(run.id);
    } catch (e) { onToast("Start failed: " + (e as Error).message); }
  }

  async function remove(t: Task) {
    const ok = await confirm({
      title: "Delete this task?",
      message: <>“{t.title}” will be removed from the board. Its past runs are not affected.</>,
      confirmLabel: "Delete task",
      danger: true,
    });
    if (!ok) return;
    try {
      await api.deleteTask(t.id);
      setTasks((prev) => prev.filter((x) => x.id !== t.id));
      setOpen((cur) => (cur && cur.id === t.id ? null : cur));
    } catch (e) { onToast("Delete failed: " + (e as Error).message); }
  }

  return (
    <div className="board">
      <div className="board-head">
        <div className="board-titles">
          <h2>Factory board</h2>
          <span className="board-sub">
            {filtered.length === tasks.length ? `${tasks.length} task${tasks.length === 1 ? "" : "s"}` : `${filtered.length} of ${tasks.length} tasks`}
          </span>
        </div>
        <div className="board-tools">
          <div className="board-search">
            <Search size={14} />
            <input placeholder="Search title or reference" aria-label="Search tasks" value={query} onChange={(e) => setQuery(e.target.value)} />
          </div>
          <select className="board-filter" aria-label="Filter by source" value={sourceFilter} onChange={(e) => setSourceFilter(e.target.value)}>
            <option value="">All sources</option>
            {sources.map((s) => <option key={s} value={s}>{s}</option>)}
          </select>
          <button className="btn primary" onClick={() => setNewOpen(true)}><Plus size={15} /> New task</button>
        </div>
      </div>
      {loading ? (
        <div className="board-cols" aria-busy="true">
          {COLUMNS.map((col) => (
            <div className={`board-col col-${col.stage}`} key={col.stage}>
              <div className="col-head"><span className="col-ico"><col.icon size={14} /></span><span className="col-title">{col.label}</span></div>
              <div className="col-cards"><div className="tcard skeleton" /><div className="tcard skeleton short" /></div>
            </div>
          ))}
        </div>
      ) : (
        <div className="board-cols">
          {COLUMNS.map((col) => {
            const items = filtered.filter((t) => t.stage === col.stage);
            return (
              <div className={`board-col col-${col.stage}`} key={col.stage}>
                <div className="col-head">
                  <span className="col-ico"><col.icon size={14} /></span>
                  <span className="col-title">{col.label}</span>
                  <span className="col-count">{items.length}</span>
                </div>
                <div className="col-cards">
                  {items.map((t) => (
                    <TaskCard key={t.id} t={t} need={needs?.[t.id]} onOpen={() => setOpen(t)} onStart={start} onOpenRun={onOpenRun} />
                  ))}
                  {items.length === 0 && <div className="col-empty">{query || sourceFilter ? "No matching tasks." : col.empty}</div>}
                </div>
              </div>
            );
          })}
        </div>
      )}
      {open && (
        <TaskDrawer
          task={open}
          onClose={() => setOpen(null)}
          onStart={start}
          onOpenRun={onOpenRun}
          onDelete={remove}
          onToast={onToast}
        />
      )}
      {newOpen && <NewTaskModal onClose={() => setNewOpen(false)} onCreate={createTask} onToast={onToast} />}
      {dialog}
    </div>
  );
}

function TaskCard({ t, need, onOpen, onStart, onOpenRun }: {
  t: Task; need?: string; onOpen: () => void; onStart: (t: Task) => void; onOpenRun: (id: string) => void;
}) {
  const conf = typeof t.confidence === "number" && t.confidence > 0 ? t.confidence : 0;
  const hasActions = t.stage === "backlog" || !!t.current_run_id || !!t.pr_url;
  return (
    <div className={`tcard s-${t.stage}`} onClick={onOpen} role="button" tabIndex={0}
      onKeyDown={(e) => { if (e.key === "Enter" && e.target === e.currentTarget) onOpen(); }}>
      <div className="tcard-top">
        <span className={`tsrc ${t.source || "manual"}`}>{t.source || "manual"}</span>
        {t.source_ref && <span className="tref">{t.source_ref}</span>}
        {need ? (
          <span className={`tneed ${need}`} title="Waiting on you" data-testid="task-needs-you">
            <BellRing size={11} /> {need === "question" ? "question for you" : "needs your approval"}
          </span>
        ) : t.stage === "implement" && <span className="tlive" title="A run is working on this task"><Activity size={11} /> running</span>}
      </div>
      <div className="tcard-title">{t.title}</div>
      {conf > 0 && (
        <div className={`tconf ${confBand(conf)}`} title="Triage confidence">
          <Gauge size={12} />
          <span className="tconf-track"><i style={{ width: `${Math.min(100, conf)}%` }} /></span>
          <b>{conf}</b>
        </div>
      )}
      {(t.branch || t.stage === "blocked") && (
        <div className="tcard-meta">
          {t.branch && <span className="tmeta"><GitBranch size={12} /> {t.branch}</span>}
          {t.stage === "blocked" && <span className="tmeta warn"><AlertTriangle size={12} /> needs attention</span>}
        </div>
      )}
      {hasActions && (
        <div className="tcard-actions" onClick={(e) => e.stopPropagation()}>
          {t.stage === "backlog" && (
            <button className="btn ok sm" onClick={() => onStart(t)}><Play size={13} /> Start</button>
          )}
          {t.current_run_id && (
            <button className="btn sm" onClick={() => onOpenRun(t.current_run_id!)}>View run</button>
          )}
          {t.pr_url && (
            <a className="btn sm" href={t.pr_url} target="_blank" rel="noreferrer"><GitPullRequest size={12} /> PR <ExternalLink size={11} /></a>
          )}
        </div>
      )}
    </div>
  );
}

function TaskDrawer({ task, onClose, onStart, onOpenRun, onDelete, onToast }: {
  task: Task; onClose: () => void; onStart: (t: Task) => void; onOpenRun: (id: string) => void;
  onDelete: (t: Task) => void; onToast: (m: string) => void;
}) {
  const [runs, setRuns] = useState<Run[]>([]);
  const [note, setNote] = useState("");
  useEffect(() => {
    api.getTask(task.id).then((d) => setRuns(d.runs || [])).catch(() => {});
  }, [task.id]);

  async function moveStage(stage: string) {
    try { await api.setTaskStage(task.id, stage); } catch (e) { onToast("Move failed: " + (e as Error).message); }
  }
  async function addNote() {
    if (!note.trim()) return;
    try { await api.addTaskNote(task.id, note.trim()); setNote(""); } catch (e) { onToast("Note failed: " + (e as Error).message); }
  }

  return (
    <div className="drawer-scrim" onClick={onClose}>
      <div className="drawer" onClick={(e) => e.stopPropagation()}>
        <div className="drawer-head">
          <span className={`tsrc ${task.source || "manual"}`}>{task.source || "manual"}</span>
          {task.source_ref && <span className="tref">{task.source_ref}</span>}
          <span className={`stage-pill s-${task.stage}`}>{task.stage}</span>
          <button className="iconbtn" onClick={onClose} aria-label="Close"><X size={17} /></button>
        </div>
        <div className="drawer-body">
          <h2 className="drawer-title">{task.title}</h2>
          {task.source_url && <a className="drawer-link" href={task.source_url} target="_blank" rel="noreferrer">{task.source_url}</a>}

          <div className="drawer-stats">
            {typeof task.confidence === "number" && task.confidence > 0 && (
              <span className="tmeta"><Gauge size={13} /> confidence {task.confidence}</span>
            )}
            {task.branch && <span className="tmeta"><GitBranch size={13} /> {task.branch}</span>}
            {task.pr_url && <a className="tmeta" href={task.pr_url} target="_blank" rel="noreferrer"><ExternalLink size={13} /> pull request</a>}
          </div>

          {task.description && (
            <>
              <h3 className="drawer-h3">Description</h3>
              <pre className="drawer-pre">{task.description}</pre>
            </>
          )}

          <h3 className="drawer-h3">Run history</h3>
          {runs.length === 0 ? (
            <div className="col-empty">No runs yet.</div>
          ) : (
            <div className="drawer-runs">
              {runs.map((r) => (
                <button className="drawer-run" key={r.id} onClick={() => onOpenRun(r.id)}>
                  <span className={`badge ${runCls(r.status)}`}>{r.status}</span>
                  <span className="drawer-run-id">{r.id}</span>
                  <span className="drawer-run-time">{new Date(r.created_at).toLocaleString()}</span>
                </button>
              ))}
            </div>
          )}

          <h3 className="drawer-h3">Notes</h3>
          <div className="drawer-notes">
            {(task.notes || []).map((n, i) => (
              <div className="dnote" key={i}>
                <div className="dnote-meta">{n.author || "someone"} · {new Date(n.at).toLocaleString()}</div>
                <div className="dnote-text">{n.text}</div>
              </div>
            ))}
            <div className="dnote-add">
              <input placeholder="Add a note…" value={note} onChange={(e) => setNote(e.target.value)}
                onKeyDown={(e) => { if (e.key === "Enter") addNote(); }} />
              <button className="btn sm" onClick={addNote}>Add</button>
            </div>
          </div>

          <h3 className="drawer-h3">Actions</h3>
          <div className="drawer-actions">
            {task.stage === "backlog" && <button className="btn ok" onClick={() => onStart(task)}><Play size={14} /> Start</button>}
            {task.current_run_id && <button className="btn" onClick={() => onOpenRun(task.current_run_id!)}>View current run</button>}
            <select className="board-filter" value={task.stage} onChange={(e) => moveStage(e.target.value)} title="Move to stage">
              {COLUMNS.map((c) => <option key={c.stage} value={c.stage}>Move to {c.label}</option>)}
            </select>
            <button className="btn danger" onClick={() => onDelete(task)}><Trash2 size={14} /> Delete</button>
          </div>
        </div>
      </div>
    </div>
  );
}

function runCls(status: string): string {
  if (status === "success" || status === "created") return "ok";
  if (status === "failed") return "fail";
  if (status === "waiting") return "wait";
  if (status === "running") return "run";
  return "idle";
}

function NewTaskModal({ onClose, onCreate, onToast }: {
  onClose: () => void; onCreate: (b: Partial<Task>) => Promise<void>; onToast: (m: string) => void;
}) {
  const [title, setTitle] = useState("");
  const [description, setDescription] = useState("");
  const [source, setSource] = useState("manual");
  const [sourceRef, setSourceRef] = useState("");
  const [busy, setBusy] = useState(false);

  async function submit() {
    if (!title.trim() || busy) return;
    setBusy(true);
    try {
      await onCreate({ title: title.trim(), summary: title.trim(), description: description.trim(), source, source_ref: sourceRef.trim() || undefined });
    } catch (e) {
      onToast("Create failed: " + (e as Error).message);
      setBusy(false);
    }
  }

  return (
    <div className="modal-scrim" onClick={onClose}>
      <div className="modal" onClick={(e) => e.stopPropagation()} role="dialog" aria-modal="true">
        <div className="modal-head">
          <Plus size={16} />
          <span className="modal-title">New task</span>
          <button className="iconbtn" onClick={onClose} aria-label="Close"><X size={17} /></button>
        </div>
        <p className="hint" style={{ padding: "0 16px" }}>
          Create a task in <b>Backlog</b>. Start it on the board to run the factory — a clear outcome and a
          checkable success condition score higher at triage.
        </p>
        <div className="modal-body">
          <label className="nt-field">
            <span className="nt-label">Title / problem statement</span>
            <input className="inp" autoFocus placeholder="e.g. Add rate limiting to the public API"
              value={title} onChange={(e) => setTitle(e.target.value)}
              onKeyDown={(e) => { if (e.key === "Enter" && (e.metaKey || e.ctrlKey)) submit(); }} />
          </label>
          <label className="nt-field">
            <span className="nt-label">Description <span className="nt-opt">optional</span></span>
            <textarea className="inp nt-area" rows={4} placeholder="What should be true after the change? Acceptance criteria, affected area, constraints…"
              value={description} onChange={(e) => setDescription(e.target.value)} />
          </label>
          <div className="nt-row">
            <label className="nt-field">
              <span className="nt-label">Source</span>
              <select className="inp" value={source} onChange={(e) => setSource(e.target.value)}>
                <option value="manual">Manual</option>
                <option value="jira">Jira</option>
                <option value="notion">Notion</option>
                <option value="github">GitHub</option>
              </select>
            </label>
            <label className="nt-field">
              <span className="nt-label">Reference <span className="nt-opt">optional</span></span>
              <input className="inp mono" placeholder="e.g. ENG-482" value={sourceRef} onChange={(e) => setSourceRef(e.target.value)} />
            </label>
          </div>
          <div className="nt-actions">
            <button className="btn" onClick={onClose}>Cancel</button>
            <button className="btn primary" onClick={submit} disabled={!title.trim() || busy}>
              <Plus size={14} /> {busy ? "Creating…" : "Create task"}
            </button>
          </div>
        </div>
      </div>
    </div>
  );
}
