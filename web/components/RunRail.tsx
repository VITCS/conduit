"use client";
import { Radio, History, ListTree } from "lucide-react";
import { Icon } from "@/lib/icons";
import { STATUS_META } from "@/lib/catalog";
import type { Run, Status, Usage, WFNode } from "@/lib/types";

/** runCost is a short label for a run's model spend, or "" when it made no model calls. */
export function runCost(u?: Usage | null): string {
  if (!u || !u.calls) return "";
  const usd = (n: number) => (n < 0.01 ? "$" + n.toFixed(4) : "$" + n.toFixed(n < 10 ? 3 : 2));
  const toks = u.input_tokens + u.output_tokens;
  const t = toks >= 1e6 ? (toks / 1e6).toFixed(1) + "M" : toks >= 1e3 ? Math.round(toks / 1e3) + "k" : String(toks);
  const parts = [`${t} tokens`];
  if (u.cost_usd > 0) parts.push(usd(u.cost_usd));
  if ((u.subscription_usd ?? 0) > 0) parts.push(`~${usd(u.subscription_usd!)} subscription`);
  return parts.join(" · ");
}

function rel(iso: string): string {
  const d = Date.now() - new Date(iso).getTime();
  const m = Math.floor(d / 60000);
  if (m < 1) return "just now";
  if (m < 60) return `${m}m ago`;
  const h = Math.floor(m / 60);
  if (h < 24) return `${h}h ago`;
  return `${Math.floor(h / 24)}d ago`;
}

export function RunRail(props: {
  hidden: boolean;
  nodes: WFNode[];
  statuses: Record<string, Status>;
  selectedId: string | null;
  onSelect: (id: string) => void;
  runLabel: string;
  runMeta: string;
  usage?: Usage | null;
  // history
  showHistory: boolean;
  onToggleHistory: () => void;
  runs: Run[];
  currentRunId: string | null;
  onPickRun: (id: string) => void;
}) {
  return (
    <aside className={`rail${props.hidden ? " hidden" : ""}`}>
      <div className="rail-head">
        <div className="rail-live">
          <span className="pulse" /> Live canvas
        </div>
        <button className="runs-label runs-toggle" onClick={props.onToggleHistory} title="Toggle run history">
          {props.showHistory ? <ListTree size={13} /> : <History size={13} />}
          {props.showHistory ? "Back to steps" : "Run history"}
        </button>
      </div>

      {props.showHistory ? (
        <div className="steps">
          {props.runs.length === 0 ? (
            <div className="empty" style={{ padding: "10px" }}>
              No runs yet.
            </div>
          ) : (
            props.runs.map((r) => {
              const meta = STATUS_META[r.status] ?? STATUS_META.pending;
              return (
                <button
                  key={r.id}
                  className={`step${props.currentRunId === r.id ? " sel" : ""}`}
                  onClick={() => props.onPickRun(r.id)}
                >
                  <span className="s-ico"><Radio size={15} /></span>
                  <span className="s-name">
                    {r.title}
                    <small style={{ display: "block", color: "var(--faint)", fontFamily: "var(--font-mono)" }}>
                      {r.id} · {rel(r.created_at)}{runCost(r.usage) ? ` · ${runCost(r.usage)}` : ""}
                    </small>
                  </span>
                  <span className={`badge ${meta.cls}`}>{meta.label}</span>
                </button>
              );
            })
          )}
        </div>
      ) : (
        <>
          <div className="run-title">
            <b>{props.runLabel}</b>
            <div className="run-meta">{props.runMeta}</div>
            {runCost(props.usage) && (
              <div className="usage-chip" style={{ marginTop: 6 }} data-testid="run-usage"
                title={Object.values(props.usage?.by_model ?? {}).map((m) => `${m.model}: ${m.calls} call(s), ${runCost(m)}`).join("\n")}>
                {runCost(props.usage)}
              </div>
            )}
          </div>
          <div className="steps">
            {props.nodes.map((n) => {
              const meta = STATUS_META[props.statuses[n.id] ?? "pending"];
              return (
                <button
                  key={n.id}
                  className={`step${props.selectedId === n.id ? " sel" : ""}`}
                  onClick={() => props.onSelect(n.id)}
                >
                  <span className="s-ico">
                    <Icon name={n.icon} size={17} />
                  </span>
                  <span className="s-name">{n.title}</span>
                  <span className={`badge ${meta.cls}`}>{meta.label}</span>
                </button>
              );
            })}
          </div>
        </>
      )}
    </aside>
  );
}
