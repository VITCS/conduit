"use client";
import { Menu, Play, Square, Save, Pencil, Sun, Moon, KeyRound, LogOut } from "lucide-react";
import { useEffect, useState } from "react";

function Logo() {
  return (
    <svg width="26" height="26" viewBox="0 0 32 32" aria-hidden>
      <defs>
        <linearGradient id="clg" x1="0" y1="0" x2="1" y2="1">
          <stop offset="0" stopColor="var(--accent)" />
          <stop offset="1" stopColor="var(--wait)" />
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
  onOpenSecrets: () => void;
  user: { name: string; email: string; role: string } | null;
  onSignOut: () => void;
}) {
  const [dark, setDark] = useState(false);
  useEffect(() => {
    const attr = document.documentElement.getAttribute("data-theme");
    setDark(attr ? attr === "dark" : window.matchMedia("(prefers-color-scheme: dark)").matches);
  }, []);
  function toggleTheme() {
    const next = !dark;
    document.documentElement.setAttribute("data-theme", next ? "dark" : "light");
    setDark(next);
  }

  return (
    <header className="topbar">
      <button className="iconbtn" onClick={props.onToggleRail} title="Toggle runs" aria-label="Toggle runs">
        <Menu size={18} />
      </button>
      <div className="brand">
        <Logo />
        <span className="wordmark">Conduit</span>
        <span className="beta">BETA</span>
      </div>
      <div className="wf-search" role="search">
        <Pencil size={14} style={{ opacity: 0 }} />
        <input value={props.name} onChange={(e) => props.onName(e.target.value)} aria-label="Workflow name" spellCheck={false} />
      </div>
      <div className="topbar-right">
        {props.running ? (
          <button className="btn" onClick={props.onCancel} title="Cancel run">
            <Square size={14} fill="currentColor" /> Cancel
          </button>
        ) : (
          <button className="btn primary" onClick={props.onRun}>
            <Play size={15} fill="currentColor" /> Run
          </button>
        )}
        {props.editMode && (
          <button className={`btn save${props.dirty ? " dirty" : ""}`} onClick={props.onSave}>
            <Save size={15} /> {props.dirty ? "Save *" : "Save"}
          </button>
        )}
        <button className="btn" onClick={props.onOpenSecrets} title="Manage secrets">
          <KeyRound size={15} /> Secrets
        </button>
        <button className="btn" onClick={toggleTheme} aria-label="Toggle theme" title="Toggle theme">
          {dark ? <Sun size={15} /> : <Moon size={15} />}
        </button>
        <button className="btn" data-on={props.editMode} onClick={props.onToggleEdit}>
          <Pencil size={15} /> Edit
        </button>
        {props.user && (
          <span className="user-chip">
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
