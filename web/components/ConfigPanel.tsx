"use client";
import { useEffect, useState } from "react";
import { X, Sliders, Trash2, Plus } from "lucide-react";
import { api } from "@/lib/api";

// ConfigPanel manages project config variables (${var:NAME}) — plain, non-secret
// values (e.g. owner, repo, model) set once per project so nodes don't repeat them.
export function ConfigPanel(props: { onClose: () => void; onToast: (m: string) => void }) {
  const [vars, setVars] = useState<Record<string, string>>({});
  const [name, setName] = useState("");
  const [value, setValue] = useState("");
  const [loading, setLoading] = useState(true);

  async function refresh() {
    try {
      setVars(await api.listVars());
    } catch (e) {
      props.onToast("Could not load config: " + (e as Error).message);
    } finally {
      setLoading(false);
    }
  }
  useEffect(() => {
    refresh();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  async function add() {
    if (!name.trim()) return;
    try {
      await api.putVar(name.trim(), value);
      props.onToast("Variable saved");
      setName("");
      setValue("");
      refresh();
    } catch (e) {
      props.onToast("Save failed: " + (e as Error).message);
    }
  }
  async function remove(n: string) {
    try {
      await api.deleteVar(n);
      refresh();
    } catch (e) {
      props.onToast("Delete failed: " + (e as Error).message);
    }
  }

  const entries = Object.entries(vars);

  return (
    <div className="modal-scrim" onClick={props.onClose}>
      <div className="modal" onClick={(e) => e.stopPropagation()}>
        <div className="modal-head">
          <Sliders size={16} />
          <span className="modal-title">Project config</span>
          <button className="iconbtn" onClick={props.onClose} aria-label="Close">
            <X size={17} />
          </button>
        </div>
        <p className="hint" style={{ padding: "0 16px" }}>
          Shared values for this project. Reference one from any node&apos;s config as{" "}
          <code>{"${var:NAME}"}</code> (e.g. set <code>owner</code> and <code>repo</code> once,
          then use <code>{"${var:owner}"}</code> everywhere). For passwords/tokens use Secrets instead.
        </p>
        <div className="modal-body">
          <div className="cfg-row" style={{ gridTemplateColumns: "1fr 1fr auto" }}>
            <input className="inp mono" placeholder="name (e.g. owner)" value={name} onChange={(e) => setName(e.target.value)} />
            <input className="inp mono" placeholder="value (e.g. your-org)" value={value} onChange={(e) => setValue(e.target.value)} />
            <button className="btn primary" onClick={add}>
              <Plus size={14} /> Add
            </button>
          </div>
          <div className="secret-list">
            {loading ? (
              <div className="empty">Loading…</div>
            ) : entries.length === 0 ? (
              <div className="empty">No variables yet.</div>
            ) : (
              entries.map(([n, v]) => (
                <div className="secret-row" key={n}>
                  <Sliders size={14} />
                  <span className="sname">{n}</span>
                  <span className="sval" style={{ letterSpacing: 0 }}>{v}</span>
                  <button className="rowdel" onClick={() => remove(n)} title="Delete variable">
                    <Trash2 size={15} />
                  </button>
                </div>
              ))
            )}
          </div>
        </div>
      </div>
    </div>
  );
}
