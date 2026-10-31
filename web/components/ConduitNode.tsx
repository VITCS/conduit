import { Handle, Position, type NodeProps } from "@xyflow/react";
import { Icon } from "@/lib/icons";
import { STATUS_META } from "@/lib/catalog";
import type { NodeData } from "@/lib/types";

export function ConduitNode({ data, selected }: NodeProps) {
  const d = data as NodeData;
  const meta = STATUS_META[d.status] ?? STATUS_META.pending;
  return (
    <div className={`cnode kind-${d.kind}${selected ? " sel" : ""}${d.status === "running" ? " active" : ""}`}>
      <Handle type="target" position={Position.Left} />
      <div className="n-head">
        <span className="n-ico">
          <Icon name={d.icon} size={15} />
        </span>
        <span className="n-title">{d.title}</span>
      </div>
      <div className="n-cfg">
        {d.config.map((f, i) => (
          <div className="n-cfg-row" key={i}>
            <span className="ic">
              <Icon name={f.icon} size={12} />
            </span>
            <span>
              <b>{f.key}</b> {f.value}
            </span>
          </div>
        ))}
      </div>
      <div className="n-foot">
        <span className={`badge ${meta.cls}`}>{meta.label}</span>
        <span className="n-ref">{d.typeName}</span>
      </div>
      <Handle type="source" position={Position.Right} />
    </div>
  );
}
