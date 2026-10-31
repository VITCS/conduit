"use client";
import { useEffect, useState } from "react";
import { X, KeyRound, Trash2, Plus } from "lucide-react";
import { api } from "@/lib/api";

export function SecretsPanel(props: { onClose: () => void; onToast: (m: string) => void }) {
  const [names, setNames] = useState<string[]>([]);
  const [name, setName] = useState("");
  const [value, setValue] = useState("");
  const [loading, setLoading] = useState(true);

  async function refresh() {
    try {
      setNames(await api.listSecrets());
    } catch (e) {
      props.onToast("Could not load secrets: " + (e as Error).message);
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
      await api.putSecret(name.trim(), value);
      props.onToast("Secret saved");
      setName("");
      setValue("");
      refresh();
    } catch (e) {
      props.onToast("Save failed: " + (e as Error).message);
    }
  }
  async function remove(n: string) {
    try {
      await api.deleteSecret(n);
      refresh();
    } catch (e) {
      props.onToast("Delete failed: " + (e as Error).message);
    }
  }

  return (
    <div className="modal-scrim" onClick={props.onClose}>
      <div className="modal" onClick={(e) => e.stopPropagation()}>
        <div className="modal-head">
          <KeyRound size={16} />
          <span className="modal-title">Secrets</span>
          <button className="iconbtn" onClick={props.onClose} aria-label="Close">
            <X size={17} />
          </button>
        </div>
        <p className="hint" style={{ padding: "0 16px" }}>
          Stored encrypted. Reference a secret from a node&apos;s config as{" "}
          <code>{"${secret:NAME}"}</code> (e.g. a Slack webhook URL). Values are write-only — they are never returned.
        </p>
        <div className="modal-body">
          <div className="cfg-row" style={{ gridTemplateColumns: "1fr 1fr auto" }}>
            <input className="inp mono" placeholder="name" value={name} onChange={(e) => setName(e.target.value)} />
            <input className="inp mono" placeholder="value" type="password" value={value} onChange={(e) => setValue(e.target.value)} />
            <button className="btn primary" onClick={add}>
              <Plus size={14} /> Add
            </button>
          </div>
          <div className="secret-list">
            {loading ? (
              <div className="empty">Loading…</div>
            ) : names.length === 0 ? (
              <div className="empty">No secrets yet.</div>
            ) : (
              names.map((n) => (
                <div className="secret-row" key={n}>
                  <KeyRound size={14} />
                  <span className="sname">{n}</span>
                  <span className="sval">••••••••</span>
                  <button className="rowdel" onClick={() => remove(n)} title="Delete secret">
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
