"use client";
import { Radio } from "lucide-react";
import { Icon } from "@/lib/icons";
import { STATUS_META } from "@/lib/catalog";
import type { Status, WFNode } from "@/lib/types";

export function RunRail(props: {
  hidden: boolean;
  nodes: WFNode[];
  statuses: Record<string, Status>;
  selectedId: string | null;
  onSelect: (id: string) => void;
  runLabel: string;
  runMeta: string;
}) {
  return (
    <aside className={`rail${props.hidden ? " hidden" : ""}`}>
      <div className="rail-head">
        <div className="rail-live">
          <span className="pulse" /> Live canvas
        </div>
        <div className="runs-label">
          <Radio size={13} style={{ verticalAlign: "-2px", marginRight: 6 }} />
          Runs
        </div>
      </div>
      <div className="run-title">
        <b>{props.runLabel}</b>
        <div className="run-meta">{props.runMeta}</div>
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
    </aside>
  );
}
