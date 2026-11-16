"use client";
import { useCallback, useEffect, useRef, useState } from "react";
import { AlertTriangle, FolderPlus, HelpCircle, X } from "lucide-react";

// In-app replacements for window.confirm / window.prompt. `useDialogs()` returns
// promise-based `confirm` and `prompt` functions plus the element to render, so a
// caller keeps the same control flow it had with the browser dialogs:
//
//   const { confirm, prompt, dialog } = useDialogs();
//   if (!(await confirm({ title: "Delete task?" }))) return;

export interface ConfirmOptions {
  title: string;
  message?: React.ReactNode;
  confirmLabel?: string;
  cancelLabel?: string;
  /** Destructive action: red confirm button and a warning icon. */
  danger?: boolean;
}

export interface PromptOptions {
  title: string;
  message?: React.ReactNode;
  label?: string;
  placeholder?: string;
  initialValue?: string;
  confirmLabel?: string;
  cancelLabel?: string;
}

type State =
  | { kind: "confirm"; opts: ConfirmOptions; resolve: (ok: boolean) => void }
  | { kind: "prompt"; opts: PromptOptions; resolve: (value: string | null) => void }
  | null;

export function useDialogs() {
  const [state, setState] = useState<State>(null);

  const confirm = useCallback(
    (opts: ConfirmOptions) => new Promise<boolean>((resolve) => setState({ kind: "confirm", opts, resolve })),
    [],
  );
  const prompt = useCallback(
    (opts: PromptOptions) => new Promise<string | null>((resolve) => setState({ kind: "prompt", opts, resolve })),
    [],
  );

  const dialog =
    state?.kind === "confirm" ? (
      <ConfirmDialog
        opts={state.opts}
        onDone={(ok) => { state.resolve(ok); setState(null); }}
      />
    ) : state?.kind === "prompt" ? (
      <PromptDialog
        opts={state.opts}
        onDone={(v) => { state.resolve(v); setState(null); }}
      />
    ) : null;

  return { confirm, prompt, dialog };
}

/** Escape closes; focus returns to whatever opened the dialog. */
function useDialogKeys(onCancel: () => void) {
  useEffect(() => {
    const prev = document.activeElement as HTMLElement | null;
    const h = (e: KeyboardEvent) => {
      if (e.key === "Escape") { e.stopPropagation(); onCancel(); }
    };
    window.addEventListener("keydown", h, true);
    return () => {
      window.removeEventListener("keydown", h, true);
      prev?.focus?.();
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);
}

function ConfirmDialog({ opts, onDone }: { opts: ConfirmOptions; onDone: (ok: boolean) => void }) {
  const okRef = useRef<HTMLButtonElement>(null);
  useDialogKeys(() => onDone(false));
  useEffect(() => { okRef.current?.focus(); }, []);
  return (
    <div className="modal-scrim dlg-scrim" onMouseDown={(e) => { if (e.target === e.currentTarget) onDone(false); }}>
      <div className="modal dlg" role="alertdialog" aria-modal="true" aria-labelledby="dlg-title" data-testid="confirm-dialog">
        <button className="iconbtn dlg-x" onClick={() => onDone(false)} aria-label="Close"><X size={16} /></button>
        <span className={`dlg-ico${opts.danger ? " danger" : ""}`}>
          {opts.danger ? <AlertTriangle size={20} /> : <HelpCircle size={20} />}
        </span>
        <h2 id="dlg-title" className="dlg-title">{opts.title}</h2>
        {opts.message && <div className="dlg-msg">{opts.message}</div>}
        <div className="dlg-actions">
          <button className="btn" onClick={() => onDone(false)} data-testid="dialog-cancel">{opts.cancelLabel || "Cancel"}</button>
          <button ref={okRef} className={`btn ${opts.danger ? "danger" : "primary"}`} onClick={() => onDone(true)} data-testid="dialog-confirm">
            {opts.confirmLabel || "Confirm"}
          </button>
        </div>
      </div>
    </div>
  );
}

function PromptDialog({ opts, onDone }: { opts: PromptOptions; onDone: (value: string | null) => void }) {
  const [value, setValue] = useState(opts.initialValue || "");
  useDialogKeys(() => onDone(null));
  const submit = () => { const v = value.trim(); if (v) onDone(v); };
  return (
    <div className="modal-scrim dlg-scrim" onMouseDown={(e) => { if (e.target === e.currentTarget) onDone(null); }}>
      <form className="modal dlg" role="dialog" aria-modal="true" aria-labelledby="dlg-title" data-testid="prompt-dialog"
        onSubmit={(e) => { e.preventDefault(); submit(); }}>
        <button type="button" className="iconbtn dlg-x" onClick={() => onDone(null)} aria-label="Close"><X size={16} /></button>
        <span className="dlg-ico"><FolderPlus size={20} /></span>
        <h2 id="dlg-title" className="dlg-title">{opts.title}</h2>
        {opts.message && <div className="dlg-msg">{opts.message}</div>}
        <label className="nt-field dlg-field">
          {opts.label && <span className="nt-label">{opts.label}</span>}
          <input className="inp" autoFocus value={value} placeholder={opts.placeholder}
            onChange={(e) => setValue(e.target.value)} data-testid="dialog-input" />
        </label>
        <div className="dlg-actions">
          <button type="button" className="btn" onClick={() => onDone(null)} data-testid="dialog-cancel">{opts.cancelLabel || "Cancel"}</button>
          <button type="submit" className="btn primary" disabled={!value.trim()} data-testid="dialog-confirm">
            {opts.confirmLabel || "Create"}
          </button>
        </div>
      </form>
    </div>
  );
}
