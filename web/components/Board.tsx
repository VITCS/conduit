"use client";
import { useCallback, useEffect, useState } from "react";
import { Plus, Play, ExternalLink, Trash2, GitBranch, Gauge, AlertTriangle } from "lucide-react";
import { api } from "@/lib/api";
import type { Task, TaskStage, SSEvent } from "@/lib/types";

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

  const refresh = useCallback(async () => {
    try {
      setTasks(await api.listTasks());
    } catch (e) {
      onToast("Could not load tasks: " + (e as Error).message);
    } finally {
      setLoading(false);
    }
  }, [onToast]);

  useEffect(() => {
    refresh();
  }, [refresh]);

  // Live board: upsert on task.* events over the project-wide stream.
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
      } catch {
        /* ignore */
      }
    };
    es.addEventListener("task.created", upsert);
    es.addEventListener("task.updated", upsert);
    return () => es.close();
  }, []);

  async function newTask() {
    const title = window.prompt("New task — title / problem statement:");
    if (!title) return;
    const description = window.prompt("Description (optional):") || "";
    try {
      await api.createTask({ title, summary: title, description, source: "manual" });
      onToast("Task created in Backlog");
      refresh();
    } catch (e) {
      onToast("Create failed: " + (e as Error).message);
    }
  }

  async function start(t: Task) {
    try {
      const { run } = await api.startTask(t.id);
      onToast("Started — running the factory");
      onOpenRun(run.id);
    } catch (e) {
      onToast("Start failed: " + (e as Error).message);
    }
  }

  async function remove(t: Task) {
    if (!window.confirm(`Delete task “${t.title}”?`)) return;
    try {
      await api.deleteTask(t.id);
      setTasks((prev) => prev.filter((x) => x.id !== t.id));
    } catch (e) {
      onToast("Delete failed: " + (e as Error).message);
    }
  }

  return (
    <div className="board">
      <div className="board-head">
        <h2>Factory board</h2>
        <span className="board-sub">{tasks.length} task{tasks.length === 1 ? "" : "s"}</span>
        <button className="btn primary" onClick={newTask}><Plus size={15} /> New task</button>
      </div>
      {loading ? (
        <div className="board-empty">Loading…</div>
      ) : (
        <div className="board-cols">
          {COLUMNS.map((col) => {
            const items = tasks.filter((t) => t.stage === col.stage);
            return (
              <div className={`board-col col-${col.stage}`} key={col.stage}>
                <div className="col-head">
                  <span className="col-dot" />
                  <span className="col-title">{col.label}</span>
                  <span className="col-count">{items.length}</span>
                </div>
                <div className="col-cards">
                  {items.map((t) => (
                    <TaskCard key={t.id} t={t} onStart={start} onOpenRun={onOpenRun} onDelete={remove} />
                  ))}
                  {items.length === 0 && <div className="col-empty">—</div>}
                </div>
              </div>
            );
          })}
        </div>
      )}
    </div>
  );
}

function TaskCard({ t, onStart, onOpenRun, onDelete }: {
  t: Task; onStart: (t: Task) => void; onOpenRun: (id: string) => void; onDelete: (t: Task) => void;
}) {
  return (
    <div className="tcard">
      <div className="tcard-top">
        <span className={`tsrc ${t.source || "manual"}`}>{t.source || "manual"}</span>
        {t.source_ref && <span className="tref">{t.source_ref}</span>}
        <button className="tcard-del" onClick={() => onDelete(t)} title="Delete"><Trash2 size={13} /></button>
      </div>
      <div className="tcard-title">{t.title}</div>
      <div className="tcard-meta">
        {typeof t.confidence === "number" && t.confidence > 0 && (
          <span className="tmeta" title="Triage confidence"><Gauge size={12} /> {t.confidence}</span>
        )}
        {t.branch && <span className="tmeta"><GitBranch size={12} /> {t.branch}</span>}
        {t.stage === "blocked" && <span className="tmeta warn"><AlertTriangle size={12} /> needs attention</span>}
      </div>
      <div className="tcard-actions">
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
