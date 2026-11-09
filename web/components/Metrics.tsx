"use client";
import { useCallback, useEffect, useState } from "react";
import { RefreshCw, CheckCircle2, AlertTriangle, Timer, RotateCcw, Users } from "lucide-react";
import { api } from "@/lib/api";
import type { BoardMetrics, SSEvent } from "@/lib/types";

const STAGES = ["backlog", "implement", "verify", "done", "blocked"] as const;

function fmtDur(ms: number): string {
  if (!ms) return "—";
  const s = Math.round(ms / 1000);
  if (s < 90) return `${s}s`;
  const m = Math.round(s / 60);
  if (m < 90) return `${m}m`;
  const h = m / 60;
  if (h < 48) return `${h.toFixed(1)}h`;
  return `${(h / 24).toFixed(1)}d`;
}

export function Metrics({ onToast }: { onToast: (m: string) => void }) {
  const [m, setM] = useState<BoardMetrics | null>(null);

  const refresh = useCallback(async () => {
    try {
      setM(await api.getMetrics());
    } catch (e) {
      onToast("Could not load metrics: " + (e as Error).message);
    }
  }, [onToast]);

  useEffect(() => { refresh(); }, [refresh]);

  // Recompute when any task changes.
  useEffect(() => {
    const es = new EventSource(api.eventsAllURL());
    const onTask = (ev: MessageEvent) => {
      try { const d: SSEvent = JSON.parse(ev.data); if (d.task) refresh(); } catch { /* ignore */ }
    };
    es.addEventListener("task.created", onTask);
    es.addEventListener("task.updated", onTask);
    return () => es.close();
  }, [refresh]);

  if (!m) return <div className="board-empty">Loading…</div>;

  const maxDwell = Math.max(1, ...STAGES.map((s) => m.stage_avg_ms[s] || 0));
  const maxFunnel = Math.max(1, ...STAGES.map((s) => m.stage_inflow[s] || m.by_stage[s] || 0));

  return (
    <div className="metrics">
      <div className="board-head">
        <h2>Factory performance</h2>
        <span className="board-sub">{m.tasks} task{m.tasks === 1 ? "" : "s"} · {m.runs} run{m.runs === 1 ? "" : "s"}</span>
        <button className="btn" onClick={refresh}><RefreshCw size={14} /> Refresh</button>
      </div>

      <div className="kpis">
        <Kpi icon={<CheckCircle2 size={16} />} label="Delivered" value={String(m.done)} sub={`${m.started} started`} tone="ok" />
        <Kpi icon={<Timer size={16} />} label="Avg lead time" value={fmtDur(m.avg_lead_ms)} sub="created → done" />
        <Kpi icon={<RotateCcw size={16} />} label="Auto-recovery rate" value={pct(m.retry_rate)} sub={`${m.retry_runs}/${m.runs} runs retried`} tone="run" />
        <Kpi icon={<Users size={16} />} label="Intervention rate" value={pct(m.intervention_pct)} sub={`${m.blocked} blocked`} tone={m.intervention_pct > 0.4 ? "warn" : undefined} />
      </div>

      <div className="metric-panels">
        <div className="mpanel">
          <h3 className="drawer-h3">Average time in stage</h3>
          {STAGES.filter((s) => s !== "backlog").map((s) => (
            <div className="mbar-row" key={s}>
              <span className="mbar-label">{s}</span>
              <div className="mbar-track">
                <div className={`mbar-fill s-${s}`} style={{ width: `${((m.stage_avg_ms[s] || 0) / maxDwell) * 100}%` }} />
              </div>
              <span className="mbar-val">{fmtDur(m.stage_avg_ms[s] || 0)}</span>
            </div>
          ))}
        </div>

        <div className="mpanel">
          <h3 className="drawer-h3">Flow (entered each stage)</h3>
          {STAGES.map((s) => (
            <div className="mbar-row" key={s}>
              <span className="mbar-label">{s}</span>
              <div className="mbar-track">
                <div className={`mbar-fill s-${s}`} style={{ width: `${((m.stage_inflow[s] || m.by_stage[s] || 0) / maxFunnel) * 100}%` }} />
              </div>
              <span className="mbar-val">{m.stage_inflow[s] || m.by_stage[s] || 0}</span>
            </div>
          ))}
        </div>
      </div>
      {m.tasks === 0 && <div className="board-empty">No tasks yet — create one on the Board to start collecting operating data.</div>}
    </div>
  );
}

function pct(v: number): string {
  return `${Math.round(v * 100)}%`;
}

function Kpi({ icon, label, value, sub, tone }: { icon: React.ReactNode; label: string; value: string; sub?: string; tone?: "ok" | "run" | "warn" }) {
  return (
    <div className={`kpi${tone ? " t-" + tone : ""}`}>
      <div className="kpi-head">{icon}<span>{label}</span></div>
      <div className="kpi-val">{value}</div>
      {sub && <div className="kpi-sub">{sub}</div>}
    </div>
  );
}
