import { Handle, Position, type NodeProps } from "@xyflow/react";
import { Icon } from "@/lib/icons";
import { STATUS_META } from "@/lib/catalog";
import { useEditor } from "@/lib/editorContext";
import { resolveRef, SOURCE_LABEL } from "@/lib/refs";
import type { NodeData } from "@/lib/types";

export function ConduitNode({ id, data, selected }: NodeProps) {
  const d = data as NodeData;
  const { vars, secretNames, toggleNodeDisabled } = useEditor();
  const meta = STATUS_META[d.status] ?? STATUS_META.pending;
  const disabled = !!d.disabled;
  const canBypass = d.kind !== "trigger";
  const running = d.status === "running";
  const waiting = d.status === "waiting";
  return (
    <div
      className={`cnode kind-${d.kind} status-${d.status}${selected ? " sel" : ""}${running ? " active" : ""}${disabled ? " bypassed" : ""}`}
    >
      {(running || waiting) && <span className="n-progress" aria-hidden />}
      <Handle type="target" position={Position.Left} />
      <div className="n-head">
        <span className={`n-ico${running ? " spin" : ""}`}>
          <Icon name={d.icon} size={15} />
        </span>
        <span className="n-title">{d.title}</span>
        {canBypass && (
          <label
            className="n-bypass"
            title={disabled ? "Bypassed — this step is skipped during runs. Uncheck to run it." : "Bypass this step (skip it during runs)"}
            onClick={(e) => e.stopPropagation()}
            onMouseDown={(e) => e.stopPropagation()}
          >
            <input type="checkbox" checked={disabled} onChange={() => toggleNodeDisabled(id)} />
            <span>skip</span>
          </label>
        )}
      </div>
      <div className="n-cfg">
        {d.config.map((f, i) => {
          const r = resolveRef(f.value, vars, secretNames);
          return (
            <div className="n-cfg-row" key={i}>
              <span className="ic">
                <Icon name={f.icon} size={12} />
              </span>
              <span>
                <b>{f.key}</b> {r.text}
              </span>
              {r.source !== "literal" && SOURCE_LABEL[r.source] && (
                <span className={`src ${r.source}`}>{SOURCE_LABEL[r.source]}</span>
              )}
            </div>
          );
        })}
      </div>
      <div className="n-foot">
        <span className={`badge ${meta.cls}`}>{meta.label}</span>
        <span className="n-ref">{d.typeName}</span>
      </div>
      <Handle type="source" position={Position.Right} />
    </div>
  );
}
