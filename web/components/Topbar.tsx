"use client";
import { PanelLeft, Play, Square, Save, Pencil, Sun, Moon, LogOut, Users, Sliders, Trash2, ChevronsUpDown, FolderKanban, BellRing } from "lucide-react";
import { useEffect, useState } from "react";

function Logo() {
  return (
    <svg width="26" height="26" viewBox="0 0 32 32" aria-hidden>
      <defs>
        <linearGradient id="clg" x1="0" y1="0" x2="1" y2="1">
          <stop offset="0" stopColor="var(--accent)" />
          <stop offset="1" stopColor="var(--accent-deep)" />
        </linearGradient>
      </defs>
      <rect x="2" y="2" width="28" height="28" rx="8" fill="var(--accent-soft)" />
      <circle cx="9" cy="16" r="3" fill="url(#clg)" />
      <circle cx="23" cy="9" r="3" fill="url(#clg)" />
      <circle cx="23" cy="23" r="3" fill="url(#clg)" />
      <path d="M11.6 14.6 20.4 10M11.6 17.4 20.4 22" stroke="url(#clg)" strokeWidth="2.2" strokeLinecap="round" />
    </svg>
  );
}

export function Topbar(props: {
  name: string;
  onName: (v: string) => void;
  onRun: () => void;
  onCancel: () => void;
  running: boolean;
  editMode: boolean;
  onToggleEdit: () => void;
  dirty: boolean;
  onSave: () => void;
  onToggleRail: () => void;
  onOpenSettings: () => void;
  onOpenMembers: () => void;
  user: { name: string; email: string; role: string } | null;
  onSignOut: () => void;
  projects: { id: string; name: string }[];
  currentProject: string;
  onSwitchProject: (id: string) => void;
  onNewProject: () => void;
  onDeleteProject: (id: string) => void;
  /** How many items need the person in each project, and in total. */
  attentionByProject?: Record<string, number>;
  attentionTotal?: number;
  onOpenNeeds?: () => void;
}) {
  const waitingElsewhere = Object.entries(props.attentionByProject ?? {})
    .filter(([id]) => id !== props.currentProject).reduce((n, [, c]) => n + c, 0);
  const BUILTIN_PROJECTS = new Set(["default", "ai-factory", "github"]);
  const canDeleteProject = !BUILTIN_PROJECTS.has(props.currentProject);
  const [dark, setDark] = useState(true);
  useEffect(() => {
    // The pre-paint script in layout already set data-theme (default dark).
    setDark(document.documentElement.getAttribute("data-theme") !== "light");
  }, []);
  function toggleTheme() {
    const next = !dark;
    document.documentElement.setAttribute("data-theme", next ? "dark" : "light");
    try {
      localStorage.setItem("conduit_theme", next ? "dark" : "light");
    } catch {
      /* ignore */
    }
    setDark(next);
  }

  return (
    <header className="topbar">
      <button className="iconbtn" onClick={props.onToggleRail} title="Toggle runs" aria-label="Toggle runs">
        <PanelLeft size={18} />
      </button>
      <div className="brand">
        <Logo />
        <span className="wordmark">Conduit Agent</span>
        <span className="beta">BETA</span>
      </div>
      <span className="project-pick">
        <FolderKanban size={14} className="pp-ico" />
        <select
          className="project-switch"
          value={props.currentProject}
          onChange={(e) => {
            if (e.target.value === "__new__") props.onNewProject();
            else props.onSwitchProject(e.target.value);
          }}
          title="Project"
          aria-label="Project"
      >
          {props.projects.map((p) => (
            <option key={p.id} value={p.id}>
              {p.name}{p.id !== props.currentProject && props.attentionByProject?.[p.id] ? ` — ${props.attentionByProject[p.id]} need${props.attentionByProject[p.id] === 1 ? "s" : ""} you` : ""}
            </option>
          ))}
          <option value="__new__">+ New project…</option>
        </select>
        <ChevronsUpDown size={13} className="pp-chev" />
        {waitingElsewhere > 0 && (
          <span className="pp-badge" title={`${waitingElsewhere} waiting on you in other projects`} data-testid="project-badge">{waitingElsewhere}</span>
        )}
      </span>
      {canDeleteProject && (
        <button
          className="iconbtn danger"
          onClick={() => props.onDeleteProject(props.currentProject)}
          title="Delete this project"
          aria-label="Delete this project"
        >
          <Trash2 size={16} />
        </button>
      )}
      <div className="wf-search" role="search">
        <Pencil size={13} />
        <input value={props.name} onChange={(e) => props.onName(e.target.value)} aria-label="Workflow name" spellCheck={false} />
      </div>
      <div className="topbar-right">
        {props.running ? (
          <button className="btn" onClick={props.onCancel} title="Cancel run" data-testid="cancel-btn">
            <Square size={14} fill="currentColor" /> Cancel
          </button>
        ) : (
          <button className="btn primary" onClick={props.onRun} data-testid="run-btn">
            <Play size={15} fill="currentColor" /> Run
          </button>
        )}
        {props.editMode && (
          <button className={`btn save${props.dirty ? " dirty" : ""}`} onClick={props.onSave}>
            <Save size={15} /> {props.dirty ? "Save *" : "Save"}
          </button>
        )}
        <button className="btn" data-on={props.editMode} onClick={props.onToggleEdit} title="Toggle edit mode">
          <Pencil size={15} /> Edit
        </button>
        <span className="tb-sep" aria-hidden />
        <button className={`iconbtn ny-bell${props.attentionTotal ? " on" : ""}`} onClick={props.onOpenNeeds}
          title={props.attentionTotal ? `${props.attentionTotal} waiting on you` : "Nothing waiting on you"} aria-label="Needs you" data-testid="needs-bell">
          <BellRing size={16} />
          {props.attentionTotal ? <span className="ny-bell-n">{props.attentionTotal}</span> : null}
        </button>
        <button className="iconbtn" onClick={props.onOpenMembers} title="Members" aria-label="Members">
          <Users size={16} />
        </button>
        <button className="iconbtn" onClick={props.onOpenSettings} title="Settings — variables & secrets" aria-label="Settings">
          <Sliders size={16} />
        </button>
        <button className="iconbtn" onClick={toggleTheme} aria-label="Toggle theme" title="Toggle theme">
          {dark ? <Sun size={16} /> : <Moon size={16} />}
        </button>
        {props.user && (
          <span className="user-chip" title={props.user.email}>
            <span className="avatar" aria-hidden>{(props.user.name || props.user.email || "?").trim().charAt(0).toUpperCase()}</span>
            <b>{props.user.name || props.user.email}</b>
            <span className="role">{props.user.role}</span>
          </span>
        )}
        <button className="iconbtn" onClick={props.onSignOut} title="Sign out" aria-label="Sign out">
          <LogOut size={16} />
        </button>
      </div>
    </header>
  );
}
