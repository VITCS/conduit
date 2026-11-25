"use client";
import { useEffect, useState } from "react";
import { CornerUpLeft, X } from "lucide-react";

// Asks the approver what should change, then sends the proposal back to the step
// that produced it. Feedback is required: "send back" without a reason would just
// make the agent guess.
export function SendBackDialog(props: {
  gateTitle: string;
  left?: number;
  onSend: (feedback: string) => Promise<void> | void;
  onClose: () => void;
}) {
  const [text, setText] = useState("");
  const [busy, setBusy] = useState(false);

  useEffect(() => {
    const h = (e: KeyboardEvent) => { if (e.key === "Escape") props.onClose(); };
    window.addEventListener("keydown", h);
    return () => window.removeEventListener("keydown", h);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  async function send() {
    if (!text.trim() || busy) return;
    setBusy(true);
    try { await props.onSend(text.trim()); } finally { setBusy(false); }
  }

  return (
    <div className="modal-scrim dlg-scrim" onMouseDown={(e) => { if (e.target === e.currentTarget) props.onClose(); }}>
      <div className="modal dlg sb" role="dialog" aria-modal="true" aria-labelledby="sb-title" data-testid="send-back-dialog">
        <button className="iconbtn dlg-x" onClick={props.onClose} aria-label="Close"><X size={16} /></button>
        <span className="dlg-ico"><CornerUpLeft size={20} /></span>
        <h2 id="sb-title" className="dlg-title">Send back with feedback</h2>
        <div className="dlg-msg">
          Tell the agent what to change at <b>{props.gateTitle}</b>. It revises its proposal with your note,
          and you review it again here.
          {typeof props.left === "number" && (
            <> {props.left === 1 ? "This is the last send-back for this step." : `${props.left} send-backs left for this step.`}</>
          )}
        </div>
        <label className="nt-field dlg-field">
          <span className="nt-label">What should change?</span>
          <textarea className="inp nt-area" autoFocus rows={5} value={text} onChange={(e) => setText(e.target.value)}
            placeholder="e.g. Keep the existing API shape; add the migration as a separate step; include a test for the empty case."
            onKeyDown={(e) => { if (e.key === "Enter" && (e.metaKey || e.ctrlKey)) send(); }} data-testid="send-back-text" />
        </label>
        <div className="dlg-actions">
          <button className="btn" onClick={props.onClose}>Cancel</button>
          <button className="btn primary" onClick={send} disabled={!text.trim() || busy} data-testid="send-back-confirm">
            <CornerUpLeft size={14} /> {busy ? "Sending…" : "Send back"}
          </button>
        </div>
      </div>
    </div>
  );
}
