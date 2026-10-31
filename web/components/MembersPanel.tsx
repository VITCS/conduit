"use client";
import { useEffect, useState } from "react";
import { X, Users, Trash2, Plus, UserPlus } from "lucide-react";
import { api } from "@/lib/api";

const ROLES = ["owner", "admin", "member", "viewer"];

export function MembersPanel(props: { projectId: string; onClose: () => void; onToast: (m: string) => void }) {
  const [members, setMembers] = useState<{ user_id: string; email: string; name: string; role: string }[]>([]);
  const [email, setEmail] = useState("");
  const [role, setRole] = useState("member");
  const [loading, setLoading] = useState(true);
  // create-user sub-form
  const [nu, setNu] = useState({ email: "", name: "", password: "", role: "member" });

  async function refresh() {
    try {
      setMembers(await api.listMembers(props.projectId));
    } catch (e) {
      props.onToast("Could not load members: " + (e as Error).message);
    } finally {
      setLoading(false);
    }
  }
  useEffect(() => {
    refresh();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  async function add() {
    if (!email.trim()) return;
    try {
      await api.addMember(props.projectId, email.trim(), role);
      props.onToast("Member added");
      setEmail("");
      refresh();
    } catch (e) {
      props.onToast("Add failed: " + (e as Error).message);
    }
  }
  async function remove(userId: string) {
    try {
      await api.removeMember(props.projectId, userId);
      refresh();
    } catch (e) {
      props.onToast("Remove failed: " + (e as Error).message);
    }
  }
  async function createUser() {
    if (!nu.email.trim() || !nu.password) return;
    try {
      await api.createUser(nu.email.trim(), nu.name, nu.password, nu.role);
      props.onToast("User created — add them to a project below");
      setNu({ email: "", name: "", password: "", role: "member" });
    } catch (e) {
      props.onToast("Create user failed: " + (e as Error).message);
    }
  }

  return (
    <div className="modal-scrim" onClick={props.onClose}>
      <div className="modal" onClick={(e) => e.stopPropagation()}>
        <div className="modal-head">
          <Users size={16} />
          <span className="modal-title">Members · {props.projectId}</span>
          <button className="iconbtn" onClick={props.onClose} aria-label="Close">
            <X size={17} />
          </button>
        </div>
        <div className="modal-body">
          <div>
            <div className="field"><label>Add a member (existing user)</label></div>
            <div className="cfg-row" style={{ gridTemplateColumns: "1fr 130px auto" }}>
              <input className="inp" placeholder="email" value={email} onChange={(e) => setEmail(e.target.value)} />
              <select className="inp" value={role} onChange={(e) => setRole(e.target.value)}>
                {ROLES.map((r) => <option key={r} value={r}>{r}</option>)}
              </select>
              <button className="btn primary" onClick={add}><Plus size={14} /> Add</button>
            </div>
          </div>

          <div className="secret-list">
            {loading ? (
              <div className="empty">Loading…</div>
            ) : members.length === 0 ? (
              <div className="empty">No members yet.</div>
            ) : (
              members.map((m) => (
                <div className="secret-row" key={m.user_id}>
                  <Users size={14} />
                  <span className="sname">{m.email || m.user_id}</span>
                  <span className="role">{m.role}</span>
                  <button className="rowdel" onClick={() => remove(m.user_id)} title="Remove member">
                    <Trash2 size={15} />
                  </button>
                </div>
              ))
            )}
          </div>

          <details>
            <summary className="hint" style={{ cursor: "pointer" }}>Create a new user (admin)</summary>
            <div style={{ display: "flex", flexDirection: "column", gap: 8, marginTop: 10 }}>
              <div className="cfg-row" style={{ gridTemplateColumns: "1fr 1fr" }}>
                <input className="inp" placeholder="email" value={nu.email} onChange={(e) => setNu({ ...nu, email: e.target.value })} />
                <input className="inp" placeholder="name" value={nu.name} onChange={(e) => setNu({ ...nu, name: e.target.value })} />
              </div>
              <div className="cfg-row" style={{ gridTemplateColumns: "1fr 130px auto" }}>
                <input className="inp" type="password" placeholder="password" value={nu.password} onChange={(e) => setNu({ ...nu, password: e.target.value })} />
                <select className="inp" value={nu.role} onChange={(e) => setNu({ ...nu, role: e.target.value })}>
                  {ROLES.map((r) => <option key={r} value={r}>{r}</option>)}
                </select>
                <button className="btn" onClick={createUser}><UserPlus size={14} /> Create</button>
              </div>
            </div>
          </details>
        </div>
      </div>
    </div>
  );
}
