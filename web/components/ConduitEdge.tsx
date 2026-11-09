import { BaseEdge, EdgeLabelRenderer, getBezierPath, type EdgeProps } from "@xyflow/react";
import { useEditor } from "@/lib/editorContext";
import { EDGE_LABELS } from "@/lib/catalog";
import type { EdgeData } from "@/lib/types";

// labelClass maps an edge label to a visual tone.
function labelClass(label: string): string {
  if (label === "failed") return "fail";
  if (label === "success" || label === "approved") return "ok";
  return "none";
}

export function ConduitEdge(props: EdgeProps) {
  const { id, sourceX, sourceY, targetX, targetY, sourcePosition, targetPosition, markerEnd, selected, data } = props;
  const { editMode, setEdgeLabel, deleteEdge } = useEditor();
  const ed = data as EdgeData | undefined;
  const label = (ed?.label ?? "").toString();
  const retry = typeof ed?.maxAttempts === "number" && ed.maxAttempts > 0 ? ed.maxAttempts : 0;
  const display = retry ? `retry ×${retry}` : label;

  const [path, labelX, labelY] = getBezierPath({
    sourceX, sourceY, sourcePosition, targetX, targetY, targetPosition,
  });

  return (
    <>
      <BaseEdge id={id} path={path} markerEnd={markerEnd} />
      <EdgeLabelRenderer>
        <div
          style={{
            position: "absolute",
            transform: `translate(-50%, -50%) translate(${labelX}px, ${labelY}px)`,
          }}
          className="nodrag nopan"
        >
          {editMode && selected ? (
            <div className="epick">
              {EDGE_LABELS.map((l) => (
                <button
                  key={l || "none"}
                  className={l === label ? "on" : ""}
                  onClick={() => setEdgeLabel(id, l)}
                  title={l ? `gate on ${l}` : "unconditional"}
                >
                  {l || "any"}
                </button>
              ))}
              <button className="del" onClick={() => deleteEdge(id)} title="Delete edge">
                ✕
              </button>
            </div>
          ) : display ? (
            <span className={`elabel ${retry ? "retry" : labelClass(label)}`}>{display}</span>
          ) : editMode ? (
            <span className="elabel none">any</span>
          ) : null}
        </div>
      </EdgeLabelRenderer>
    </>
  );
}
