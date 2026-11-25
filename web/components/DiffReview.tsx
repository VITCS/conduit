"use client";
import { useEffect, useMemo, useRef, useState } from "react";
import { ChevronDown, ChevronRight, CornerUpLeft, FileCode2, FilePlus2, FileMinus2, GitBranch, X, Check } from "lucide-react";

// Review the exact code change before it is pushed. Renders the unified diff the
// coding step prepared (one section per file, old/new line numbers) and offers the
// gate's decisions. Nothing reaches the repository until "Approve & push".

type Line = { kind: " " | "+" | "-" | "@"; text: string; a?: number; b?: number };
type FileDiff = { path: string; status: "new" | "deleted" | "modified"; added: number; removed: number; lines: Line[] };

export function parseUnifiedDiff(diff: string): FileDiff[] {
  const files: FileDiff[] = [];
  let cur: FileDiff | null = null;
  let a = 0, b = 0;
  for (const raw of diff.split("\n")) {
    if (raw.startsWith("diff --git ")) {
      const m = raw.match(/^diff --git a\/(.+) b\/(.+)$/);
      cur = { path: m ? m[2] : raw.slice(11), status: "modified", added: 0, removed: 0, lines: [] };
      files.push(cur);
      continue;
    }
    if (!cur) continue;
    if (raw.startsWith("new file mode")) { cur.status = "new"; continue; }
    if (raw.startsWith("deleted file mode")) { cur.status = "deleted"; continue; }
    if (raw.startsWith("--- ") || raw.startsWith("+++ ")) continue;
    if (raw.startsWith("@@")) {
      const m = raw.match(/^@@ -(\d+)(?:,\d+)? \+(\d+)(?:,\d+)? @@/);
      if (m) { a = Number(m[1]); b = Number(m[2]); }
      cur.lines.push({ kind: "@", text: raw });
      continue;
    }
    const k = raw[0];
    if (k === "+") { cur.lines.push({ kind: "+", text: raw.slice(1), b: b++ }); cur.added++; }
    else if (k === "-") { cur.lines.push({ kind: "-", text: raw.slice(1), a: a++ }); cur.removed++; }
    else if (k === " ") { cur.lines.push({ kind: " ", text: raw.slice(1), a: a++, b: b++ }); }
    else if (raw.startsWith("…")) { cur.lines.push({ kind: "@", text: raw }); }
  }
  return files;
}

export function DiffReview(props: {
  diff: string;
  title: string;
  summary?: string;
  sendBacksLeft?: number;
  onApprove: () => void;
  onSendBack?: () => void;
  onReject: () => void;
  onClose: () => void;
}) {
  const files = useMemo(() => parseUnifiedDiff(props.diff), [props.diff]);
  const [collapsed, setCollapsed] = useState<Record<string, boolean>>({});
  const refs = useRef<Record<string, HTMLElement | null>>({});
  const added = files.reduce((n, f) => n + f.added, 0);
  const removed = files.reduce((n, f) => n + f.removed, 0);

  useEffect(() => {
    const h = (e: KeyboardEvent) => { if (e.key === "Escape") props.onClose(); };
    window.addEventListener("keydown", h);
    return () => window.removeEventListener("keydown", h);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  return (
    <div className="modal-scrim dlg-scrim" onMouseDown={(e) => { if (e.target === e.currentTarget) props.onClose(); }}>
      <div className="dr" role="dialog" aria-modal="true" aria-label="Review the code change" data-testid="diff-review">
        <header className="dr-head">
          <div className="dr-title">
            <span className="dr-ico"><GitBranch size={17} /></span>
            <div>
              <b>{props.title}</b>
              <small>
                {files.length} file{files.length === 1 ? "" : "s"} · <span className="dr-add">+{added}</span>{" "}
                <span className="dr-del">−{removed}</span> · not pushed yet
              </small>
            </div>
          </div>
          <div className="dr-actions">
            <button className="btn ok" onClick={props.onApprove} data-testid="diff-approve"><Check size={14} /> Approve &amp; push</button>
            {props.onSendBack && (
              <button className="btn" onClick={props.onSendBack} data-testid="diff-send-back"
                title={props.sendBacksLeft !== undefined ? `${props.sendBacksLeft} send-back(s) left` : undefined}>
                <CornerUpLeft size={14} /> Send back
              </button>
            )}
            <button className="btn danger" onClick={props.onReject} data-testid="diff-reject">Reject</button>
            <button className="iconbtn" onClick={props.onClose} aria-label="Close"><X size={17} /></button>
          </div>
        </header>
        <div className="dr-body">
          <nav className="dr-files" aria-label="Changed files">
            {files.map((f) => (
              <button key={f.path} className="dr-file" onClick={() => refs.current[f.path]?.scrollIntoView({ block: "start", behavior: "smooth" })}>
                <FileIcon status={f.status} />
                <span className="dr-path" title={f.path}>{f.path}</span>
                <span className="dr-n"><span className="dr-add">+{f.added}</span> <span className="dr-del">−{f.removed}</span></span>
              </button>
            ))}
          </nav>
          <div className="dr-main">
            {props.summary && <p className="dr-summary">{props.summary}</p>}
            {files.length === 0 && <div className="empty">No diff to show.</div>}
            {files.map((f) => {
              const closed = !!collapsed[f.path];
              return (
                <section key={f.path} className="dr-sec" ref={(el) => { refs.current[f.path] = el; }} data-testid="diff-file">
                  <button className="dr-sec-head" onClick={() => setCollapsed((c) => ({ ...c, [f.path]: !closed }))} aria-expanded={!closed}>
                    {closed ? <ChevronRight size={14} /> : <ChevronDown size={14} />}
                    <FileIcon status={f.status} />
                    <span className="dr-path">{f.path}</span>
                    {f.status !== "modified" && <em className={`dr-tag ${f.status}`}>{f.status}</em>}
                    <span className="dr-n"><span className="dr-add">+{f.added}</span> <span className="dr-del">−{f.removed}</span></span>
                  </button>
                  {!closed && (
                    <table className="dr-code">
                      <tbody>
                        {f.lines.map((l, i) =>
                          l.kind === "@" ? (
                            <tr key={i} className="hunk"><td colSpan={3}>{l.text}</td></tr>
                          ) : (
                            <tr key={i} className={l.kind === "+" ? "add" : l.kind === "-" ? "del" : ""}>
                              <td className="ln">{l.a ?? ""}</td>
                              <td className="ln">{l.b ?? ""}</td>
                              <td className="tx"><span className="sg">{l.kind === " " ? " " : l.kind}</span>{l.text}</td>
                            </tr>
                          ),
                        )}
                      </tbody>
                    </table>
                  )}
                </section>
              );
            })}
          </div>
        </div>
      </div>
    </div>
  );
}

function FileIcon({ status }: { status: FileDiff["status"] }) {
  if (status === "new") return <FilePlus2 size={14} className="dr-fi new" />;
  if (status === "deleted") return <FileMinus2 size={14} className="dr-fi deleted" />;
  return <FileCode2 size={14} className="dr-fi" />;
}
