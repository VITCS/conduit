"use client";
import { BellRing, ChevronDown, ChevronUp, GitPullRequest, Hand, MessageCircleQuestion, OctagonAlert, ArrowRight } from "lucide-react";
import type { Attention, AttentionItem } from "@/lib/types";
import { QuestionCard } from "./QuestionCard";

const KIND: Record<AttentionItem["kind"], { label: string; Icon: typeof Hand; cls: string }> = {
  question: { label: "Question", Icon: MessageCircleQuestion, cls: "q" },
  approval: { label: "Approval", Icon: Hand, cls: "a" },
  stuck: { label: "Stuck", Icon: OctagonAlert, cls: "s" },
  review: { label: "PR review", Icon: GitPullRequest, cls: "r" },
};

export function itemKey(it: AttentionItem) {
  return [it.kind, it.project_id, it.run_id, it.node_id, it.task_id, it.review_id].join("|");
}

function ago(iso: string) {
  const s = Math.max(0, (Date.now() - new Date(iso).getTime()) / 1000);
  if (s < 60) return "just now";
  if (s < 3600) return `${Math.floor(s / 60)} min`;
  if (s < 86400) return `${Math.floor(s / 3600)} h`;
  return `${Math.floor(s / 86400)} d`;
}

/**
 * "Needs you": one strip listing everything waiting on you across your projects —
 * agent questions (answer them right here), approval gates, stuck tasks and pull
 * requests waiting for review.
 */
export function NeedsYou(props: {
  data: Attention | null;
  open: boolean;
  onToggle: () => void;
  currentProject: string;
  onOpen: (it: AttentionItem) => void;
  onAnswer: (it: AttentionItem, answer: string) => Promise<void>;
}) {
  const items = props.data?.items ?? [];
  if (items.length === 0) return null;
  const counts = items.reduce<Record<string, number>>((m, it) => ((m[it.kind] = (m[it.kind] || 0) + 1), m), {});
  const where = (it: AttentionItem) => (it.project_id !== props.currentProject ? `${it.project_name} · ` : "");

  return (
    <section className={`needs${props.open ? " open" : ""}`} aria-label="Needs you" data-testid="needs-you">
      <div className="ny-bar">
        <span className="ny-title"><BellRing size={15} /> <b>Needs you</b> <span className="ny-count">{items.length}</span></span>
        <span className="ny-kinds">
          {(["question", "approval", "stuck", "review"] as const).filter((k) => counts[k]).map((k) => (
            <span key={k} className={`ny-kind k-${KIND[k].cls}`}>{counts[k]} {KIND[k].label.toLowerCase()}{counts[k] > 1 && k !== "review" ? "s" : ""}</span>
          ))}
        </span>
        {!props.open && (
          <span className="ny-chips">
            {items.slice(0, 3).map((it) => {
              const K = KIND[it.kind];
              return (
                <button key={itemKey(it)} className={`ny-chip k-${K.cls}`} onClick={() => (it.kind === "question" ? props.onToggle() : props.onOpen(it))}
                  title={it.text || it.title}>
                  <K.Icon size={13} /> <span>{where(it)}{it.step ? `${it.step} — ` : ""}{it.title}</span>
                </button>
              );
            })}
          </span>
        )}
        <button className="btn ghost ny-toggle" onClick={props.onToggle} data-testid="needs-you-toggle">
          {props.open ? <>Hide <ChevronUp size={14} /></> : <>Show all <ChevronDown size={14} /></>}
        </button>
      </div>
      {props.open && (
        <ul className="ny-list">
          {items.map((it) => {
            const K = KIND[it.kind];
            return (
              <li key={itemKey(it)} className={`ny-item k-${K.cls}`} data-testid={`needs-item-${it.kind}`}>
                <div className="ny-head">
                  <span className={`ny-badge k-${K.cls}`}><K.Icon size={12} /> {K.label}</span>
                  <span className="ny-what">
                    <b>{it.step || it.title}</b>
                    {it.step && <span> · {it.title}</span>}
                  </span>
                  <span className="ny-meta">{it.project_name} · {ago(it.since)}</span>
                  <button className="btn ghost" onClick={() => props.onOpen(it)}>
                    {it.kind === "review" ? "Open inbox" : it.run_id ? "Open run" : "Open board"} <ArrowRight size={13} />
                  </button>
                </div>
                {it.kind === "question" ? (
                  <QuestionCard compact question={it.text || "The step has a question."} options={it.options}
                    onAnswer={(a) => props.onAnswer(it, a)} testid="needs-question" />
                ) : it.text ? (
                  <p className="ny-text">{it.text}</p>
                ) : null}
              </li>
            );
          })}
        </ul>
      )}
    </section>
  );
}
