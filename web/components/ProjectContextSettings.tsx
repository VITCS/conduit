"use client";
import { useState } from "react";
import { FileText, Wand2 } from "lucide-react";
import { api } from "@/lib/api";

// The project's shared context: what every agent in this project should know.
// Stored as the project variable `project_context` and added to every agent's
// instructions (planning, coding with either engine, PR review, triage).

const MAX = 12000;

const TEMPLATE = `Goal: what this product is and what "done" looks like.

Stack: languages, frameworks and versions (e.g. Next.js 16, TypeScript, Postgres).

Build and test: the exact commands (e.g. npm ci, npm test, npm run lint).

Conventions: code style, folder layout, naming, how tests are written, commit/PR style.

Already decided: libraries and patterns to keep using.

Off-limits: files or areas agents must not change (e.g. billing/, database migrations, CI config).`;

export function ProjectContextSettings(props: { value: string; onSaved: () => void; onToast: (m: string) => void }) {
  const [text, setText] = useState(props.value);
  const [busy, setBusy] = useState(false);
  const dirty = text !== props.value;

  async function save() {
    setBusy(true);
    try {
      if (text.trim()) await api.putVar("project_context", text.trim());
      else if (props.value) await api.deleteVar("project_context");
      props.onToast(text.trim() ? "Project context saved" : "Project context cleared");
      props.onSaved();
    } catch (e) {
      props.onToast("Save failed: " + (e as Error).message);
    } finally {
      setBusy(false);
    }
  }

  return (
    <div className="modal-body">
      <p className="hint" style={{ marginTop: -4 }}>
        What every agent in this project should know. It is added to the instructions of every agent — planning,
        coding (Claude Code and the direct API), PR review and triage — so plans and code follow your stack and
        conventions. With the direct API, the repository&apos;s own <code>CLAUDE.md</code> / <code>AGENTS.md</code> is
        added too; Claude Code reads those itself.
      </p>
      <label className="nt-field">
        <span className="nt-label" style={{ display: "flex", alignItems: "center", gap: 8 }}>
          <FileText size={13} /> Project context
          <span className="nt-opt" style={{ marginLeft: "auto" }}>{text.length.toLocaleString()} / {MAX.toLocaleString()}</span>
        </span>
        <textarea className="inp nt-area pc-area" rows={14} value={text} maxLength={MAX} onChange={(e) => setText(e.target.value)}
          placeholder={TEMPLATE} data-testid="project-context" />
      </label>
      <div className="nt-actions" style={{ justifyContent: "space-between" }}>
        <button className="linkbtn" onClick={() => setText(text.trim() ? text : TEMPLATE)} disabled={!!text.trim()}
          title={text.trim() ? "Clear the box to start from the template" : "Start from a template"}>
          <Wand2 size={13} /> Start from a template
        </button>
        <button className="btn primary" onClick={save} disabled={busy || !dirty} data-testid="project-context-save">
          {busy ? "Saving…" : "Save"}
        </button>
      </div>
    </div>
  );
}
