"use client";
import { useEffect, useState } from "react";
import { X, Sliders, KeyRound, Trash2, Plus, Settings as Gear } from "lucide-react";
import { api } from "@/lib/api";

// One place for everything a project reads at run time, split into two clearly
// labelled kinds so it's never ambiguous which to use:
//   • Variables — plain, visible values (owner, repo, model). Referenced ${var:NAME}.
//   • Secrets   — encrypted, hidden credentials (tokens, keys). Referenced ${secret:NAME}.
// Same idea (set once, read everywhere); the only difference is whether the value
// is sensitive. That difference is real (secrets are encrypted at rest and never
// returned), so they stay separate — but they live behind one "Settings" button.

type Tab = "variables" | "secrets";

const VAR_PRESETS = ["owner", "repo", "jira_base", "jira_email", "jira_jql", "notion_db", "llm_provider", "llm_model", "audit_url", "auto_start_confidence"];
const SECRET_PRESETS = ["github_token", "llm_key", "jira_token", "notion_token", "scan_token"];

export function SettingsPanel(props: { initialTab?: Tab; onClose: () => void; onToast: (m: string) => void }) {
  const [tab, setTab] = useState<Tab>(props.initialTab ?? "variables");

  // variables state
  const [vars, setVars] = useState<Record<string, string>>({});
  const [vName, setVName] = useState("");
  const [vValue, setVValue] = useState("");
  // secrets state
  const [secrets, setSecrets] = useState<string[]>([]);
  const [sName, setSName] = useState("");
  const [sValue, setSValue] = useState("");
  const [loading, setLoading] = useState(true);

  async function refresh() {
    try {
      const [v, s] = await Promise.all([api.listVars(), api.listSecrets()]);
      setVars(v);
      setSecrets(s);
    } catch (e) {
      props.onToast("Could not load settings: " + (e as Error).message);
    } finally {
      setLoading(false);
    }
  }
  useEffect(() => {
    refresh();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  async function addVar() {
    if (!vName.trim()) return;
    try {
      await api.putVar(vName.trim(), vValue);
      props.onToast("Variable saved");
      setVName(""); setVValue(""); refresh();
    } catch (e) { props.onToast("Save failed: " + (e as Error).message); }
  }
  async function delVar(n: string) {
    try { await api.deleteVar(n); refresh(); } catch (e) { props.onToast("Delete failed: " + (e as Error).message); }
  }
  async function addSecret() {
    if (!sName.trim()) return;
    try {
      await api.putSecret(sName.trim(), sValue);
      props.onToast("Secret saved");
      setSName(""); setSValue(""); refresh();
    } catch (e) { props.onToast("Save failed: " + (e as Error).message); }
  }
  async function delSecret(n: string) {
    try { await api.deleteSecret(n); refresh(); } catch (e) { props.onToast("Delete failed: " + (e as Error).message); }
  }

  const varEntries = Object.entries(vars);

  return (
    <div className="modal-scrim" onClick={props.onClose}>
      <div className="modal" onClick={(e) => e.stopPropagation()}>
        <div className="modal-head">
          <Gear size={16} />
          <span className="modal-title">Project settings</span>
          <button className="iconbtn" onClick={props.onClose} aria-label="Close"><X size={17} /></button>
        </div>

        <div className="set-tabs">
          <button className={`set-tab${tab === "variables" ? " on" : ""}`} onClick={() => setTab("variables")}>
            <Sliders size={14} /> Variables
          </button>
          <button className={`set-tab${tab === "secrets" ? " on" : ""}`} onClick={() => setTab("secrets")}>
            <KeyRound size={14} /> Secrets
          </button>
        </div>

        {tab === "variables" ? (
          <>
            <p className="hint" style={{ padding: "0 16px" }}>
              Plain, non-secret values shared by every node in this project — set once, read everywhere.
              Reference one as <code>{"${var:NAME}"}</code>. Visible here and on nodes. For tokens and API keys use
              the <b>Secrets</b> tab instead.
            </p>
            <div className="modal-body">
              <div className="hint" style={{ marginTop: -4 }}>
                Quick add:{" "}
                {VAR_PRESETS.map((p) => (
                  <button key={p} className="linkbtn" onClick={() => setVName(p)}>{p}</button>
                ))}
              </div>
              <div className="hint" style={{ marginTop: -2 }}>
                Using <b>Notion</b> as the ticket source? Drop the <b>Notion</b> component onto the canvas in place of
                the Jira node, set <code>notion_db</code> here and a <code>notion_token</code> secret.
              </div>
              <div className="cfg-row" style={{ gridTemplateColumns: "1fr 1fr auto" }}>
                <input className="inp mono" placeholder="name (e.g. owner)" value={vName} onChange={(e) => setVName(e.target.value)} />
                <input className="inp mono" placeholder="value (e.g. your-org)" value={vValue} onChange={(e) => setVValue(e.target.value)} />
                <button className="btn primary" onClick={addVar}><Plus size={14} /> Add</button>
              </div>
              <div className="secret-list">
                {loading ? <div className="empty">Loading…</div>
                  : varEntries.length === 0 ? <div className="empty">No variables yet.</div>
                  : varEntries.map(([n, v]) => (
                    <div className="secret-row" key={n}>
                      <Sliders size={14} />
                      <span className="sname">{n}</span>
                      <span className="sval" style={{ letterSpacing: 0 }}>{v}</span>
                      <button className="rowdel" onClick={() => delVar(n)} title="Delete variable"><Trash2 size={15} /></button>
                    </div>
                  ))}
              </div>
            </div>
          </>
        ) : (
          <>
            <p className="hint" style={{ padding: "0 16px" }}>
              Encrypted at rest (AES-256-GCM), hidden, and never returned by the API — for tokens and keys.
              Reference one as <code>{"${secret:NAME}"}</code>. Values are write-only.
            </p>
            <div className="modal-body">
              <div className="hint" style={{ marginTop: -4 }}>
                Quick add:{" "}
                {SECRET_PRESETS.map((p) => (
                  <button key={p} className="linkbtn" onClick={() => setSName(p)}>{p}</button>
                ))}
              </div>
              <div className="cfg-row" style={{ gridTemplateColumns: "1fr 1fr auto" }}>
                <input className="inp mono" placeholder="name (e.g. jira_token)" value={sName} onChange={(e) => setSName(e.target.value)} />
                <input className="inp mono" placeholder="value" type="password" value={sValue} onChange={(e) => setSValue(e.target.value)} />
                <button className="btn primary" onClick={addSecret}><Plus size={14} /> Add</button>
              </div>
              <div className="secret-list">
                {loading ? <div className="empty">Loading…</div>
                  : secrets.length === 0 ? <div className="empty">No secrets yet.</div>
                  : secrets.map((n) => (
                    <div className="secret-row" key={n}>
                      <KeyRound size={14} />
                      <span className="sname">{n}</span>
                      <span className="sval">••••••••</span>
                      <button className="rowdel" onClick={() => delSecret(n)} title="Delete secret"><Trash2 size={15} /></button>
                    </div>
                  ))}
              </div>
            </div>
          </>
        )}
      </div>
    </div>
  );
}
