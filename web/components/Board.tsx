"use client";
import { useCallback, useEffect, useMemo, useState } from "react";
import { Plus, Play, ExternalLink, Trash2, GitBranch, Gauge, AlertTriangle, X, Search } from "lucide-react";
import { api } from "@/lib/api";
import type { Task, TaskStage, SSEvent, Run } from "@/lib/types";

const COLUMNS: { stage: TaskStage; label: string }[] = [
  { stage: "backlog", label: "Backlog" },
  { stage: "implement", label: "Implement" },
  { stage: "verify", label: "Verify" },
  { stage: "done", label: "Done" },
  { stage: "blocked", label: "Blocked" },
];

export function Board({ onOpenRun, onToast }: { onOpenRun: (runId: string) => void; onToast: (m: string) => void }) {
  const [tasks, setTasks] = useState<Task[]>([]);
  const [loading, setLoading] = useState(true);
  const [query, setQuery] = useState("");
  const [sourceFilter, setSourceFilter] = useState("");
  const [open, setOpen] = useState<Task | null>(null);

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

  async function newTask() {
    const title = window.prompt("New task — title / problem statement:");
    if (!title) return;
    const description = window.prompt("Description (optional):") || "";
    try {
      await api.createTask({ title, summary: title, description, source: "manual" });
      onToast("Task created in Backlog");
      refresh();
    } catch (e) { onToast("Create failed: " + (e as Error).message); }
  }

  async function start(t: Task) {
    try {
      const { run } = await api.startTask(t.id);
      onToast("Started — running the factory");
      onOpenRun(run.id);
    } catch (e) { onToast("Start failed: " + (e as Error).message); }
  }

  async function remove(t: Task) {
    if (!window.confirm(`Delete task “${t.title}”?`)) return;
    try {
      await api.deleteTask(t.id);
      setTasks((prev) => prev.filter((x) => x.id !== t.id));
      setOpen((cur) => (cur && cur.id === t.id ? null : cur));
    } catch (e) { onToast("Delete failed: " + (e as Error).message); }
  }

  return (
    <div className="board">
      <div className="board-head">
        <h2>Factory board</h2>
        <span className="board-sub">{filtered.length} of {tasks.length}</span>
        <div className="board-search">
          <Search size={14} />
          <input placeholder="Search title / ref…" value={query} onChange={(e) => setQuery(e.target.value)} />
        </div>
        <select className="board-filter" value={sourceFilter} onChange={(e) => setSourceFilter(e.target.value)}>
          <option value="">All sources</option>
          {sources.map((s) => <option key={s} value={s}>{s}</option>)}
        </select>
        <button className="btn primary" onClick={newTask}><Plus size={15} /> New task</button>
      </div>
      {loading ? (
        <div className="board-empty">Loading…</div>
      ) : (
        <div className="board-cols">
          {COLUMNS.map((col) => {
            const items = filtered.filter((t) => t.stage === col.stage);
            return (
              <div className={`board-col col-${col.stage}`} key={col.stage}>
                <div className="col-head">
                  <span className="col-dot" />
                  <span className="col-title">{col.label}</span>
                  <span className="col-count">{items.length}</span>
                </div>
                <div className="col-cards">
                  {items.map((t) => (
                    <TaskCard key={t.id} t={t} onOpen={() => setOpen(t)} onStart={start} onOpenRun={onOpenRun} />
                  ))}
                  {items.length === 0 && <div className="col-empty">—</div>}
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
    </div>
  );
}

function TaskCard({ t, onOpen, onStart, onOpenRun }: {
  t: Task; onOpen: () => void; onStart: (t: Task) => void; onOpenRun: (id: string) => void;
}) {
  return (
    <div className="tcard" onClick={onOpen}>
      <div className="tcard-top">
        <span className={`tsrc ${t.source || "manual"}`}>{t.source || "manual"}</span>
        {t.source_ref && <span className="tref">{t.source_ref}</span>}
      </div>
      <div className="tcard-title">{t.title}</div>
      <div className="tcard-meta">
        {typeof t.confidence === "number" && t.confidence > 0 && (
          <span className="tmeta" title="Triage confidence"><Gauge size={12} /> {t.confidence}</span>
        )}
        {t.branch && <span className="tmeta"><GitBranch size={12} /> {t.branch}</span>}
        {t.stage === "blocked" && <span className="tmeta warn"><AlertTriangle size={12} /> needs attention</span>}
      </div>
      <div className="tcard-actions" onClick={(e) => e.stopPropagation()}>
        {t.stage === "backlog" && (
          <button className="btn ok sm" onClick={() => onStart(t)}><Play size={13} /> Start</button>
        )}
        {t.current_run_id && (
          <button className="btn sm" onClick={() => onOpenRun(t.current_run_id!)}>View run</button>
        )}
        {t.pr_url && (
          <a className="btn sm" href={t.pr_url} target="_blank" rel="noreferrer"><ExternalLink size={12} /> PR</a>
        )}
      </div>
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
