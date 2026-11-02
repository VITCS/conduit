"use client";
import { useState } from "react";
import { ChevronLeft, ChevronRight, X, Trash2, Plus } from "lucide-react";
import { Icon } from "@/lib/icons";
import { STATUS_META, CATALOG } from "@/lib/catalog";
import { useEditor } from "@/lib/editorContext";
import { resolveRef, SOURCE_LABEL } from "@/lib/refs";
import type { Field, NodeData, Status } from "@/lib/types";

const ICON_KEYS = [
  "github", "webhook", "clock", "server", "terminal", "heart", "approve", "branch",
  "db", "flag", "globe", "wait", "pin", "size", "img", "key", "file", "user",
];

type Tab = "details" | "payload" | "config";

export function Inspector(props: {
  data: NodeData;
  output?: Record<string, unknown>;
  status: Status;
  editMode: boolean;
  isApprovalWaiting: boolean;
  onChange: (patch: Partial<NodeData>) => void;
  onDelete: () => void;
  onApprove: (decision: "approved" | "rejected") => void;
  onPrev: () => void;
  onNext: () => void;
  onClose: () => void;
}) {
  const { data, output, status, editMode } = props;
  const [tab, setTab] = useState<Tab>("details");
  const meta = STATUS_META[status] ?? STATUS_META.pending;
  const { vars, secretNames } = useEditor();

  function setField(i: number, patch: Partial<Field>) {
    const config = data.config.map((f, idx) => (idx === i ? { ...f, ...patch } : f));
    props.onChange({ config });
  }
  function addField() {
    props.onChange({ config: [...data.config, { key: "key", icon: "", value: "value" }] });
  }
  function removeField(i: number) {
    props.onChange({ config: data.config.filter((_, idx) => idx !== i) });
  }

  return (
    <div className="inspector">
      <div className="insp-head">
        <span className="n-ico" style={{ width: 24, height: 24 }}>
          <Icon name={data.icon} size={14} />
        </span>
        <span className="insp-title">{data.title}</span>
        <span className="insp-nav">
          {editMode && (
            <button className="iconbtn" onClick={props.onDelete} title="Delete node" style={{ color: "var(--fail)" }}>
              <Trash2 size={17} />
            </button>
          )}
          <button className="iconbtn" onClick={props.onPrev} title="Previous"><ChevronLeft size={17} /></button>
          <button className="iconbtn" onClick={props.onNext} title="Next"><ChevronRight size={17} /></button>
          <button className="iconbtn" onClick={props.onClose} title="Close"><X size={17} /></button>
        </span>
      </div>

      <div className="insp-tabs">
        {(["details", "payload", "config"] as Tab[]).map((t) => (
          <button key={t} className="insp-tab" data-on={tab === t} onClick={() => setTab(t)}>
            {t[0].toUpperCase() + t.slice(1)}
          </button>
        ))}
      </div>

      <div className="insp-body">
        {props.isApprovalWaiting && (
          <div className="approve-bar">
            <span className="msg">
              This run is held for approval from {data.config.find((f) => f.key === "approvers")?.value || "reviewers"}.
            </span>
            <button className="btn ok" onClick={() => props.onApprove("approved")}>Approve</button>
            <button className="btn" onClick={() => props.onApprove("rejected")}>Reject</button>
          </div>
        )}

        {tab === "details" && (
          <dl className="kv">
            <dt>Status</dt>
            <dd><span className={`badge ${meta.cls}`}>{meta.label}</span></dd>
            <dt>Component</dt>
            <dd>{data.typeName}</dd>
            {output &&
              Object.entries(output)
                .filter(([, v]) => typeof v !== "object")
                .slice(0, 8)
                .map(([k, v]) => (
                  <div key={k} style={{ display: "contents" }}>
                    <dt>{k.replace(/_/g, " ")}</dt>
                    <dd>{String(v)}</dd>
                  </div>
                ))}
          </dl>
        )}

        {tab === "payload" &&
          (output ? (
            <pre className="code">{JSON.stringify(output, null, 2)}</pre>
          ) : (
            <div className="empty">No payload yet — run the workflow to capture this step&apos;s output.</div>
          ))}

        {tab === "config" &&
          (editMode ? (
            <div className="cfg-edit">
              <div className="cfg-top">
                <div className="field" style={{ flex: "1 1 240px" }}>
                  <label>Title</label>
                  <input className="inp" value={data.title} onChange={(e) => props.onChange({ title: e.target.value })} />
                </div>
                <div className="field">
                  <label>Component type</label>
                  <select
                    className="inp"
                    value={data.typeName}
                    onChange={(e) => {
                      const c = CATALOG.find((x) => x.type === e.target.value);
                      if (c) props.onChange({ typeName: c.type, icon: c.icon, kind: c.kind });
                    }}
                  >
                    {CATALOG.map((c) => (
                      <option key={c.type} value={c.type}>{c.name}</option>
                    ))}
                  </select>
                </div>
              </div>

              <div className="field">
                <label>Configuration fields</label>
                <div className="cfg-rows">
                  {data.config.map((f, i) => (
                    <div className="cfg-row" key={i}>
                      <input className="inp mono" value={f.key} placeholder="key" onChange={(e) => setField(i, { key: e.target.value })} />
                      <input className="inp mono" value={f.value} placeholder="value" onChange={(e) => setField(i, { value: e.target.value })} />
                      <select className="inp" value={f.icon} onChange={(e) => setField(i, { icon: e.target.value })} title="Row icon">
                        <option value="">—</option>
                        {ICON_KEYS.map((k) => (
                          <option key={k} value={k}>{k}</option>
                        ))}
                      </select>
                      <button className="rowdel" onClick={() => removeField(i)} title="Remove field"><Trash2 size={15} /></button>
                    </div>
                  ))}
                </div>
                <button className="linkbtn" onClick={addField}><Plus size={14} /> Add field</button>
              </div>
            </div>
          ) : (
            <>
              <p className="hint" style={{ marginBottom: 10 }}>
                Resolved from the project&apos;s <b>Config</b> &amp; <b>Secrets</b> and the triggering event — set shared values once, every node reads them.
              </p>
              {data.config.length === 0 ? (
                <div className="empty">No configuration.</div>
              ) : (
                <dl className="kv">
                  {data.config.map((f, i) => {
                    const r = resolveRef(f.value, vars, secretNames);
                    return (
                      <div key={i} style={{ display: "contents" }}>
                        <dt>{f.key}</dt>
                        <dd>
                          {r.text}
                          {r.source !== "literal" && SOURCE_LABEL[r.source] && (
                            <span className={`src ${r.source}`} style={{ marginLeft: 8 }}>{SOURCE_LABEL[r.source]}</span>
                          )}
                        </dd>
                      </div>
                    );
                  })}
                </dl>
              )}
            </>
          ))}
      </div>
    </div>
  );
}
